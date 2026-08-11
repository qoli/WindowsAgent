package visuallog

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestModelClientSendsSingleImageBeforeTextAndParsesDescription(t *testing.T) {
	config, _ := ParseConfig([]byte(validConfigJSON()))
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/chat/completions" || r.Header.Get("Authorization") != "Bearer secret" {
			t.Fatalf("request path=%q authorization=%q", r.URL.Path, r.Header.Get("Authorization"))
		}
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatal(err)
		}
		messages := body["messages"].([]any)
		user := messages[1].(map[string]any)
		content := user["content"].([]any)
		if len(content) != 2 || content[0].(map[string]any)["type"] != "image_url" || content[1].(map[string]any)["type"] != "text" {
			t.Fatalf("user content = %#v", content)
		}
		if body["model"] != config.Model.ID || body["temperature"] != config.Model.Temperature || body["top_p"] != config.Model.TopP {
			t.Fatalf("model request = %#v", body)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"{\"description\":\"Vast illuminated station interior surrounds large curved industrial docking structures.\"}"},"finish_reason":"stop"}]}`))
	}))
	defer server.Close()
	client, err := NewModelClient(server.URL+"/v1", "secret", server.Client(), config)
	if err != nil {
		t.Fatal(err)
	}
	description, err := client.Describe(context.Background(), Frame{ContentType: "image/jpeg", Content: []byte("jpeg")})
	if err != nil {
		t.Fatal(err)
	}
	if description.ModelID != config.Model.ID || !strings.HasPrefix(description.Text, "Vast illuminated") {
		t.Fatalf("description = %+v", description)
	}
}

func TestModelClientRejectsOutOfContractDescription(t *testing.T) {
	config, _ := ParseConfig([]byte(validConfigJSON()))
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"{\"description\":\"Too short.\"}"},"finish_reason":"stop"}]}`))
	}))
	defer server.Close()
	client, err := NewModelClient(server.URL, "secret", server.Client(), config)
	if err != nil {
		t.Fatal(err)
	}
	_, err = client.Describe(context.Background(), Frame{ContentType: "image/jpeg", Content: []byte("jpeg")})
	if err == nil || !strings.Contains(err.Error(), "8 through 16 words") {
		t.Fatalf("error = %v", err)
	}
}
