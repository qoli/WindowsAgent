// Command windows-action-check validates external Rule Action packages and
// their static child-Action dependencies without starting WindowsAgent.
package main

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/qoli/WindowsAgent/internal/actioncheck"
)

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

func run(args []string, stdout, stderr io.Writer) int {
	flags := flag.NewFlagSet("windows-action-check", flag.ContinueOnError)
	flags.SetOutput(stderr)
	var rulesDir string
	var jsonOutput bool
	flags.StringVar(&rulesDir, "rules-dir", "Rules", "Rule plugin directory")
	flags.BoolVar(&jsonOutput, "json", false, "write the machine-readable JSON report")
	if err := flags.Parse(args); err != nil {
		return 2
	}
	if flags.NArg() != 0 {
		fmt.Fprintln(stderr, "unexpected positional arguments")
		return 2
	}
	absoluteRulesDir, err := filepath.Abs(rulesDir)
	if err != nil {
		fmt.Fprintln(stderr, "resolve Rules directory:", err)
		return 2
	}
	result, err := actioncheck.Check(absoluteRulesDir)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 2
	}
	if jsonOutput {
		encoder := json.NewEncoder(stdout)
		encoder.SetIndent("", "  ")
		err = encoder.Encode(result)
	} else {
		err = actioncheck.WriteText(stdout, result)
	}
	if err != nil {
		if !errors.Is(err, os.ErrClosed) {
			fmt.Fprintln(stderr, "write report:", err)
		}
		return 2
	}
	if !result.Valid {
		return 1
	}
	return 0
}
