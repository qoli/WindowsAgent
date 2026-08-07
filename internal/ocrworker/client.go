// Package ocrworker owns the bounded resident PP-OCR DirectML worker client.
package ocrworker

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"time"

	"github.com/qoli/WindowsAgent/internal/strictjson"
)

const (
	ProtocolVersion = 1
	MaxFrameBytes   = 512 << 10
)

type Request struct {
	RequestID  string
	ArtifactID string
	CapturedAt time.Time
	Width      int
	Height     int
	RGB        []byte
}

type Evidence struct {
	ArtifactID string    `json:"artifactId"`
	CapturedAt time.Time `json:"capturedAt"`
	Width      int       `json:"width"`
	Height     int       `json:"height"`
	RGBSHA256  string    `json:"rgbSha256"`
}

type Model struct {
	ArtifactID   string `json:"artifactId"`
	Provider     string `json:"provider"`
	AdapterIndex int    `json:"adapterIndex"`
	InputWidth   int    `json:"inputWidth"`
	InputHeight  int    `json:"inputHeight"`
}

type Timing struct {
	PreprocessMS  float64 `json:"preprocessMs"`
	InferenceMS   float64 `json:"inferenceMs"`
	PostprocessMS float64 `json:"postprocessMs"`
	TotalMS       float64 `json:"totalMs"`
}

type Result struct {
	RequestID   string    `json:"requestId"`
	CompletedAt time.Time `json:"completedAt"`
	Text        string    `json:"text"`
	Confidence  float64   `json:"confidence"`
	Evidence    Evidence  `json:"evidence"`
	Model       Model     `json:"model"`
	Timing      Timing    `json:"timing"`
}

type Initialized struct {
	Runtime      string  `json:"runtime"`
	Pipeline     string  `json:"pipeline"`
	Provider     string  `json:"provider"`
	AdapterIndex int     `json:"adapterIndex"`
	ProcessID    int     `json:"processId"`
	ModelLoadMS  float64 `json:"modelLoadMs"`
	Model        struct {
		ArtifactID  string `json:"artifactId"`
		Filename    string `json:"filename"`
		SHA256      string `json:"sha256"`
		InputWidth  int    `json:"inputWidth"`
		InputHeight int    `json:"inputHeight"`
	} `json:"model"`
}

type responseEnvelope struct {
	SchemaVersion int             `json:"schemaVersion"`
	ID            string          `json:"id"`
	OK            bool            `json:"ok"`
	Result        json.RawMessage `json:"result,omitempty"`
	Error         *struct {
		Code    string `json:"code"`
		Message string `json:"message"`
	} `json:"error,omitempty"`
}

type Client struct {
	mu          sync.Mutex
	command     *exec.Cmd
	stdin       io.WriteCloser
	stdout      io.ReadCloser
	stderr      *limitedBuffer
	nextID      uint64
	initialized Initialized
	closed      bool
}

