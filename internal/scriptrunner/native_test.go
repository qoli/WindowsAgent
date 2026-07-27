package scriptrunner

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/qoli/WindowsAgent/internal/scriptpackage"
	"go.starlark.net/starlark"
)

func TestNativeArtifactValidationRequiresRegularFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "fixture.dll")
	if err := validateNativeArtifact(path); err == nil {
		t.Fatal("missing native artifact was accepted")
	}
	content := []byte("verified DLL fixture")
	if err := os.WriteFile(path, content, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := validateNativeArtifact(path); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("changed"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := validateNativeArtifact(path); err != nil {
		t.Fatalf("locally changed native artifact was rejected: %v", err)
	}
}

func loadInventoryFixturePackage(t *testing.T) *scriptpackage.Package {
	t.Helper()
	root, err := filepath.Abs(filepath.Join("..", "..", "Rules", "CrimsonDesert.exe", "Scripts", "inventory"))
	if err != nil {
		t.Fatal(err)
	}
	pkg, err := scriptpackage.Load(root, "crimson-desert/inventory")
	if err != nil {
		t.Fatal(err)
	}
	return pkg
}

func TestNativeLoadRequiresDeclaredAlias(t *testing.T) {
	host := newNativeHost(context.Background(), loadInventoryFixturePackage(t), &fixtureBroker{}, fixtureNativeBackend{})
	_, err := host.loadBuiltin(nil, nil, starlark.Tuple{starlark.String("undeclared")}, nil)
	var typed *nativeError
	if !errors.As(err, &typed) || typed.code != "NATIVE_LIBRARY_NOT_DECLARED" {
		t.Fatalf("error = %#v", err)
	}
}

func TestNativeLoadRejectsPlatformMismatch(t *testing.T) {
	original := loadInventoryFixturePackage(t)
	cloned := *original
	cloned.Manifest = original.Manifest
	cloned.Manifest.NativeLibraries = make(map[string]scriptpackage.NativeLibrary, len(original.Manifest.NativeLibraries))
	for alias, library := range original.Manifest.NativeLibraries {
		cloned.Manifest.NativeLibraries[alias] = library
	}
	library := cloned.Manifest.NativeLibraries["save-decoder"]
	library.Platform = "windows-arm64"
	cloned.Manifest.NativeLibraries["save-decoder"] = library
	host := newNativeHost(context.Background(), &cloned, &fixtureBroker{}, fixtureNativeBackend{})
	_, err := host.loadBuiltin(nil, nil, starlark.Tuple{starlark.String("save-decoder")}, nil)
	var typed *nativeError
	if !errors.As(err, &typed) || typed.code != "NATIVE_PLATFORM_MISMATCH" {
		t.Fatalf("error = %#v", err)
	}
}

func TestNativeBindMissingExportFailsExplicitly(t *testing.T) {
	host := newNativeHost(context.Background(), loadInventoryFixturePackage(t), &fixtureBroker{}, fixtureNativeBackend{})
	value, err := host.loadBuiltin(nil, nil, starlark.Tuple{starlark.String("save-decoder")}, nil)
	if err != nil {
		t.Fatal(err)
	}
	library := value.(*nativeLibraryValue)
	_, err = library.bindBuiltin(nil, nil, nil, []starlark.Tuple{
		{starlark.String("name"), starlark.String("missing_export")},
		{starlark.String("parameters"), starlark.NewList(nil)},
		{starlark.String("result"), scalarType(nativeVoid)},
	})
	var typed *nativeError
	if !errors.As(err, &typed) || typed.code != "NATIVE_EXPORT_NOT_FOUND" {
		t.Fatalf("error = %#v", err)
	}
}

func TestNativeScalarSizesAndStructAlignment(t *testing.T) {
	for _, test := range []struct {
		kind  nativeKind
		size  uint64
		align uint64
	}{
		{nativeI32, 4, 4}, {nativeU32, 4, 4}, {nativeU64, 8, 8},
		{nativeUSize, 8, 8}, {nativePointer, 8, 8}, {nativeHandle, 8, 8},
	} {
		typ := scalarType(test.kind)
		if typ.size != test.size || typ.align != test.align {
			t.Fatalf("%s size/alignment = %d/%d, want %d/%d", test.kind, typ.size, typ.align, test.size, test.align)
		}
	}
	fields := starlark.NewList([]starlark.Value{
		fieldFixture("first", scalarType(nativeU32)),
		fieldFixture("wide", scalarType(nativeU64)),
		fieldFixture("last", scalarType(nativeU32)),
	})
	value, err := structTypeBuiltin(nil, nil, nil, []starlark.Tuple{
		{starlark.String("fields"), fields},
	})
	if err != nil {
		t.Fatal(err)
	}
	typ := value.(*nativeType)
	if typ.size != 24 || typ.align != 8 ||
		typ.fields[0].offset != 0 || typ.fields[1].offset != 8 || typ.fields[2].offset != 16 {
		t.Fatalf("struct layout = size %d align %d fields %#v", typ.size, typ.align, typ.fields)
	}

	recordFields := make([]starlark.Value, 0, 10)
	for index := 0; index < 8; index++ {
		recordFields = append(recordFields, fieldFixture(fmt.Sprintf("u32_%d", index), scalarType(nativeU32)))
	}
	recordFields = append(recordFields,
		fieldFixture("u64_0", scalarType(nativeU64)),
		fieldFixture("u64_1", scalarType(nativeU64)),
	)
	value, err = structTypeBuiltin(nil, nil, nil, []starlark.Tuple{
		{starlark.String("fields"), starlark.NewList(recordFields)},
	})
	if err != nil {
		t.Fatal(err)
	}
	record := value.(*nativeType)
	if record.size != 48 || record.align != 8 {
		t.Fatalf("8xu32+2xu64 layout = %d/%d, want 48/8", record.size, record.align)
	}
}

func TestNativeCallAndMemoryLimitsAreExplicit(t *testing.T) {
	broker := &fixtureBroker{}
	host := newNativeHost(context.Background(), loadInventoryFixturePackage(t), broker, fixtureNativeBackend{})
	library := &nativeLibraryValue{
		host: host, alias: "fixture",
		declaration: scriptpackage.NativeLibrary{
			MaxCalls: 1, MaxNativeMemoryBytes: 8,
		},
		dll: fixtureNativeDLL{},
	}
	function := &nativeFunctionValue{
		library: library, name: "crimson_save_free",
		result: scalarType(nativeVoid), procedure: fixtureNativeProcedure{name: "crimson_save_free"},
	}
	if _, err := function.callBuiltin(nil, nil, nil, nil); err != nil {
		t.Fatal(err)
	}
	_, err := function.callBuiltin(nil, nil, nil, nil)
	var typed *nativeError
	if !errors.As(err, &typed) || typed.code != "NATIVE_CALL_LIMIT_EXCEEDED" {
		t.Fatalf("call limit error = %#v", err)
	}

	library.callsUsed = 0
	function.parameters = []*nativeType{{kind: nativeOut, elem: &nativeType{kind: nativeArray, elem: scalarType(nativeU64), count: 2, size: 16, align: 8}}}
	_, err = function.callBuiltin(nil, nil, nil, nil)
	if !errors.As(err, &typed) || typed.code != "NATIVE_MEMORY_LIMIT_EXCEEDED" {
		t.Fatalf("memory limit error = %#v", err)
	}
}

func TestNativeResultLimitIsExplicit(t *testing.T) {
	pkg := loadInventoryFixturePackage(t)
	cloned := *pkg
	cloned.Manifest = pkg.Manifest
	cloned.Manifest.Limits.MaxResultBytes = 1
	host := newNativeHost(context.Background(), &cloned, &fixtureBroker{}, fixtureNativeBackend{})
	library := &nativeLibraryValue{
		host: host, alias: "fixture",
		declaration: scriptpackage.NativeLibrary{
			MaxCalls: 1, MaxNativeMemoryBytes: 8,
		},
		dll: fixtureNativeDLL{},
	}
	function := &nativeFunctionValue{
		library: library, name: "crimson_save_free",
		result: scalarType(nativeVoid), procedure: fixtureNativeProcedure{name: "crimson_save_free"},
	}
	_, err := function.callBuiltin(nil, nil, nil, nil)
	var typed *nativeError
	if !errors.As(err, &typed) || typed.code != "NATIVE_RESULT_LIMIT_EXCEEDED" {
		t.Fatalf("error = %#v", err)
	}
}

func fieldFixture(name string, typ *nativeType) *starlark.Dict {
	field := starlark.NewDict(2)
	_ = field.SetKey(starlark.String("name"), starlark.String(name))
	_ = field.SetKey(starlark.String("type"), typ)
	return field
}
