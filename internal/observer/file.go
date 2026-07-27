package observer

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

type FileBackend struct {
	roots    map[string]string
	decoders map[string]FileDecoder
}

type FileDecoder interface {
	Decode(ctx context.Context, data []byte, options map[string]any) (any, error)
}

func NewFileBackend(roots map[string]string) (*FileBackend, error) {
	return NewFileBackendWithDecoders(roots, nil)
}

func NewFileBackendWithDecoders(roots map[string]string, decoders map[string]FileDecoder) (*FileBackend, error) {
	if len(roots) == 0 {
		return nil, errors.New("at least one logical file root is required")
	}
	canonical := make(map[string]string, len(roots))
	for id, root := range roots {
		if id == "" || root == "" || !filepath.IsAbs(root) {
			return nil, fmt.Errorf("root %q must have a non-empty ID and absolute path", id)
		}
		resolved, err := filepath.EvalSymlinks(root)
		if err != nil {
			return nil, fmt.Errorf("resolve root %q: %w", id, err)
		}
		info, err := os.Stat(resolved)
		if err != nil {
			return nil, fmt.Errorf("stat root %q: %w", id, err)
		}
		if !info.IsDir() {
			return nil, fmt.Errorf("root %q is not a directory", id)
		}
		canonical[id] = resolved
	}
	registered := make(map[string]FileDecoder, len(decoders))
	for id, decoder := range decoders {
		if id == "" || decoder == nil {
			return nil, errors.New("decoder IDs and implementations must not be empty")
		}
		registered[id] = decoder
	}
	return &FileBackend{roots: canonical, decoders: registered}, nil
}

func (b *FileBackend) Call(ctx context.Context, namespace, operation string, arguments map[string]any) (BackendResult, error) {
	if namespace != "file" {
		return BackendResult{}, fmt.Errorf("file backend does not implement namespace %q", namespace)
	}
	if err := ctx.Err(); err != nil {
		return BackendResult{}, err
	}
	pathValue, ok := arguments["path"].(map[string]any)
	if !ok {
		return BackendResult{}, errors.New("path must be an object")
	}
	rootID, ok := pathValue["root"].(string)
	if !ok || rootID == "" {
		return BackendResult{}, errors.New("path.root must be a non-empty string")
	}
	relative, ok := pathValue["relative"].(string)
	if !ok {
		return BackendResult{}, errors.New("path.relative must be a string")
	}
	path, err := b.resolve(rootID, relative)
	if err != nil {
		return BackendResult{}, err
	}
	switch operation {
	case "stat":
		return b.stat(path, rootID, relative)
	case "read":
		return b.read(path, rootID, relative, arguments)
	case "hash":
		return b.hash(ctx, path, rootID, relative, arguments)
	case "decode":
		return b.decode(ctx, path, rootID, relative, arguments)
	default:
		return BackendResult{}, fmt.Errorf("unsupported file operation %q", operation)
	}
}

func (b *FileBackend) Estimate(namespace, operation string, arguments map[string]any) (uint64, uint64, error) {
	if namespace != "file" {
		return 0, 0, fmt.Errorf("file backend does not implement namespace %q", namespace)
	}
	pathValue, ok := arguments["path"].(map[string]any)
	if !ok {
		return 0, 0, errors.New("path must be an object")
	}
	rootID, rootOK := pathValue["root"].(string)
	relative, relativeOK := pathValue["relative"].(string)
	if !rootOK || !relativeOK {
		return 0, 0, errors.New("path.root and path.relative must be strings")
	}
	path, err := b.resolve(rootID, relative)
	if err != nil {
		return 0, 0, err
	}
	switch operation {
	case "stat":
		return 0, 0, nil
	case "read":
		length, err := positiveInt64(arguments["length"], "length")
		if err != nil {
			return 0, 0, err
		}
		return 0, uint64(length), nil
	case "hash", "decode":
		info, err := os.Stat(path)
		if err != nil {
			return 0, 0, err
		}
		return 0, uint64(info.Size()), nil
	default:
		return 0, 0, fmt.Errorf("unsupported file operation %q", operation)
	}
}

func (b *FileBackend) decode(ctx context.Context, path, rootID, relative string, arguments map[string]any) (BackendResult, error) {
	decoderID, ok := arguments["decoder"].(string)
	if !ok || decoderID == "" {
		return BackendResult{}, errors.New("decoder must be a non-empty registered decoder ID")
	}
	decoder, ok := b.decoders[decoderID]
	if !ok {
		return BackendResult{}, fmt.Errorf("decoder %q is not registered", decoderID)
	}
	options := map[string]any{}
	if supplied := arguments["options"]; supplied != nil {
		var valid bool
		options, valid = supplied.(map[string]any)
		if !valid {
			return BackendResult{}, errors.New("options must be an object")
		}
	}
	if err := ctx.Err(); err != nil {
		return BackendResult{}, err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return BackendResult{}, err
	}
	value, err := decoder.Decode(ctx, data, options)
	if err != nil {
		return BackendResult{}, fmt.Errorf("decoder %q failed: %w", decoderID, err)
	}
	info, err := os.Stat(path)
	if err != nil {
		return BackendResult{}, err
	}
	return BackendResult{
		Value: map[string]any{
			"path":       map[string]any{"root": rootID, "relative": relative},
			"decoder":    decoderID,
			"size":       len(data),
			"modifiedAt": info.ModTime().UTC().Format("2006-01-02T15:04:05.000000000Z"),
			"value":      value,
		},
		FileBytesRead: uint64(len(data)),
	}, nil
}

