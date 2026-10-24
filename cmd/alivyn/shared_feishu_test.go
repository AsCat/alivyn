package main

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/AsCat/alivyn/internal/config"
	"github.com/AsCat/alivyn/internal/sharedrelay"
)

func TestSharedPairCommandNeedsNoUserFeishuApp(t *testing.T) {
	t.Setenv("ALIVYN_HOME", t.TempDir())
	if err := config.SaveNew(config.Config{SchemaVersion: 1, Name: "Ari", Language: "en", Provider: "custom", BaseURL: "http://localhost:1234", Model: "mock"}); err != nil {
		t.Fatal(err)
	}
	secret := "a-local-token-that-is-never-printed-0123456789"
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/v1/pair":
			_ = json.NewEncoder(w).Encode(sharedrelay.Pairing{Token: secret, Code: "ABCDEF-GHIJKL", BotURL: "https://applink.feishu.cn/example", ExpiresAt: time.Now().Add(time.Minute)})
		case "/v1/status":
			if r.Header.Get("Authorization") != "Bearer "+secret {
				http.Error(w, "bad token", http.StatusUnauthorized)
				return
			}
			_, _ = w.Write([]byte(`{"paired":true}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	var out bytes.Buffer
	if err := pairSharedFeishu(context.Background(), server.URL, &out); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "ABCDEF-GHIJKL") || strings.Contains(out.String(), secret) {
		t.Fatal("pairing output omitted code or exposed agent token")
	}
	saved, err := config.LoadSharedFeishu()
	if err != nil || saved.Token != secret || saved.Code != "" {
		t.Fatalf("pairing state was not safely stored: %+v, %v", saved, err)
	}
	if _, err := config.LoadFeishu(); err == nil {
		t.Fatal("pair command unexpectedly created a user-owned Feishu app")
	}
}