func Start(ctx context.Context, root string) (*Client, error) {
	if ctx == nil {
		return nil, errors.New("OCR worker context is required")
	}
	if root == "" || !filepath.IsAbs(root) {
		return nil, errors.New("OCR runtime root must be absolute")
	}
	paths := map[string]string{
		"executable": filepath.Join(root, "PpOcr.DirectML.exe"),
		"config":     filepath.Join(root, "runtime-config.json"),
		"model":      filepath.Join(root, "ppocrv6-small-rec-w480.onnx"),
		"characters": filepath.Join(root, "ppocrv6-small-characters.json"),
	}
	for label, name := range paths {
		info, err := os.Stat(name)
		if err != nil {
			return nil, fmt.Errorf("stat OCR worker %s: %w", label, err)
		}
		if !info.Mode().IsRegular() {
			return nil, fmt.Errorf("OCR worker %s must be a regular file", label)
		}
	}
	command := exec.CommandContext(
		ctx, paths["executable"], "--worker",
		"--config", paths["config"], "--model", paths["model"], "--characters", paths["characters"],
	)
	configureWorkerCommand(command)
	stdin, err := command.StdinPipe()
	if err != nil {
		return nil, fmt.Errorf("open OCR worker stdin: %w", err)
	}
	stdout, err := command.StdoutPipe()
	if err != nil {
		_ = stdin.Close()
		return nil, fmt.Errorf("open OCR worker stdout: %w", err)
	}
	stderr := &limitedBuffer{limit: 64 << 10}
	command.Stderr = stderr
	if err := command.Start(); err != nil {
		_ = stdin.Close()
		_ = stdout.Close()
		return nil, fmt.Errorf("start OCR worker: %w", err)
	}
	client := &Client{command: command, stdin: stdin, stdout: stdout, stderr: stderr}
	var initialized Initialized
	if err := client.call(ctx, "initialize", map[string]any{}, &initialized); err != nil {
		_ = command.Process.Kill()
		_ = command.Wait()
		return nil, fmt.Errorf("initialize OCR worker: %w", err)
	}
	if initialized.Runtime != "ppocr-onnx-dml-v1" || initialized.Pipeline != "text-line-recognition" ||
		initialized.Provider != "DirectML" || initialized.AdapterIndex != 0 || initialized.ProcessID <= 0 ||
		initialized.Model.ArtifactID == "" || initialized.Model.InputWidth != 480 || initialized.Model.InputHeight != 48 {
		_ = command.Process.Kill()
		_ = command.Wait()
		return nil, errors.New("OCR worker initialize response does not match the w480 DirectML contract")
	}
	client.initialized = initialized
	return client, nil
}

