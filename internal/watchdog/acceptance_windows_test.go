//go:build windows

package watchdog

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"golang.org/x/sys/windows"
)

type liveFixture struct {
	Listen         string
	Control        string
	Starts         string
	StartupDelayMS int
}

// Invoked only by the isolated Scheduled Tasks created by the opt-in test.
func TestWatchdogLiveHelperProcess(t *testing.T) {
	args := flag.Args()
	if len(args) != 2 || args[0] != "watchdog-acceptance-helper" {
		t.Skip("Scheduled Task fixture only")
	}
	data, err := os.ReadFile(args[1])
	if err != nil {
		t.Fatal(err)
	}
	var config liveFixture
	if err := json.Unmarshal(data, &config); err != nil {
		t.Fatal(err)
	}
	starts, err := os.OpenFile(config.Starts, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0600)
	if err != nil {
		t.Fatal(err)
	}
	_, err = fmt.Fprintf(starts, "%d\n", os.Getpid())
	closeErr := starts.Close()
	if err != nil || closeErr != nil {
		t.Fatalf("record start: write=%v close=%v", err, closeErr)
	}
	time.Sleep(time.Duration(config.StartupDelayMS) * time.Millisecond)
	server := &http.Server{Addr: config.Listen, ReadHeaderTimeout: time.Second, Handler: http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		state, err := os.ReadFile(config.Control)
		if err != nil || string(state) != "healthy" {
			w.WriteHeader(http.StatusServiceUnavailable)
			_, _ = w.Write([]byte(`{"status":"not-ready"}`))
			return
		}
		_, _ = w.Write([]byte(`{"status":"ok"}`))
	})}
	t.Fatal(server.ListenAndServe())
}

type liveTaskRecoverer struct{ upstream string }

func (r liveTaskRecoverer) RestartScheduledTask(ctx context.Context, recovery RecoveryConfig) error {
	if err := (WindowsTaskRecoverer{}).RestartScheduledTask(ctx, recovery); err != nil {
		return err
	}
	if recovery.ScheduledTaskName == r.upstream {
		// Reproduce a slow Task Scheduler return, scaled from the incident's
		// 17-second restart and five-second startup budget.
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(2 * time.Second):
		}
	}
	return nil
}

