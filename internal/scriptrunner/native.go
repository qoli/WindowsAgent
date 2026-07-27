package scriptrunner

import (
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"unsafe"

	"github.com/qoli/WindowsAgent/internal/scriptpackage"
	"go.starlark.net/starlark"
	"go.starlark.net/starlarkstruct"
)

const nativePlatform = "windows-amd64"

type nativeError struct {
	code  string
	stage string
	cause error
}

func (e *nativeError) Error() string {
	return fmt.Sprintf("%s at %s: %v", e.code, e.stage, e.cause)
}

func (e *nativeError) Unwrap() error { return e.cause }

type nativeBackend interface {
	load(path string) (nativeDLL, error)
}

type nativeDLL interface {
	bind(name string) (nativeProcedure, error)
	close() error
}

type nativeProcedure interface {
	call(frame nativeCallFrame) (uintptr, error)
}

type nativeCallFrame struct {
	arguments []uintptr
	buffers   map[int][]byte
}

type nativeHost struct {
	ctx       context.Context
	pkg       *scriptpackage.Package
	broker    Broker
	backend   nativeBackend
	libraries map[string]*nativeLibraryValue
}

func newNativeHost(ctx context.Context, pkg *scriptpackage.Package, broker Broker, backend nativeBackend) *nativeHost {
	return &nativeHost{
		ctx: ctx, pkg: pkg, broker: broker, backend: backend,
		libraries: map[string]*nativeLibraryValue{},
	}
}

func (h *nativeHost) module() starlark.Value {
	fields := starlark.StringDict{
		"load_library": starlark.NewBuiltin("native.load_library", h.loadBuiltin),
		"blob_path":    starlark.NewBuiltin("native.blob_path", h.blobPathBuiltin),
		"void":         typeConstructor("native.void", nativeVoid),
		"i32":          typeConstructor("native.i32", nativeI32),
		"u32":          typeConstructor("native.u32", nativeU32),
		"u64":          typeConstructor("native.u64", nativeU64),
		"usize":        typeConstructor("native.usize", nativeUSize),
		"pointer":      typeConstructor("native.pointer", nativePointer),
		"handle":       typeConstructor("native.handle", nativeHandle),
		"c_string":     typeConstructor("native.c_string", nativeCString),
		"out":          starlark.NewBuiltin("native.out", outTypeBuiltin),
		"struct":       starlark.NewBuiltin("native.struct", structTypeBuiltin),
		"array":        starlark.NewBuiltin("native.array", arrayTypeBuiltin),
		"null":         starlark.NewBuiltin("native.null", nullBuiltin),
	}
	return starlarkstruct.FromStringDict(starlark.String("native"), fields)
}

func (h *nativeHost) close() {
	for _, library := range h.libraries {
		_ = library.dll.close()
	}
}

func (h *nativeHost) loadBuiltin(_ *starlark.Thread, _ *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	var alias string
	if err := starlark.UnpackArgs("native.load_library", args, kwargs, "alias", &alias); err != nil {
		return nil, err
	}
	declaration, declared := h.pkg.Manifest.NativeLibraries[alias]
	if !declared {
		return nil, &nativeError{code: "NATIVE_LIBRARY_NOT_DECLARED", stage: "loading-library", cause: fmt.Errorf("native library alias %q is absent from the verified manifest", alias)}
	}
	if declaration.Platform != nativePlatform {
		return nil, &nativeError{code: "NATIVE_PLATFORM_MISMATCH", stage: "loading-library", cause: fmt.Errorf("native library %q requires %s", alias, declaration.Platform)}
	}
	if existing := h.libraries[alias]; existing != nil {
		return existing, nil
	}
	path := filepath.Join(h.pkg.Root, filepath.FromSlash(declaration.Artifact))
	if err := verifyNativeArtifact(path, declaration.SHA256); err != nil {
		record := NativeRecord{Alias: alias, Action: "load", Phase: "failed", ErrorKind: "NATIVE_LIBRARY_DIGEST_MISMATCH"}
		_ = h.broker.RecordNative(h.ctx, record)
		return nil, &nativeError{code: record.ErrorKind, stage: "verifying-library", cause: err}
	}
	dll, err := h.backend.load(path)
	if err != nil {
		record := NativeRecord{Alias: alias, Action: "load", Phase: "failed", ErrorKind: "NATIVE_LIBRARY_LOAD_FAILED"}
		_ = h.broker.RecordNative(h.ctx, record)
		return nil, &nativeError{code: record.ErrorKind, stage: "loading-library", cause: err}
	}
	library := &nativeLibraryValue{
		host: h, alias: alias, declaration: declaration, dll: dll,
	}
	h.libraries[alias] = library
	if err := h.broker.RecordNative(h.ctx, NativeRecord{Alias: alias, Action: "load", Phase: "completed"}); err != nil {
		_ = dll.close()
		delete(h.libraries, alias)
		return nil, &nativeError{code: "NATIVE_PROVENANCE_FAILED", stage: "recording-library-load", cause: err}
	}
	return library, nil
}

