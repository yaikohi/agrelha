package ports

import (
	"context"
)

// Document represents a configuration document. In Kubernetes/GitOps, it is
// typically a ConfigMap YAML with a data map and optional metadata annotations.
// In Docker/Compose, it represents an env file, service config, or file entry.
type Document struct {
	// Data maps key names to string values (e.g. "mods.txt", "server.properties").
	Data map[string]string
	// Annotations holds domain metadata (e.g. "agrelha.dev/tier": "medium").
	Annotations map[string]string
	// Raw holds raw file bytes when the document is not structured key-value data.
	Raw []byte
}

// StateStore persists and retrieves desired state. It is indifferent to how or
// when that state becomes reality on a server.
//
// Every implementation must guarantee that an unchanged write produces no
// change/commit (the no-op path).
type StateStore interface {
	// Get retrieves a document by its relative path.
	Get(ctx context.Context, path string) (Document, error)
	// Put writes a document at path. If the content is identical to the current
	// state, it is a no-op and produces no commit/change.
	Put(ctx context.Context, path string, doc Document, msg string) error
	// Patch reads the document at path, passes it to mutate, and writes the
	// updated document if mutate returns changed=true. If changed=false, no
	// commit or write occurs.
	Patch(ctx context.Context, path string, msg string, mutate func(doc *Document) (bool, error)) (bool, error)
	// Delete removes a document or directory at path. If path does not exist,
	// it is a no-op.
	Delete(ctx context.Context, path string, msg string) error
	// PutTree writes a collection of documents under dirPath, creating new files,
	// updating modified ones, and removing any files in dirPath that are omitted
	// from docs.
	PutTree(ctx context.Context, dirPath string, docs map[string]Document, msg string) error
}
