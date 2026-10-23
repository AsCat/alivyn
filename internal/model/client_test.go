package model

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestCompatibleChatCompletion(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/chat/completions" || r.Method != http.MethodPost {
			t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
			http.Error(w, "unexpected request", http.StatusBadRequest)
			return
		}
		if got := r.Header.Get("Authorization"); got != "Bearer test-key" {
			t.Errorf("authorization header = %q", got)
			http.Error(w, "unexpected authorization", http.StatusBadRequest)
			return
		}
		var input request
		if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
			t.Error(err)
			http.Error(w, "invalid request", http.StatusBadRequest)
			return
		}
		if input.Model != "sample-model" || len(input.Messages) != 1 {
			t.Errorf("unexpected input: %+v", input)
			http.Error(w, "unexpected input", http.StatusBadRequest)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"choices":[{"message":{"role":"assistant","content":"hello"}}]}`))
	}))
	defer server.Close()

	client := Client{BaseURL: server.URL + "/v1", APIKey: "test-key"}
	got, err := client.Complete(context.Background(), "sample-model", []Message{{Role: "user", Content: "hi"}})
	if err != nil {
		t.Fatal(err)
	}
	if got != "hello" {
		t.Fatalf("completion = %q", got)
	}
}

func TestProviderErrorDoesNotRevealCredentials(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		w.Write([]byte(`{"error":"secret-token-123"}`))
	}))
	defer server.Close()

	client := Client{BaseURL: server.URL, APIKey: "secret-token-123"}
	_, err := client.Complete(context.Background(), "sample-model", []Message{{Role: "user", Content: "hi"}})
	if err == nil || err.Error() != "provider returned HTTP 401" {
		t.Fatalf("unexpected error: %v", err)
	}
}
