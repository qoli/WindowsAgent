//go:build windows

package processinventory

import (
	"context"
	"errors"
	"fmt"
	"runtime"
	"syscall"
	"time"
	"unicode/utf16"
	"unsafe"

	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/registry"
)

const (
	processCommandLineInformation = 60
	stillActiveExitCode           = 259
	maxWindowsPathCharacters      = 32768

	statusInfoLengthMismatch = 0xC0000004
	statusBufferOverflow     = 0x80000005
	statusBufferTooSmall     = 0xC0000023
)

var ntQueryInformationProcess = windows.NewLazySystemDLL("ntdll.dll").NewProc("NtQueryInformationProcess")

type osSource struct{}

func (osSource) Processes(ctx context.Context) ([]Process, error) {
	snapshot, err := windows.CreateToolhelp32Snapshot(windows.TH32CS_SNAPPROCESS, 0)
	if err != nil {
		return nil, fmt.Errorf("create process snapshot: %w", err)
	}
	defer windows.CloseHandle(snapshot)

	entry := windows.ProcessEntry32{Size: uint32(unsafe.Sizeof(windows.ProcessEntry32{}))}
	if err := windows.Process32First(snapshot, &entry); err != nil {
		return nil, fmt.Errorf("read first process: %w", err)
	}
	processes := make([]Process, 0)
	for {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		process := Process{
			PID:     entry.ProcessID,
			Parent:  entry.ParentProcessID,
			Name:    windows.UTF16ToString(entry.ExeFile[:]),
			Threads: entry.Threads,
		}
		enrichProcess(&process)
		processes = append(processes, process)

		if err := windows.Process32Next(snapshot, &entry); err != nil {
			if errors.Is(err, windows.ERROR_NO_MORE_FILES) {
				break
			}
			return nil, fmt.Errorf("read next process: %w", err)
		}
	}
	return processes, nil
}

func enrichProcess(process *Process) {
	var sessionID uint32
	if err := windows.ProcessIdToSessionId(process.PID, &sessionID); err == nil {
		process.SessionID = &sessionID
	}
	if process.PID == 0 {
		return
	}
	handle, err := windows.OpenProcess(windows.PROCESS_QUERY_LIMITED_INFORMATION, false, process.PID)
	if err != nil {
		return
	}
	defer windows.CloseHandle(handle)

	if path, err := processPath(handle); err == nil {
		process.Path = &path
	}
	if commandLine, err := processCommandLine(handle); err == nil {
		process.Cmdline = &commandLine
	}
	var exitCode uint32
	if err := windows.GetExitCodeProcess(handle, &exitCode); err == nil {
		state := "EXITED"
		if exitCode == stillActiveExitCode {
			state = "STILL_ACTIVE"
		}
		process.State = &state
	}
	var creation, exit, kernel, user windows.Filetime
	if err := windows.GetProcessTimes(handle, &creation, &exit, &kernel, &user); err == nil {
		startTime := creation.Nanoseconds() / int64(time.Second)
		process.StartTime = &startTime
	}
}

func processPath(handle windows.Handle) (string, error) {
	buffer := make([]uint16, maxWindowsPathCharacters)
	size := uint32(len(buffer))
	if err := windows.QueryFullProcessImageName(handle, 0, &buffer[0], &size); err != nil {
		return "", err
	}
	if size == 0 || size > uint32(len(buffer)) {
		return "", errors.New("QueryFullProcessImageNameW returned an invalid path length")
	}
	return windows.UTF16ToString(buffer[:size]), nil
}

type unicodeString struct {
	Length        uint16
	MaximumLength uint16
	Buffer        *uint16
}

func processCommandLine(handle windows.Handle) (string, error) {
	var required uint32
	status, _, _ := ntQueryInformationProcess.Call(
		uintptr(handle),
		processCommandLineInformation,
		0,
		0,
		uintptr(unsafe.Pointer(&required)),
	)
	switch uint32(status) {
	case statusInfoLengthMismatch, statusBufferOverflow, statusBufferTooSmall:
	default:
		return "", fmt.Errorf("query process command-line size: NTSTATUS 0x%08X", uint32(status))
	}
	if required < uint32(unsafe.Sizeof(unicodeString{})) {
		return "", fmt.Errorf("query process command-line size returned %d bytes", required)
	}
	buffer := make([]byte, required)
	status, _, _ = ntQueryInformationProcess.Call(
		uintptr(handle),
		processCommandLineInformation,
		uintptr(unsafe.Pointer(&buffer[0])),
		uintptr(len(buffer)),
		uintptr(unsafe.Pointer(&required)),
	)
	if uint32(status) != 0 {
		return "", fmt.Errorf("query process command line: NTSTATUS 0x%08X", uint32(status))
	}
	value := (*unicodeString)(unsafe.Pointer(&buffer[0]))
	if value.Length > value.MaximumLength || value.Length%2 != 0 {
		return "", errors.New("process command line returned an invalid UTF-16 length")
	}
	if value.Length == 0 {
		return "", nil
	}
	base := uintptr(unsafe.Pointer(&buffer[0]))
	end := base + uintptr(len(buffer))
	start := uintptr(unsafe.Pointer(value.Buffer))
	length := uintptr(value.Length)
	if value.Buffer == nil || start < base || start > end || length > end-start {
		return "", errors.New("process command line returned a buffer outside the response")
	}
	characters := unsafe.Slice(value.Buffer, int(value.Length/2))
	result := string(utf16.Decode(characters))
	runtime.KeepAlive(buffer)
	return result, nil
}

