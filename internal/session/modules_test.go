package session

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"github.com/AsCat/alivyn/internal/character"
	"github.com/AsCat/alivyn/internal/config"
	"github.com/AsCat/alivyn/internal/life"
	"github.com/AsCat/alivyn/internal/store"
	"github.com/AsCat/alivyn/pkg/extension"
)

type testMemoryModule struct{ seen int64 }

func (m *testMemoryModule) Recall(_ context.Context, q extension.RecallQuery) ([]extension.MemoryHit, error) {
	m.seen = q.BeforeMessageID
	episode, _ := json.Marshal(character.Episode{Event: "上周聊过旧唱片", Effect: "想起那张封面", SourceIDs: []string{"chat:1"}})
	return []extension.MemoryHit{{ID: "episode:1", Kind: "episode", Content: "上周聊过旧唱片", SourceIDs: []string{"chat:1"}, Fields: episode}}, nil
}

type testAffectModule struct{ seen int64 }

func (m *testAffectModule) Project(_ context.Context, q extension.ProjectionRequest) (extension.ProjectionResult, error) {
	m.seen = q.BeforeMessageID
	value, _ := json.Marshal(continuityProjection{Moment: character.Moment{Feeling: "有点好奇"}})
	return extension.ProjectionResult{Namespace: "character.continuity", Version: "1", At: q.AsOf, Value: value}, nil
}

type testReadingModule struct{ invalid bool }

func (m testReadingModule) Observe(_ context.Context, at time.Time) ([]extension.Observation, error) {
	candidate := life.Candidate{Title: "A sourced event", URL: "https://example.org/article", Excerpt: "A verified public excerpt that the character may choose to read."}
	if m.invalid {
		candidate.URL = "https://example.org/other"
	}
	payload, _ := json.Marshal(candidate)
	return []extension.Observation{{Kind: "public_feed_excerpt", At: at, Source: "https://example.org/article", Payload: payload}}, nil
}

func TestReadingPerceptorMustKeepSourceAttached(t *testing.T) {
	for _, invalid := range []bool{false, true} {
		r := extension.NewRegistry()
		if err := r.Register(extension.Registration{Manifest: extension.Manifest{ID: "test.reading", Version: "1", APIVersion: extension.APIVersion, Capabilities: []extension.Capability{extension.Perceive}}, Perceptor: testReadingModule{invalid}}); err != nil {
			t.Fatal(err)
		}
		if err := r.Use("life.reading", extension.Perceive, "test.reading"); err != nil {
			t.Fatal(err)
		}
		s := Session{Extensions: r}
		items, err := s.readingCandidates(context.Background(), time.Now())
		if invalid && err == nil || !invalid && (err != nil || len(items) != 1) {
			t.Fatalf("invalid=%t items=%+v error=%v", invalid, items, err)
		}
	}
}

func TestCharacterContextUsesSelectedMemoryAndAffectModules(t *testing.T) {
	ctx := context.Background()
	h, err := store.OpenHistory(ctx, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer h.Close()
	now := time.Date(2026, 10, 29, 12, 0, 0, 0, time.UTC)
	if err := h.AddUserMessage(ctx, "上周聊过旧唱片", now.Add(-7*24*time.Hour), time.UTC); err != nil {
		t.Fatal(err)
	}
	if err := h.AddUserMessage(ctx, "你还记得那张唱片吗", now, time.UTC); err != nil {
		t.Fatal(err)
	}
	entries, err := h.Recent(ctx, 1)
	if err != nil || len(entries) != 1 {
		t.Fatal(entries, err)
	}
	memory, affect := &testMemoryModule{}, &testAffectModule{}
	r := extension.NewRegistry()
	for _, module := range []extension.Registration{
		{Manifest: extension.Manifest{ID: "test.memory", Version: "1", APIVersion: extension.APIVersion, Capabilities: []extension.Capability{extension.Recall}}, Retriever: memory},
		{Manifest: extension.Manifest{ID: "test.affect", Version: "1", APIVersion: extension.APIVersion, Capabilities: []extension.Capability{extension.Project}}, Projector: affect},
	} {
		if err := r.Register(module); err != nil {
			t.Fatal(err)
		}
	}
	if err := r.Use("character.memory", extension.Recall, "test.memory"); err != nil {
		t.Fatal(err)
	}
	if err := r.Use("character.affect", extension.Project, "test.affect"); err != nil {
		t.Fatal(err)
	}
	s := Session{Config: config.Config{Name: "意续", Language: "zh-CN", Engine: "character_v2", WorldPack: "none"}, History: h, Extensions: r}
	c, err := s.characterContext(ctx, now, entries[0].ID)
	if err != nil {
		t.Fatal(err)
	}
	if c.Moment.Feeling != "有点好奇" || len(c.Episodes) != 1 || c.Episodes[0].Event != "上周聊过旧唱片" {
		t.Fatalf("selected module results were not used: %+v %+v", c.Moment, c.Episodes)
	}
	if memory.seen != entries[0].ID || affect.seen != entries[0].ID {
		t.Fatal(fmt.Sprintf("source boundary was lost: memory=%d affect=%d", memory.seen, affect.seen))
	}
}