func (b *FileBackend) resolve(rootID, relative string) (string, error) {
	root, ok := b.roots[rootID]
	if !ok {
		return "", fmt.Errorf("unknown logical root %q", rootID)
	}
	if relative == "" || filepath.IsAbs(relative) || strings.Contains(relative, `\`) ||
		strings.Contains(relative, ":") || filepath.Clean(relative) != filepath.FromSlash(relative) {
		return "", errors.New("path.relative must be a canonical slash-separated relative file path")
	}
	if relative == "." || relative == ".." || strings.HasPrefix(relative, "../") ||
		strings.Contains(relative, "/../") {
		return "", errors.New("path traversal is forbidden")
	}
	candidate := filepath.Join(root, filepath.FromSlash(relative))
	resolved, err := filepath.EvalSymlinks(candidate)
	if err != nil {
		return "", err
	}
	within, err := filepath.Rel(root, resolved)
	if err != nil {
		return "", err
	}
	if within == ".." || strings.HasPrefix(within, ".."+string(filepath.Separator)) {
		return "", errors.New("file resolves outside authorized root")
	}
	info, err := os.Stat(resolved)
	if err != nil {
		return "", err
	}
	if !info.Mode().IsRegular() {
		return "", errors.New("file operation requires a regular file")
	}
	return resolved, nil
}

func (b *FileBackend) stat(path, rootID, relative string) (BackendResult, error) {
	info, err := os.Stat(path)
	if err != nil {
		return BackendResult{}, err
	}
	return BackendResult{Value: map[string]any{
		"path":       map[string]any{"root": rootID, "relative": relative},
		"size":       info.Size(),
		"modifiedAt": info.ModTime().UTC().Format("2006-01-02T15:04:05.000000000Z"),
	}}, nil
}

func (b *FileBackend) read(path, rootID, relative string, arguments map[string]any) (BackendResult, error) {
	offset, err := nonNegativeInt64(arguments["offset"], "offset")
	if err != nil {
		return BackendResult{}, err
	}
	length, err := positiveInt64(arguments["length"], "length")
	if err != nil {
		return BackendResult{}, err
	}
	file, err := os.Open(path)
	if err != nil {
		return BackendResult{}, err
	}
	defer file.Close()
	buffer := make([]byte, length)
	count, readErr := file.ReadAt(buffer, offset)
	if readErr != nil && !errors.Is(readErr, io.EOF) {
		return BackendResult{}, readErr
	}
	buffer = buffer[:count]
	return BackendResult{
		Value: map[string]any{
			"path":       map[string]any{"root": rootID, "relative": relative},
			"offset":     offset,
			"bytesRead":  count,
			"dataBase64": base64.StdEncoding.EncodeToString(buffer),
		},
		FileBytesRead: uint64(count),
	}, nil
}

func (b *FileBackend) hash(ctx context.Context, path, rootID, relative string, arguments map[string]any) (BackendResult, error) {
	algorithm, ok := arguments["algorithm"].(string)
	if !ok || algorithm != "sha256" {
		return BackendResult{}, errors.New("algorithm must equal sha256")
	}
	file, err := os.Open(path)
	if err != nil {
		return BackendResult{}, err
	}
	defer file.Close()
	hash := sha256.New()
	buffer := make([]byte, 64<<10)
	var total uint64
	for {
		if err := ctx.Err(); err != nil {
			return BackendResult{}, err
		}
		count, readErr := file.Read(buffer)
		if count != 0 {
			hash.Write(buffer[:count])
			total += uint64(count)
		}
		if errors.Is(readErr, io.EOF) {
			break
		}
		if readErr != nil {
			return BackendResult{}, readErr
		}
	}
	return BackendResult{
		Value: map[string]any{
			"path":      map[string]any{"root": rootID, "relative": relative},
			"algorithm": "sha256",
			"digest":    hex.EncodeToString(hash.Sum(nil)),
			"bytesRead": total,
		},
		FileBytesRead: total,
	}, nil
}

func positiveInt64(value any, name string) (int64, error) {
	number, err := numberInt64(value, name)
	if err != nil {
		return 0, err
	}
	if number <= 0 {
		return 0, fmt.Errorf("%s must be positive", name)
	}
	return number, nil
}

func nonNegativeInt64(value any, name string) (int64, error) {
	number, err := numberInt64(value, name)
	if err != nil {
		return 0, err
	}
	if number < 0 {
		return 0, fmt.Errorf("%s must not be negative", name)
	}
	return number, nil
}

func numberInt64(value any, name string) (int64, error) {
	switch value := value.(type) {
	case int:
		return int64(value), nil
	case int64:
		return value, nil
	case float64:
		if value != float64(int64(value)) {
			return 0, fmt.Errorf("%s must be an integer", name)
		}
		return int64(value), nil
	case interface{ Int64() (int64, error) }:
		number, err := value.Int64()
		if err != nil {
			return 0, fmt.Errorf("%s must be an integer: %w", name, err)
		}
		return number, nil
	default:
		return 0, fmt.Errorf("%s must be an integer", name)
	}
}
