// Package memory holds Desired state in process. It is the plane a test or a
// throwaway dev run wants: writes are accepted and read back, so the code under
// test exercises the same branches it would against git or a disk, without a
// repository or a writable directory.
//
// It is not durable and must never be wired into a deployment: nothing survives
// a restart, which is the one property the declarative plane exists to provide.
package memory

import (
	"context"
	"io/fs"
	"maps"
	"strings"
	"sync"

	"agrelha/internal/ports"
)

// Store is an in-memory ports.StateStore. The zero value is not usable; call New.
type Store struct {
	mu   sync.RWMutex
	docs map[string]ports.Document
}

// New returns an empty in-memory state store.
func New() *Store {
	return &Store{docs: make(map[string]ports.Document)}
}

func cloneDoc(d ports.Document) ports.Document {
	out := ports.Document{Raw: append([]byte(nil), d.Raw...)}
	if d.Data != nil {
		out.Data = maps.Clone(d.Data)
	}
	if d.Annotations != nil {
		out.Annotations = maps.Clone(d.Annotations)
	}
	return out
}

// Get returns the document at path, or fs.ErrNotExist. A missing path is an
// error rather than an empty document, so a caller's absent-file branch stays
// reachable.
func (s *Store) Get(_ context.Context, path string) (ports.Document, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	doc, ok := s.docs[path]
	if !ok {
		return ports.Document{}, fs.ErrNotExist
	}
	return cloneDoc(doc), nil
}

// Put writes doc at path.
func (s *Store) Put(_ context.Context, path string, doc ports.Document, _ string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.docs[path] = cloneDoc(doc)
	return nil
}

// Patch reads path, applies mutate, and stores the result when it reports a change.
func (s *Store) Patch(_ context.Context, path string, _ string, mutate func(doc *ports.Document) (bool, error)) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	doc, ok := s.docs[path]
	if !ok {
		doc = ports.Document{}
	}
	working := cloneDoc(doc)
	if working.Data == nil {
		working.Data = make(map[string]string)
	}
	changed, err := mutate(&working)
	if err != nil || !changed {
		return false, err
	}
	s.docs[path] = working
	return true, nil
}

// Delete removes path and anything beneath it. A missing path is a no-op.
func (s *Store) Delete(_ context.Context, path string, _ string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.docs, path)
	prefix := strings.TrimSuffix(path, "/") + "/"
	for key := range s.docs {
		if strings.HasPrefix(key, prefix) {
			delete(s.docs, key)
		}
	}
	return nil
}

// PutTree writes docs under dirPath and removes any file there that docs omits,
// matching the git adapter: a render that stops emitting a file deletes it.
func (s *Store) PutTree(_ context.Context, dirPath string, docs map[string]ports.Document, _ string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	prefix := strings.TrimSuffix(dirPath, "/") + "/"
	for key := range s.docs {
		if strings.HasPrefix(key, prefix) {
			delete(s.docs, key)
		}
	}
	for name, doc := range docs {
		s.docs[prefix+name] = cloneDoc(doc)
	}
	return nil
}
