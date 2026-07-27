package observer

import (
	"context"
	"crypto/rand"
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
	blobRoot string
}

func NewFileBackend(roots map[string]string) (*FileBackend, error) {
	return NewFileBackendWithBlobRoot(roots, "")
}

func NewFileBackendWithBlobRoot(roots map[string]string, blobRoot string) (*FileBackend, error) {
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
	canonicalBlobRoot := ""
	if blobRoot != "" {
		if !filepath.IsAbs(blobRoot) {
			return nil, errors.New("blob root must be absolute")
		}
		resolvedBlobRoot, err := filepath.EvalSymlinks(blobRoot)
		if err != nil {
			return nil, fmt.Errorf("resolve blob root: %w", err)
		}
		canonicalBlobRoot = resolvedBlobRoot
		info, err := os.Stat(canonicalBlobRoot)
		if err != nil {
			return nil, fmt.Errorf("stat blob root: %w", err)
		}
		if !info.IsDir() {
			return nil, errors.New("blob root must be a directory")
		}
	}
	return &FileBackend{roots: canonical, blobRoot: canonicalBlobRoot}, nil
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
	case "openBlob":
		return b.openBlob(ctx, path, rootID, relative)
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
	case "hash", "openBlob":
		info, err := os.Stat(path)
		if err != nil {
			return 0, 0, err
		}
		return 0, uint64(info.Size()), nil
	default:
		return 0, 0, fmt.Errorf("unsupported file operation %q", operation)
	}
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

func (b *FileBackend) openBlob(ctx context.Context, path, rootID, relative string) (_ BackendResult, resultErr error) {
	if b.blobRoot == "" {
		return BackendResult{}, errors.New("job blob root is not configured")
	}
	var random [32]byte
	if _, err := rand.Read(random[:]); err != nil {
		return BackendResult{}, fmt.Errorf("generate blob handle: %w", err)
	}
	handle := hex.EncodeToString(random[:])
	target := filepath.Join(b.blobRoot, handle+".blob")
	source, err := os.Open(path)
	if err != nil {
		return BackendResult{}, err
	}
	defer source.Close()
	destination, err := os.OpenFile(target, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return BackendResult{}, fmt.Errorf("create job blob: %w", err)
	}
	defer func() {
		if closeErr := destination.Close(); resultErr == nil && closeErr != nil {
			resultErr = closeErr
		}
		if resultErr != nil {
			_ = os.Remove(target)
		}
	}()
	buffer := make([]byte, 64<<10)
	var total uint64
	for {
		if err := ctx.Err(); err != nil {
			return BackendResult{}, err
		}
		count, readErr := source.Read(buffer)
		if count > 0 {
			written, writeErr := destination.Write(buffer[:count])
			total += uint64(written)
			if writeErr != nil {
				return BackendResult{}, writeErr
			}
			if written != count {
				return BackendResult{}, io.ErrShortWrite
			}
		}
		if errors.Is(readErr, io.EOF) {
			break
		}
		if readErr != nil {
			return BackendResult{}, readErr
		}
	}
	info, err := os.Stat(path)
	if err != nil {
		return BackendResult{}, err
	}
	return BackendResult{
		Value: map[string]any{
			"path":       map[string]any{"root": rootID, "relative": relative},
			"blob":       map[string]any{"blobHandle": handle},
			"size":       total,
			"modifiedAt": info.ModTime().UTC().Format("2006-01-02T15:04:05.000000000Z"),
		},
		FileBytesRead: total,
	}, nil
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
