package observationjob

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/qoli/WindowsAgent/internal/observationapi"
)

type blobCatalog struct {
	root  string
	sizes map[string]uint64
}

func newBlobCatalog(root string) *blobCatalog {
	return &blobCatalog{root: root, sizes: map[string]uint64{}}
}

func (c *blobCatalog) registerObserverValue(value json.RawMessage) error {
	var envelope struct {
		Path json.RawMessage `json:"path"`
		Blob struct {
			BlobHandle string `json:"blobHandle"`
		} `json:"blob"`
		Size       uint64 `json:"size"`
		ModifiedAt string `json:"modifiedAt"`
	}
	if err := decodeStrict(value, &envelope); err != nil {
		return fmt.Errorf("decode openBlob result: %w", err)
	}
	if err := observationapi.ValidateBlobHandle(envelope.Blob.BlobHandle); err != nil {
		return err
	}
	info, err := os.Stat(filepath.Join(c.root, envelope.Blob.BlobHandle+".blob"))
	if err != nil || !info.Mode().IsRegular() || uint64(info.Size()) != envelope.Size {
		return errors.New("openBlob result does not match the job blob artifact")
	}
	if _, exists := c.sizes[envelope.Blob.BlobHandle]; exists {
		return errors.New("duplicate job blob handle")
	}
	c.sizes[envelope.Blob.BlobHandle] = envelope.Size
	return nil
}

func (c *blobCatalog) path(reference map[string]any) (string, error) {
	if len(reference) != 1 {
		return "", errors.New("blob reference must contain only blobHandle")
	}
	handle, ok := reference["blobHandle"].(string)
	if !ok {
		return "", errors.New("blobHandle must be a string")
	}
	if err := observationapi.ValidateBlobHandle(handle); err != nil {
		return "", err
	}
	expectedSize, issued := c.sizes[handle]
	if !issued {
		return "", errors.New("blob handle was not issued for this job")
	}
	path := filepath.Join(c.root, handle+".blob")
	info, err := os.Stat(path)
	if err != nil || !info.Mode().IsRegular() || uint64(info.Size()) != expectedSize {
		return "", errors.New("job blob artifact is unavailable or changed")
	}
	return path, nil
}

func (c *blobCatalog) inputBytes(input json.RawMessage) (uint64, error) {
	total := uint64(len(input))
	seen := map[string]struct{}{}
	var value any
	decoder := json.NewDecoder(bytes.NewReader(input))
	decoder.UseNumber()
	if err := decoder.Decode(&value); err != nil {
		return 0, err
	}
	var visit func(any) error
	visit = func(value any) error {
		switch value := value.(type) {
		case map[string]any:
			if rawHandle, exists := value["blobHandle"]; exists {
				handle, ok := rawHandle.(string)
				if !ok || len(value) != 1 {
					return errors.New("blob reference must contain only a string blobHandle")
				}
				size, registered := c.sizes[handle]
				if !registered {
					return errors.New("blob handle was not issued for this job")
				}
				if _, duplicate := seen[handle]; !duplicate {
					if size > ^uint64(0)-total {
						return errors.New("blob byte accounting overflow")
					}
					total += size
					seen[handle] = struct{}{}
				}
			}
			for _, child := range value {
				if err := visit(child); err != nil {
					return err
				}
			}
		case []any:
			for _, child := range value {
				if err := visit(child); err != nil {
					return err
				}
			}
		}
		return nil
	}
	if err := visit(value); err != nil {
		return 0, err
	}
	return total, nil
}
