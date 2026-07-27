//go:build windows

package observer

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"unsafe"

	"golang.org/x/sys/windows"
)

const (
	memPrivate = 0x00020000
	memMapped  = 0x00040000
	memImage   = 0x01000000
)

type MemoryBackend struct {
	handle          windows.Handle
	identity        ProcessIdentity
	maxBytesPerCall uint64
}

func NewMemoryBackend(expected ProcessIdentity, maxBytesPerCall uint64) (*MemoryBackend, error) {
	if expected.PID == 0 || expected.CreationTimeWindows == 0 ||
		expected.ImagePath == "" || expected.ImageSHA256 == "" || maxBytesPerCall == 0 {
		return nil, errors.New("complete expected process identity and maxBytesPerCall are required")
	}
	handle, err := windows.OpenProcess(windows.PROCESS_QUERY_LIMITED_INFORMATION|windows.PROCESS_VM_READ, false, expected.PID)
	if err != nil {
		return nil, fmt.Errorf("open target process: %w", err)
	}
	backend := &MemoryBackend{handle: handle, identity: expected, maxBytesPerCall: maxBytesPerCall}
	if err := backend.verifyIdentity(); err != nil {
		windows.CloseHandle(handle)
		return nil, err
	}
	return backend, nil
}

func ResolveProcessIdentity(pid uint32, expectedImagePath string) (ProcessIdentity, error) {
	if pid == 0 || expectedImagePath == "" || !filepath.IsAbs(expectedImagePath) {
		return ProcessIdentity{}, errors.New("PID and absolute expected image path are required")
	}
	handle, err := windows.OpenProcess(windows.PROCESS_QUERY_LIMITED_INFORMATION, false, pid)
	if err != nil {
		return ProcessIdentity{}, fmt.Errorf("open process for identity: %w", err)
	}
	defer windows.CloseHandle(handle)

	buffer := make([]uint16, 32768)
	size := uint32(len(buffer))
	if err := windows.QueryFullProcessImageName(handle, 0, &buffer[0], &size); err != nil {
		return ProcessIdentity{}, fmt.Errorf("query process image path: %w", err)
	}
	actualPath := windows.UTF16ToString(buffer[:size])
	if !strings.EqualFold(filepath.Clean(actualPath), filepath.Clean(expectedImagePath)) {
		return ProcessIdentity{}, errors.New("resolved process image path does not match foreground identity")
	}
	var creation, exit, kernel, user windows.Filetime
	if err := windows.GetProcessTimes(handle, &creation, &exit, &kernel, &user); err != nil {
		return ProcessIdentity{}, fmt.Errorf("query process creation time: %w", err)
	}
	file, err := os.Open(actualPath)
	if err != nil {
		return ProcessIdentity{}, fmt.Errorf("open process image for identity hash: %w", err)
	}
	defer file.Close()
	hash := sha256.New()
	if _, err := io.Copy(hash, file); err != nil {
		return ProcessIdentity{}, fmt.Errorf("hash process image: %w", err)
	}
	return ProcessIdentity{
		PID:                 pid,
		CreationTimeWindows: uint64(creation.HighDateTime)<<32 | uint64(creation.LowDateTime),
		ImagePath:           actualPath,
		ImageSHA256:         hex.EncodeToString(hash.Sum(nil)),
	}, nil
}

func (b *MemoryBackend) Close() error {
	return windows.CloseHandle(b.handle)
}

func (b *MemoryBackend) Call(ctx context.Context, namespace, operation string, arguments map[string]any) (BackendResult, error) {
	if namespace != "memory" {
		return BackendResult{}, fmt.Errorf("memory backend does not implement namespace %q", namespace)
	}
	if err := ctx.Err(); err != nil {
		return BackendResult{}, err
	}
	if err := b.verifyCreationTime(); err != nil {
		return BackendResult{}, err
	}
	switch operation {
	case "modules":
		return b.modules()
	case "regions":
		return b.regions(arguments)
	case "scan":
		return b.scan(ctx, arguments)
	case "resolveRip":
		return b.resolveRIP(arguments)
	case "readBatch":
		return b.readBatch(arguments)
	case "readStrided":
		return b.readStrided(arguments)
	default:
		return BackendResult{}, fmt.Errorf("unsupported memory operation %q", operation)
	}
}

