// Package eventclient is the authenticated loopback client for windows-event-stream.
package eventclient

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"

	"github.com/qoli/WindowsAgent/internal/eventstream"
	"github.com/qoli/WindowsAgent/internal/strictjson"
)

type Client struct {
	baseURL string
	token   string
	http    *http.Client
}

type replayResponse struct {
	Events       []eventstream.Event `json:"events"`
	NextCursor   uint64              `json:"nextCursor"`
	LastSequence uint64              `json:"lastSequence"`
}

func (c *Client) Health(ctx context.Context) error {
	if c == nil || ctx == nil {
		return errors.New("event client and context are required")
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+"/healthz", nil)
	if err != nil {
		return err
	}
	response, err := c.http.Do(request)
	if err != nil {
		return fmt.Errorf("check event service health: %w", err)
	}
	defer response.Body.Close()
	data, err := io.ReadAll(io.LimitReader(response.Body, 4097))
	if err != nil {
		return fmt.Errorf("read event service health: %w", err)
	}
	if len(data) > 4096 {
		return errors.New("event service health response exceeds bound")
	}
	if response.StatusCode != http.StatusOK {
		return fmt.Errorf("event service health returned HTTP %d", response.StatusCode)
	}
	var health struct {
		Status string `json:"status"`
	}
	if err := decodeStrict(data, &health); err != nil {
		return fmt.Errorf("decode event service health: %w", err)
	}
	if health.Status != "ok" {
		return fmt.Errorf("event service health status is %q", health.Status)
	}
	return nil
}

func New(baseURL, tokenFile string, client *http.Client) (*Client, error) {
	parsed, err := url.Parse(baseURL)
	if err != nil || parsed.Scheme != "http" || parsed.Host == "" || parsed.Path != "" || parsed.RawQuery != "" || parsed.Fragment != "" {
		return nil, errors.New("event API URL must be one canonical HTTP origin")
	}
	host, _, err := net.SplitHostPort(parsed.Host)
	if err != nil || net.ParseIP(host) == nil || !net.ParseIP(host).IsLoopback() {
		return nil, errors.New("event API URL must use an explicit loopback IP and port")
	}
	token, err := readToken(tokenFile)
	if err != nil {
		return nil, err
	}
	if client == nil {
		return nil, errors.New("HTTP client is required")
	}
	return &Client{baseURL: strings.TrimSuffix(baseURL, "/"), token: token, http: client}, nil
}

func (c *Client) Append(ctx context.Context, request eventstream.AppendRequest) (eventstream.Event, error) {
	if c == nil || ctx == nil {
		return eventstream.Event{}, errors.New("event client and context are required")
	}
	body, err := json.Marshal(request)
	if err != nil {
		return eventstream.Event{}, fmt.Errorf("encode event append: %w", err)
	}
	httpRequest, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/v1/events", bytes.NewReader(body))
	if err != nil {
		return eventstream.Event{}, err
	}
	httpRequest.Header.Set("Content-Type", "application/json")
	httpRequest.Header.Set("Authorization", "Bearer "+c.token)
	response, err := c.http.Do(httpRequest)
	if err != nil {
		return eventstream.Event{}, fmt.Errorf("append event: %w", err)
	}
	defer response.Body.Close()
	data, err := io.ReadAll(io.LimitReader(response.Body, eventstream.MaxEventBytes+1))
	if err != nil {
		return eventstream.Event{}, err
	}
	if len(data) > eventstream.MaxEventBytes {
		return eventstream.Event{}, errors.New("event append response exceeds bound")
	}
	if response.StatusCode != http.StatusCreated {
		return eventstream.Event{}, fmt.Errorf("event append returned HTTP %d: %s", response.StatusCode, strings.TrimSpace(string(data)))
	}
	var event eventstream.Event
	if err := decodeStrict(data, &event); err != nil {
		return eventstream.Event{}, fmt.Errorf("decode event append response: %w", err)
	}
	return event, nil
}

func (c *Client) LastSequence(ctx context.Context) (uint64, error) {
	if c == nil || ctx == nil {
		return 0, errors.New("event client and context are required")
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+"/v1/events?after=0&limit=1", nil)
	if err != nil {
		return 0, err
	}
	request.Header.Set("Authorization", "Bearer "+c.token)
	response, err := c.http.Do(request)
	if err != nil {
		return 0, fmt.Errorf("read event sequence: %w", err)
	}
	defer response.Body.Close()
	data, err := io.ReadAll(io.LimitReader(response.Body, eventstream.MaxEventBytes+1))
	if err != nil {
		return 0, fmt.Errorf("read event sequence response: %w", err)
	}
	if len(data) > eventstream.MaxEventBytes {
		return 0, errors.New("event sequence response exceeds bound")
	}
	if response.StatusCode != http.StatusOK {
		return 0, fmt.Errorf("event sequence returned HTTP %d: %s", response.StatusCode, strings.TrimSpace(string(data)))
	}
	var replay replayResponse
	if err := decodeStrict(data, &replay); err != nil {
		return 0, fmt.Errorf("decode event sequence response: %w", err)
	}
	return replay.LastSequence, nil
}

