// Package local implements ports.StateStore on top of local files and SQLite.
// It provides reproducibility, history, and rollback without requiring Git or ArgoCD.
package local

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"agrelha/internal/ports"
	"gopkg.in/yaml.v3"
)

// Revision captures a recorded state change in the local store.
type Revision struct {
	ID        int64
	Path      string
	Message   string
	Content   []byte
	CreatedAt time.Time
}

// Adapter persists desired state to local files with optional revision history in SQLite.
type Adapter struct {
	rootDir string
	db      *sql.DB
	mu      sync.Mutex
}

// Option configures an Adapter.
type Option func(*Adapter)

// WithDB attaches a SQLite database to record state revision history and enable rollback.
func WithDB(db *sql.DB) Option {
	return func(a *Adapter) {
		a.db = db
	}
}

// New creates a new local StateStore adapter rooted at rootDir.
func New(rootDir string, opts ...Option) (*Adapter, error) {
	if rootDir == "" {
		rootDir = "."
	}
	if err := os.MkdirAll(rootDir, 0755); err != nil {
		return nil, fmt.Errorf("create state root dir %s: %w", rootDir, err)
	}
	abs, err := filepath.Abs(rootDir)
	if err != nil {
		return nil, err
	}
	a := &Adapter{rootDir: abs}
	for _, opt := range opts {
		opt(a)
	}
	if a.db != nil {
		if err := a.migrate(); err != nil {
			return nil, fmt.Errorf("migrate state history table: %w", err)
		}
	}
	return a, nil
}

var _ ports.StateStore = (*Adapter)(nil)

func (a *Adapter) migrate() error {
	_, err := a.db.Exec(`
	CREATE TABLE IF NOT EXISTS state_history (
		id         INTEGER PRIMARY KEY AUTOINCREMENT,
		path       TEXT NOT NULL,
		msg        TEXT NOT NULL,
		content    BLOB,
		created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
	);
	CREATE INDEX IF NOT EXISTS idx_state_history_path ON state_history (path);
	`)
	return err
}

func (a *Adapter) resolve(relPath string) (string, error) {
	cleaned := filepath.Clean(relPath)
	if strings.HasPrefix(cleaned, "..") || filepath.IsAbs(cleaned) {
		return "", fmt.Errorf("invalid relative path: %s", relPath)
	}
	full := filepath.Join(a.rootDir, cleaned)
	if !strings.HasPrefix(full, a.rootDir) {
		return "", fmt.Errorf("path escapes root directory: %s", relPath)
	}
	return full, nil
}

// Get retrieves a document by its relative path.
func (a *Adapter) Get(ctx context.Context, path string) (ports.Document, error) {
	a.mu.Lock()
	defer a.mu.Unlock()

	full, err := a.resolve(path)
	if err != nil {
		return ports.Document{}, err
	}

	raw, err := os.ReadFile(full)
	if err != nil {
		return ports.Document{}, err
	}

	var doc yaml.Node
	if err := yaml.Unmarshal(raw, &doc); err != nil || len(doc.Content) == 0 {
		return ports.Document{Raw: raw}, nil
	}

	root := doc.Content[0]
	var data map[string]string
	if dataNode := mapValue(root, "data"); dataNode != nil && dataNode.Kind == yaml.MappingNode {
		data = make(map[string]string)
		for i := 0; i+1 < len(dataNode.Content); i += 2 {
			data[dataNode.Content[i].Value] = dataNode.Content[i+1].Value
		}
	}

	var annotations map[string]string
	if metaNode := mapValue(root, "metadata"); metaNode != nil {
		if annNode := mapValue(metaNode, "annotations"); annNode != nil && annNode.Kind == yaml.MappingNode {
			annotations = make(map[string]string)
			for i := 0; i+1 < len(annNode.Content); i += 2 {
				annotations[annNode.Content[i].Value] = annNode.Content[i+1].Value
			}
		}
	}

	return ports.Document{
		Data:        data,
		Annotations: annotations,
		Raw:         raw,
	}, nil
}

func mapValue(node *yaml.Node, key string) *yaml.Node {
	if node.Kind != yaml.MappingNode {
		return nil
	}
	for i := 0; i+1 < len(node.Content); i += 2 {
		if node.Content[i].Value == key {
			return node.Content[i+1]
		}
	}
	return nil
}

// Put writes a document at path. If content is identical to current state, it is a no-op.
func (a *Adapter) Put(ctx context.Context, path string, doc ports.Document, msg string) error {
	a.mu.Lock()
	defer a.mu.Unlock()

	full, err := a.resolve(path)
	if err != nil {
		return err
	}

	content, err := serializeDocument(doc)
	if err != nil {
		return err
	}

	// No-op check: if identical, return nil without writing or logging
	if existing, err := os.ReadFile(full); err == nil && bytes.Equal(existing, content) {
		return nil
	}

	if err := os.MkdirAll(filepath.Dir(full), 0755); err != nil {
		return err
	}

	if err := os.WriteFile(full, content, 0644); err != nil {
		return err
	}

	if a.db != nil {
		_ = a.recordRevision(path, msg, content)
	}

	return nil
}

