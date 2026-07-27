// Package foreground defines foreground-window process observations.
package foreground

import (
	"errors"
	"strings"
	"time"
)

type Info struct {
	ObservedAt     time.Time `json:"observed_at"`
	ProcessID      uint32    `json:"process_id"`
	ExecutableName string    `json:"executable_name"`
	ExecutablePath string    `json:"executable_path"`
	WindowTitle    string    `json:"window_title,omitempty"`
}

func (i Info) Validate() error {
	switch {
	case i.ObservedAt.IsZero():
		return errors.New("foreground observed_at is required")
	case i.ProcessID == 0:
		return errors.New("foreground process_id must be positive")
	case i.ExecutableName == "":
		return errors.New("foreground executable_name is required")
	case i.ExecutablePath == "":
		return errors.New("foreground executable_path is required")
	case executableName(i.ExecutablePath) != i.ExecutableName:
		return errors.New("foreground executable_name does not match executable_path")
	default:
		return nil
	}
}

func executableName(path string) string {
	if index := strings.LastIndexAny(path, `\/`); index >= 0 {
		return path[index+1:]
	}
	return path
}
