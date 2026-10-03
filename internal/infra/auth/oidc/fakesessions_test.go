package oidc

import (
	"context"
	"sync"
	"time"

	"agrelha/internal/ports"
)

type fakeSessions struct {
	mu   sync.Mutex
	rows map[string]ports.Session
}

func newFakeSessions() *fakeSessions {
	return &fakeSessions{rows: map[string]ports.Session{}}
}

func (f *fakeSessions) CreateSession(_ context.Context, s ports.Session) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.rows[s.ID] = s
	return nil
}

func (f *fakeSessions) LoadSession(_ context.Context, id string) (*ports.Session, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	s, ok := f.rows[id]
	if !ok || !s.ExpiresAt.After(time.Now()) {
		return nil, nil
	}
	out := s
	return &out, nil
}

func (f *fakeSessions) TouchSession(_ context.Context, id string, expiresAt time.Time) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if s, ok := f.rows[id]; ok {
		s.ExpiresAt = expiresAt
		f.rows[id] = s
	}
	return nil
}

func (f *fakeSessions) DeleteSession(_ context.Context, id string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	delete(f.rows, id)
	return nil
}

func (f *fakeSessions) DeleteSessionsFor(_ context.Context, subject string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	for id, s := range f.rows {
		if s.Subject == subject {
			delete(f.rows, id)
		}
	}
	return nil
}

func (f *fakeSessions) PurgeExpiredSessions(_ context.Context, now time.Time) (int, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	n := 0
	for id, s := range f.rows {
		if !s.ExpiresAt.After(now) {
			delete(f.rows, id)
			n++
		}
	}
	return n, nil
}

func (f *fakeSessions) count() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.rows)
}
