package visuallog

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/qoli/WindowsAgent/internal/strictjson"
)

const MaxModelResponseBytes = 1 << 20

type Description struct {
	Text    string
	ModelID string
	Latency time.Duration
}

type Describer interface {
	Describe(context.Context, Frame) (Description, error)
}

type ModelClient struct {
	endpoint string
	apiKey   string
	http     *http.Client
	config   Config
}

func NewModelClient(baseURL, apiKey string, client *http.Client, config Config) (*ModelClient, error) {
	parsed, err := url.Parse(baseURL)
	if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Host == "" || parsed.RawQuery != "" || parsed.Fragment != "" {
		return nil, errors.New("model API base URL must be one canonical HTTP or HTTPS URL")
	}
	if parsed.Path != "" && parsed.Path != "/" && parsed.Path != "/v1" && parsed.Path != "/v1/" {
		return nil, errors.New("model API base URL path must be empty or /v1")
	}
	if strings.TrimSpace(apiKey) == "" || strings.TrimSpace(apiKey) != apiKey {
		return nil, errors.New("model API key is required and must be canonical")
	}
	if client == nil {
		return nil, errors.New("model HTTP client is required")
	}
	if err := config.Validate(); err != nil {
		return nil, err
	}
	root := strings.TrimSuffix(baseURL, "/")
	if !strings.HasSuffix(root, "/v1") {
		root += "/v1"
	}
	return &ModelClient{endpoint: root + "/chat/completions", apiKey: apiKey, http: client, config: config}, nil
}

func (c *ModelClient) Describe(ctx context.Context, frame Frame) (Description, error) {
	if c == nil || ctx == nil {
		return Description{}, errors.New("model client and context are required")
	}
	if frame.ContentType != "image/jpeg" || len(frame.Content) == 0 {
		return Description{}, errors.New("visual log model input must be a non-empty JPEG")
	}
	encoded := base64.StdEncoding.EncodeToString(frame.Content)
	body := map[string]any{
		"model": c.config.Model.ID,
		"messages": []any{
			map[string]any{"role": "system", "content": c.config.Prompt},
			map[string]any{"role": "user", "content": []any{
				map[string]any{"type": "image_url", "image_url": map[string]any{"url": "data:image/jpeg;base64," + encoded}},
				map[string]any{"type": "text", "text": "Describe this image."},
			}},
		},
		"temperature":          c.config.Model.Temperature,
		"top_p":                c.config.Model.TopP,
		"top_k":                c.config.Model.TopK,
		"max_tokens":           c.config.Model.MaxTokens,
		"chat_template_kwargs": map[string]any{"enable_thinking": false},
		"thinking_budget":      0,
		"response_format": map[string]any{
			"type": "json_schema",
			"json_schema": map[string]any{
				"name":   "screen_description",
				"strict": true,
				"schema": map[string]any{
					"type":                 "object",
					"properties":           map[string]any{"description": map[string]any{"type": "string"}},
					"required":             []string{"description"},
					"additionalProperties": false,
				},
			},
		},
	}
	encodedBody, err := json.Marshal(body)
	if err != nil {
		return Description{}, fmt.Errorf("encode model request: %w", err)
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, c.endpoint, bytes.NewReader(encodedBody))
	if err != nil {
		return Description{}, err
	}
	request.Header.Set("Authorization", "Bearer "+c.apiKey)
	request.Header.Set("Content-Type", "application/json")
	started := time.Now()
	response, err := c.http.Do(request)
	if err != nil {
		return Description{}, fmt.Errorf("request visual description: %w", err)
	}
	defer response.Body.Close()
	data, err := io.ReadAll(io.LimitReader(response.Body, MaxModelResponseBytes+1))
	if err != nil {
		return Description{}, fmt.Errorf("read visual description response: %w", err)
	}
	if len(data) > MaxModelResponseBytes {
		return Description{}, fmt.Errorf("visual description response exceeds %d bytes", MaxModelResponseBytes)
	}
	if response.StatusCode != http.StatusOK {
		return Description{}, fmt.Errorf("visual description returned HTTP %d: %s", response.StatusCode, strings.TrimSpace(string(data)))
	}
	var completion struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
			FinishReason string `json:"finish_reason"`
		} `json:"choices"`
	}
	if err := json.Unmarshal(data, &completion); err != nil {
		return Description{}, fmt.Errorf("decode visual description response: %w", err)
	}
	if len(completion.Choices) != 1 || completion.Choices[0].FinishReason != "stop" {
		return Description{}, errors.New("visual description response must contain one stopped choice")
	}
	content := []byte(completion.Choices[0].Message.Content)
	if err := strictjson.Validate(content); err != nil {
		return Description{}, fmt.Errorf("visual description content must be strict JSON: %w", err)
	}
	decoder := json.NewDecoder(bytes.NewReader(content))
	decoder.DisallowUnknownFields()
	var output struct {
		Description string `json:"description"`
	}
	if err := decoder.Decode(&output); err != nil {
		return Description{}, fmt.Errorf("decode visual description content: %w", err)
	}
	if strings.TrimSpace(output.Description) == "" || strings.TrimSpace(output.Description) != output.Description {
		return Description{}, errors.New("visual description must be non-empty and canonical")
	}
	wordCount := len(strings.Fields(output.Description))
	if wordCount < int(c.config.Output.DescriptionMinWords) || wordCount > int(c.config.Output.DescriptionMaxWords) {
		return Description{}, fmt.Errorf("visual description must contain %d through %d words, got %d",
			c.config.Output.DescriptionMinWords, c.config.Output.DescriptionMaxWords, wordCount)
	}
	return Description{Text: output.Description, ModelID: c.config.Model.ID, Latency: time.Since(started)}, nil
}
