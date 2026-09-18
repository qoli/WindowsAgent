package releasecatalog

import (
	"debug/pe"
	"strings"
	"testing"
)

func TestClassifyPERequiresAMD64PE32Plus(t *testing.T) {
	tests := []struct {
		name string
		file *pe.File
		want string
	}{
		{name: "amd64 gui", file: &pe.File{FileHeader: pe.FileHeader{Machine: pe.IMAGE_FILE_MACHINE_AMD64}, OptionalHeader: &pe.OptionalHeader64{Subsystem: 2}}, want: SubsystemGUI},
		{name: "amd64 console", file: &pe.File{FileHeader: pe.FileHeader{Machine: pe.IMAGE_FILE_MACHINE_AMD64}, OptionalHeader: &pe.OptionalHeader64{Subsystem: 3}}, want: SubsystemConsole},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, err := classifyPE(test.file)
			if err != nil || got != test.want {
				t.Fatalf("classifyPE() = %q, %v; want %q", got, err, test.want)
			}
		})
	}
	if _, err := classifyPE(&pe.File{FileHeader: pe.FileHeader{Machine: pe.IMAGE_FILE_MACHINE_I386}, OptionalHeader: &pe.OptionalHeader32{Subsystem: 2}}); err == nil || !strings.Contains(err.Error(), "expected amd64") {
		t.Fatalf("wrong-machine error = %v", err)
	}
	if _, err := classifyPE(&pe.File{FileHeader: pe.FileHeader{Machine: pe.IMAGE_FILE_MACHINE_AMD64}, OptionalHeader: &pe.OptionalHeader32{Subsystem: 2}}); err == nil || !strings.Contains(err.Error(), "unsupported PE optional header") {
		t.Fatalf("PE32 error = %v", err)
	}
}
