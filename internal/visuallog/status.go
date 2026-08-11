package visuallog

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

func WriteJSONAtomic(name string, value any) error {
	if name == "" || !filepath.IsAbs(name) {
		return fmt.Errorf("visual log status path must be absolute")
	}
	if err := os.MkdirAll(filepath.Dir(name), 0o700); err != nil {
		return fmt.Errorf("create visual log status directory: %w", err)
	}
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return fmt.Errorf("encode visual log status: %w", err)
	}
	data = append(data, '\n')
	temporary, err := os.CreateTemp(filepath.Dir(name), ".visual-log-status-*.tmp")
	if err != nil {
		return fmt.Errorf("create visual log status staging file: %w", err)
	}
	temporaryName := temporary.Name()
	defer os.Remove(temporaryName)
	if err := temporary.Chmod(0o600); err != nil {
		temporary.Close()
		return fmt.Errorf("restrict visual log status staging file: %w", err)
	}
	if _, err := temporary.Write(data); err != nil {
		temporary.Close()
		return fmt.Errorf("write visual log status staging file: %w", err)
	}
	if err := temporary.Sync(); err != nil {
		temporary.Close()
		return fmt.Errorf("sync visual log status staging file: %w", err)
	}
	if err := temporary.Close(); err != nil {
		return fmt.Errorf("close visual log status staging file: %w", err)
	}
	if err := os.Rename(temporaryName, name); err != nil {
		return fmt.Errorf("replace visual log status: %w", err)
	}
	return nil
}
