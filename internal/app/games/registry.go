package games

import (
	"sync"

	"agrelha/internal/domain"
	"agrelha/internal/ports"
)

// Entry associates a game's declarative profile with its runtime engine.
type Entry struct {
	Profile domain.GameProfile
	Engine  ports.Game
}

// Registry manages the set of supported games.
type Registry struct {
	mu      sync.RWMutex
	entries map[domain.GameID]Entry
	order   []domain.GameID
}

// NewRegistry creates an empty games Registry.
func NewRegistry() *Registry {
	return &Registry{
		entries: make(map[domain.GameID]Entry),
	}
}

// Register adds or updates a game entry.
func (r *Registry) Register(entry Entry) {
	r.mu.Lock()
	defer r.mu.Unlock()
	id := entry.Profile.ID
	if _, exists := r.entries[id]; !exists {
		r.order = append(r.order, id)
	}
	r.entries[id] = entry
}

// Get returns the game Entry for a given GameID, or false if not found.
func (r *Registry) Get(id domain.GameID) (Entry, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	e, ok := r.entries[id]
	return e, ok
}

// Engine returns the ports.Game engine for a given GameID, or false if not found.
func (r *Registry) Engine(id domain.GameID) (ports.Game, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	e, ok := r.entries[id]
	if !ok {
		return nil, false
	}
	return e.Engine, true
}

// Profiles returns the profiles of all registered games in registration order.
func (r *Registry) Profiles() []domain.GameProfile {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]domain.GameProfile, 0, len(r.order))
	for _, id := range r.order {
		out = append(out, r.entries[id].Profile)
	}
	return out
}

// Entries returns all registered game entries in registration order.
func (r *Registry) Entries() []Entry {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]Entry, 0, len(r.order))
	for _, id := range r.order {
		out = append(out, r.entries[id])
	}
	return out
}