func verifyNativeArtifact(path, expectedSHA256 string) error {
	info, err := os.Stat(path)
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() {
		return errors.New("native library artifact is not a regular file")
	}
	file, err := os.Open(path)
	if err != nil {
		return err
	}
	defer file.Close()
	hash := sha256.New()
	if _, err := io.Copy(hash, file); err != nil {
		return err
	}
	if hex.EncodeToString(hash.Sum(nil)) != expectedSHA256 {
		return errors.New("native library artifact SHA-256 changed after package verification")
	}
	return nil
}

func (h *nativeHost) blobPathBuiltin(_ *starlark.Thread, _ *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	var reference starlark.Value
	if err := starlark.UnpackArgs("native.blob_path", args, kwargs, "blob", &reference); err != nil {
		return nil, err
	}
	value, err := fromStarlark(reference)
	if err != nil {
		return nil, &nativeError{code: "NATIVE_BLOB_INVALID", stage: "resolving-blob", cause: err}
	}
	object, ok := value.(map[string]any)
	if !ok {
		return nil, &nativeError{code: "NATIVE_BLOB_INVALID", stage: "resolving-blob", cause: errors.New("blob must be an opaque object")}
	}
	path, err := h.broker.BlobPath(h.ctx, object)
	if err != nil {
		return nil, &nativeError{code: "NATIVE_BLOB_INVALID", stage: "resolving-blob", cause: err}
	}
	return starlark.String(path), nil
}

type nativeKind string

const (
	nativeVoid    nativeKind = "void"
	nativeI32     nativeKind = "i32"
	nativeU32     nativeKind = "u32"
	nativeU64     nativeKind = "u64"
	nativeUSize   nativeKind = "usize"
	nativePointer nativeKind = "pointer"
	nativeHandle  nativeKind = "handle"
	nativeCString nativeKind = "c_string"
	nativeOut     nativeKind = "out"
	nativeStruct  nativeKind = "struct"
	nativeArray   nativeKind = "array"
)

type nativeField struct {
	name   string
	typ    *nativeType
	offset uint64
}

type nativeType struct {
	kind   nativeKind
	elem   *nativeType
	count  uint64
	fields []nativeField
	size   uint64
	align  uint64
}

func scalarType(kind nativeKind) *nativeType {
	switch kind {
	case nativeVoid:
		return &nativeType{kind: kind, size: 0, align: 1}
	case nativeI32, nativeU32:
		return &nativeType{kind: kind, size: 4, align: 4}
	default:
		return &nativeType{kind: kind, size: 8, align: 8}
	}
}

func typeConstructor(name string, kind nativeKind) *starlark.Builtin {
	return starlark.NewBuiltin(name, func(_ *starlark.Thread, _ *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
		if len(args) != 0 || len(kwargs) != 0 {
			return nil, fmt.Errorf("%s accepts no arguments", name)
		}
		return scalarType(kind), nil
	})
}

func outTypeBuiltin(_ *starlark.Thread, _ *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	var value starlark.Value
	if err := starlark.UnpackArgs("native.out", args, kwargs, "type", &value); err != nil {
		return nil, err
	}
	typ, ok := value.(*nativeType)
	if !ok || typ.kind == nativeVoid || typ.kind == nativeOut || typ.kind == nativeCString {
		return nil, errors.New("native.out requires a concrete non-string type")
	}
	return &nativeType{kind: nativeOut, elem: typ, size: 8, align: 8}, nil
}