func (b *MemoryBackend) Estimate(namespace, operation string, arguments map[string]any) (uint64, uint64, error) {
	if namespace != "memory" {
		return 0, 0, fmt.Errorf("memory backend does not implement namespace %q", namespace)
	}
	switch operation {
	case "modules", "regions":
		return 0, 0, nil
	case "resolveRip":
		return 4, 0, nil
	case "scan":
		regions, ok := arguments["regions"].([]any)
		if !ok || len(regions) == 0 || len(regions) > 4096 {
			return 0, 0, errors.New("regions must be a non-empty bounded list")
		}
		var total uint64
		for index, value := range regions {
			item, ok := value.(map[string]any)
			if !ok {
				return 0, 0, fmt.Errorf("regions[%d] must be an object", index)
			}
			size, err := positiveInt64(item["size"], fmt.Sprintf("regions[%d].size", index))
			if err != nil || uint64(size) > ^uint64(0)-total {
				return 0, 0, errors.New("invalid or overflowing scan region size")
			}
			total += uint64(size)
		}
		return total, 0, nil
	case "readBatch":
		reads, ok := arguments["reads"].([]any)
		if !ok || len(reads) == 0 || len(reads) > 4096 {
			return 0, 0, errors.New("reads must be a non-empty bounded list")
		}
		var total uint64
		for index, value := range reads {
			item, ok := value.(map[string]any)
			if !ok {
				return 0, 0, fmt.Errorf("reads[%d] must be an object", index)
			}
			typeName, ok := item["type"].(string)
			if !ok {
				return 0, 0, fmt.Errorf("reads[%d].type must be a string", index)
			}
			var explicit int64
			var err error
			if item["length"] != nil {
				explicit, err = positiveInt64(item["length"], fmt.Sprintf("reads[%d].length", index))
				if err != nil {
					return 0, 0, err
				}
			}
			size, err := typeSize(typeName, explicit)
			if err != nil || uint64(size) > ^uint64(0)-total {
				return 0, 0, errors.New("invalid or overflowing readBatch size")
			}
			total += uint64(size)
		}
		return total, 0, nil
	case "readStrided":
		count, err := positiveInt64(arguments["count"], "count")
		if err != nil {
			return 0, 0, err
		}
		stride, err := positiveInt64(arguments["stride"], "stride")
		if err != nil || uint64(count) > ^uint64(0)/uint64(stride) {
			return 0, 0, errors.New("invalid or overflowing strided read size")
		}
		return uint64(count) * uint64(stride), 0, nil
	default:
		return 0, 0, fmt.Errorf("unsupported memory operation %q", operation)
	}
}

func (b *MemoryBackend) verifyIdentity() error {
	if err := b.verifyCreationTime(); err != nil {
		return err
	}
	buffer := make([]uint16, 32768)
	size := uint32(len(buffer))
	if err := windows.QueryFullProcessImageName(b.handle, 0, &buffer[0], &size); err != nil {
		return fmt.Errorf("query process image path: %w", err)
	}
	path := windows.UTF16ToString(buffer[:size])
	if !strings.EqualFold(filepath.Clean(path), filepath.Clean(b.identity.ImagePath)) {
		return errors.New("process image path does not match expected identity")
	}
	file, err := os.Open(path)
	if err != nil {
		return fmt.Errorf("open process image for identity hash: %w", err)
	}
	defer file.Close()
	hash := sha256.New()
	if _, err := io.Copy(hash, file); err != nil {
		return fmt.Errorf("hash process image: %w", err)
	}
	if hex.EncodeToString(hash.Sum(nil)) != strings.ToLower(b.identity.ImageSHA256) {
		return errors.New("process image SHA-256 does not match expected identity")
	}
	return nil
}

func (b *MemoryBackend) verifyCreationTime() error {
	var creation, exit, kernel, user windows.Filetime
	if err := windows.GetProcessTimes(b.handle, &creation, &exit, &kernel, &user); err != nil {
		return fmt.Errorf("query process creation time: %w", err)
	}
	actual := uint64(creation.HighDateTime)<<32 | uint64(creation.LowDateTime)
	if actual != b.identity.CreationTimeWindows {
		return errors.New("process creation time does not match expected identity")
	}
	return nil
}

