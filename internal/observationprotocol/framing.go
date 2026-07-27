package observationprotocol

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"

	"github.com/qoli/WindowsAgent/internal/strictjson"
)

const (
	DefaultMaxFrameBytes = 1 << 20
	contentType          = "application/windowsagent-observation+json; charset=utf-8"
)

type Message struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      string          `json:"id,omitempty"`
	Method  string          `json:"method,omitempty"`
	Params  json.RawMessage `json:"params,omitempty"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   *ResponseError  `json:"error,omitempty"`
}

type ResponseError struct {
	Code    int             `json:"code"`
	Message string          `json:"message"`
	Data    json.RawMessage `json:"data,omitempty"`
}

type Conn struct {
	reader   *bufio.Reader
	writer   io.Writer
	maxFrame int
}

func NewConn(reader io.Reader, writer io.Writer, maxFrame int) (*Conn, error) {
	if reader == nil {
		return nil, errors.New("reader is required")
	}
	if writer == nil {
		return nil, errors.New("writer is required")
	}
	if maxFrame <= 0 {
		return nil, errors.New("max frame bytes must be positive")
	}
	return &Conn{
		reader:   bufio.NewReader(reader),
		writer:   writer,
		maxFrame: maxFrame,
	}, nil
}

func (c *Conn) Read() (Message, error) {
	var length int
	seenLength := false
	seenType := false
	for {
		line, err := c.reader.ReadString('\n')
		if err != nil {
			return Message{}, fmt.Errorf("read frame header: %w", err)
		}
		if !strings.HasSuffix(line, "\r\n") {
			return Message{}, errors.New("frame headers must use CRLF")
		}
		line = strings.TrimSuffix(line, "\r\n")
		if line == "" {
			break
		}
		name, value, ok := strings.Cut(line, ": ")
		if !ok {
			return Message{}, errors.New("malformed frame header")
		}
		switch name {
		case "Content-Length":
			if seenLength {
				return Message{}, errors.New("duplicate Content-Length header")
			}
			parsed, err := strconv.Atoi(value)
			if err != nil || parsed <= 0 {
				return Message{}, errors.New("invalid Content-Length header")
			}
			if parsed > c.maxFrame {
				return Message{}, fmt.Errorf("frame length %d exceeds limit %d", parsed, c.maxFrame)
			}
			length = parsed
			seenLength = true
		case "Content-Type":
			if seenType {
				return Message{}, errors.New("duplicate Content-Type header")
			}
			if value != contentType {
				return Message{}, fmt.Errorf("unsupported Content-Type %q", value)
			}
			seenType = true
		default:
			return Message{}, fmt.Errorf("unsupported frame header %q", name)
		}
	}
	if !seenLength || !seenType {
		return Message{}, errors.New("Content-Length and Content-Type are required")
	}
	body := make([]byte, length)
	if _, err := io.ReadFull(c.reader, body); err != nil {
		return Message{}, fmt.Errorf("read frame body: %w", err)
	}
	if err := strictjson.Validate(body); err != nil {
		return Message{}, fmt.Errorf("validate JSON-RPC message: %w", err)
	}
	var message Message
	decoder := json.NewDecoder(bytes.NewReader(body))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&message); err != nil {
		return Message{}, fmt.Errorf("decode JSON-RPC message: %w", err)
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		if err == nil {
			return Message{}, errors.New("multiple JSON values in one frame")
		}
		return Message{}, fmt.Errorf("decode trailing frame content: %w", err)
	}
	if message.JSONRPC != "2.0" {
		return Message{}, errors.New("jsonrpc must equal 2.0")
	}
	if message.ID == "" {
		return Message{}, errors.New("message id is required")
	}
	if err := validateShape(message); err != nil {
		return Message{}, err
	}
	return message, nil
}

func (c *Conn) Write(message Message) error {
	if message.JSONRPC == "" {
		message.JSONRPC = "2.0"
	}
	if message.JSONRPC != "2.0" {
		return errors.New("jsonrpc must equal 2.0")
	}
	if message.ID == "" {
		return errors.New("message id is required")
	}
	if err := validateShape(message); err != nil {
		return err
	}
	body, err := json.Marshal(message)
	if err != nil {
		return fmt.Errorf("encode JSON-RPC message: %w", err)
	}
	if len(body) > c.maxFrame {
		return fmt.Errorf("frame length %d exceeds limit %d", len(body), c.maxFrame)
	}
	header := fmt.Sprintf(
		"Content-Length: %d\r\nContent-Type: %s\r\n\r\n",
		len(body),
		contentType,
	)
	if _, err := io.WriteString(c.writer, header); err != nil {
		return fmt.Errorf("write frame header: %w", err)
	}
	if _, err := c.writer.Write(body); err != nil {
		return fmt.Errorf("write frame body: %w", err)
	}
	return nil
}

func validateShape(message Message) error {
	isRequest := message.Method != ""
	hasResult := len(message.Result) != 0
	hasError := message.Error != nil
	if isRequest {
		if hasResult || hasError {
			return errors.New("JSON-RPC request cannot contain result or error")
		}
		return nil
	}
	if len(message.Params) != 0 || hasResult == hasError {
		return errors.New("JSON-RPC response requires exactly one of result or error")
	}
	return nil
}