func arrayTypeBuiltin(_ *starlark.Thread, _ *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	var value starlark.Value
	var count int
	if err := starlark.UnpackArgs("native.array", args, kwargs, "type", &value, "count", &count); err != nil {
		return nil, err
	}
	elem, ok := value.(*nativeType)
	if !ok || elem.kind == nativeVoid || elem.kind == nativeOut || count < 0 || count > 100000 {
		return nil, errors.New("native.array requires a concrete type and count from 0 through 100000")
	}
	size, ok := checkedMultiply(elem.size, uint64(count))
	if !ok {
		return nil, errors.New("native.array size overflows")
	}
	return &nativeType{kind: nativeArray, elem: elem, count: uint64(count), size: size, align: elem.align}, nil
}

func structTypeBuiltin(_ *starlark.Thread, _ *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	var value starlark.Value
	if err := starlark.UnpackArgs("native.struct", args, kwargs, "fields", &value); err != nil {
		return nil, err
	}
	list, ok := value.(*starlark.List)
	if !ok || list.Len() == 0 || list.Len() > 128 {
		return nil, errors.New("native.struct fields must be a non-empty bounded list")
	}
	fields := make([]nativeField, 0, list.Len())
	var offset, alignment uint64 = 0, 1
	iterator := list.Iterate()
	defer iterator.Done()
	var item starlark.Value
	seen := map[string]struct{}{}
	for iterator.Next(&item) {
		dict, ok := item.(*starlark.Dict)
		if !ok || dict.Len() != 2 {
			return nil, errors.New("native.struct field must contain name and type")
		}
		nameValue, found, _ := dict.Get(starlark.String("name"))
		typeValue, typeFound, _ := dict.Get(starlark.String("type"))
		name, nameOK := starlark.AsString(nameValue)
		typ, typeOK := typeValue.(*nativeType)
		if !found || !typeFound || !nameOK || name == "" || !typeOK ||
			typ.kind == nativeVoid || typ.kind == nativeOut || typ.kind == nativeCString {
			return nil, errors.New("native.struct field name or type is invalid")
		}
		if _, duplicate := seen[name]; duplicate {
			return nil, fmt.Errorf("duplicate native.struct field %q", name)
		}
		seen[name] = struct{}{}
		offset = alignUp(offset, typ.align)
		fields = append(fields, nativeField{name: name, typ: typ, offset: offset})
		offset += typ.size
		alignment = max(alignment, typ.align)
	}
	return &nativeType{kind: nativeStruct, fields: fields, size: alignUp(offset, alignment), align: alignment}, nil
}

func nullBuiltin(_ *starlark.Thread, _ *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	if len(args) != 0 || len(kwargs) != 0 {
		return nil, errors.New("native.null accepts no arguments")
	}
	return nativeNullValue{}, nil
}

func (t *nativeType) String() string        { return "native." + string(t.kind) }
func (t *nativeType) Type() string          { return "native_type" }
func (t *nativeType) Freeze()               {}
func (t *nativeType) Truth() starlark.Bool  { return starlark.True }
func (t *nativeType) Hash() (uint32, error) { return 0, errors.New("native types are unhashable") }

type nativeNullValue struct{}

func (nativeNullValue) String() string        { return "native.null()" }
func (nativeNullValue) Type() string          { return "native_null" }
func (nativeNullValue) Freeze()               {}
func (nativeNullValue) Truth() starlark.Bool  { return starlark.False }
func (nativeNullValue) Hash() (uint32, error) { return 0, errors.New("native null is unhashable") }

type nativeLibraryValue struct {
	host        *nativeHost
	alias       string
	declaration scriptpackage.NativeLibrary
	dll         nativeDLL
	callsUsed   uint32
	memoryUsed  uint64
}