func (b *MemoryBackend) modules() (BackendResult, error) {
	snapshot, err := windows.CreateToolhelp32Snapshot(
		windows.TH32CS_SNAPMODULE|windows.TH32CS_SNAPMODULE32,
		b.identity.PID,
	)
	if err != nil {
		return BackendResult{}, err
	}
	defer windows.CloseHandle(snapshot)
	entry := windows.ModuleEntry32{Size: uint32(unsafe.Sizeof(windows.ModuleEntry32{}))}
	if err := windows.Module32First(snapshot, &entry); err != nil {
		return BackendResult{}, err
	}
	result := make([]any, 0, 32)
	for {
		result = append(result, map[string]any{
			"name":        windows.UTF16ToString(entry.Module[:]),
			"path":        windows.UTF16ToString(entry.ExePath[:]),
			"baseAddress": formatAddress(uint64(entry.ModBaseAddr)),
			"size":        uint64(entry.ModBaseSize),
		})
		entry.Size = uint32(unsafe.Sizeof(windows.ModuleEntry32{}))
		err = windows.Module32Next(snapshot, &entry)
		if errors.Is(err, windows.ERROR_NO_MORE_FILES) {
			break
		}
		if err != nil {
			return BackendResult{}, err
		}
		if len(result) >= 1024 {
			return BackendResult{}, errors.New("module count exceeds observer limit")
		}
	}
	return BackendResult{Value: map[string]any{
		"process": b.identity,
		"modules": result,
	}}, nil
}

func (b *MemoryBackend) regions(arguments map[string]any) (BackendResult, error) {
	maxRegions, err := positiveInt64(arguments["max_regions"], "max_regions")
	if err != nil || maxRegions > 4096 {
		return BackendResult{}, errors.New("max_regions must be between 1 and 4096")
	}
	var result []any
	var address uintptr
	for len(result) < int(maxRegions) {
		var info windows.MemoryBasicInformation
		err := windows.VirtualQueryEx(b.handle, address, &info, unsafe.Sizeof(info))
		if err != nil {
			if errors.Is(err, windows.ERROR_INVALID_PARAMETER) {
				break
			}
			return BackendResult{}, err
		}
		next := info.BaseAddress + info.RegionSize
		if next <= address {
			break
		}
		address = next
		if info.State != windows.MEM_COMMIT || !readableProtection(info.Protect) {
			continue
		}
		result = append(result, map[string]any{
			"baseAddress": formatAddress(uint64(info.BaseAddress)),
			"size":        uint64(info.RegionSize),
			"type":        memoryTypeName(info.Type),
			"protection":  uint64(info.Protect),
			"writable":    writableProtection(info.Protect),
			"executable":  executableProtection(info.Protect),
		})
	}
	return BackendResult{Value: map[string]any{"regions": result}}, nil
}

func (b *MemoryBackend) scan(ctx context.Context, arguments map[string]any) (BackendResult, error) {
	const scanChunkBytes = uint64(4 << 20)

	patternText, ok := arguments["pattern"].(string)
	if !ok {
		return BackendResult{}, errors.New("pattern must be a string")
	}
	pattern, err := parsePattern(patternText)
	if err != nil {
		return BackendResult{}, err
	}
	maxMatches, err := positiveInt64(arguments["max_matches"], "max_matches")
	if err != nil || maxMatches > 1024 {
		return BackendResult{}, errors.New("max_matches must be between 1 and 1024")
	}
	regions, ok := arguments["regions"].([]any)
	if !ok || len(regions) == 0 || len(regions) > 4096 {
		return BackendResult{}, errors.New("regions must be a non-empty bounded list")
	}
	type region struct{ base, size uint64 }
	parsed := make([]region, 0, len(regions))
	var total uint64
	for index, value := range regions {
		item, ok := value.(map[string]any)
		if !ok {
			return BackendResult{}, fmt.Errorf("regions[%d] must be an object", index)
		}
		base, err := parseAddress(item["base_address"], fmt.Sprintf("regions[%d].base_address", index))
		if err != nil {
			return BackendResult{}, err
		}
		size, err := positiveInt64(item["size"], fmt.Sprintf("regions[%d].size", index))
		if err != nil {
			return BackendResult{}, err
		}
		if uint64(size) > b.maxBytesPerCall-total {
			return BackendResult{}, errors.New("scan regions exceed per-call byte limit")
		}
		total += uint64(size)
		parsed = append(parsed, region{base: base, size: uint64(size)})
	}
	matches := make([]any, 0, maxMatches)
	var bytesRead uint64
	for _, region := range parsed {
		var tail []byte
		for offset := uint64(0); offset < region.size; {
			if err := ctx.Err(); err != nil {
				return BackendResult{}, err
			}
			chunkSize := min(scanChunkBytes, region.size-offset)
			chunk, count, err := b.read(region.base+offset, chunkSize)
			bytesRead += uint64(count)
			if err != nil {
				return BackendResult{}, err
			}
			window := make([]byte, 0, len(tail)+len(chunk))
			window = append(window, tail...)
			window = append(window, chunk...)
			windowBase := region.base + offset - uint64(len(tail))
			found, err := scanPatternContext(ctx, window, pattern, int(maxMatches)-len(matches))
			if err != nil {
				return BackendResult{}, err
			}
			for _, matchOffset := range found {
				matches = append(matches, map[string]any{
					"address": formatAddress(windowBase + uint64(matchOffset)),
				})
			}
			if len(matches) == int(maxMatches) {
				break
			}
			tailLength := min(len(pattern.bytes)-1, len(window))
			tail = append(tail[:0], window[len(window)-tailLength:]...)
			offset += chunkSize
		}
		if len(matches) == int(maxMatches) {
			break
		}
	}
	return BackendResult{
		Value:           map[string]any{"matches": matches},
		MemoryBytesRead: bytesRead,
	}, nil
}

