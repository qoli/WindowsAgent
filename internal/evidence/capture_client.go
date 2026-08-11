package evidence

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"image/jpeg"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/qoli/WindowsAgent/internal/capture"
	"github.com/qoli/WindowsAgent/internal/foreground"
	"github.com/qoli/WindowsAgent/internal/rules"
)

const MaxFrameBytes = 32 << 20

type CaptureClient struct {
	baseURL          string
	http             *http.Client
	targetExecutable string
}
type captureMetadata struct {
	ID          string           `json:"id"`
	CreatedAt   time.Time        `json:"created_at"`
	Profile     capture.Profile  `json:"profile"`
	ContentType string           `json:"content_type"`
	Bytes       int64            `json:"bytes"`
	SHA256      string           `json:"sha256"`
	Width       int              `json:"width"`
	Height      int              `json:"height"`
	Foreground  foreground.Info  `json:"foreground"`
	Rule        rules.Resolution `json:"rule"`
	ContentURL  string           `json:"content_url"`
}

func NewCaptureClient(baseURL string, client *http.Client, targetExecutable string) (*CaptureClient, error) {
	parsed, err := url.Parse(baseURL)
	if err != nil || parsed.Scheme != "http" || parsed.Path != "" || parsed.RawQuery != "" || parsed.Fragment != "" {
		return nil, errors.New("capture API URL must be one canonical HTTP origin")
	}
	host, _, err := net.SplitHostPort(parsed.Host)
	if err != nil || net.ParseIP(host) == nil || !net.ParseIP(host).IsLoopback() {
		return nil, errors.New("capture API URL must use an explicit loopback IP and port")
	}
	if client == nil {
		return nil, errors.New("capture HTTP client is required")
	}
	if strings.ContainsAny(targetExecutable, `/\\`) || !strings.HasSuffix(strings.ToLower(targetExecutable), ".exe") {
		return nil, errors.New("capture target executable is invalid")
	}
	return &CaptureClient{baseURL: strings.TrimSuffix(baseURL, "/"), http: client, targetExecutable: targetExecutable}, nil
}
func (c *CaptureClient) Capture(ctx context.Context) (Frame, error) {
	body, _ := json.Marshal(map[string]any{"profile": capture.Profile1080pJPEG, "include_cursor": false})
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/v1/captures", bytes.NewReader(body))
	if err != nil {
		return Frame{}, err
	}
	request.Header.Set("Content-Type", "application/json")
	response, err := c.http.Do(request)
	if err != nil {
		return Frame{}, fmt.Errorf("request evidence capture: %w", err)
	}
	defer response.Body.Close()
	data, err := io.ReadAll(io.LimitReader(response.Body, 1<<20))
	if err != nil {
		return Frame{}, err
	}
	if response.StatusCode != http.StatusCreated {
		return Frame{}, fmt.Errorf("evidence capture returned HTTP %d: %s", response.StatusCode, strings.TrimSpace(string(data)))
	}
	var metadata captureMetadata
	if err = json.Unmarshal(data, &metadata); err != nil {
		return Frame{}, err
	}
	if err := c.validateMetadata(metadata); err != nil {
		return Frame{}, err
	}
	contentRequest, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+metadata.ContentURL, nil)
	if err != nil {
		return Frame{}, err
	}
	contentResponse, err := c.http.Do(contentRequest)
	if err != nil {
		return Frame{}, err
	}
	defer contentResponse.Body.Close()
	if contentResponse.StatusCode != http.StatusOK {
		return Frame{}, fmt.Errorf("evidence content returned HTTP %d", contentResponse.StatusCode)
	}
	content, err := io.ReadAll(io.LimitReader(contentResponse.Body, MaxFrameBytes+1))
	if err != nil {
		return Frame{}, err
	}
	sum := sha256.Sum256(content)
	digest := hex.EncodeToString(sum[:])
	imageConfig, decodeErr := jpeg.DecodeConfig(bytes.NewReader(content))
	if len(content) > MaxFrameBytes || int64(len(content)) != metadata.Bytes || digest != metadata.SHA256 || decodeErr != nil || imageConfig.Width != 1920 || imageConfig.Height != 1080 {
		return Frame{}, errors.New("evidence capture content integrity validation failed")
	}
	return Frame{CaptureID: metadata.ID, ObservedAt: metadata.Foreground.ObservedAt.UTC(), ContentType: metadata.ContentType, Content: content, Width: metadata.Width, Height: metadata.Height, SHA256: digest}, nil
}

func (c *CaptureClient) validateMetadata(metadata captureMetadata) error {
	if metadata.ID == "" || metadata.CreatedAt.IsZero() || metadata.Foreground.ObservedAt.IsZero() {
		return errors.New("evidence capture metadata is missing identity or timestamps")
	}
	if metadata.Profile != capture.Profile1080pJPEG {
		return fmt.Errorf("evidence capture profile is %q", metadata.Profile)
	}
	if metadata.ContentType != "image/jpeg" || metadata.Bytes < 1 || metadata.Bytes > MaxFrameBytes {
		return errors.New("evidence capture metadata does not describe a bounded JPEG")
	}
	if metadata.Width != 1920 || metadata.Height != 1080 {
		return fmt.Errorf("evidence capture dimensions are %dx%d", metadata.Width, metadata.Height)
	}
	if metadata.Foreground.ExecutableName != c.targetExecutable {
		return fmt.Errorf("evidence capture foreground is %q, expected %q", metadata.Foreground.ExecutableName, c.targetExecutable)
	}
	if metadata.Rule.Status != rules.StatusMatched || metadata.Rule.ID != c.targetExecutable {
		return fmt.Errorf("evidence capture Rule is status=%q id=%q", metadata.Rule.Status, metadata.Rule.ID)
	}
	if metadata.ContentURL != "/v1/captures/"+metadata.ID+"/content" {
		return errors.New("evidence capture content URL does not match identity")
	}
	return nil
}