func (l *nativeLibraryValue) String() string       { return "native_library(" + l.alias + ")" }
func (l *nativeLibraryValue) Type() string         { return "native_library" }
func (l *nativeLibraryValue) Freeze()              {}
func (l *nativeLibraryValue) Truth() starlark.Bool { return starlark.True }
func (l *nativeLibraryValue) Hash() (uint32, error) {
	return 0, errors.New("native libraries are unhashable")
}
func (l *nativeLibraryValue) AttrNames() []string { return []string{"bind"} }
func (l *nativeLibraryValue) Attr(name string) (starlark.Value, error) {
	if name == "bind" {
		return starlark.NewBuiltin("native_library.bind", l.bindBuiltin), nil
	}
	return nil, nil
}

func (l *nativeLibraryValue) bindBuiltin(_ *starlark.Thread, _ *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	var name string
	var parametersValue, resultValue starlark.Value
	if err := starlark.UnpackArgs("library.bind", args, kwargs, "name", &name, "parameters", &parametersValue, "result", &resultValue); err != nil {
		return nil, err
	}
	if name == "" || strings.IndexByte(name, 0) >= 0 {
		return nil, &nativeError{code: "NATIVE_SIGNATURE_INVALID", stage: "binding-function", cause: errors.New("function name is invalid")}
	}
	parametersList, ok := parametersValue.(*starlark.List)
	if !ok || parametersList.Len() > 64 {
		return nil, &nativeError{code: "NATIVE_SIGNATURE_INVALID", stage: "binding-function", cause: errors.New("parameters must be a bounded list")}
	}
	parameters := make([]*nativeType, 0, parametersList.Len())
	iterator := parametersList.Iterate()
	defer iterator.Done()
	var item starlark.Value
	for iterator.Next(&item) {
		typ, ok := item.(*nativeType)
		if !ok || typ.kind == nativeVoid {
			return nil, &nativeError{code: "NATIVE_SIGNATURE_INVALID", stage: "binding-function", cause: errors.New("parameter is not a native type")}
		}
		parameters = append(parameters, typ)
	}
	result, ok := resultValue.(*nativeType)
	if !ok || result.kind == nativeOut || result.kind == nativeStruct || result.kind == nativeArray || result.kind == nativeCString {
		return nil, &nativeError{code: "NATIVE_SIGNATURE_INVALID", stage: "binding-function", cause: errors.New("result type must be void or a scalar")}
	}
	procedure, err := l.dll.bind(name)
	if err != nil {
		record := NativeRecord{Alias: l.alias, Action: "bind", Function: name, Phase: "failed", ErrorKind: "NATIVE_EXPORT_NOT_FOUND", CallsUsed: l.callsUsed, NativeMemoryBytes: l.memoryUsed}
		_ = l.host.broker.RecordNative(l.host.ctx, record)
		return nil, &nativeError{code: record.ErrorKind, stage: "binding-function", cause: err}
	}
	if err := l.host.broker.RecordNative(l.host.ctx, NativeRecord{Alias: l.alias, Action: "bind", Function: name, Phase: "completed", CallsUsed: l.callsUsed, NativeMemoryBytes: l.memoryUsed}); err != nil {
		return nil, &nativeError{code: "NATIVE_PROVENANCE_FAILED", stage: "recording-function-bind", cause: err}
	}
	return &nativeFunctionValue{library: l, name: name, parameters: parameters, result: result, procedure: procedure}, nil
}

type nativeFunctionValue struct {
	library    *nativeLibraryValue
	name       string
	parameters []*nativeType
	result     *nativeType
	procedure  nativeProcedure
}

func (f *nativeFunctionValue) String() string       { return "native_function(" + f.name + ")" }
func (f *nativeFunctionValue) Type() string         { return "native_function" }
func (f *nativeFunctionValue) Freeze()              {}
func (f *nativeFunctionValue) Truth() starlark.Bool { return starlark.True }
func (f *nativeFunctionValue) Hash() (uint32, error) {
	return 0, errors.New("native functions are unhashable")
}
func (f *nativeFunctionValue) AttrNames() []string { return []string{"call"} }
func (f *nativeFunctionValue) Attr(name string) (starlark.Value, error) {
	if name == "call" {
		return starlark.NewBuiltin("native_function.call", f.callBuiltin), nil
	}
	return nil, nil
}

