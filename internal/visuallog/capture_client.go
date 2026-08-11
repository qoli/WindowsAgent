package visuallog

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
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

type Frame struct {
	CaptureID          string
	ObservedAt         time.Time
	ContentType        string
	Content            []byte
	Foreground         foreground.Info
	ForegroundRevision uint64
}

type CaptureSource interface {
	Capture(context.Context) (Frame, error)
}

type CaptureClient struct {
	baseURL          string
	http             *http.Client
	profile          capture.Profile
	targetExecutable string
}

type captureMetadata struct {
	ID          string           `json:"id"`
	CreatedAt   time.Time        `json:"created_at"`
	Profile     capture.Profile  `json:"profile"`
	ContentType string           `json:"content_type"`
	Bytes       int64            `json:"bytes"`
	SHA256      string           `json:"sha256"`
	Foreground  foreground.Info  `json:"foreground"`
	Rule        rules.Resolution `json:"rule"`
	ContentURL  string           `json:"content_url"`
}

func NewCaptureClient(baseURL string, client *http.Client, profile capture.Profile, targetExecutable string) (*CaptureClient, error) {
	canonical, err := canonicalLoopbackOrigin(baseURL, "capture API")
	if err != nil {
		return nil, err
	}
	if client == nil {
		return nil, errors.New("capture HTTP client is required")
	}
	if _, err := capture.ParseProfile(string(profile)); err != nil {
		return nil, err
	}
	if strings.ContainsAny(targetExecutable, `/\\`) || !strings.HasSuffix(strings.ToLower(targetExecutable), ".exe") {
		return nil, errors.New("capture target executable must be one executable name ending in .exe")
	}
	return &CaptureClient{baseURL: canonical, http: client, profile: profile, targetExecutable: targetExecutable}, nil
}

func (c *CaptureClient) Capture(ctx context.Context) (Frame, error) {
	if c == nil || ctx == nil {
		return Frame{}, errors.New("capture client and context are required")
	}
	body, err := json.Marshal(map[string]any{"profile": c.profile, "include_cursor": false})
	if err != nil {
		return Frame{}, err
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/v1/captures", bytes.NewReader(body))
	if err != nil {
		return Frame{}, err
	}
	request.Header.Set("Content-Type", "application/json")
	response, err := c.http.Do(request)
	if err != nil {
		return Frame{}, fmt.Errorf("request visual log capture: %w", err)
	}
	defer response.Body.Close()
	data, err := io.ReadAll(io.LimitReader(response.Body, 1<<20))
	if err != nil {
		return Frame{}, fmt.Errorf("read visual log capture metadata: %w", err)
	}
	if response.StatusCode != http.StatusCreated {
		return Frame{}, fmt.Errorf("visual log capture returned HTTP %d: %s", response.StatusCode, strings.TrimSpace(string(data)))
	}
	var metadata captureMetadata
	if err := json.Unmarshal(data, &metadata); err != nil {
		return Frame{}, fmt.Errorf("decode visual log capture metadata: %w", err)
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
		return Frame{}, fmt.Errorf("download visual log capture %s: %w", metadata.ID, err)
	}
	defer contentResponse.Body.Close()
	if contentResponse.StatusCode != http.StatusOK {
		return Frame{}, fmt.Errorf("visual log capture content returned HTTP %d", contentResponse.StatusCode)
	}
	content, err := io.ReadAll(io.LimitReader(contentResponse.Body, MaxFrameBytes+1))
	if err != nil {
		return Frame{}, fmt.Errorf("read visual log capture content: %w", err)
	}
	if len(content) > MaxFrameBytes {
		return Frame{}, fmt.Errorf("visual log capture exceeds %d bytes", MaxFrameBytes)
	}
	if int64(len(content)) != metadata.Bytes {
		return Frame{}, errors.New("visual log capture byte length does not match metadata")
	}
	sum := sha256.Sum256(content)
	if hex.EncodeToString(sum[:]) != metadata.SHA256 {
		return Frame{}, errors.New("visual log capture SHA-256 does not match metadata")
	}
	return Frame{
		CaptureID: metadata.ID, ObservedAt: metadata.Foreground.ObservedAt.UTC(), ContentType: metadata.ContentType,
		Content: content, Foreground: metadata.Foreground, ForegroundRevision: 1,
	}, nil
}

func (c *CaptureClient) validateMetadata(metadata captureMetadata) error {
	if metadata.ID == "" || metadata.CreatedAt.IsZero() || metadata.Foreground.ObservedAt.IsZero() {
		return errors.New("visual log capture metadata is missing identity or timestamps")
	}
	if metadata.Profile != c.profile {
		return fmt.Errorf("visual log capture profile is %q, expected %q", metadata.Profile, c.profile)
	}
	if metadata.ContentType != "image/jpeg" || metadata.Bytes < 1 || metadata.Bytes > MaxFrameBytes {
		return errors.New("visual log capture metadata does not describe a bounded JPEG")
	}
	if len(metadata.SHA256) != 64 {
		return errors.New("visual log capture metadata SHA-256 is invalid")
	}
	if metadata.Foreground.ExecutableName != c.targetExecutable || metadata.Rule.Status != rules.StatusMatched || metadata.Rule.ID != c.targetExecutable {
		return fmt.Errorf("visual log capture foreground Rule is not %s", c.targetExecutable)
	}
	if metadata.ContentURL != "/v1/captures/"+metadata.ID+"/content" {
		return errors.New("visual log capture content URL does not match capture identity")
	}
	return nil
}

func canonicalLoopbackOrigin(raw, label string) (string, error) {
	parsed, err := url.Parse(raw)
	if err != nil || parsed.Scheme != "http" || parsed.Host == "" || parsed.Path != "" || parsed.RawQuery != "" || parsed.Fragment != "" {
		return "", fmt.Errorf("%s URL must be one canonical HTTP origin", label)
	}
	host, _, err := net.SplitHostPort(parsed.Host)
	if err != nil {
		return "", fmt.Errorf("%s URL must include an explicit port", label)
	}
	ip := net.ParseIP(host)
	if ip == nil || !ip.IsLoopback() {
		return "", fmt.Errorf("%s URL must use an explicit loopback IP", label)
	}
	return strings.TrimSuffix(raw, "/"), nil
}