func (osSource) Services(ctx context.Context) ([]Service, error) {
	manager, err := windows.OpenSCManager(nil, nil, windows.SC_MANAGER_ENUMERATE_SERVICE)
	if err != nil {
		return nil, fmt.Errorf("open Service Control Manager: %w", err)
	}
	defer windows.CloseServiceHandle(manager)

	buffer, serviceCount, err := enumerateServices(ctx, manager)
	if err != nil {
		return nil, err
	}
	if serviceCount == 0 {
		return []Service{}, nil
	}
	entrySize := unsafe.Sizeof(windows.ENUM_SERVICE_STATUS_PROCESS{})
	if uintptr(serviceCount) > uintptr(len(buffer))/entrySize {
		return nil, errors.New("Service Control Manager returned an invalid service count")
	}
	entries := unsafe.Slice((*windows.ENUM_SERVICE_STATUS_PROCESS)(unsafe.Pointer(&buffer[0])), int(serviceCount))
	services := make([]Service, 0, serviceCount)
	for _, entry := range entries {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		status := entry.ServiceStatusProcess
		service := Service{
			PID:                 status.ProcessId,
			Name:                windows.UTF16PtrToString(entry.ServiceName),
			DisplayName:         windows.UTF16PtrToString(entry.DisplayName),
			Status:              serviceStatus(status.CurrentState),
			ServiceType:         serviceType(status.ServiceType),
			Win32ExitCode:       status.Win32ExitCode,
			ServiceSpecificCode: status.ServiceSpecificExitCode,
		}
		enrichService(manager, &service)
		services = append(services, service)
	}
	runtime.KeepAlive(buffer)
	return services, nil
}

func enumerateServices(ctx context.Context, manager windows.Handle) ([]byte, uint32, error) {
	var buffer []byte
	for {
		if err := ctx.Err(); err != nil {
			return nil, 0, err
		}
		var bytesNeeded, serviceCount uint32
		var address *byte
		if len(buffer) != 0 {
			address = &buffer[0]
		}
		err := windows.EnumServicesStatusEx(
			manager, windows.SC_ENUM_PROCESS_INFO, windows.SERVICE_TYPE_ALL, windows.SERVICE_STATE_ALL,
			address, uint32(len(buffer)), &bytesNeeded, &serviceCount, nil, nil,
		)
		if err == nil {
			return buffer, serviceCount, nil
		}
		if !errors.Is(err, windows.ERROR_MORE_DATA) {
			return nil, 0, fmt.Errorf("enumerate services: %w", err)
		}
		if bytesNeeded == 0 {
			return nil, 0, errors.New("Service Control Manager requested an empty retry buffer")
		}
		buffer = make([]byte, bytesNeeded)
	}
}

func enrichService(manager windows.Handle, service *Service) {
	name, err := windows.UTF16PtrFromString(service.Name)
	if err != nil {
		return
	}
	handle, err := windows.OpenService(manager, name, windows.SERVICE_QUERY_CONFIG)
	if err != nil {
		return
	}
	defer windows.CloseServiceHandle(handle)

	if config, buffer, err := queryServiceConfig(handle); err == nil {
		startType := serviceStartType(config.StartType)
		service.StartType = &startType
		if config.BinaryPathName != nil {
			path := windows.UTF16PtrToString(config.BinaryPathName)
			service.Path = &path
		}
		if config.ServiceStartName != nil {
			account := windows.UTF16PtrToString(config.ServiceStartName)
			service.UserAccount = &account
		}
		runtime.KeepAlive(buffer)
	}
	if description, buffer, err := queryServiceDescription(handle); err == nil && description.Description != nil {
		value := windows.UTF16PtrToString(description.Description)
		service.Description = &value
		runtime.KeepAlive(buffer)
	}
	if modulePath, ok := serviceModulePath(service.Name); ok {
		service.ModulePath = &modulePath
	}
}