func (f *nativeFunctionValue) callBuiltin(_ *starlark.Thread, _ *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	if len(kwargs) != 0 {
		return nil, errors.New("function.call accepts positional arguments only")
	}
	if err := f.library.host.ctx.Err(); err != nil {
		return nil, &nativeError{code: "NATIVE_DEADLINE_EXCEEDED", stage: "calling-function", cause: err}
	}
	if f.library.callsUsed >= f.library.declaration.MaxCalls {
		return nil, &nativeError{code: "NATIVE_CALL_LIMIT_EXCEEDED", stage: "calling-function", cause: errors.New("native call limit exhausted")}
	}
	expectedInputs := 0
	for _, parameter := range f.parameters {
		if parameter.kind != nativeOut {
			expectedInputs++
		}
	}
	if len(args) != expectedInputs {
		return nil, &nativeError{code: "NATIVE_SIGNATURE_INVALID", stage: "calling-function", cause: fmt.Errorf("got %d arguments, want %d", len(args), expectedInputs)}
	}
	var (
		callArguments = make([]uintptr, 0, len(f.parameters))
		callBuffers   = map[int][]byte{}
		allocations   [][]byte
		outputBuffers [][]byte
		outputs       []*nativeType
		inputIndex    int
		callMemory    uint64
	)
	for _, parameter := range f.parameters {
		if parameter.kind == nativeOut {
			buffer := make([]byte, parameter.elem.size)
			callMemory += uint64(len(buffer))
			allocations = append(allocations, buffer)
			outputBuffers = append(outputBuffers, buffer)
			outputs = append(outputs, parameter.elem)
			callBuffers[len(callArguments)] = buffer
			callArguments = append(callArguments, bufferPointer(buffer))
			continue
		}
		argument, buffers, memory, err := marshalArgument(parameter, args[inputIndex])
		if err != nil {
			return nil, &nativeError{code: "NATIVE_ARGUMENT_INVALID", stage: "marshalling-call", cause: fmt.Errorf("argument %d: %w", inputIndex, err)}
		}
		inputIndex++
		callArguments = append(callArguments, argument)
		if len(buffers) == 1 {
			callBuffers[len(callArguments)-1] = buffers[0]
		}
		allocations = append(allocations, buffers...)
		callMemory += memory
	}
	if callMemory > f.library.declaration.MaxNativeMemoryBytes-f.library.memoryUsed {
		return nil, &nativeError{code: "NATIVE_MEMORY_LIMIT_EXCEEDED", stage: "marshalling-call", cause: errors.New("native memory budget exceeded")}
	}
	f.library.callsUsed++
	f.library.memoryUsed += callMemory
	start := NativeRecord{
		Alias: f.library.alias, Action: "call", Function: f.name, Phase: "started",
		CallsUsed: f.library.callsUsed, NativeMemoryBytes: f.library.memoryUsed,
	}
	if err := f.library.host.broker.RecordNative(f.library.host.ctx, start); err != nil {
		return nil, &nativeError{code: "NATIVE_PROVENANCE_FAILED", stage: "recording-call-start", cause: err}
	}
	rawResult, err := f.procedure.call(nativeCallFrame{arguments: callArguments, buffers: callBuffers})
	runtime.KeepAlive(allocations)
	if err != nil {
		start.Phase = "failed"
		start.ErrorKind = "NATIVE_CALL_FAILED"
		_ = f.library.host.broker.RecordNative(f.library.host.ctx, start)
		return nil, &nativeError{code: start.ErrorKind, stage: "calling-function", cause: err}
	}
	if err := f.library.host.ctx.Err(); err != nil {
		return nil, &nativeError{code: "NATIVE_DEADLINE_EXCEEDED", stage: "calling-function", cause: err}
	}
	resultValue, err := decodeScalar(f.result, uint64(rawResult))
	if err != nil {
		return nil, &nativeError{code: "NATIVE_RESULT_INVALID", stage: "decoding-call", cause: err}
	}
	outValues := make([]starlark.Value, len(outputs))
	for outputIndex, output := range outputs {
		value, err := decodeBuffer(output, outputBuffers[outputIndex])
		if err != nil {
			return nil, &nativeError{code: "NATIVE_RESULT_INVALID", stage: "decoding-call", cause: err}
		}
		outValues[outputIndex] = value
	}
	result := starlark.NewDict(2)
	_ = result.SetKey(starlark.String("result"), resultValue)
	_ = result.SetKey(starlark.String("out"), starlark.NewList(outValues))
	nativeResult, err := fromStarlark(result)
	if err != nil {
		return nil, &nativeError{code: "NATIVE_RESULT_INVALID", stage: "decoding-call", cause: err}
	}
	encoded, err := json.Marshal(nativeResult)
	if err != nil {
		return nil, &nativeError{code: "NATIVE_RESULT_INVALID", stage: "decoding-call", cause: err}
	}
	if uint64(len(encoded)) > f.library.host.pkg.Manifest.Limits.MaxResultBytes {
		return nil, &nativeError{code: "NATIVE_RESULT_LIMIT_EXCEEDED", stage: "decoding-call", cause: errors.New("native result exceeds Script Package result limit")}
	}
	start.Phase = "completed"
	start.ResultBytes = uint64(len(encoded))
	if err := f.library.host.broker.RecordNative(f.library.host.ctx, start); err != nil {
		return nil, &nativeError{code: "NATIVE_PROVENANCE_FAILED", stage: "recording-call-completion", cause: err}
	}
	return result, nil
}

