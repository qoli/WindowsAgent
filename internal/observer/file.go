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
	"runtime"
	"strings"

	"github.com/qoli/WindowsAgent/internal/scriptpackage"
)

type FileBackend struct {
	roots    map[string]string
	blobRoot string
}

func ResolveFileRoots(permission *scriptpackage.FilePermissions, localAppData string) (map[string]string, error) {
	if permission == nil {
		return nil, nil
	}
	if localAppData == "" || !filepath.IsAbs(localAppData) {
		return nil, errors.New("LOCALAPPDATA must be an absolute path for declared file roots")
	}
	roots := make(map[string]string, len(permission.Roots))
	for _, declaration := range permission.Roots {
		if declaration.Resolver.Kind != "windows-known-folder" ||
			declaration.Resolver.KnownFolder != "LocalAppData" {
			return nil, fmt.Errorf(
				"unsupported resolver for file root %q",
				declaration.ID,
			)
		}
		root := filepath.Join(
			localAppData,
			filepath.FromSlash(declaration.Resolver.Relative),
		)
		within, err := filepath.Rel(localAppData, root)
		if err != nil || within == ".." ||
			strings.HasPrefix(within, ".."+string(filepath.Separator)) {
			return nil, fmt.Errorf("file root %q escapes LocalAppData", declaration.ID)
		}
		roots[declaration.ID] = root
	}
	return roots, nil
}

func NewFileBackend(roots map[string]string) (*FileBackend, error) {
	return NewFileBackendWithBlobRoot(roots, "")
}

