// Command windows-key invokes one foreground-pinned key press through the
// WindowsAgent game-neutral input runtime and waits for its durable terminal
// result.
package main

import (
	"bytes"
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
	"github.com/qoli/WindowsAgent/internal/inputaction"
)

const invocationPath = "/v1/key-inputs/invoke"

var pollInterval = 200 * time.Millisecond

func main() { os.Exit(run(os.Args[1:], os.Stdout, os.Stderr, http.DefaultClient)) }

func run(args []string, stdout, stderr io.Writer, client *http.Client) int {
	request, baseURL, err := parseRequest(args, stderr)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 2
	}
	endpoint, err := resolveURL(baseURL, invocationPath)
	if err != nil {
		fmt.Fprintln(stderr, "invalid --url:", err)
		return 2
	}
	body, err := json.Marshal(request)
	if err != nil {
		fmt.Fprintln(stderr, "encode request:", err)
		return 1
	}
	response, err := client.Post(endpoint, "application/json", bytes.NewReader(body))
	if err != nil {
		fmt.Fprintln(stderr, "invoke WindowsAgent:", err)
		return 1
	}
	acceptedBody, err := readBody(response)
	if err != nil {
		fmt.Fprintln(stderr, "read WindowsAgent response:", err)
		return 1
	}
	if response.StatusCode != http.StatusAccepted {
		fmt.Fprintf(stderr, "WindowsAgent returned HTTP %d: %s\n", response.StatusCode, strings.TrimSpace(string(acceptedBody)))
		return 1
	}
	var accepted actionrun.Invocation
	if err := json.Unmarshal(acceptedBody, &accepted); err != nil || accepted.InvocationID == "" {
		fmt.Fprintln(stderr, "decode invocation response: missing valid invocationId")
		return 1
	}
	location := response.Header.Get("Location")
	if location == "" || accepted.Stop == nil || accepted.Stop.Method != http.MethodPost || accepted.Stop.URL == "" {
		fmt.Fprintln(stderr, "WindowsAgent response is missing required status or stop target")
		return 1
	}
	statusURL, err := resolveURL(endpoint, location)
	if err != nil {
		fmt.Fprintln(stderr, "invalid invocation status URL:", err)
		return 1
	}
	stopURL, err := resolveURL(endpoint, accepted.Stop.URL)
	if err != nil {
		fmt.Fprintln(stderr, "invalid invocation stop URL:", err)
		return 1
	}
	interrupt := make(chan os.Signal, 1)
	signal.Notify(interrupt, os.Interrupt)
	defer signal.Stop(interrupt)
	for {
		select {
		case <-interrupt:
			request, _ := http.NewRequest(http.MethodPost, stopURL, nil)
			stopResponse, stopErr := client.Do(request)
			if stopErr != nil {
				fmt.Fprintln(stderr, "stop WindowsAgent invocation:", stopErr)
				return 1
			}
			_, _ = readBody(stopResponse)
		default:
		}
		statusResponse, err := client.Get(statusURL)
		if err != nil {
			fmt.Fprintln(stderr, "read invocation status:", err)
			return 1
		}
		statusBody, err := readBody(statusResponse)
		if err != nil {
			fmt.Fprintln(stderr, "read invocation status body:", err)
			return 1
		}
		if statusResponse.StatusCode != http.StatusOK {
			fmt.Fprintf(stderr, "WindowsAgent returned HTTP %d: %s\n", statusResponse.StatusCode, strings.TrimSpace(string(statusBody)))
			return 1
		}
		var status actionrun.Invocation
		if err := json.Unmarshal(statusBody, &status); err != nil || status.InvocationID != accepted.InvocationID {
			fmt.Fprintln(stderr, "decode invocation status: invalid invocation identity")
			return 1
		}
		switch status.State {
		case actionrun.StateCompleted:
			_, _ = stdout.Write(append(statusBody, '\n'))
			return 0
		case actionrun.StateFailed, actionrun.StateCancelled:
			_, _ = stdout.Write(append(statusBody, '\n'))
			return 1
		case actionrun.StateRunning, actionrun.StateCancelling:
			time.Sleep(pollInterval)
		default:
			fmt.Fprintln(stderr, "invocation returned invalid state:", status.State)
			return 1
		}
	}
}

func parseRequest(args []string, stderr io.Writer) (inputaction.DirectPressRequest, string, error) {
	if len(args) == 0 || args[0] != "press" {
		return inputaction.DirectPressRequest{}, "", errors.New("subcommand is required: press")
	}
	flags := flag.NewFlagSet("windows-key press", flag.ContinueOnError)
	flags.SetOutput(stderr)
	var baseURL, key, executableName, executablePath string
	var processID uint
	var hold time.Duration
	flags.StringVar(&baseURL, "url", "", "WindowsAgent base URL")
	flags.StringVar(&key, "key", "", "canonical Windows key")
	flags.DurationVar(&hold, "hold", 0, "bounded key hold, for example 40ms")
	flags.UintVar(&processID, "expected-process-id", 0, "fresh foreground process ID")
	flags.StringVar(&executableName, "expected-executable-name", "", "fresh foreground executable name")
	flags.StringVar(&executablePath, "expected-executable-path", "", "fresh foreground executable path")
	if err := flags.Parse(args[1:]); err != nil {
		return inputaction.DirectPressRequest{}, "", err
	}
	if flags.NArg() != 0 {
		return inputaction.DirectPressRequest{}, "", errors.New("positional arguments are not accepted")
	}
	if baseURL == "" || processID == 0 || uint64(processID) > uint64(^uint32(0)) {
		return inputaction.DirectPressRequest{}, "", errors.New("--url and a valid --expected-process-id are required")
	}
	if hold <= 0 || hold%time.Millisecond != 0 {
		return inputaction.DirectPressRequest{}, "", errors.New("--hold must be a positive whole-millisecond duration")
	}
	request := inputaction.DirectPressRequest{
		SchemaVersion: inputaction.DirectSchemaVersion,
		ExpectedForeground: inputaction.ExpectedForeground{
			ProcessID: uint32(processID), ExecutableName: executableName, ExecutablePath: executablePath,
		},
		Key: key, HoldMS: uint32(hold / time.Millisecond),
	}
	if err := request.Validate(); err != nil {
		return inputaction.DirectPressRequest{}, "", err
	}
	return request, baseURL, nil
}

func resolveURL(base, target string) (string, error) {
	baseURL, err := url.Parse(base)
	if err != nil || baseURL.Scheme == "" || baseURL.Host == "" {
		return "", errors.New("absolute HTTP URL is required")
	}
	if baseURL.Scheme != "http" && baseURL.Scheme != "https" {
		return "", errors.New("HTTP or HTTPS URL is required")
	}
	reference, err := url.Parse(target)
	if err != nil {
		return "", err
	}
	if strings.HasPrefix(target, "/") {
		baseURL.Path, baseURL.RawQuery, baseURL.Fragment = "", "", ""
	}
	return baseURL.ResolveReference(reference).String(), nil
}

func readBody(response *http.Response) ([]byte, error) {
	if response == nil {
		return nil, errors.New("HTTP response is required")
	}
	defer response.Body.Close()
	body, err := io.ReadAll(io.LimitReader(response.Body, 1<<20))
	if err != nil {
		return nil, err
	}
	if len(body) == 1<<20 {
		return nil, errors.New("HTTP response exceeds one MiB")
	}
	return body, nil
}