func marshalArgument(typ *nativeType, value starlark.Value) (uintptr, [][]byte, uint64, error) {
	if _, ok := value.(nativeNullValue); ok {
		if typ.kind != nativePointer && typ.kind != nativeHandle && typ.kind != nativeCString {
			return 0, nil, 0, errors.New("native.null is only valid for pointer-like parameters")
		}
		return 0, nil, 0, nil
	}
	switch typ.kind {
	case nativeI32:
		integer, err := signedInteger(value, math.MinInt32, math.MaxInt32)
		return uintptr(uint32(int32(integer))), nil, 0, err
	case nativeU32:
		integer, err := unsignedInteger(value, math.MaxUint32)
		return uintptr(integer), nil, 0, err
	case nativeU64, nativeUSize, nativePointer, nativeHandle:
		integer, err := unsignedInteger(value, math.MaxUint64)
		return uintptr(integer), nil, 0, err
	case nativeCString:
		text, ok := starlark.AsString(value)
		if !ok || strings.IndexByte(text, 0) >= 0 {
			return 0, nil, 0, errors.New("c_string requires a NUL-free string")
		}
		buffer := append([]byte(text), 0)
		return bufferPointer(buffer), [][]byte{buffer}, uint64(len(buffer)), nil
	case nativeStruct, nativeArray:
		buffer := make([]byte, typ.size)
		if err := encodeBuffer(typ, value, buffer); err != nil {
			return 0, nil, 0, err
		}
		return bufferPointer(buffer), [][]byte{buffer}, uint64(len(buffer)), nil
	default:
		return 0, nil, 0, fmt.Errorf("unsupported parameter type %s", typ.kind)
	}
}

func encodeBuffer(typ *nativeType, value starlark.Value, buffer []byte) error {
	switch typ.kind {
	case nativeStruct:
		dict, ok := value.(*starlark.Dict)
		if !ok {
			return errors.New("struct input requires a dict")
		}
		for _, field := range typ.fields {
			fieldValue, found, err := dict.Get(starlark.String(field.name))
			if err != nil || !found {
				return fmt.Errorf("struct field %q is required", field.name)
			}
			if err := encodeScalar(field.typ, fieldValue, buffer[field.offset:]); err != nil {
				return fmt.Errorf("struct field %q: %w", field.name, err)
			}
		}
		return nil
	case nativeArray:
		list, ok := value.(*starlark.List)
		if !ok || uint64(list.Len()) != typ.count {
			return errors.New("array input length does not match type")
		}
		iterator := list.Iterate()
		defer iterator.Done()
		var item starlark.Value
		var index uint64
		for iterator.Next(&item) {
			start := index * typ.elem.size
			if typ.elem.kind == nativeStruct || typ.elem.kind == nativeArray {
				if err := encodeBuffer(typ.elem, item, buffer[start:start+typ.elem.size]); err != nil {
					return err
				}
			} else if err := encodeScalar(typ.elem, item, buffer[start:]); err != nil {
				return err
			}
			index++
		}
		return nil
	default:
		return errors.New("buffer input requires struct or array")
	}
}