func serializeDocument(doc ports.Document) ([]byte, error) {
	if doc.Data != nil || doc.Annotations != nil {
		type Meta struct {
			Annotations map[string]string `yaml:"annotations,omitempty"`
		}
		type DocYAML struct {
			APIVersion string            `yaml:"apiVersion,omitempty"`
			Kind       string            `yaml:"kind,omitempty"`
			Metadata   Meta              `yaml:"metadata,omitempty"`
			Data       map[string]string `yaml:"data,omitempty"`
		}
		y := DocYAML{
			APIVersion: "v1",
			Kind:       "ConfigMap",
			Metadata:   Meta{Annotations: doc.Annotations},
			Data:       doc.Data,
		}
		var buf bytes.Buffer
		enc := yaml.NewEncoder(&buf)
		enc.SetIndent(2)
		if err := enc.Encode(y); err != nil {
			return nil, err
		}
		return buf.Bytes(), nil
	}
	return doc.Raw, nil
}

func (a *Adapter) recordRevision(path, msg string, content []byte) error {
	_, err := a.db.Exec(
		`INSERT INTO state_history (path, msg, content) VALUES (?, ?, ?)`,
		path, msg, content,
	)
	return err
}

// Patch loads document at path, calls mutate, and writes updated document if changed=true.
func (a *Adapter) Patch(ctx context.Context, path string, msg string, mutate func(doc *ports.Document) (bool, error)) (bool, error) {
	existingDoc, err := a.Get(ctx, path)
	if err != nil {
		if !errors.Is(err, os.ErrNotExist) {
			return false, err
		}
		existingDoc = ports.Document{
			Data:        make(map[string]string),
			Annotations: make(map[string]string),
		}
	}

	changed, err := mutate(&existingDoc)
	if err != nil || !changed {
		return false, err
	}

	if err := a.Put(ctx, path, existingDoc, msg); err != nil {
		return false, err
	}
	return true, nil
}

// Delete removes a document or directory at path. If path does not exist, it is a no-op.
func (a *Adapter) Delete(ctx context.Context, path string, msg string) error {
	a.mu.Lock()
	defer a.mu.Unlock()

	full, err := a.resolve(path)
	if err != nil {
		return err
	}

	if _, err := os.Stat(full); os.IsNotExist(err) {
		return nil
	}

	if err := os.RemoveAll(full); err != nil {
		return err
	}

	if a.db != nil {
		_ = a.recordRevision(path, msg, nil)
	}

	return nil
}

// PutTree writes documents under dirPath, creating/updating them and removing unmentioned files.
func (a *Adapter) PutTree(ctx context.Context, dirPath string, docs map[string]ports.Document, msg string) error {
	a.mu.Lock()
	defer a.mu.Unlock()

	fullDir, err := a.resolve(dirPath)
	if err != nil {
		return err
	}

	if err := os.MkdirAll(fullDir, 0755); err != nil {
		return err
	}

	keep := make(map[string]bool)
	var anyChanged bool

	for fname, doc := range docs {
		rel := filepath.Join(dirPath, fname)
		keep[fname] = true

		content, err := serializeDocument(doc)
		if err != nil {
			return err
		}

		targetFile := filepath.Join(fullDir, fname)
		if existing, err := os.ReadFile(targetFile); err == nil && bytes.Equal(existing, content) {
			continue
		}

		if err := os.WriteFile(targetFile, content, 0644); err != nil {
			return err
		}
		anyChanged = true
		if a.db != nil {
			_ = a.recordRevision(rel, msg, content)
		}
	}

	// Remove files not in keep
	entries, err := os.ReadDir(fullDir)
	if err == nil {
		for _, entry := range entries {
			if !entry.IsDir() && !keep[entry.Name()] {
				_ = os.Remove(filepath.Join(fullDir, entry.Name()))
				anyChanged = true
				if a.db != nil {
					_ = a.recordRevision(filepath.Join(dirPath, entry.Name()), msg+" (deleted)", nil)
				}
			}
		}
	}

	_ = anyChanged
	return nil
}

// History returns recorded revisions for a path in chronological order.
func (a *Adapter) History(ctx context.Context, path string) ([]Revision, error) {
	if a.db == nil {
		return nil, nil
	}
	a.mu.Lock()
	defer a.mu.Unlock()

	rows, err := a.db.QueryContext(ctx,
		`SELECT id, path, msg, content, created_at FROM state_history WHERE path = ? ORDER BY id ASC`,
		path,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var revs []Revision
	for rows.Next() {
		var r Revision
		if err := rows.Scan(&r.ID, &r.Path, &r.Message, &r.Content, &r.CreatedAt); err != nil {
			return nil, err
		}
		revs = append(revs, r)
	}
	return revs, rows.Err()
}

// Rollback restores a document to a prior revision ID.
func (a *Adapter) Rollback(ctx context.Context, path string, revID int64, msg string) error {
	if a.db == nil {
		return errors.New("rollback requires database-backed state history")
	}
	a.mu.Lock()
	var content []byte
	err := a.db.QueryRowContext(ctx,
		`SELECT content FROM state_history WHERE id = ? AND path = ?`,
		revID, path,
	).Scan(&content)
	a.mu.Unlock()
	if err != nil {
		return fmt.Errorf("find revision %d: %w", revID, err)
	}

	if content == nil {
		return a.Delete(ctx, path, msg)
	}

	var doc ports.Document
	var y struct {
		Metadata struct {
			Annotations map[string]string `yaml:"annotations"`
		} `yaml:"metadata"`
		Data map[string]string `yaml:"data"`
	}
	if err := yaml.Unmarshal(content, &y); err == nil && (len(y.Data) > 0 || len(y.Metadata.Annotations) > 0) {
		doc.Data = y.Data
		doc.Annotations = y.Metadata.Annotations
		doc.Raw = content
	} else {
		doc.Raw = content
	}

	return a.Put(ctx, path, doc, msg)
}
