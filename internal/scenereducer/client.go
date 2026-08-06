package scenereducer

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/qoli/WindowsAgent/internal/eventstream"
)

type Client struct {
	baseURL string
	token   string
	http    *http.Client
}

type ReplayResponse struct {
	Events       []eventstream.Event `json:"events"`
	NextCursor   uint64              `json:"nextCursor"`
	LastSequence uint64              `json:"lastSequence"`
}

func NewClient(baseURL, token string) (*Client, error) {
	parsed, err := url.Parse(baseURL)
	if err != nil || parsed.Scheme != "http" || parsed.Hostname() != "127.0.0.1" || parsed.Port() == "" || parsed.Path != "" || parsed.RawQuery != "" || parsed.Fragment != "" {
		return nil, errors.New("event base URL must use exact http://127.0.0.1:<port> form")
	}
	if len(token) < 32 || strings.TrimSpace(token) != token {
		return nil, errors.New("event API token must be canonical and at least 32 bytes")
	}
	return &Client{baseURL: strings.TrimSuffix(baseURL, "/"), token: token, http: &http.Client{Timeout: 15 * time.Second}}, nil
}

func (c *Client) Replay(ctx context.Context, after uint64, limit int) (ReplayResponse, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+"/v1/events?after="+strconv.FormatUint(after, 10)+"&limit="+strconv.Itoa(limit), nil)
	if err != nil {
		return ReplayResponse{}, err
	}
	c.authorize(request)
	response, err := c.http.Do(request)
	if err != nil {
		return ReplayResponse{}, fmt.Errorf("replay events: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return ReplayResponse{}, responseError("replay events", response)
	}
	var result ReplayResponse
	decoder := json.NewDecoder(io.LimitReader(response.Body, 16<<20))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&result); err != nil {
		return ReplayResponse{}, fmt.Errorf("decode replay response: %w", err)
	}
	return result, nil
}

func (c *Client) Append(ctx context.Context, requestBody eventstream.AppendRequest) (eventstream.Event, error) {
	data, err := json.Marshal(requestBody)
	if err != nil {
		return eventstream.Event{}, fmt.Errorf("encode append request: %w", err)
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/v1/events", bytes.NewReader(data))
	if err != nil {
		return eventstream.Event{}, err
	}
	request.Header.Set("Content-Type", "application/json")
	c.authorize(request)
	response, err := c.http.Do(request)
	if err != nil {
		return eventstream.Event{}, fmt.Errorf("append reduced event: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusCreated {
		return eventstream.Event{}, responseError("append reduced event", response)
	}
	var event eventstream.Event
	decoder := json.NewDecoder(io.LimitReader(response.Body, 2<<20))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&event); err != nil {
		return eventstream.Event{}, fmt.Errorf("decode append response: %w", err)
	}
	return event, nil
}

func (c *Client) Stream(ctx context.Context, after uint64) (io.ReadCloser, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+"/v1/events/stream?after="+strconv.FormatUint(after, 10), nil)
	if err != nil {
		return nil, err
	}
	c.authorize(request)
	streamClient := *c.http
	streamClient.Timeout = 0
	response, err := streamClient.Do(request)
	if err != nil {
		return nil, fmt.Errorf("open event stream: %w", err)
	}
	if response.StatusCode != http.StatusOK {
		defer response.Body.Close()
		return nil, responseError("open event stream", response)
	}
	if mediaType := strings.Split(response.Header.Get("Content-Type"), ";")[0]; mediaType != "application/x-ndjson" {
		response.Body.Close()
		return nil, fmt.Errorf("event stream content type is %q", response.Header.Get("Content-Type"))
	}
	return response.Body, nil
}

func (c *Client) authorize(request *http.Request) {
	request.Header.Set("Authorization", "Bearer "+c.token)
}

func responseError(operation string, response *http.Response) error {
	data, _ := io.ReadAll(io.LimitReader(response.Body, 64<<10))
	return fmt.Errorf("%s: HTTP %d: %s", operation, response.StatusCode, strings.TrimSpace(string(data)))
}