// Run explicitly in a signed-in interactive session. It creates and removes
// only its own two randomly named Tasks and never targets the installation.
func TestWatchdogLiveAcceptance(t *testing.T) {
	if os.Getenv("WINDOWSAGENT_WATCHDOG_LIVE_ACCEPTANCE") != "1" {
		t.Skip("opt-in interactive Windows acceptance")
	}
	var session uint32
	if err := windows.ProcessIdToSessionId(uint32(os.Getpid()), &session); err != nil || session == 0 {
		t.Fatalf("interactive session=%d error=%v", session, err)
	}
	priority, err := windows.GetPriorityClass(windows.CurrentProcess())
	if err != nil {
		t.Fatal(err)
	}
	if err := windows.SetPriorityClass(windows.CurrentProcess(), windows.BELOW_NORMAL_PRIORITY_CLASS); err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := windows.SetPriorityClass(windows.CurrentProcess(), priority); err != nil {
			t.Error(err)
		}
	}()
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	root := t.TempDir()
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	binary, err := os.ReadFile(executable)
	if err != nil {
		t.Fatal(err)
	}
	driver := filepath.Join(root, "tasks.ps1")
	if err := os.WriteFile(driver, []byte(liveTaskDriver), 0600); err != nil {
		t.Fatal(err)
	}
	description := "WindowsAgent isolated watchdog acceptance fixture"
	config, err := ParseConfig([]byte(validConfigJSON()))
	if err != nil {
		t.Fatal(err)
	}
	config.CheckIntervalMS = 250
	config.Targets = nil
	fixtures := map[string]liveFixture{}
	tasks := map[string]string{}
	for _, id := range []string{"upstream", "dependent"} {
		path := filepath.Join(root, id+".exe")
		if err := os.WriteFile(path, binary, 0700); err != nil {
			t.Fatal(err)
		}
		listener, err := net.Listen("tcp4", "127.0.0.1:0")
		if err != nil {
			t.Fatal(err)
		}
		address := listener.Addr().String()
		if err := listener.Close(); err != nil {
			t.Fatal(err)
		}
		fixture := liveFixture{Listen: address, Control: filepath.Join(root, id+"-control"), Starts: filepath.Join(root, id+"-starts")}
		if id == "upstream" {
			fixture.StartupDelayMS = 3500
		}
		fixtures[id] = fixture
		if err := os.WriteFile(fixture.Control, []byte("healthy"), 0600); err != nil {
			t.Fatal(err)
		}
		settings := filepath.Join(root, id+".json")
		encoded, err := json.Marshal(fixture)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(settings, encoded, 0600); err != nil {
			t.Fatal(err)
		}
		name := "WindowsAgent Acceptance " + filepath.Base(filepath.Dir(root)) + " " + id
		tasks[id] = name
		liveTask(t, driver, "Register", name, description, path, settings)
		t.Cleanup(func() { liveTask(t, driver, "Delete", name, description, "", "") })
		interactive := true
		target := Target{ID: id, DesiredState: "running", FailureThreshold: 1,
			Probes: []ProbeConfig{
				{Type: "process", ExecutablePath: path, RequireInteractiveSession: &interactive},
				{Type: "http-json", URL: "http://" + address + "/healthz", TimeoutMS: 1000, ExpectedStatusCode: 200, ExpectedJSONStatus: "ok"},
			},
			Recovery: RecoveryConfig{ScheduledTaskName: name, ExpectedTaskDescription: description, MaxAttempts: 3, AttemptWindowMS: 60000, BackoffMS: 1, ActionTimeoutMS: 10000, StartupGraceMS: 3000},
		}
		if id == "dependent" {
			target.StartAfterHealthy = []string{"upstream"}
		}
		config.Targets = append(config.Targets, target)
	}
	logFile, err := os.Create(filepath.Join(root, "watchdog.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	defer logFile.Close()
	observer, err := NewTargetObserver(&http.Client{}, WindowsProcessInspector{})
	if err != nil {
		t.Fatal(err)
	}
	engine, err := NewEngine(config, observer, liveTaskRecoverer{upstream: tasks["upstream"]}, FileStatusSink{Name: filepath.Join(root, "status.json")}, slog.New(slog.NewJSONHandler(logFile, nil)))
	if err != nil {
		t.Fatal(err)
	}
	// Finite CPU pressure exercises the native scheduler alongside the slow
	// initialization fixture. It ends independently even if an assertion fails.
	pressure, stopPressure := context.WithTimeout(ctx, 8*time.Second)
	var workers sync.WaitGroup
	for range runtime.NumCPU() {
		workers.Add(1)
		go func() {
			defer workers.Done()
			var n uint64
			for pressure.Err() == nil {
				n = n*1664525 + 1013904223
			}
			runtime.KeepAlive(n)
		}()
	}
	defer func() { stopPressure(); workers.Wait() }()
	liveWaitHealthy(t, ctx, engine)
	for _, id := range []string{"upstream", "dependent"} {
		if starts := liveStarts(t, fixtures[id].Starts); starts != 1 {
			t.Fatalf("initialization restarted %s %d times", id, starts)
		}
	}
	if err := os.WriteFile(fixtures["upstream"].Control, []byte("unhealthy"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := engine.Cycle(ctx); err != nil {
		t.Fatal(err)
	}
	if engine.targets["dependent"].status.State != StateHealthy || liveStarts(t, fixtures["dependent"].Starts) != 1 {
		t.Fatal("healthy dependent was disturbed by upstream recovery")
	}
	processes, err := (WindowsProcessInspector{}).FindByExecutablePath(ctx, config.Targets[1].Probes[0].ExecutablePath)
	if err != nil || len(processes) != 1 {
		t.Fatalf("fixture process identity: %v error=%v", processes, err)
	}
	process, err := os.FindProcess(int(processes[0].PID))
	if err != nil {
		t.Fatal(err)
	}
	if err := process.Kill(); err != nil {
		t.Fatal(err)
	}
	if _, err := process.Wait(); err != nil {
		t.Fatal(err)
	}
	if err := engine.Cycle(ctx); err != nil {
		t.Fatal(err)
	}
	if state := engine.targets["dependent"].status.State; state != StateBlocked {
		t.Fatalf("dependency did not block recovery: %s", state)
	}
	if liveStarts(t, fixtures["dependent"].Starts) != 1 {
		t.Fatal("dependent restarted before upstream readiness")
	}
	if err := os.WriteFile(fixtures["upstream"].Control, []byte("healthy"), 0600); err != nil {
		t.Fatal(err)
	}
	liveWaitHealthy(t, ctx, engine)
	for _, id := range []string{"upstream", "dependent"} {
		if starts := liveStarts(t, fixtures[id].Starts); starts != 2 {
			t.Fatalf("recovery restarted %s %d times", id, starts)
		}
	}
	encoded, err := json.Marshal(engine.snapshot(StateIdle, time.Now(), time.Now()))
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("interactiveSession=%d cpuPressureWorkers=%d cpuPressureMax=8s schemaVersion=%d final=%s", session, runtime.NumCPU(), SchemaVersion, encoded)
}

func liveWaitHealthy(t *testing.T, ctx context.Context, engine *Engine) {
	t.Helper()
	for {
		if err := ctx.Err(); err != nil {
			t.Fatalf("acceptance deadline: %v targets=%+v", err, engine.snapshot(StateIdle, time.Time{}, time.Time{}).Targets)
		}
		if err := engine.Cycle(ctx); err != nil {
			t.Fatal(err)
		}
		if engine.targets["upstream"].status.State == StateHealthy && engine.targets["dependent"].status.State == StateHealthy {
			return
		}
		select {
		case <-ctx.Done():
		case <-time.After(250 * time.Millisecond):
		}
	}
}

func liveStarts(t *testing.T, path string) int {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return len(strings.Fields(string(data)))
}

func liveTask(t *testing.T, driver, operation, name, description, binary, settings string) {
	t.Helper()
	args := []string{"-NoProfile", "-NonInteractive", "-ExecutionPolicy", "Bypass", "-File", driver, "-Operation", operation, "-Name", name, "-Description", description}
	if operation == "Register" {
		args = append(args, "-Binary", binary, "-Settings", settings)
	}
	output, err := exec.Command("powershell.exe", args...).CombinedOutput()
	if err != nil {
		t.Fatalf("fixture Task %s %s: %v %s", operation, name, err, output)
	}
}

const liveTaskDriver = `param([string]$Operation,[string]$Name,[string]$Description,[string]$Binary,[string]$Settings)
$ErrorActionPreference='Stop'
if ($Operation -eq 'Register') {
    $arguments='-test.run=^TestWatchdogLiveHelperProcess$ -- watchdog-acceptance-helper "'+$Settings+'"'
    $action=New-ScheduledTaskAction -Execute $Binary -Argument $arguments
    $identity=[Security.Principal.WindowsIdentity]::GetCurrent().Name
    $principal=New-ScheduledTaskPrincipal -UserId $identity -LogonType Interactive -RunLevel Limited
    $settingsObject=New-ScheduledTaskSettingsSet -AllowStartIfOnBatteries -DontStopIfGoingOnBatteries -Hidden -ExecutionTimeLimit ([timespan]::FromMinutes(2)) -MultipleInstances IgnoreNew
    Register-ScheduledTask -TaskName $Name -InputObject (New-ScheduledTask -Action $action -Principal $principal -Settings $settingsObject -Description $Description) | Out-Null
    exit
}
$task=Get-ScheduledTask -TaskName $Name
if ($task.Description -cne $Description) { throw 'acceptance Task ownership mismatch' }
if ($task.State -eq 'Running') { Stop-ScheduledTask -TaskName $Name }
$deadline=[DateTime]::UtcNow.AddSeconds(15)
while ((Get-ScheduledTask -TaskName $Name).State -eq 'Running') {
    if ([DateTime]::UtcNow -gt $deadline) { throw 'acceptance Task did not stop' }
    Start-Sleep -Milliseconds 100
}
if ($Operation -eq 'Delete') { Unregister-ScheduledTask -TaskName $Name -Confirm:$false }
`
