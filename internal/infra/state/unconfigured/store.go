// Package unconfigured provides a StateStore for installations that have no
// declarative plane configured. Reads return empty documents so pages render;
// writes fail with ErrUnconfigured instead of panicking on a nil store.
package unconfigured

import (
	"context"
	"errors"

	"agrelha/internal/ports"
)

var ErrUnconfigured = errors.New("declarative plane not configured: set GIT_REPO_URL and GIT_TOKEN (or RUNTIME=docker) to enable editing")

type Store struct{}

func New() *Store { return &Store{} }

var _ ports.StateStore = (*Store)(nil)

func (s *Store) Get(ctx context.Context, path string) (ports.Document, error) {
	return ports.Document{Data: map[string]string{}, Annotations: map[string]string{}}, nil
}

func (s *Store) Put(ctx context.Context, path string, doc ports.Document, msg string) error {
	return ErrUnconfigured
}

func (s *Store) Patch(ctx context.Context, path, msg string, mutate func(doc *ports.Document) (bool, error)) (bool, error) {
	return false, ErrUnconfigured
}

func (s *Store) Delete(ctx context.Context, path, msg string) error { return ErrUnconfigured }

func (s *Store) PutTree(ctx context.Context, dirPath string, docs map[string]ports.Document, msg string) error {
	return ErrUnconfigured
}
