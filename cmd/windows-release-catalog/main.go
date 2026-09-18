package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"

	"github.com/qoli/WindowsAgent/internal/releasecatalog"
)

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}

func run(args []string) error {
	flags := flag.NewFlagSet("windows-release-catalog", flag.ContinueOnError)
	flags.SetOutput(os.Stderr)
	var inputDir, version, outputJSON, outputSums string
	flags.StringVar(&inputDir, "input-dir", "", "directory containing the complete Windows release executable set")
	flags.StringVar(&version, "version", "", "canonical release version")
	flags.StringVar(&outputJSON, "output-json", "", "output path for windowsagent-release.json")
	flags.StringVar(&outputSums, "output-sums", "", "output path for SHA256SUMS")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() != 0 {
		return fmt.Errorf("unexpected positional arguments: %v", flags.Args())
	}
	if inputDir == "" || version == "" || outputJSON == "" || outputSums == "" {
		return fmt.Errorf("--input-dir, --version, --output-json, and --output-sums are required")
	}
	catalog, err := releasecatalog.Generate(inputDir, version)
	if err != nil {
		return err
	}
	if err := writeFile(outputJSON, func(file *os.File) error { return releasecatalog.WriteJSON(file, catalog) }); err != nil {
		return err
	}
	if err := writeFile(outputSums, func(file *os.File) error { return releasecatalog.WriteSHA256Sums(file, catalog) }); err != nil {
		return err
	}
	return nil
}

func writeFile(path string, write func(*os.File) error) (resultErr error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	file, err := os.Create(path)
	if err != nil {
		return err
	}
	defer func() {
		if closeErr := file.Close(); resultErr == nil {
			resultErr = closeErr
		}
	}()
	return write(file)
}