func (c *Client) Replay(ctx context.Context, after uint64, limit int) ([]eventstream.Event, uint64, uint64, error) {
	if c == nil || ctx == nil {
		return nil, 0, 0, errors.New("event client and context are required")
	}
	if limit < 1 || limit > eventstream.MaxReplayLimit {
		return nil, 0, 0, fmt.Errorf("event replay limit must be between 1 and %d", eventstream.MaxReplayLimit)
	}
	endpoint := c.baseURL + "/v1/events?after=" + strconv.FormatUint(after, 10) + "&limit=" + strconv.Itoa(limit)
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, 0, 0, err
	}
	request.Header.Set("Authorization", "Bearer "+c.token)
	response, err := c.http.Do(request)
	if err != nil {
		return nil, 0, 0, fmt.Errorf("replay events: %w", err)
	}
	defer response.Body.Close()
	maxResponseBytes := int64(eventstream.MaxEventBytes)*int64(limit) + 1
	data, err := io.ReadAll(io.LimitReader(response.Body, maxResponseBytes))
	if err != nil {
		return nil, 0, 0, fmt.Errorf("read event replay response: %w", err)
	}
	if int64(len(data)) >= maxResponseBytes {
		return nil, 0, 0, errors.New("event replay response exceeds bound")
	}
	if response.StatusCode != http.StatusOK {
		return nil, 0, 0, fmt.Errorf("event replay returned HTTP %d: %s", response.StatusCode, strings.TrimSpace(string(data)))
	}
	var replay replayResponse
	if err := decodeStrict(data, &replay); err != nil {
		return nil, 0, 0, fmt.Errorf("decode event replay response: %w", err)
	}
	return replay.Events, replay.NextCursor, replay.LastSequence, nil
}

func (c *Client) Stream(ctx context.Context, after uint64, visit func(eventstream.Event) error) error {
	if c == nil || ctx == nil || visit == nil {
		return errors.New("event client, context, and visitor are required")
	}
	endpoint := c.baseURL + "/v1/events/stream?after=" + strconv.FormatUint(after, 10)
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return err
	}
	request.Header.Set("Authorization", "Bearer "+c.token)
	response, err := c.http.Do(request)
	if err != nil {
		return fmt.Errorf("open event stream: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		data, _ := io.ReadAll(io.LimitReader(response.Body, 64<<10))
		return fmt.Errorf("event stream returned HTTP %d: %s", response.StatusCode, strings.TrimSpace(string(data)))
	}
	if mediaType := strings.Split(response.Header.Get("Content-Type"), ";")[0]; mediaType != "application/x-ndjson" {
		return fmt.Errorf("event stream Content-Type is %q", response.Header.Get("Content-Type"))
	}
	scanner := bufio.NewScanner(response.Body)
	scanner.Buffer(make([]byte, 64<<10), eventstream.MaxEventBytes)
	cursor := after
	for scanner.Scan() {
		var event eventstream.Event
		if err := decodeStrict(scanner.Bytes(), &event); err != nil {
			return fmt.Errorf("decode event stream record: %w", err)
		}
		if event.Sequence <= cursor {
			return fmt.Errorf("event stream sequence %d is not after cursor %d", event.Sequence, cursor)
		}
		cursor = event.Sequence
		if err := visit(event); err != nil {
			return err
		}
	}
	if err := scanner.Err(); err != nil {
		return fmt.Errorf("read event stream: %w", err)
	}
	return errors.New("event stream ended without cancellation")
}

func readToken(name string) (string, error) {
	if name == "" {
		return "", errors.New("event API token file is required")
	}
	info, err := os.Stat(name)
	if err != nil {
		return "", fmt.Errorf("stat event API token file: %w", err)
	}
	if !info.Mode().IsRegular() || info.Size() < 32 || info.Size() > 4096 {
		return "", errors.New("event API token file must be a regular file between 32 and 4096 bytes")
	}
	data, err := os.ReadFile(name)
	if err != nil {
		return "", fmt.Errorf("read event API token file: %w", err)
	}
	token := string(data)
	if strings.TrimSpace(token) != token {
		return "", errors.New("event API token file must not contain leading or trailing whitespace")
	}
	return token, nil
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
