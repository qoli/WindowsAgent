package releasecatalog

import (
	"debug/pe"
	"fmt"
)

func ReadPESubsystem(path string) (string, error) {
	file, err := pe.Open(path)
	if err != nil {
		return "", fmt.Errorf("open PE file: %w", err)
	}
	defer file.Close()
	return classifyPE(file)
}

func classifyPE(file *pe.File) (string, error) {
	if file.FileHeader.Machine != pe.IMAGE_FILE_MACHINE_AMD64 {
		return "", fmt.Errorf("PE machine is 0x%04x, expected amd64", file.FileHeader.Machine)
	}
	var subsystem uint16
	switch header := file.OptionalHeader.(type) {
	case *pe.OptionalHeader64:
		subsystem = header.Subsystem
	default:
		return "", fmt.Errorf("unsupported PE optional header %T", file.OptionalHeader)
	}
	switch subsystem {
	case 2:
		return SubsystemGUI, nil
	case 3:
		return SubsystemConsole, nil
	default:
		return "", fmt.Errorf("unsupported PE subsystem %d", subsystem)
	}
}
