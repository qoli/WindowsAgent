// Command windows-starlark-invoke preflights and uploads one ephemeral
// windows-starlark-action-v1 package to a WindowsAgent instance.
package main

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"

	"github.com/qoli/WindowsAgent/internal/strictjson"
	"github.com/qoli/WindowsAgent/internal/windowsautomation"
)

const invocationPath = "/v1/starlark-actions/invoke"

type invocationRequest struct {
	SchemaVersion uint32         `json:"schemaVersion"`
	PackageBase64 string         `json:"packageBase64"`
	Inputs        map[string]any `json:"inputs"`
}

func main() { os.Exit(run(os.Args[1:], os.Stdout, os.Stderr, http.DefaultClient)) }

func run(args []string, stdout, stderr io.Writer, client *http.Client) int {
	flags := flag.NewFlagSet("windows-starlark-invoke", flag.ContinueOnError)
	flags.SetOutput(stderr)
	var baseURL, packageDirectory, inputsPath string
	flags.StringVar(&baseURL, "url", "", "WindowsAgent base URL")
	flags.StringVar(&packageDirectory, "package", "", "Starlark package directory")
	flags.StringVar(&inputsPath, "inputs", "", "strict JSON inputs object")
	if err := flags.Parse(args); err != nil {
		return 2
	}
	if flags.NArg() != 0 || baseURL == "" || packageDirectory == "" || inputsPath == "" {
		fmt.Fprintln(stderr, "--url, --package, and --inputs are required; positional arguments are not accepted")
		return 2
	}
	if client == nil {
		fmt.Fprintln(stderr, "HTTP client is required")
		return 1
	}
	endpoint, err := invocationURL(baseURL)
	if err != nil {
		fmt.Fprintln(stderr, "invalid --url:", err)
		return 2
	}
	pkg, err := windowsautomation.LoadDirectory(packageDirectory)
	if err != nil {
		fmt.Fprintln(stderr, "preflight package:", err)
		return 1
	}
	inputs, err := readInputs(inputsPath)
	if err != nil {
		fmt.Fprintln(stderr, "read inputs:", err)
		return 1
	}
	if err := pkg.ValidateInputs(inputs); err != nil {
		fmt.Fprintln(stderr, "preflight inputs:", err)
		return 1
	}
	archive, err := windowsautomation.ArchiveDirectory(packageDirectory)
	if err != nil {
		fmt.Fprintln(stderr, "archive package:", err)
		return 1
	}
	body, err := json.Marshal(invocationRequest{
		SchemaVersion: 1,
		PackageBase64: base64.StdEncoding.EncodeToString(archive),
		Inputs:        inputs,
	})
	if err != nil {
		fmt.Fprintln(stderr, "encode request:", err)
		return 1
	}
	request, err := http.NewRequest(http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		fmt.Fprintln(stderr, "create request:", err)
		return 1
	}
	request.Header.Set("Content-Type", "application/json")
	httpClient := *client
	httpClient.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	response, err := httpClient.Do(request)
	if err != nil {
		fmt.Fprintln(stderr, "invoke WindowsAgent:", err)
		return 1
	}
	defer response.Body.Close()
	responseBody, err := io.ReadAll(response.Body)
	if err != nil {
		fmt.Fprintln(stderr, "read WindowsAgent response:", err)
		return 1
	}
	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		fmt.Fprintf(stderr, "WindowsAgent returned HTTP %d: %s\n", response.StatusCode, strings.TrimSpace(string(responseBody)))
		return 1
	}
	if _, err := io.Copy(stdout, bytes.NewReader(responseBody)); err != nil {
		fmt.Fprintln(stderr, "write WindowsAgent response:", err)
		return 1
	}
	return 0
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

func readInputs(name string) (map[string]any, error) {
	data, err := os.ReadFile(name)
	if err != nil {
		return nil, err
	}
	if err := strictjson.Validate(data); err != nil {
		return nil, fmt.Errorf("validate strict JSON: %w", err)
	}
	var inputs map[string]any
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.UseNumber()
	if err := decoder.Decode(&inputs); err != nil {
		return nil, fmt.Errorf("decode JSON object: %w", err)
	}
	if inputs == nil {
		return nil, errors.New("inputs must be a JSON object")
	}
	return inputs, nil
}