func (b *MemoryBackend) resolveRIP(arguments map[string]any) (BackendResult, error) {
	address, err := parseAddress(arguments["address"], "address")
	if err != nil {
		return BackendResult{}, err
	}
	displacementOffset, err := nonNegativeInt64(arguments["displacement_offset"], "displacement_offset")
	if err != nil {
		return BackendResult{}, err
	}
	instructionLength, err := positiveInt64(arguments["instruction_length"], "instruction_length")
	if err != nil {
		return BackendResult{}, err
	}
	data, count, err := b.read(address+uint64(displacementOffset), 4)
	if err != nil {
		return BackendResult{}, err
	}
	displacement := int64(int32(uint32(data[0]) | uint32(data[1])<<8 | uint32(data[2])<<16 | uint32(data[3])<<24))
	target := uint64(int64(address) + instructionLength + displacement)
	return BackendResult{
		Value: map[string]any{
			"instructionAddress": formatAddress(address),
			"targetAddress":      formatAddress(target),
			"displacement":       displacement,
		},
		MemoryBytesRead: uint64(count),
	}, nil
}

func (b *MemoryBackend) readBatch(arguments map[string]any) (BackendResult, error) {
	reads, ok := arguments["reads"].([]any)
	if !ok || len(reads) == 0 || len(reads) > 4096 {
		return BackendResult{}, errors.New("reads must be a non-empty bounded list")
	}
	var total uint64
	result := make([]any, 0, len(reads))
	for index, value := range reads {
		item, ok := value.(map[string]any)
		if !ok {
			return BackendResult{}, fmt.Errorf("reads[%d] must be an object", index)
		}
		address, err := parseAddress(item["address"], fmt.Sprintf("reads[%d].address", index))
		if err != nil {
			return BackendResult{}, err
		}
		typeName, ok := item["type"].(string)
		if !ok {
			return BackendResult{}, fmt.Errorf("reads[%d].type must be a string", index)
		}
		var explicit int64
		if item["length"] != nil {
			explicit, err = positiveInt64(item["length"], fmt.Sprintf("reads[%d].length", index))
			if err != nil {
				return BackendResult{}, err
			}
		}
		size, err := typeSize(typeName, explicit)
		if err != nil {
			return BackendResult{}, err
		}
		if uint64(size) > b.maxBytesPerCall-total {
			return BackendResult{}, errors.New("readBatch exceeds per-call byte limit")
		}
		total += uint64(size)
		data, count, err := b.read(address, uint64(size))
		if err != nil {
			return BackendResult{}, err
		}
		decoded, err := decodeTyped(data, typeName)
		if err != nil {
			return BackendResult{}, err
		}
		result = append(result, map[string]any{
			"address":   formatAddress(address),
			"type":      typeName,
			"bytesRead": count,
			"value":     decoded,
		})
	}
	return BackendResult{Value: map[string]any{"reads": result}, MemoryBytesRead: total}, nil
}

