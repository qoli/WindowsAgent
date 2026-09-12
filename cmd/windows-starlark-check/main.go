// Command windows-starlark-check performs local package and syntax preflight
// without executing the Starlark Action.
package main

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/qoli/WindowsAgent/internal/windowsautomation"
)

type report struct {
	Valid   bool   `json:"valid"`
	Runtime string `json:"runtime"`
	Title   string `json:"title,omitempty"`
	Version uint32 `json:"version,omitempty"`
	Digest  string `json:"digest,omitempty"`
	Error   string `json:"error,omitempty"`
}

func main() { os.Exit(run(os.Args[1:], os.Stdout, os.Stderr)) }

func run(args []string, stdout, stderr io.Writer) int {
	flags := flag.NewFlagSet("windows-starlark-check", flag.ContinueOnError)
	flags.SetOutput(stderr)
	var packagePath string
	var jsonOutput bool
	flags.StringVar(&packagePath, "package", "", "Starlark package directory or deterministic ZIP archive")
	flags.BoolVar(&jsonOutput, "json", false, "write a machine-readable JSON report")
	if err := flags.Parse(args); err != nil {
		return 2
	}
	if flags.NArg() != 0 || packagePath == "" {
		fmt.Fprintln(stderr, "--package is required and positional arguments are not accepted")
		return 2
	}
	pkg, err := load(packagePath)
	result := report{Valid: err == nil, Runtime: windowsautomation.RuntimeID}
	if err != nil {
		result.Error = err.Error()
	} else {
		result.Title, result.Version, result.Digest = pkg.Manifest.Title, pkg.Manifest.Version, pkg.Digest
	}
	if jsonOutput {
		encoder := json.NewEncoder(stdout)
		encoder.SetIndent("", "  ")
		if encodeErr := encoder.Encode(result); encodeErr != nil {
			fmt.Fprintln(stderr, "write report:", encodeErr)
			return 2
		}
	} else if err != nil {
		fmt.Fprintln(stdout, "INVALID:", err)
	} else {
		fmt.Fprintf(stdout, "VALID %s %s v%d sha256:%s\n", result.Runtime, result.Title, result.Version, result.Digest)
	}
	if err != nil {
		return 1
	}
	return 0
}

func load(name string) (*windowsautomation.Package, error) {
	info, err := os.Stat(name)
	if err != nil {
		return nil, err
	}
	if info.IsDir() {
		return windowsautomation.LoadDirectory(name)
	}
	if !info.Mode().IsRegular() {
		return nil, errors.New("package must be a directory or regular ZIP file")
	}
	if filepath.Ext(name) != ".zip" {
		return nil, errors.New("package archive must use the .zip extension")
	}
	data, err := os.ReadFile(name)
	if err != nil {
		return nil, err
	}
	return windowsautomation.LoadArchive(data)
}
