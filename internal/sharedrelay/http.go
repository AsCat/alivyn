package sharedrelay

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"
)

type rateEntry struct {
	count int
	until time.Time
}

func (r *Relay) Handler() http.Handler {
	mux := http.NewServeMux()
	var rateMu sync.Mutex
	rates := make(map[string]rateEntry)
	mux.HandleFunc("POST /v1/pair", func(w http.ResponseWriter, req *http.Request) {
		host, _, _ := net.SplitHostPort(req.RemoteAddr)
		if ip := net.ParseIP(host); ip != nil && ip.IsLoopback() {
			if forwarded := strings.TrimSpace(strings.Split(req.Header.Get("X-Forwarded-For"), ",")[0]); net.ParseIP(forwarded) != nil {
				host = forwarded
			}
		}
		rateMu.Lock()
		now := time.Now()
		entry := rates[host]
		if now.After(entry.until) {
			entry = rateEntry{until: now.Add(time.Minute)}
		}
		entry.count++
		rates[host] = entry
		for key, value := range rates {
			if now.After(value.until) {
				delete(rates, key)
			}
		}
		rateMu.Unlock()
		if entry.count > 5 {
			http.Error(w, "pairing rate limit", http.StatusTooManyRequests)
			return
		}
		pair, err := r.CreatePair(req.Context(), now)
		if err != nil {
			http.Error(w, "cannot create pairing", http.StatusServiceUnavailable)
			return
		}
		writeJSON(w, pair)
	})
	mux.HandleFunc("GET /v1/status", r.authenticated(func(w http.ResponseWriter, req *http.Request, id string) {
		paired, err := r.Status(req.Context(), id)
		if err != nil {
			http.Error(w, "status unavailable", http.StatusServiceUnavailable)
			return
		}
		writeJSON(w, struct {
			Paired bool `json:"paired"`
		}{paired})
	}))
	mux.HandleFunc("GET /v1/messages", r.authenticated(func(w http.ResponseWriter, req *http.Request, id string) {
		paired, err := r.Status(req.Context(), id)
		if err != nil || !paired {
			http.Error(w, "agent is not paired", http.StatusConflict)
			return
		}
		ctx, cancel := context.WithTimeout(req.Context(), 20*time.Second)
		defer cancel()
		messages, err := r.Poll(ctx, id)
		if err != nil && !errors.Is(err, context.DeadlineExceeded) {
			return
		}
		if messages == nil {
			messages = []Message{}
		}
		writeJSON(w, struct {
			Messages []Message `json:"messages"`
		}{messages})
	}))
	mux.HandleFunc("POST /v1/send", r.authenticated(func(w http.ResponseWriter, req *http.Request, id string) {
		req.Body = http.MaxBytesReader(w, req.Body, 16384)
		var input struct {
			Text string `json:"text"`
		}
		if err := json.NewDecoder(req.Body).Decode(&input); err != nil {
			http.Error(w, "invalid message", http.StatusBadRequest)
			return
		}
		if err := r.Send(req.Context(), id, input.Text); err != nil {
			http.Error(w, "message could not be delivered", http.StatusBadGateway)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	mux.HandleFunc("POST /v1/ack", r.authenticated(func(w http.ResponseWriter, req *http.Request, id string) {
		req.Body = http.MaxBytesReader(w, req.Body, 4096)
		var input struct {
			EventIDs []string `json:"event_ids"`
		}
		if err := json.NewDecoder(req.Body).Decode(&input); err != nil || len(input.EventIDs) > 32 {
			http.Error(w, "invalid ack", http.StatusBadRequest)
			return
		}
		r.Ack(id, input.EventIDs)
		w.WriteHeader(http.StatusNoContent)
	}))
	return mux
}

func (r *Relay) authenticated(next func(http.ResponseWriter, *http.Request, string)) http.HandlerFunc {
	return func(w http.ResponseWriter, req *http.Request) {
		header := req.Header.Get("Authorization")
		if !strings.HasPrefix(header, "Bearer ") {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		token := strings.TrimPrefix(header, "Bearer ")
		id, err := r.Authenticate(req.Context(), token)
		if err != nil {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		next(w, req, id)
	}
}

func writeJSON(w http.ResponseWriter, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	_ = json.NewEncoder(w).Encode(value)
}
