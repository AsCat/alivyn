package extension

import (
	"context"
	"testing"
)

type recallStub struct{}

func (recallStub) Recall(context.Context, RecallQuery) ([]MemoryHit, error) { return nil, nil }

func TestRegistryRequiresExplicitCapabilityAndSelection(t *testing.T) {
	r := NewRegistry()
	m := Registration{Manifest: Manifest{ID: "example.memory", Version: "0.1.0", APIVersion: APIVersion, Capabilities: []Capability{Recall}}, Retriever: recallStub{}}
	if err := r.Register(m); err != nil {
		t.Fatal(err)
	}
	if _, ok := r.Active("character.memory", Recall); ok {
		t.Fatal("registration activated a module without selection")
	}
	if err := r.Use("character.memory", Act, m.Manifest.ID); err == nil {
		t.Fatal("selected undeclared action capability")
	}
	if err := r.Use("character.memory", Recall, m.Manifest.ID); err != nil {
		t.Fatal(err)
	}
	if active, ok := r.Active("character.memory", Recall); !ok || active.Manifest.ID != m.Manifest.ID {
		t.Fatal("explicit selection was not resolved")
	}
	if err := r.Register(m); err == nil {
		t.Fatal("duplicate registration replaced the existing module")
	}
	bad := m
	bad.Manifest.ID = "example.invalid"
	bad.Manifest.Capabilities = []Capability{Act}
	if err := r.Register(bad); err == nil {
		t.Fatal("accepted a capability without an implementation")
	}
}
