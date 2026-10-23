package main

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/AsCat/alivyn/internal/config"
	"github.com/AsCat/alivyn/internal/model"
)

func TestConversationPersistsAcrossChatRuns(t *testing.T) {
	t.Setenv("ALIVYN_HOME", t.TempDir())
	t.Setenv("ALIVYN_API_KEY", "test-key")
	var mu sync.Mutex
	var requests [][]model.Message
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/chat/completions" || r.Header.Get("Authorization") != "Bearer test-key" {
			http.Error(w, "bad request", http.StatusBadRequest)
			return
		}
		var input struct {
			Model    string          `json:"model"`
			Messages []model.Message `json:"messages"`
		}
		if err := json.NewDecoder(r.Body).Decode(&input); err != nil || input.Model != "mock-model" {
			http.Error(w, "bad input", http.StatusBadRequest)
			return
		}
		mu.Lock()
		requests = append(requests, input.Messages)
		mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"choices":[{"message":{"role":"assistant","content":"hello from Ari"}}]}`))
	}))
	defer server.Close()

	input := "en\nAri\ncustom\n" + server.URL + "/v1\nmock-model\n"
	var output bytes.Buffer
	if err := initialize(strings.NewReader(input), &output); err != nil {
		t.Fatal(err)
	}
	if err := initialize(strings.NewReader(input), &output); err == nil {
		t.Fatal("second initialization overwrote existing identity")
	}
	if err := checkModel(context.Background(), &output); err != nil {
		t.Fatal(err)
	}
	for _, userInput := range []string{"Do you remember me?", "What did I ask?"} {
		if err := chat(context.Background(), strings.NewReader(userInput+"\n/exit\n"), &output); err != nil {
			t.Fatal(err)
		}
	}
	mu.Lock()
	defer mu.Unlock()
	if len(requests) != 3 {
		t.Fatalf("expected check and two chats, got %d requests", len(requests))
	}
	last := requests[2]
	if len(last) != 4 || last[0].Role != "system" || last[1].Content != "Do you remember me?" || last[2].Content != "hello from Ari" || last[3].Content != "What did I ask?" {
		t.Fatalf("conversation was not retained: %+v", last)
	}
	if !strings.Contains(output.String(), "Ari> hello from Ari") {
		t.Fatalf("assistant reply not shown: %s", output.String())
	}
	for _, filename := range []string{"config.json", "alivyn.db"} {
		info, err := os.Stat(filepath.Join(os.Getenv("ALIVYN_HOME"), filename))
		if err != nil {
			t.Fatal(err)
		}
		if info.Mode().Perm() != 0600 {
			t.Fatalf("%s permissions are %o", filename, info.Mode().Perm())
		}
	}
	c, err := config.Load()
	if err != nil || c.Name != "Ari" || c.Provider != "custom" {
		t.Fatalf("config changed after restart: %+v, %v", c, err)
	}
}