func (b *MemoryBackend) readStrided(arguments map[string]any) (BackendResult, error) {
	base, err := parseAddress(arguments["base_address"], "base_address")
	if err != nil {
		return BackendResult{}, err
	}
	count, err := positiveInt64(arguments["count"], "count")
	if err != nil || count > 1_000_000 {
		return BackendResult{}, errors.New("count must be between 1 and 1000000")
	}
	stride, err := positiveInt64(arguments["stride"], "stride")
	if err != nil || stride > 1<<20 {
		return BackendResult{}, errors.New("stride must be between 1 and 1048576")
	}
	if uint64(count) > b.maxBytesPerCall/uint64(stride) {
		return BackendResult{}, errors.New("readStrided exceeds per-call byte limit")
	}
	fieldsValue, ok := arguments["fields"].([]any)
	if !ok || len(fieldsValue) == 0 || len(fieldsValue) > 256 {
		return BackendResult{}, errors.New("fields must be a non-empty bounded list")
	}
	type field struct {
		name, typeName string
		offset, size   int
	}
	fields := make([]field, 0, len(fieldsValue))
	for index, value := range fieldsValue {
		item, ok := value.(map[string]any)
		if !ok {
			return BackendResult{}, fmt.Errorf("fields[%d] must be an object", index)
		}
		name, nameOK := item["name"].(string)
		typeName, typeOK := item["type"].(string)
		if !nameOK || name == "" || !typeOK {
			return BackendResult{}, fmt.Errorf("fields[%d] requires name and type", index)
		}
		offset, err := nonNegativeInt64(item["offset"], fmt.Sprintf("fields[%d].offset", index))
		if err != nil {
			return BackendResult{}, err
		}
		var explicit int64
		if item["length"] != nil {
			explicit, err = positiveInt64(item["length"], fmt.Sprintf("fields[%d].length", index))
			if err != nil {
				return BackendResult{}, err
			}
		}
		size, err := typeSize(typeName, explicit)
		if err != nil || offset+int64(size) > stride {
			return BackendResult{}, fmt.Errorf("field %q exceeds record stride", name)
		}
		fields = append(fields, field{name: name, typeName: typeName, offset: int(offset), size: size})
	}
	total := uint64(count * stride)
	data, bytesRead, err := b.read(base, total)
	if err != nil {
		return BackendResult{}, err
	}
	records := make([]any, count)
	for index := int64(0); index < count; index++ {
		record := make(map[string]any, len(fields)+1)
		record["index"] = index
		start := int(index * stride)
		for _, field := range fields {
			value, err := decodeTyped(data[start+field.offset:start+field.offset+field.size], field.typeName)
			if err != nil {
				return BackendResult{}, err
			}
			record[field.name] = value
		}
		records[index] = record
	}
	return BackendResult{
		Value: map[string]any{
			"baseAddress": formatAddress(base),
			"count":       count,
			"stride":      stride,
			"records":     records,
		},
		MemoryBytesRead: uint64(bytesRead),
	}, nil
}

func (b *MemoryBackend) read(address, size uint64) ([]byte, int, error) {
	if size == 0 || size > b.maxBytesPerCall || size > uint64(^uint(0)>>1) {
		return nil, 0, errors.New("memory read size is outside observer limit")
	}
	buffer := make([]byte, int(size))
	var count uintptr
	err := windows.ReadProcessMemory(b.handle, uintptr(address), &buffer[0], uintptr(size), &count)
	if err != nil {
		return nil, int(count), err
	}
	if count != uintptr(size) {
		return nil, int(count), fmt.Errorf("short memory read: got %d, expected %d", count, size)
	}
	return buffer, int(count), nil
}

func readableProtection(protect uint32) bool {
	if protect&windows.PAGE_GUARD != 0 || protect&windows.PAGE_NOACCESS != 0 {
		return false
	}
	base := protect & 0xff
	return base == windows.PAGE_READONLY || base == windows.PAGE_READWRITE ||
		base == windows.PAGE_WRITECOPY || base == windows.PAGE_EXECUTE_READ ||
		base == windows.PAGE_EXECUTE_READWRITE || base == windows.PAGE_EXECUTE_WRITECOPY
}

func writableProtection(protect uint32) bool {
	base := protect & 0xff
	return base == windows.PAGE_READWRITE || base == windows.PAGE_WRITECOPY ||
		base == windows.PAGE_EXECUTE_READWRITE || base == windows.PAGE_EXECUTE_WRITECOPY
}

func executableProtection(protect uint32) bool {
	base := protect & 0xff
	return base == windows.PAGE_EXECUTE || base == windows.PAGE_EXECUTE_READ ||
		base == windows.PAGE_EXECUTE_READWRITE || base == windows.PAGE_EXECUTE_WRITECOPY
}

func memoryTypeName(value uint32) string {
	switch value {
	case memImage:
		return "image"
	case memMapped:
		return "mapped"
	case memPrivate:
		return "private"
	default:
		return "unknown"
	}
}
