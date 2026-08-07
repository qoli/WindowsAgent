package ocrworker

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
)

func TestWorkerFrameRoundTrip(t *testing.T) {
	body := []byte(`{"schemaVersion":2,"id":"ocr-1","ok":true,"result":{"state":"ready"}}`)
	var framed bytes.Buffer
	if err := writeFrame(&framed, body); err != nil {
		t.Fatal(err)
	}
	decoded, err := readFrame(&framed)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(decoded, body) {
		t.Fatalf("decoded = %s", decoded)
	}
}

func TestWorkerResponseRejectsUnknownFields(t *testing.T) {
	var response responseEnvelope
	err := decodeStrict([]byte(`{"schemaVersion":2,"id":"ocr-1","ok":true,"result":{},"extra":true}`), &response)
	if err == nil || !strings.Contains(err.Error(), "unknown field") {
		t.Fatalf("error = %v", err)
	}
}

func TestWorkerRequestRGBUsesCanonicalBase64(t *testing.T) {
	request := map[string]any{"rgbBase64": []byte{1, 2, 3}}
	encoded, err := json.Marshal(request)
	if err != nil {
		t.Fatal(err)
	}
	if string(encoded) != `{"rgbBase64":"AQID"}` {
		t.Fatalf("encoded = %s", encoded)
	}
}
