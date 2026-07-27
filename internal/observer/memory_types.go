package observer

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"math"
	"strconv"
	"strings"
)

type ProcessIdentity struct {
	PID                 uint32 `json:"pid"`
	CreationTimeWindows uint64 `json:"creationTimeWindows"`
	ImagePath           string `json:"imagePath"`
	ImageSHA256         string `json:"imageSha256"`
}

type bytePattern struct {
	bytes []byte
	known []bool
}

func parsePattern(text string) (bytePattern, error) {
	parts := strings.Fields(text)
	if len(parts) == 0 {
		return bytePattern{}, errors.New("pattern must not be empty")
	}
	result := bytePattern{bytes: make([]byte, len(parts)), known: make([]bool, len(parts))}
	for index, part := range parts {
		if part == "?" || part == "??" {
			continue
		}
		if len(part) != 2 {
			return bytePattern{}, fmt.Errorf("pattern token %q must be two hexadecimal digits or ??", part)
		}
		value, err := strconv.ParseUint(part, 16, 8)
		if err != nil {
			return bytePattern{}, fmt.Errorf("invalid pattern token %q", part)
		}
		result.bytes[index] = byte(value)
		result.known[index] = true
	}
	return result, nil
}

func (p bytePattern) matches(data []byte) bool {
	if len(data) != len(p.bytes) {
		return false
	}
	for index := range p.bytes {
		if p.known[index] && data[index] != p.bytes[index] {
			return false
		}
	}
	return true
}

func scanPattern(data []byte, pattern bytePattern, maxMatches int) []int {
	result, _ := scanPatternContext(context.Background(), data, pattern, maxMatches)
	return result
}

func scanPatternContext(ctx context.Context, data []byte, pattern bytePattern, maxMatches int) ([]int, error) {
	if len(pattern.bytes) > len(data) || maxMatches <= 0 {
		return nil, nil
	}
	result := make([]int, 0, maxMatches)
	anchorOffset, anchor := pattern.longestKnownRun()
	if len(anchor) == 0 {
		for offset := 0; offset <= len(data)-len(pattern.bytes) && len(result) < maxMatches; offset++ {
			if offset&0xffff == 0 {
				if err := ctx.Err(); err != nil {
					return nil, err
				}
			}
			result = append(result, offset)
		}
		return result, nil
	}
	searchStart := 0
	for searchStart <= len(data)-len(anchor) && len(result) < maxMatches {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		found := bytes.Index(data[searchStart:], anchor)
		if found < 0 {
			break
		}
		anchorAt := searchStart + found
		candidate := anchorAt - anchorOffset
		if candidate >= 0 && candidate+len(pattern.bytes) <= len(data) &&
			pattern.matches(data[candidate:candidate+len(pattern.bytes)]) {
			result = append(result, candidate)
		}
		searchStart = anchorAt + 1
	}
	return result, nil
}

func (p bytePattern) longestKnownRun() (int, []byte) {
	bestStart, bestLength := 0, 0
	for start := 0; start < len(p.known); {
		for start < len(p.known) && !p.known[start] {
			start++
		}
		end := start
		for end < len(p.known) && p.known[end] {
			end++
		}
		if end-start > bestLength {
			bestStart, bestLength = start, end-start
		}
		start = end + 1
	}
	return bestStart, p.bytes[bestStart : bestStart+bestLength]
}

func parseAddress(value any, name string) (uint64, error) {
	text, ok := value.(string)
	if !ok || !strings.HasPrefix(text, "0x") || len(text) < 3 {
		return 0, fmt.Errorf("%s must be a 0x-prefixed hexadecimal string", name)
	}
	address, err := strconv.ParseUint(text[2:], 16, 64)
	if err != nil {
		return 0, fmt.Errorf("invalid %s: %w", name, err)
	}
	return address, nil
}

func formatAddress(address uint64) string {
	return fmt.Sprintf("0x%016X", address)
}

func typeSize(name string, explicitLength int64) (int, error) {
	switch name {
	case "u8", "i8":
		return 1, nil
	case "u16", "i16":
		return 2, nil
	case "u32", "i32", "f32":
		return 4, nil
	case "u64", "i64", "f64", "pointer":
		return 8, nil
	case "bytes":
		if explicitLength <= 0 || explicitLength > math.MaxInt32 {
			return 0, errors.New("bytes read requires a positive bounded length")
		}
		return int(explicitLength), nil
	default:
		return 0, fmt.Errorf("unsupported read type %q", name)
	}
}

func decodeTyped(data []byte, name string) (any, error) {
	switch name {
	case "u8":
		return uint64(data[0]), nil
	case "i8":
		return int64(int8(data[0])), nil
	case "u16":
		return uint64(binary.LittleEndian.Uint16(data)), nil
	case "i16":
		return int64(int16(binary.LittleEndian.Uint16(data))), nil
	case "u32":
		return uint64(binary.LittleEndian.Uint32(data)), nil
	case "i32":
		return int64(int32(binary.LittleEndian.Uint32(data))), nil
	case "u64", "pointer":
		return binary.LittleEndian.Uint64(data), nil
	case "i64":
		return int64(binary.LittleEndian.Uint64(data)), nil
	case "f32":
		return float64(math.Float32frombits(binary.LittleEndian.Uint32(data))), nil
	case "f64":
		return math.Float64frombits(binary.LittleEndian.Uint64(data)), nil
	case "bytes":
		return fmt.Sprintf("%X", data), nil
	default:
		return nil, fmt.Errorf("unsupported read type %q", name)
	}
}
