// Command windows-exec invokes one structured process operation through a
// WindowsAgent instance and waits for its durable invocation to terminate.
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"strings"
	"time"

	"github.com/qoli/WindowsAgent/internal/actionrun"
	"github.com/qoli/WindowsAgent/internal/strictjson"
	"github.com/qoli/WindowsAgent/internal/windowsexec"
)

const invocationPath = "/v1/executions/invoke"

var pollInterval = 200 * time.Millisecond
var notifyInterrupt = signal.Notify
var stopInterrupt = signal.Stop

type stringValues []string

func (values *stringValues) String() string { return strings.Join(*values, ",") }

func (values *stringValues) Set(value string) error {
	*values = append(*values, value)
	return nil
}

func main() { os.Exit(run(os.Args[1:], os.Stdout, os.Stderr, http.DefaultClient)) }

func run(args []string, stdout, stderr io.Writer, client *http.Client) int {
	if len(args) == 0 {
		fmt.Fprintln(stderr, "subcommand is required: run, start, or ps1")
		return 2
	}
	if client == nil {
		fmt.Fprintln(stderr, "HTTP client is required")
		return 1
	}
	request, baseURL, err := parseRequest(args[0], args[1:], stderr)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 2
	}
	endpoint, err := invocationURL(baseURL)
	if err != nil {
		fmt.Fprintln(stderr, "invalid --url:", err)
		return 2
	}
	body, err := json.Marshal(request)
	if err != nil {
		fmt.Fprintln(stderr, "encode request:", err)
		return 1
	}
	httpClient := *client
	httpClient.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	response, err := doRequest(&httpClient, http.MethodPost, endpoint, body)
	if err != nil {
		fmt.Fprintln(stderr, "invoke WindowsAgent:", err)
		return 1
	}
	responseBody, err := readResponse(response)
	if err != nil {
		fmt.Fprintln(stderr, "read WindowsAgent response:", err)
		return 1
	}
	if response.StatusCode != http.StatusAccepted {
		writeHTTPError(stderr, response.StatusCode, responseBody)
		return 1
	}
	accepted, err := decodeInvocation(responseBody)
	if err != nil {
		fmt.Fprintln(stderr, "decode invocation response:", err)
		return 1
	}
	location := response.Header.Get("Location")
	if location == "" {
		fmt.Fprintln(stderr, "WindowsAgent response is missing required Location header")
		return 1
	}
	statusURL, err := resolveStatusURL(endpoint, location)
	if err != nil {
		fmt.Fprintln(stderr, "invalid invocation status URL:", err)
		return 1
	}
	stopURL := ""
	if request.Operation == windowsexec.OperationStart {
		if accepted.Stop != nil {
			fmt.Fprintln(stderr, "WindowsAgent returned an unexpected stop target for detached start")
			return 1
		}
	} else {
		if accepted.Stop == nil || accepted.Stop.Method != http.MethodPost || accepted.Stop.URL == "" {
			fmt.Fprintln(stderr, "WindowsAgent response is missing required POST stop target")
			return 1
		}
		stopURL, err = resolveStatusURL(endpoint, accepted.Stop.URL)
		if err != nil {
			fmt.Fprintln(stderr, "invalid invocation stop URL:", err)
			return 1
		}
	}
	var interrupt chan os.Signal
	if stopURL != "" {
		interrupt = make(chan os.Signal, 1)
		notifyInterrupt(interrupt, os.Interrupt)
		defer stopInterrupt(interrupt)
	}
	return waitForTerminal(&httpClient, statusURL, stopURL, accepted.InvocationID, interrupt, stdout, stderr)
}

