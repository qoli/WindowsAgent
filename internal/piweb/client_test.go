package piweb

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"
)

func TestClientSessionLifecycleAndEvents(t *testing.T) {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		switch request.URL.Path {
		case "/api/machines/local/sessions":
			writer.Header().Set("Content-Type", "application/json")
			_, _ = writer.Write([]byte(`{"id":"session-1","cwd":"C:\\work","path":"ignored","created":"now","modified":"now","messageCount":0,"firstMessage":""}`))
		case "/api/machines/local/sessions/session-1/prompt":
			writer.Header().Set("Content-Type", "application/json")
			_, _ = writer.Write([]byte(`{"accepted":true}`))
		case "/api/machines/local/sessions/session-1/abort":
			writer.Header().Set("Content-Type", "application/json")
			_, _ = writer.Write([]byte(`{"aborted":true}`))
		case "/api/machines/local/sessions/session-1/events":
			connection, err := websocket.Accept(writer, request, nil)
			if err != nil {
				t.Error(err)
				return
			}
			defer connection.CloseNow()
			_ = connection.Write(request.Context(), websocket.MessageText, []byte(`{"type":"assistant.delta","seq":7,"text":"working"}`))
			<-request.Context().Done()
		default:
			http.NotFound(writer, request)
		}
	}))
	defer server.Close()

	client, err := New(server.URL, server.Client())
	if err != nil {
		t.Fatal(err)
	}
	session, err := client.StartSession(context.Background(), `C:\work`)
	if err != nil {
		t.Fatal(err)
	}
	if session.ID != "session-1" || session.CWD != `C:\work` {
		t.Fatalf("session = %+v", session)
	}
	ctx, cancel := context.WithCancel(context.Background())
	stream, err := client.Subscribe(ctx, session)
	if err != nil {
		t.Fatal(err)
	}
	select {
	case event := <-stream.Events:
		if event.Type != "assistant.delta" || event.Seq != 7 || !strings.Contains(string(event.Raw), "working") {
			t.Fatalf("event = %+v", event)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for PI WEB event")
	}
	if err := client.Prompt(context.Background(), session, "do work", "steer"); err != nil {
		t.Fatal(err)
	}
	if err := client.Abort(context.Background(), session); err != nil {
		t.Fatal(err)
	}
	cancel()
	if got := client.WebURL(session); !strings.Contains(got, "session=session-1") || !strings.Contains(got, "view=chat") {
		t.Fatalf("WebURL = %q", got)
	}
}

func TestNewRejectsNonLoopbackPiWeb(t *testing.T) {
	if _, err := New("http://192.0.2.10:8504", http.DefaultClient); err == nil {
		t.Fatal("expected non-loopback PI WEB URL to fail")
	}
}
