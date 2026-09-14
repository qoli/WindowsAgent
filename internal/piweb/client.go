// Package piweb adapts the loopback PI WEB session API used by windows-agent-pi.
package piweb

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"

	"github.com/coder/websocket"
	"github.com/qoli/WindowsAgent/internal/strictjson"
)

const maxResponseBytes = 1 << 20

type Session struct {
	ID  string `json:"id"`
	CWD string `json:"cwd"`
}

type Event struct {
	Type string          `json:"type"`
	Seq  uint64          `json:"seq,omitempty"`
	Raw  json.RawMessage `json:"-"`
}

type EventStream struct {
	Events <-chan Event
	Errors <-chan error
}

type Client struct {
	baseURL *url.URL
	http    *http.Client
}

func New(baseURL string, httpClient *http.Client) (*Client, error) {
	if httpClient == nil {
		return nil, errors.New("HTTP client is required")
	}
	parsed, err := url.Parse(baseURL)
	if err != nil {
		return nil, fmt.Errorf("parse PI WEB URL: %w", err)
	}
	if parsed.Scheme != "http" || parsed.Host == "" || parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" {
		return nil, errors.New("PI WEB URL must be an absolute loopback http URL without credentials, query, or fragment")
	}
	host := parsed.Hostname()
	if host != "127.0.0.1" && host != "::1" && !strings.EqualFold(host, "localhost") {
		return nil, errors.New("PI WEB URL must use a loopback host")
	}
	parsed.Path = strings.TrimRight(parsed.Path, "/") + "/"
	return &Client{baseURL: parsed, http: httpClient}, nil
}

func (c *Client) StartSession(ctx context.Context, cwd string) (Session, error) {
	var session Session
	if err := c.request(ctx, http.MethodPost, "api/machines/local/sessions", map[string]string{"cwd": cwd}, &session); err != nil {
		return Session{}, err
	}
	if session.ID == "" || session.CWD == "" {
		return Session{}, errors.New("PI WEB returned an incomplete session identity")
	}
	return session, nil
}

func (c *Client) Prompt(ctx context.Context, session Session, text, behavior string) error {
	body := map[string]string{"cwd": session.CWD, "text": text}
	if behavior != "" {
		body["streamingBehavior"] = behavior
	}
	var response struct {
		Accepted bool `json:"accepted"`
	}
	if err := c.request(ctx, http.MethodPost, c.sessionPath(session, "prompt"), body, &response); err != nil {
		return err
	}
	if !response.Accepted {
		return errors.New("PI WEB did not accept the prompt")
	}
	return nil
}

func (c *Client) Abort(ctx context.Context, session Session) error {
	var response struct {
		Aborted bool `json:"aborted"`
	}
	if err := c.request(ctx, http.MethodPost, c.sessionPath(session, "abort"), map[string]string{"cwd": session.CWD}, &response); err != nil {
		return err
	}
	if !response.Aborted {
		return errors.New("PI WEB did not confirm abort")
	}
	return nil
}

func (c *Client) Subscribe(ctx context.Context, session Session) (EventStream, error) {
	endpoint := *c.baseURL
	if endpoint.Scheme == "http" {
		endpoint.Scheme = "ws"
	}
	endpoint.Path = strings.TrimRight(endpoint.Path, "/") + "/" + c.sessionPath(session, "events")
	query := endpoint.Query()
	query.Set("cwd", session.CWD)
	endpoint.RawQuery = query.Encode()
	connection, _, err := websocket.Dial(ctx, endpoint.String(), &websocket.DialOptions{HTTPClient: c.http})
	if err != nil {
		return EventStream{}, fmt.Errorf("subscribe to PI WEB session events: %w", err)
	}
	connection.SetReadLimit(maxResponseBytes)
	events := make(chan Event)
	errorsChannel := make(chan error, 1)
	go func() {
		defer close(events)
		defer close(errorsChannel)
		defer connection.CloseNow()
		for {
			_, payload, readErr := connection.Read(ctx)
			if readErr != nil {
				if ctx.Err() == nil {
					errorsChannel <- fmt.Errorf("read PI WEB session event: %w", readErr)
				}
				return
			}
			if err := strictjson.Validate(payload); err != nil {
				errorsChannel <- fmt.Errorf("validate PI WEB session event: %w", err)
				return
			}
			var envelope struct {
				Type string `json:"type"`
				Seq  uint64 `json:"seq,omitempty"`
			}
			if err := json.Unmarshal(payload, &envelope); err != nil || envelope.Type == "" {
				errorsChannel <- errors.New("PI WEB session event is missing its type")
				return
			}
			select {
			case events <- Event{Type: envelope.Type, Seq: envelope.Seq, Raw: append(json.RawMessage(nil), payload...)}:
			case <-ctx.Done():
				return
			}
		}
	}()
	return EventStream{Events: events, Errors: errorsChannel}, nil
}

func (c *Client) WebURL(session Session) string {
	result := *c.baseURL
	result.Path = strings.TrimSuffix(result.Path, "/") + "/"
	query := result.Query()
	query.Set("session", session.ID)
	query.Set("view", "chat")
	result.RawQuery = query.Encode()
	return result.String()
}

func (c *Client) sessionPath(session Session, action string) string {
	return "api/machines/local/sessions/" + url.PathEscape(session.ID) + "/" + action
}

func (c *Client) request(ctx context.Context, method, relative string, body any, target any) error {
	endpoint := c.baseURL.ResolveReference(&url.URL{Path: relative})
	encoded, err := json.Marshal(body)
	if err != nil {
		return fmt.Errorf("encode PI WEB request: %w", err)
	}
	request, err := http.NewRequestWithContext(ctx, method, endpoint.String(), bytes.NewReader(encoded))
	if err != nil {
		return fmt.Errorf("create PI WEB request: %w", err)
	}
	request.Header.Set("Content-Type", "application/json")
	response, err := c.http.Do(request)
	if err != nil {
		return fmt.Errorf("call PI WEB: %w", err)
	}
	defer response.Body.Close()
	payload, err := io.ReadAll(io.LimitReader(response.Body, maxResponseBytes+1))
	if err != nil {
		return fmt.Errorf("read PI WEB response: %w", err)
	}
	if len(payload) > maxResponseBytes {
		return errors.New("PI WEB response exceeds 1 MiB")
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		var failure struct {
			Error string `json:"error"`
		}
		if json.Unmarshal(payload, &failure) == nil && failure.Error != "" {
			return fmt.Errorf("PI WEB returned HTTP %d: %s", response.StatusCode, failure.Error)
		}
		return fmt.Errorf("PI WEB returned HTTP %d", response.StatusCode)
	}
	if err := strictjson.Validate(payload); err != nil {
		return fmt.Errorf("validate PI WEB response: %w", err)
	}
	if err := json.Unmarshal(payload, target); err != nil {
		return fmt.Errorf("decode PI WEB response: %w", err)
	}
	return nil
}