func parseRequest(subcommand string, args []string, stderr io.Writer) (windowsexec.Request, string, error) {
	var operation windowsexec.Operation
	switch subcommand {
	case "run":
		operation = windowsexec.OperationRun
	case "start":
		operation = windowsexec.OperationStart
	case "ps1":
		operation = windowsexec.OperationPowerShellFile
	default:
		return windowsexec.Request{}, "", fmt.Errorf("unknown subcommand %q: expected run, start, or ps1", subcommand)
	}
	flags := flag.NewFlagSet("windows-exec "+subcommand, flag.ContinueOnError)
	flags.SetOutput(stderr)
	var baseURL, executable, scriptPath, cwd, stdinFile, window string
	var argv, environment stringValues
	var maxOutputBytes uint64
	var timeout time.Duration
	flags.StringVar(&baseURL, "url", "", "WindowsAgent base URL")
	flags.StringVar(&executable, "executable", "", "Windows executable path")
	flags.StringVar(&scriptPath, "script-path", "", "remote Windows PowerShell script path")
	flags.Var(&argv, "arg", "process argument (repeatable)")
	flags.StringVar(&cwd, "cwd", "", "Windows working directory")
	flags.Var(&environment, "env", "environment NAME=VALUE (repeatable)")
	flags.StringVar(&window, "window", "", "window mode: normal or hidden")
	flags.StringVar(&stdinFile, "stdin-file", "", "local file used as process stdin")
	flags.Uint64Var(&maxOutputBytes, "max-output-bytes", 0, "per-stream captured output limit; 0 uses 65536 bytes, larger values are rejected")
	flags.DurationVar(&timeout, "timeout", 0, "remote execution timeout, for example 30s")
	if err := flags.Parse(args); err != nil {
		return windowsexec.Request{}, "", err
	}
	if flags.NArg() != 0 {
		return windowsexec.Request{}, "", errors.New("positional arguments and raw command strings are not accepted; use repeatable --arg")
	}
	if baseURL == "" {
		return windowsexec.Request{}, "", errors.New("--url is required")
	}
	if timeout < 0 {
		return windowsexec.Request{}, "", errors.New("--timeout must not be negative")
	}
	if timeout > 0 && timeout%time.Millisecond != 0 {
		return windowsexec.Request{}, "", errors.New("--timeout must resolve to whole milliseconds")
	}
	env, err := parseEnvironment(environment)
	if err != nil {
		return windowsexec.Request{}, "", err
	}
	request := windowsexec.Request{
		SchemaVersion:       windowsexec.SchemaVersion,
		Operation:           operation,
		Argv:                argv,
		Cwd:                 cwd,
		Env:                 env,
		MaxOutputBytes:      maxOutputBytes,
		TimeoutMilliseconds: uint64(timeout / time.Millisecond),
	}
	switch window {
	case "":
	case string(windowsexec.WindowNormal):
		request.Window = windowsexec.WindowNormal
	case string(windowsexec.WindowHidden):
		request.Window = windowsexec.WindowHidden
	default:
		return windowsexec.Request{}, "", errors.New("--window must be normal or hidden")
	}
	switch operation {
	case windowsexec.OperationRun, windowsexec.OperationStart:
		if executable == "" {
			return windowsexec.Request{}, "", errors.New("--executable is required")
		}
		if scriptPath != "" {
			return windowsexec.Request{}, "", errors.New("--script-path is only accepted by ps1")
		}
		request.Executable = executable
	case windowsexec.OperationPowerShellFile:
		if scriptPath == "" {
			return windowsexec.Request{}, "", errors.New("--script-path is required")
		}
		if executable != "" {
			return windowsexec.Request{}, "", errors.New("--executable is not accepted by ps1")
		}
		request.ScriptPath = scriptPath
	}
	if operation == windowsexec.OperationStart && (stdinFile != "" || maxOutputBytes != 0 || timeout != 0) {
		return windowsexec.Request{}, "", errors.New("start does not accept --stdin-file, --max-output-bytes, or --timeout")
	}
	if stdinFile != "" {
		stdin, err := os.ReadFile(stdinFile)
		if err != nil {
			return windowsexec.Request{}, "", fmt.Errorf("read --stdin-file: %w", err)
		}
		request.Stdin = stdin
	}
	if err := request.Validate(); err != nil {
		return windowsexec.Request{}, "", fmt.Errorf("validate execution request: %w", err)
	}
	return request, baseURL, nil
}

func parseEnvironment(values []string) (map[string]string, error) {
	if len(values) == 0 {
		return nil, nil
	}
	result := make(map[string]string, len(values))
	for _, value := range values {
		name, content, found := strings.Cut(value, "=")
		if !found || name == "" {
			return nil, fmt.Errorf("invalid --env %q: expected NAME=VALUE", value)
		}
		if _, duplicate := result[name]; duplicate {
			return nil, fmt.Errorf("duplicate --env name %q", name)
		}
		result[name] = content
	}
	return result, nil
}