func NewFileBackendWithBlobRoot(roots map[string]string, blobRoot string) (*FileBackend, error) {
	if len(roots) == 0 {
		return nil, errors.New("at least one logical file root is required")
	}
	configured := make(map[string]string, len(roots))
	for id, root := range roots {
		if id == "" || root == "" || !filepath.IsAbs(root) {
			return nil, fmt.Errorf("root %q must have a non-empty ID and absolute path", id)
		}
		configured[id] = filepath.Clean(root)
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
	return &FileBackend{roots: configured, blobRoot: canonicalBlobRoot}, nil
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
	switch operation {
	case "list":
		return b.list(ctx, rootID, relative, arguments)
	case "stat":
		path, err := b.resolveFile(rootID, relative)
		if err != nil {
			return BackendResult{}, err
		}
		return b.stat(path, rootID, relative)
	case "read":
		path, err := b.resolveFile(rootID, relative)
		if err != nil {
			return BackendResult{}, err
		}
		return b.read(path, rootID, relative, arguments)
	case "hash":
		path, err := b.resolveFile(rootID, relative)
		if err != nil {
			return BackendResult{}, err
		}
		return b.hash(ctx, path, rootID, relative, arguments)
	case "openBlob":
		path, err := b.resolveFile(rootID, relative)
		if err != nil {
			return BackendResult{}, err
		}
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
	switch operation {
	case "list":
		if _, _, err := listBounds(arguments); err != nil {
			return 0, 0, err
		}
		return 0, 0, nil
	case "stat":
		if _, err := b.resolveFile(rootID, relative); err != nil {
			return 0, 0, err
		}
		return 0, 0, nil
	case "read":
		if _, err := b.resolveFile(rootID, relative); err != nil {
			return 0, 0, err
		}
		length, err := positiveInt64(arguments["length"], "length")
		if err != nil {
			return 0, 0, err
		}
		return 0, uint64(length), nil
	case "hash", "openBlob":
		path, err := b.resolveFile(rootID, relative)
		if err != nil {
			return 0, 0, err
		}
		info, err := os.Stat(path)
		if err != nil {
			return 0, 0, err
		}
		return 0, uint64(info.Size()), nil
	default:
		return 0, 0, fmt.Errorf("unsupported file operation %q", operation)
	}
}

func (b *FileBackend) canonicalRoot(rootID string) (string, error) {
	root, ok := b.roots[rootID]
	if !ok {
		return "", fmt.Errorf("unknown logical root %q", rootID)
	}
	info, err := os.Lstat(root)
	if err != nil {
		return "", err
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return "", errors.New("logical file root must not be a reparse point")
	}
	resolved, err := filepath.EvalSymlinks(root)
	if err != nil {
		return "", err
	}
	if runtime.GOOS == "windows" &&
		!strings.EqualFold(filepath.Clean(root), filepath.Clean(resolved)) {
		return "", errors.New("logical file root path contains a reparse point")
	}
	info, err = os.Stat(resolved)
	if err != nil {
		return "", err
	}
	if !info.IsDir() {
		return "", errors.New("logical file root must be a directory")
	}
	return resolved, nil
}

func (b *FileBackend) resolveFile(rootID, relative string) (string, error) {
	root, err := b.canonicalRoot(rootID)
	if err != nil {
		return "", err
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
	if !strings.EqualFold(filepath.Clean(candidate), filepath.Clean(resolved)) {
		return "", errors.New("file path contains a reparse point")
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

func (b *FileBackend) list(
	ctx context.Context,
	rootID, relative string,
	arguments map[string]any,
) (BackendResult, error) {
	maxDepth, maxEntries, err := listBounds(arguments)
	if err != nil {
		return BackendResult{}, err
	}
	root, err := b.canonicalRoot(rootID)
	if err != nil {
		return BackendResult{}, err
	}
	if relative == "" || filepath.IsAbs(relative) || strings.Contains(relative, `\`) ||
		strings.Contains(relative, ":") || filepath.Clean(relative) != filepath.FromSlash(relative) ||
		relative == ".." || strings.HasPrefix(relative, "../") ||
		strings.Contains(relative, "/../") {
		return BackendResult{}, errors.New("path.relative must be a canonical slash-separated relative directory path")
	}
	start := root
	if relative != "." {
		start = filepath.Join(root, filepath.FromSlash(relative))
	}
	startInfo, err := os.Lstat(start)
	if err != nil {
		return BackendResult{}, err
	}
	if startInfo.Mode()&os.ModeSymlink != 0 || !startInfo.IsDir() {
		return BackendResult{}, errors.New("list path must be a regular directory, not a reparse point")
	}
	entries := make([]map[string]any, 0)
	err = filepath.WalkDir(start, func(name string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		if name == start {
			return nil
		}
		fromStart, err := filepath.Rel(start, name)
		if err != nil {
			return err
		}
		depth := int64(strings.Count(fromStart, string(filepath.Separator)) + 1)
		if depth > maxDepth {
			if entry.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if int64(len(entries)) >= maxEntries {
			return fmt.Errorf("directory listing exceeds maxEntries %d", maxEntries)
		}
		fromRoot, err := filepath.Rel(root, name)
		if err != nil {
			return err
		}
		kind := "file"
		if entry.Type()&os.ModeSymlink != 0 {
			kind = "reparse-point"
		} else if entry.IsDir() {
			kind = "directory"
		} else if !entry.Type().IsRegular() {
			kind = "other"
		}
		value := map[string]any{
			"relative": filepath.ToSlash(fromRoot),
			"kind":     kind,
		}
		if kind == "file" || kind == "directory" {
			info, err := entry.Info()
			if err != nil {
				return err
			}
			value["modifiedAt"] = info.ModTime().UTC().Format("2006-01-02T15:04:05.000000000Z")
			if kind == "file" {
				value["size"] = info.Size()
			}
		}
		entries = append(entries, value)
		if kind == "reparse-point" && entry.IsDir() {
			return filepath.SkipDir
		}
		return nil
	})
	if err != nil {
		return BackendResult{}, err
	}
	return BackendResult{Value: map[string]any{
		"path":    map[string]any{"root": rootID, "relative": relative},
		"entries": entries,
	}}, nil
}

func listBounds(arguments map[string]any) (int64, int64, error) {
	maxDepth, err := positiveInt64(arguments["maxDepth"], "maxDepth")
	if err != nil {
		return 0, 0, err
	}
	maxEntries, err := positiveInt64(arguments["maxEntries"], "maxEntries")
	if err != nil {
		return 0, 0, err
	}
	if maxDepth > 8 {
		return 0, 0, errors.New("maxDepth must not exceed 8")
	}
	if maxEntries > 4096 {
		return 0, 0, errors.New("maxEntries must not exceed 4096")
	}
	return maxDepth, maxEntries, nil
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
