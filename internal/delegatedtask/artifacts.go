package delegatedtask

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

var ErrArtifactNotFound = errors.New("delegated task artifact not found")

type EventArtifact struct {
	ID        string `json:"id"`
	MediaType string `json:"mediaType"`
	Bytes     int    `json:"bytes"`
	SHA256    string `json:"sha256"`
	URL       string `json:"url"`
}

// putEventArtifact commits the exact PI WEB event before publishing a journal
// reference. Task data stays in the independent delegated runtime's data root.
func (run *taskRun) putEventArtifact(raw []byte) (EventArtifact, error) {
	digest := sha256.Sum256(raw)
	id := hex.EncodeToString(digest[:])
	directory := filepath.Join(run.manager.journal.Root(), "pi-events", run.task.TaskID)
	if err := os.MkdirAll(directory, 0o700); err != nil {
		return EventArtifact{}, err
	}
	file, err := os.CreateTemp(directory, ".event-*")
	if err != nil {
		return EventArtifact{}, err
	}
	name := file.Name()
	defer os.Remove(name)
	_, writeErr := file.Write(raw)
	if writeErr == nil {
		writeErr = file.Sync()
	}
	closeErr := file.Close()
	if err := errors.Join(writeErr, closeErr); err != nil {
		return EventArtifact{}, err
	}
	target := filepath.Join(directory, id+".json")
	// Content addressing makes an already-committed identical event reusable.
	if existing, err := os.ReadFile(target); err == nil {
		if sha256.Sum256(existing) != digest {
			return EventArtifact{}, errors.New("PI event artifact hash mismatch")
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return EventArtifact{}, err
	} else if err := os.Rename(name, target); err != nil {
		return EventArtifact{}, err
	}
	return EventArtifact{ID: id, MediaType: "application/json", Bytes: len(raw), SHA256: id,
		URL: "/v1/delegated-tasks/" + run.task.TaskID + "/artifacts/" + id}, nil
}

func (m *Manager) ReadArtifact(ctx context.Context, taskID, id string) ([]byte, error) {
	if _, err := m.lookup(taskID); err != nil {
		return nil, err
	}
	decoded, err := hex.DecodeString(id)
	if err != nil || len(decoded) != sha256.Size || hex.EncodeToString(decoded) != id {
		return nil, ErrArtifactNotFound
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	raw, err := os.ReadFile(filepath.Join(m.journal.Root(), "pi-events", taskID, id+".json"))
	if errors.Is(err, os.ErrNotExist) {
		return nil, ErrArtifactNotFound
	}
	if err != nil {
		return nil, err
	}
	digest := sha256.Sum256(raw)
	if hex.EncodeToString(digest[:]) != id {
		return nil, fmt.Errorf("PI event artifact %s hash mismatch", id)
	}
	return raw, nil
}