func invocationURL(value string) (string, error) {
	parsed, err := url.Parse(value)
	if err != nil {
		return "", err
	}
	if (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Host == "" {
		return "", errors.New("must be an absolute HTTP or HTTPS URL")
	}
	if parsed.RawQuery != "" || parsed.Fragment != "" {
		return "", errors.New("query and fragment are not accepted")
	}
	parsed.Path = strings.TrimRight(parsed.Path, "/") + invocationPath
	return parsed.String(), nil
}

func resolveStatusURL(endpoint, location string) (string, error) {
	base, err := url.Parse(endpoint)
	if err != nil {
		return "", err
	}
	reference, err := url.Parse(location)
	if err != nil {
		return "", err
	}
	resolved := base.ResolveReference(reference)
	if (resolved.Scheme != "http" && resolved.Scheme != "https") || resolved.Host == "" {
		return "", errors.New("Location must resolve to an absolute HTTP or HTTPS URL")
	}
	return resolved.String(), nil
}

func waitForTerminal(client *http.Client, statusURL, stopURL, invocationID string, interrupt <-chan os.Signal, stdout, stderr io.Writer) int {
	stopRequested := false
	for {
		pollContext, cancelPoll := context.WithCancel(context.Background())
		polls := make(chan statusPoll, 1)
		go func() { polls <- pollStatus(pollContext, client, statusURL) }()
		var poll statusPoll
		select {
		case poll = <-polls:
			cancelPoll()
		case <-interrupt:
			cancelPoll()
			<-polls
			if stopRequested {
				fmt.Fprintln(stderr, "interrupted while waiting for cancellation")
				return 1
			}
			if stopURL == "" {
				fmt.Fprintln(stderr, "invocation is not interruptible")
				return 1
			}
			if err := requestStop(client, stopURL, invocationID); err != nil {
				fmt.Fprintln(stderr, "stop invocation after interrupt:", err)
				return 1
			}
			stopRequested = true
			continue
		}
		if poll.err != nil {
			fmt.Fprintln(stderr, "read invocation status:", poll.err)
			return 1
		}
		body, invocation := poll.body, poll.invocation
		if invocation.InvocationID != invocationID {
			fmt.Fprintf(stderr, "invocation status identity changed from %q to %q\n", invocationID, invocation.InvocationID)
			return 1
		}
		switch invocation.State {
		case actionrun.StateCompleted:
			if err := writeTerminal(stdout, body); err != nil {
				fmt.Fprintln(stderr, "write terminal invocation:", err)
				return 1
			}
			return 0
		case actionrun.StateFailed, actionrun.StateCancelled:
			if err := writeTerminal(stdout, body); err != nil {
				fmt.Fprintln(stderr, "write terminal invocation:", err)
			}
			return 1
		case actionrun.StateRunning, actionrun.StateCancelling:
			timer := time.NewTimer(pollInterval)
			select {
			case <-interrupt:
				if !timer.Stop() {
					<-timer.C
				}
				if stopRequested {
					fmt.Fprintln(stderr, "interrupted while waiting for cancellation")
					return 1
				}
				if stopURL == "" {
					fmt.Fprintln(stderr, "invocation is not interruptible")
					return 1
				}
				if err := requestStop(client, stopURL, invocationID); err != nil {
					fmt.Fprintln(stderr, "stop invocation after interrupt:", err)
					return 1
				}
				stopRequested = true
			case <-timer.C:
			}
		default:
			fmt.Fprintf(stderr, "invalid invocation state %q\n", invocation.State)
			return 1
		}
	}
}

type statusPoll struct {
	body       []byte
	invocation actionrun.Invocation
	err        error
}

func pollStatus(ctx context.Context, client *http.Client, statusURL string) statusPoll {
	response, err := doRequestContext(ctx, client, http.MethodGet, statusURL, nil)
	if err != nil {
		return statusPoll{err: err}
	}
	body, err := readResponse(response)
	if err != nil {
		return statusPoll{err: fmt.Errorf("read response: %w", err)}
	}
	if response.StatusCode != http.StatusOK {
		return statusPoll{err: fmt.Errorf("WindowsAgent returned HTTP %d: %s", response.StatusCode, strings.TrimSpace(string(body)))}
	}
	invocation, err := decodeInvocation(body)
	if err != nil {
		return statusPoll{err: fmt.Errorf("decode response: %w", err)}
	}
	return statusPoll{body: body, invocation: invocation}
}

func requestStop(client *http.Client, stopURL, invocationID string) error {
	response, err := doRequest(client, http.MethodPost, stopURL, nil)
	if err != nil {
		return err
	}
	body, err := readResponse(response)
	if err != nil {
		return fmt.Errorf("read response: %w", err)
	}
	if response.StatusCode != http.StatusAccepted {
		return fmt.Errorf("WindowsAgent returned HTTP %d: %s", response.StatusCode, strings.TrimSpace(string(body)))
	}
	invocation, err := decodeInvocation(body)
	if err != nil {
		return fmt.Errorf("decode response: %w", err)
	}
	if invocation.InvocationID != invocationID {
		return fmt.Errorf("response identity changed from %q to %q", invocationID, invocation.InvocationID)
	}
	return nil
}

func doRequest(client *http.Client, method, endpoint string, body []byte) (*http.Response, error) {
	return doRequestContext(context.Background(), client, method, endpoint, body)
}

func doRequestContext(ctx context.Context, client *http.Client, method, endpoint string, body []byte) (*http.Response, error) {
	request, err := http.NewRequestWithContext(ctx, method, endpoint, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	if body != nil {
		request.Header.Set("Content-Type", "application/json")
	}
	return client.Do(request)
}

func readResponse(response *http.Response) ([]byte, error) {
	defer response.Body.Close()
	return io.ReadAll(response.Body)
}

func decodeInvocation(data []byte) (actionrun.Invocation, error) {
	if err := strictjson.Validate(data); err != nil {
		return actionrun.Invocation{}, fmt.Errorf("validate strict JSON: %w", err)
	}
	var invocation actionrun.Invocation
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&invocation); err != nil {
		return actionrun.Invocation{}, err
	}
	if invocation.InvocationID == "" || invocation.State == "" {
		return actionrun.Invocation{}, errors.New("invocationId and state are required")
	}
	return invocation, nil
}

func writeHTTPError(stderr io.Writer, status int, body []byte) {
	fmt.Fprintf(stderr, "WindowsAgent returned HTTP %d: %s\n", status, strings.TrimSpace(string(body)))
}

func writeTerminal(stdout io.Writer, body []byte) error {
	if _, err := stdout.Write(body); err != nil {
		return err
	}
	if len(body) == 0 || body[len(body)-1] != '\n' {
		_, err := io.WriteString(stdout, "\n")
		return err
	}
	return nil
}
