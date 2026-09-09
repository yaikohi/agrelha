// Package git adapts gitops.Committer to the ports.StateStore interface.
package git

import (
	"context"
	"path/filepath"

	"agrelha/internal/infra/gitops"
	"agrelha/internal/ports"
)

type Adapter struct {
	c *gitops.Committer
}

func New(c *gitops.Committer) *Adapter {
	return &Adapter{c: c}
}

var _ ports.StateStore = (*Adapter)(nil)

func (a *Adapter) Get(ctx context.Context, path string) (ports.Document, error) {
	if a == nil || a.c == nil {
		return ports.Document{}, ports.ErrNotImplemented
	}
	data, ann, raw, err := a.c.ReadDocument(ctx, path)
	if err != nil {
		return ports.Document{}, err
	}
	return ports.Document{Data: data, Annotations: ann, Raw: raw}, nil
}

func (a *Adapter) Put(ctx context.Context, path string, doc ports.Document, msg string) error {
	if a == nil || a.c == nil {
		return ports.ErrNotImplemented
	}
	if doc.Data != nil {
		if doc.Annotations != nil {
			_, err := a.c.ReplaceConfigMap(ctx, path, doc.Data, doc.Annotations, msg)
			return err
		}
		_, err := a.c.ReplaceData(ctx, path, doc.Data, msg)
		return err
	}
	dirRel := filepath.Dir(path)
	fname := filepath.Base(path)
	_, err := a.c.WriteDirectory(ctx, dirRel, map[string][]byte{fname: doc.Raw}, msg)
	return err
}

func (a *Adapter) Patch(ctx context.Context, path string, msg string, mutate func(doc *ports.Document) (bool, error)) (bool, error) {
	if a == nil || a.c == nil {
		return false, ports.ErrNotImplemented
	}
	return a.c.PatchDocument(ctx, path, msg, func(data, annotations map[string]string) (bool, error) {
		doc := ports.Document{
			Data:        data,
			Annotations: annotations,
		}
		changed, err := mutate(&doc)
		if err != nil || !changed {
			return false, err
		}
		return true, nil
	})
}

func (a *Adapter) Delete(ctx context.Context, path string, msg string) error {
	if a == nil || a.c == nil {
		return ports.ErrNotImplemented
	}
	_, err := a.c.DeleteDirectory(ctx, path, msg)
	return err
}

func (a *Adapter) PutTree(ctx context.Context, dirPath string, docs map[string]ports.Document, msg string) error {
	if a == nil || a.c == nil {
		return ports.ErrNotImplemented
	}
	files := make(map[string][]byte, len(docs))
	for fname, doc := range docs {
		if doc.Raw != nil {
			files[fname] = doc.Raw
		}
	}
	_, err := a.c.WriteDirectory(ctx, dirPath, files, msg)
	return err
}