func encodeScalar(typ *nativeType, value starlark.Value, buffer []byte) error {
	switch typ.kind {
	case nativeI32:
		integer, err := signedInteger(value, math.MinInt32, math.MaxInt32)
		if err == nil {
			binary.LittleEndian.PutUint32(buffer, uint32(int32(integer)))
		}
		return err
	case nativeU32:
		integer, err := unsignedInteger(value, math.MaxUint32)
		if err == nil {
			binary.LittleEndian.PutUint32(buffer, uint32(integer))
		}
		return err
	case nativeU64, nativeUSize, nativePointer, nativeHandle:
		integer, err := unsignedInteger(value, math.MaxUint64)
		if err == nil {
			binary.LittleEndian.PutUint64(buffer, integer)
		}
		return err
	default:
		return fmt.Errorf("unsupported scalar %s", typ.kind)
	}
}

func decodeBuffer(typ *nativeType, buffer []byte) (starlark.Value, error) {
	switch typ.kind {
	case nativeStruct:
		dict := starlark.NewDict(len(typ.fields))
		for _, field := range typ.fields {
			value, err := decodeBuffer(field.typ, buffer[field.offset:field.offset+field.typ.size])
			if err != nil {
				return nil, err
			}
			_ = dict.SetKey(starlark.String(field.name), value)
		}
		return dict, nil
	case nativeArray:
		values := make([]starlark.Value, typ.count)
		for index := uint64(0); index < typ.count; index++ {
			start := index * typ.elem.size
			value, err := decodeBuffer(typ.elem, buffer[start:start+typ.elem.size])
			if err != nil {
				return nil, err
			}
			values[index] = value
		}
		return starlark.NewList(values), nil
	default:
		if len(buffer) < int(typ.size) {
			return nil, errors.New("native output buffer is truncated")
		}
		var raw uint64
		if typ.size == 4 {
			raw = uint64(binary.LittleEndian.Uint32(buffer))
		} else {
			raw = binary.LittleEndian.Uint64(buffer)
		}
		return decodeScalar(typ, raw)
	}
}

func decodeScalar(typ *nativeType, raw uint64) (starlark.Value, error) {
	switch typ.kind {
	case nativeVoid:
		return starlark.None, nil
	case nativeI32:
		return starlark.MakeInt64(int64(int32(raw))), nil
	case nativeU32:
		return starlark.MakeUint64(uint64(uint32(raw))), nil
	case nativeU64, nativeUSize, nativePointer, nativeHandle:
		return starlark.MakeUint64(raw), nil
	default:
		return nil, fmt.Errorf("unsupported scalar result %s", typ.kind)
	}
}

func signedInteger(value starlark.Value, minimum, maximum int64) (int64, error) {
	integer, ok := value.(starlark.Int)
	if !ok {
		return 0, errors.New("integer is required")
	}
	result, ok := integer.Int64()
	if !ok || result < minimum || result > maximum {
		return 0, errors.New("integer is outside type range")
	}
	return result, nil
}

func unsignedInteger(value starlark.Value, maximum uint64) (uint64, error) {
	integer, ok := value.(starlark.Int)
	if !ok {
		return 0, errors.New("integer is required")
	}
	result, ok := integer.Uint64()
	if !ok || result > maximum {
		return 0, errors.New("integer is outside type range")
	}
	return result, nil
}

func bufferPointer(buffer []byte) uintptr {
	if len(buffer) == 0 {
		return 0
	}
	return uintptr(unsafe.Pointer(&buffer[0]))
}

func alignUp(value, alignment uint64) uint64 {
	return (value + alignment - 1) &^ (alignment - 1)
}

func checkedMultiply(left, right uint64) (uint64, bool) {
	if right != 0 && left > math.MaxUint64/right {
		return 0, false
	}
	return left * right, true
}
