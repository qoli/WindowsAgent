package ocrworker

import (
	"context"
	"crypto/sha256"
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
)

const textRegionsProtocolVersion = 1

const MaxTextRegionsFrameBytes = 8 << 20

type TextRegionPoint struct {
	X float64 `json:"x"`
	Y float64 `json:"y"`
}

type TextRegion struct {
	Points                []TextRegionPoint `json:"points"`
	DetectionConfidence   float64           `json:"detectionConfidence"`
	Text                  string            `json:"text"`
	RecognitionConfidence float64           `json:"recognitionConfidence"`
}

type TextRegionsModels struct {
	DetectionArtifactID    string `json:"detectionArtifactId"`
	RecognitionArtifactID  string `json:"recognitionArtifactId"`
	Provider               string `json:"provider"`
	AdapterIndex           int    `json:"adapterIndex"`
	DetectionInputWidth    int    `json:"detectionInputWidth"`
	DetectionInputHeight   int    `json:"detectionInputHeight"`
	RecognitionInputWidth  int    `json:"recognitionInputWidth"`
	RecognitionInputHeight int    `json:"recognitionInputHeight"`
}

type TextRegionsTiming struct {
	DetectionPreprocessMS    float64 `json:"detectionPreprocessMs"`
	DetectionInferenceMS     float64 `json:"detectionInferenceMs"`
	DetectionPostprocessMS   float64 `json:"detectionPostprocessMs"`
	RecognitionPreprocessMS  float64 `json:"recognitionPreprocessMs"`
	RecognitionInferenceMS   float64 `json:"recognitionInferenceMs"`
	RecognitionPostprocessMS float64 `json:"recognitionPostprocessMs"`
	TotalMS                  float64 `json:"totalMs"`
}

type TextRegionsResult struct {
	RequestID   string            `json:"requestId"`
	CompletedAt time.Time         `json:"completedAt"`
	Evidence    Evidence          `json:"evidence"`
	Models      TextRegionsModels `json:"models"`
	Timing      TextRegionsTiming `json:"timing"`
	Regions     []TextRegion      `json:"regions"`
}

type TextRegionsInitialized struct {
	Runtime      string  `json:"runtime"`
	Pipeline     string  `json:"pipeline"`
	Provider     string  `json:"provider"`
	AdapterIndex int     `json:"adapterIndex"`
	ProcessID    int     `json:"processId"`
	ModelLoadMS  float64 `json:"modelLoadMs"`
	Detection    struct {
		ArtifactID string `json:"artifactId"`
		Filename   string `json:"filename"`
		SHA256     string `json:"sha256"`
		InputWidth int    `json:"inputWidth"`
	} `json:"detectionModel"`
	Recognition struct {
		ArtifactID  string `json:"artifactId"`
		Filename    string `json:"filename"`
		SHA256      string `json:"sha256"`
		InputWidth  int    `json:"inputWidth"`
		InputHeight int    `json:"inputHeight"`
	} `json:"recognitionModel"`
}

type TextRegionsClient struct {
	mu          sync.Mutex
	command     *exec.Cmd
	stdin       io.WriteCloser
	stdout      io.ReadCloser
	stderr      *limitedBuffer
	nextID      uint64
	initialized TextRegionsInitialized
	closed      bool
}

func StartTextRegions(ctx context.Context, root string) (*TextRegionsClient, error) {
	if ctx == nil {
		return nil, errors.New("OCR text regions worker context is required")
	}
	if root == "" || !filepath.IsAbs(root) {
		return nil, errors.New("OCR runtime root must be absolute")
	}
	paths := map[string]string{
		"executable":        filepath.Join(root, "PpOcr.DirectML.exe"),
		"config":            filepath.Join(root, "text-regions-runtime-config.json"),
		"detection model":   filepath.Join(root, "ppocrv6-small-det.onnx"),
		"recognition model": filepath.Join(root, "ppocrv6-small-rec-w480.onnx"),
		"characters":        filepath.Join(root, "ppocrv6-small-characters.json"),
	}
	for label, name := range paths {
		info, err := os.Stat(name)
		if err != nil {
			return nil, fmt.Errorf("stat OCR text regions worker %s: %w", label, err)
		}
		if !info.Mode().IsRegular() {
			return nil, fmt.Errorf("OCR text regions worker %s must be a regular file", label)
		}
	}
	command := exec.CommandContext(
		ctx,
		paths["executable"],
		"--text-regions-worker",
		"--config", paths["config"],
		"--detection-model", paths["detection model"],
		"--recognition-model", paths["recognition model"],
		"--characters", paths["characters"],
	)
	configureWorkerCommand(command)
	stdin, err := command.StdinPipe()
	if err != nil {
		return nil, fmt.Errorf("open OCR text regions worker stdin: %w", err)
	}
	stdout, err := command.StdoutPipe()
	if err != nil {
		_ = stdin.Close()
		return nil, fmt.Errorf("open OCR text regions worker stdout: %w", err)
	}
	stderr := &limitedBuffer{limit: 64 << 10}
	command.Stderr = stderr
	if err := command.Start(); err != nil {
		_ = stdin.Close()
		_ = stdout.Close()
		return nil, fmt.Errorf("start OCR text regions worker: %w", err)
	}
	client := &TextRegionsClient{command: command, stdin: stdin, stdout: stdout, stderr: stderr}
	var initialized TextRegionsInitialized
	if err := client.call(ctx, "initialize", map[string]any{}, &initialized); err != nil {
		_ = command.Process.Kill()
		_ = command.Wait()
		return nil, fmt.Errorf("initialize OCR text regions worker: %w", err)
	}
	if initialized.Runtime != "ppocr-onnx-dml-text-regions-v1" ||
		initialized.Pipeline != "text-region-detection-recognition" ||
		initialized.Provider != "DirectML" || initialized.AdapterIndex != 0 || initialized.ProcessID <= 0 ||
		initialized.Detection.ArtifactID == "" || initialized.Detection.InputWidth != 1280 ||
		initialized.Recognition.ArtifactID == "" || initialized.Recognition.InputWidth != 480 ||
		initialized.Recognition.InputHeight != 48 {
		_ = command.Process.Kill()
		_ = command.Wait()
		return nil, errors.New("OCR text regions worker initialize response does not match the DirectML contract")
	}
	client.initialized = initialized
	return client, nil
}