func queryServiceConfig(handle windows.Handle) (*windows.QUERY_SERVICE_CONFIG, []byte, error) {
	var required uint32
	err := windows.QueryServiceConfig(handle, nil, 0, &required)
	if err != nil && !errors.Is(err, windows.ERROR_INSUFFICIENT_BUFFER) {
		return nil, nil, err
	}
	if required < uint32(unsafe.Sizeof(windows.QUERY_SERVICE_CONFIG{})) {
		return nil, nil, fmt.Errorf("QueryServiceConfigW returned an invalid size %d", required)
	}
	buffer := make([]byte, required)
	config := (*windows.QUERY_SERVICE_CONFIG)(unsafe.Pointer(&buffer[0]))
	if err := windows.QueryServiceConfig(handle, config, uint32(len(buffer)), &required); err != nil {
		return nil, nil, err
	}
	return config, buffer, nil
}

func queryServiceDescription(handle windows.Handle) (*windows.SERVICE_DESCRIPTION, []byte, error) {
	var required uint32
	err := windows.QueryServiceConfig2(handle, windows.SERVICE_CONFIG_DESCRIPTION, nil, 0, &required)
	if err != nil && !errors.Is(err, windows.ERROR_INSUFFICIENT_BUFFER) {
		return nil, nil, err
	}
	if required < uint32(unsafe.Sizeof(windows.SERVICE_DESCRIPTION{})) {
		return nil, nil, fmt.Errorf("QueryServiceConfig2W returned an invalid size %d", required)
	}
	buffer := make([]byte, required)
	description := (*windows.SERVICE_DESCRIPTION)(unsafe.Pointer(&buffer[0]))
	if err := windows.QueryServiceConfig2(handle, windows.SERVICE_CONFIG_DESCRIPTION, &buffer[0], uint32(len(buffer)), &required); err != nil {
		return nil, nil, err
	}
	return description, buffer, nil
}

func serviceModulePath(name string) (string, bool) {
	key, err := registry.OpenKey(
		registry.LOCAL_MACHINE,
		`SYSTEM\CurrentControlSet\Services\`+name+`\Parameters`,
		registry.QUERY_VALUE,
	)
	if err != nil {
		return "", false
	}
	defer key.Close()
	value, valueType, err := key.GetStringValue("ServiceDll")
	if err != nil || value == "" {
		return "", false
	}
	if valueType != registry.EXPAND_SZ {
		return value, true
	}
	source, err := syscall.UTF16PtrFromString(value)
	if err != nil {
		return "", false
	}
	buffer := make([]uint16, windows.MAX_PATH)
	for {
		size, err := windows.ExpandEnvironmentStrings(source, &buffer[0], uint32(len(buffer)))
		if err != nil {
			return "", false
		}
		if size <= uint32(len(buffer)) {
			return windows.UTF16ToString(buffer[:size]), true
		}
		buffer = make([]uint16, size)
	}
}

func serviceStatus(value uint32) string {
	statuses := [...]string{"UNKNOWN", "STOPPED", "START_PENDING", "STOP_PENDING", "RUNNING", "CONTINUE_PENDING", "PAUSE_PENDING", "PAUSED"}
	if value >= uint32(len(statuses)) {
		return "UNKNOWN"
	}
	return statuses[value]
}

func serviceStartType(value uint32) string {
	types := [...]string{"BOOT_START", "SYSTEM_START", "AUTO_START", "DEMAND_START", "DISABLED"}
	if value >= uint32(len(types)) {
		return "UNKNOWN"
	}
	return types[value]
}

func serviceType(value uint32) string {
	types := map[uint32]string{
		0x00000001: "KERNEL_DRIVER",
		0x00000002: "FILE_SYSTEM_DRIVER",
		0x00000010: "OWN_PROCESS",
		0x00000020: "SHARE_PROCESS",
		0x00000050: "USER_OWN_PROCESS",
		0x00000060: "USER_SHARE_PROCESS",
		0x000000d0: "USER_OWN_PROCESS(Instance)",
		0x000000e0: "USER_SHARE_PROCESS(Instance)",
		0x00000100: "INTERACTIVE_PROCESS",
		0x00000110: "OWN_PROCESS(Interactive)",
		0x00000120: "SHARE_PROCESS(Interactive)",
	}
	if result, ok := types[value]; ok {
		return result
	}
	return "UNKNOWN"
}