func (c *Client) Initialized() Initialized {
	if c == nil {
		return Initialized{}
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.initialized
}

func (c *Client) Recognize(ctx context.Context, request Request) (Result, error) {
	if c == nil {
		return Result{}, errors.New("OCR worker client is required")
	}
	if request.RequestID == "" || request.ArtifactID == "" || request.CapturedAt.IsZero() ||
		request.Width <= 0 || request.Height <= 0 {
		return Result{}, errors.New("OCR request identity, capture time, and dimensions are required")
	}
	expectedBytes := request.Width * request.Height * 3
	if expectedBytes <= 0 || len(request.RGB) != expectedBytes {
		return Result{}, fmt.Errorf("OCR RGB byte length mismatch: expected=%d actual=%d", expectedBytes, len(request.RGB))
	}
	digest := sha256.Sum256(request.RGB)
	parameters := map[string]any{
		"requestId": request.RequestID, "artifactId": request.ArtifactID,
		"capturedAt": request.CapturedAt.UTC().Format("2006-01-02T15:04:05.000000Z07:00"),
		"width":      request.Width, "height": request.Height, "rgbBase64": request.RGB,
		"sha256": hex.EncodeToString(digest[:]),
	}
	var result Result
	if err := c.call(ctx, "recognize", parameters, &result); err != nil {
		return Result{}, err
	}
	if result.RequestID != request.RequestID || result.Evidence.ArtifactID != request.ArtifactID ||
		result.Evidence.Width != request.Width || result.Evidence.Height != request.Height ||
		result.Evidence.RGBSHA256 != hex.EncodeToString(digest[:]) || result.Model.Provider != "DirectML" ||
		result.Model.AdapterIndex != 0 || result.Model.ArtifactID != c.initialized.Model.ArtifactID {
		return Result{}, errors.New("OCR worker result provenance does not match the request or initialized model")
	}
	return result, nil
}

func (c *Client) call(ctx context.Context, method string, parameters any, target any) error {
	if ctx == nil {
		return errors.New("OCR worker call context is required")
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.closed {
		return errors.New("OCR worker is closed")
	}
	c.nextID++
	id := fmt.Sprintf("ocr-%d", c.nextID)
	request := map[string]any{"schemaVersion": ProtocolVersion, "id": id, "method": method, "params": parameters}
	body, err := json.Marshal(request)
	if err != nil {
		return fmt.Errorf("encode OCR worker request: %w", err)
	}
	result := make(chan error, 1)
	go func() {
		if err := writeFrame(c.stdin, body); err != nil {
			result <- err
			return
		}
		responseBody, err := readFrame(c.stdout)
		if err != nil {
			result <- err
			return
		}
		var envelope responseEnvelope
		if err := decodeStrict(responseBody, &envelope); err != nil {
			result <- fmt.Errorf("decode OCR worker response: %w", err)
			return
		}
		if envelope.SchemaVersion != ProtocolVersion || envelope.ID != id {
			result <- errors.New("OCR worker response version or ID mismatch")
			return
		}
		if !envelope.OK {
			if envelope.Error == nil || envelope.Error.Code == "" || envelope.Error.Message == "" {
				result <- errors.New("OCR worker returned malformed error")
				return
			}
			result <- fmt.Errorf("OCR worker %s: %s", envelope.Error.Code, envelope.Error.Message)
			return
		}
		if envelope.Error != nil || len(envelope.Result) == 0 {
			result <- errors.New("OCR worker success response is malformed")
			return
		}
		if err := decodeStrict(envelope.Result, target); err != nil {
			result <- fmt.Errorf("decode OCR worker result: %w", err)
			return
		}
		result <- nil
	}()
	select {
	case err := <-result:
		return err
	case <-ctx.Done():
		_ = c.command.Process.Kill()
		return ctx.Err()
	}
}

func (c *Client) Close() error {
	if c == nil {
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	var stopped struct {
		State string `json:"state"`
	}
	err := c.call(ctx, "shutdown", map[string]any{}, &stopped)
	c.mu.Lock()
	c.closed = true
	c.mu.Unlock()
	_ = c.stdin.Close()
	_ = c.stdout.Close()
	waitErr := c.command.Wait()
	if err != nil {
		return err
	}
	if stopped.State != "stopped" {
		return errors.New("OCR worker shutdown response is invalid")
	}
	if waitErr != nil {
		return fmt.Errorf("wait for OCR worker shutdown: %w: %s", waitErr, c.stderr.String())
	}
	return nil
}

func writeFrame(writer io.Writer, body []byte) error {
	return writeFrameLimit(writer, body, MaxFrameBytes)
}

func writeFrameLimit(writer io.Writer, body []byte, limit int) error {
	if len(body) == 0 || len(body) > limit {
		return fmt.Errorf("OCR worker frame length must be from 1 through %d", limit)
	}
	header := make([]byte, 4)
	binary.LittleEndian.PutUint32(header, uint32(len(body)))
	if _, err := writer.Write(header); err != nil {
		return fmt.Errorf("write OCR worker frame header: %w", err)
	}
	if _, err := writer.Write(body); err != nil {
		return fmt.Errorf("write OCR worker frame body: %w", err)
	}
	return nil
}

func readFrame(reader io.Reader) ([]byte, error) {
	return readFrameLimit(reader, MaxFrameBytes)
}

func readFrameLimit(reader io.Reader, limit int) ([]byte, error) {
	header := make([]byte, 4)
	if _, err := io.ReadFull(reader, header); err != nil {
		return nil, fmt.Errorf("read OCR worker frame header: %w", err)
	}
	length := int(binary.LittleEndian.Uint32(header))
	if length <= 0 || length > limit {
		return nil, fmt.Errorf("OCR worker frame length must be from 1 through %d", limit)
	}
	body := make([]byte, length)
	if _, err := io.ReadFull(reader, body); err != nil {
		return nil, fmt.Errorf("read OCR worker frame body: %w", err)
	}
	return body, nil
}

func decodeStrict(data []byte, target any) error {
	if err := strictjson.Validate(data); err != nil {
		return err
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return err
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		return errors.New("multiple JSON values are forbidden")
	}
	return nil
}

type limitedBuffer struct {
	mu    sync.Mutex
	data  bytes.Buffer
	limit int
}

func (b *limitedBuffer) Write(value []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.data.Len()+len(value) > b.limit {
		return 0, errors.New("OCR worker stderr exceeded its bound")
	}
	return b.data.Write(value)
}

func (b *limitedBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.data.String()
}