func (c *TextRegionsClient) Initialized() TextRegionsInitialized {
	if c == nil {
		return TextRegionsInitialized{}
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.initialized
}

func (c *TextRegionsClient) DetectRecognize(ctx context.Context, request Request) (TextRegionsResult, error) {
	if c == nil {
		return TextRegionsResult{}, errors.New("OCR text regions worker client is required")
	}
	if request.RequestID == "" || request.ArtifactID == "" || request.CapturedAt.IsZero() ||
		request.Width <= 0 || request.Height <= 0 {
		return TextRegionsResult{}, errors.New("OCR text regions request identity, capture time, and dimensions are required")
	}
	expectedBytes := request.Width * request.Height * 3
	if expectedBytes <= 0 || len(request.RGB) != expectedBytes {
		return TextRegionsResult{}, fmt.Errorf("OCR text regions RGB byte length mismatch: expected=%d actual=%d", expectedBytes, len(request.RGB))
	}
	digest := sha256.Sum256(request.RGB)
	parameters := map[string]any{
		"requestId": request.RequestID, "artifactId": request.ArtifactID,
		"capturedAt": request.CapturedAt.UTC().Format("2006-01-02T15:04:05.000000Z07:00"),
		"width":      request.Width, "height": request.Height, "rgbBase64": request.RGB,
		"sha256": hex.EncodeToString(digest[:]),
	}
	var result TextRegionsResult
	if err := c.call(ctx, "detectRecognize", parameters, &result); err != nil {
		return TextRegionsResult{}, err
	}
	if result.RequestID != request.RequestID || result.Evidence.ArtifactID != request.ArtifactID ||
		result.Evidence.Width != request.Width || result.Evidence.Height != request.Height ||
		result.Evidence.RGBSHA256 != hex.EncodeToString(digest[:]) ||
		result.Models.Provider != "DirectML" || result.Models.AdapterIndex != 0 ||
		result.Models.DetectionArtifactID != c.initialized.Detection.ArtifactID ||
		result.Models.RecognitionArtifactID != c.initialized.Recognition.ArtifactID {
		return TextRegionsResult{}, errors.New("OCR text regions worker result provenance does not match the request or initialized models")
	}
	for index, region := range result.Regions {
		if len(region.Points) != 4 || region.DetectionConfidence < 0 || region.DetectionConfidence > 1 ||
			region.RecognitionConfidence < 0 || region.RecognitionConfidence > 1 {
			return TextRegionsResult{}, fmt.Errorf("OCR text regions worker returned invalid region %d", index)
		}
		for _, point := range region.Points {
			if point.X < 0 || point.Y < 0 || point.X > float64(request.Width-1) || point.Y > float64(request.Height-1) {
				return TextRegionsResult{}, fmt.Errorf("OCR text regions worker returned out-of-range point in region %d", index)
			}
		}
	}
	return result, nil
}

func (c *TextRegionsClient) call(ctx context.Context, method string, parameters any, target any) error {
	if ctx == nil {
		return errors.New("OCR text regions worker call context is required")
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.closed {
		return errors.New("OCR text regions worker is closed")
	}
	c.nextID++
	id := fmt.Sprintf("ocr-regions-%d", c.nextID)
	request := map[string]any{"schemaVersion": textRegionsProtocolVersion, "id": id, "method": method, "params": parameters}
	body, err := json.Marshal(request)
	if err != nil {
		return fmt.Errorf("encode OCR text regions worker request: %w", err)
	}
	result := make(chan error, 1)
	go func() {
		if err := writeFrameLimit(c.stdin, body, MaxTextRegionsFrameBytes); err != nil {
			result <- err
			return
		}
		responseBody, err := readFrameLimit(c.stdout, MaxTextRegionsFrameBytes)
		if err != nil {
			result <- err
			return
		}
		var envelope responseEnvelope
		if err := decodeStrict(responseBody, &envelope); err != nil {
			result <- fmt.Errorf("decode OCR text regions worker response: %w", err)
			return
		}
		if envelope.SchemaVersion != textRegionsProtocolVersion || envelope.ID != id {
			result <- errors.New("OCR text regions worker response version or ID mismatch")
			return
		}
		if !envelope.OK {
			if envelope.Error == nil || envelope.Error.Code == "" || envelope.Error.Message == "" {
				result <- errors.New("OCR text regions worker returned malformed error")
				return
			}
			result <- fmt.Errorf("OCR text regions worker %s: %s", envelope.Error.Code, envelope.Error.Message)
			return
		}
		if envelope.Error != nil || len(envelope.Result) == 0 {
			result <- errors.New("OCR text regions worker success response is malformed")
			return
		}
		if err := decodeStrict(envelope.Result, target); err != nil {
			result <- fmt.Errorf("decode OCR text regions worker result: %w", err)
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

func (c *TextRegionsClient) Close() error {
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
		return errors.New("OCR text regions worker shutdown response is invalid")
	}
	if waitErr != nil {
		return fmt.Errorf("wait for OCR text regions worker shutdown: %w: %s", waitErr, c.stderr.String())
	}
	return nil
}
