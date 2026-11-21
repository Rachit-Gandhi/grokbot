package openai

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestComplete(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/chat/completions" {
			t.Fatalf("path %s", r.URL.Path)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer test-key" {
			t.Fatalf("auth %s", got)
		}
		var req chatRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatal(err)
		}
		if req.Stream {
			t.Fatal("expected non-stream")
		}
		_ = json.NewEncoder(w).Encode(chatResponse{
			Choices: []chatChoice{{Message: Message{Role: "assistant", Content: "hello"}}},
		})
	}))
	defer srv.Close()

	c := New(srv.URL+"/v1", "test-key")
	out, err := c.Complete(context.Background(), "gpt-test", []Message{{Role: "user", Content: "hi"}})
	if err != nil {
		t.Fatal(err)
	}
	if out != "hello" {
		t.Fatalf("got %q", out)
	}
}

func TestStream(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		flusher, ok := w.(http.Flusher)
		if !ok {
			t.Fatal("no flusher")
		}
		chunks := []string{`{"choices":[{"delta":{"content":"Hel"}}]}`, `{"choices":[{"delta":{"content":"lo"}}]}`}
		for _, c := range chunks {
			_, _ = w.Write([]byte("data: " + c + "\n\n"))
			flusher.Flush()
		}
		_, _ = w.Write([]byte("data: [DONE]\n\n"))
		flusher.Flush()
	}))
	defer srv.Close()

	c := New(srv.URL+"/v1", "k")
	var parts []string
	out, err := c.Stream(context.Background(), "m", []Message{{Role: "user", Content: "x"}}, func(d string) error {
		parts = append(parts, d)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if out != "Hello" {
		t.Fatalf("assembled %q", out)
	}
	if strings.Join(parts, "") != "Hello" {
		t.Fatalf("parts %#v", parts)
	}
}

func TestCompleteAPIError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"error":{"message":"bad model"}}`))
	}))
	defer srv.Close()

	c := New(srv.URL+"/v1", "k")
	_, err := c.Complete(context.Background(), "nope", nil)
	if err == nil || !strings.Contains(err.Error(), "400") {
		t.Fatalf("expected status error, got %v", err)
	}
}
