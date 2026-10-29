package extension

import (
	"errors"
	"fmt"
	"regexp"
	"sync"
)

var moduleID = regexp.MustCompile(`^[a-z][a-z0-9._-]*$`)

// Registration binds a manifest to implementations. Registration does not
// start a module or grant any of its requested permissions.
type Registration struct {
	Manifest  Manifest
	Perceptor Perceptor
	Simulator Simulator
	Projector Projector
	Retriever Retriever
	Decider   Decider
	Actuator  Actuator
	Expresser Expresser
}

func (m Registration) implements(cap Capability) bool {
	switch cap {
	case Perceive:
		return m.Perceptor != nil
	case Simulate:
		return m.Simulator != nil
	case Project:
		return m.Projector != nil
	case Recall:
		return m.Retriever != nil
	case Decide:
		return m.Decider != nil
	case Act:
		return m.Actuator != nil
	case Express:
		return m.Expresser != nil
	default:
		return false
	}
}

func (m Registration) validate() error {
	d := m.Manifest
	if !moduleID.MatchString(d.ID) || d.Version == "" || d.APIVersion != APIVersion {
		return errors.New("invalid extension identity, version, or API version")
	}
	if len(d.Capabilities) == 0 {
		return errors.New("extension declares no capabilities")
	}
	seen := make(map[Capability]bool, len(d.Capabilities))
	for _, cap := range d.Capabilities {
		if seen[cap] || !m.implements(cap) {
			return fmt.Errorf("extension %s has duplicate or unimplemented capability %q", d.ID, cap)
		}
		seen[cap] = true
	}
	for _, cap := range []Capability{Perceive, Simulate, Project, Recall, Decide, Act, Express} {
		if m.implements(cap) && !seen[cap] {
			return fmt.Errorf("extension %s implements undeclared capability %q", d.ID, cap)
		}
	}
	return nil
}

// Registry is an explicit runtime composition table. A slot identifies a
// purpose such as "character.memory"; multiple slots may use one module.
// Loading and isolation of third-party processes is a separate host concern.
type Registry struct {
	mu      sync.RWMutex
	modules map[string]Registration
	active  map[string]string
}

func NewRegistry() *Registry {
	return &Registry{modules: map[string]Registration{}, active: map[string]string{}}
}

func (r *Registry) Register(m Registration) error {
	if err := m.validate(); err != nil {
		return err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.modules == nil {
		r.modules = map[string]Registration{}
	}
	if _, exists := r.modules[m.Manifest.ID]; exists {
		return fmt.Errorf("extension %s is already registered", m.Manifest.ID)
	}
	r.modules[m.Manifest.ID] = m
	return nil
}

// Use binds one purpose to one declared capability. This is an explicit
// selection, not automatic activation of every installed module.
func (r *Registry) Use(slot string, cap Capability, id string) error {
	if slot == "" {
		return errors.New("extension slot is empty")
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	m, ok := r.modules[id]
	if !ok || !m.implements(cap) {
		return fmt.Errorf("extension %s cannot provide %q", id, cap)
	}
	declared := false
	for _, offered := range m.Manifest.Capabilities {
		declared = declared || offered == cap
	}
	if !declared {
		return fmt.Errorf("extension %s did not declare %q", id, cap)
	}
	if r.active == nil {
		r.active = map[string]string{}
	}
	r.active[string(cap)+":"+slot] = id
	return nil
}

func (r *Registry) Active(slot string, cap Capability) (Registration, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	id, ok := r.active[string(cap)+":"+slot]
	if !ok {
		return Registration{}, false
	}
	m, ok := r.modules[id]
	return m, ok
}
