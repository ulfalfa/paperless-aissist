package rag

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"

	chromem "github.com/philippgille/chromem-go"
)

const collectionName = "paperless-docs"

// Store wraps a chromem-go persistent DB and tracks indexed document metadata.
type Store struct {
	db         *chromem.DB
	collection *chromem.Collection
	metaPath   string
	meta       map[string]string // docID -> modified timestamp
}

// OpenStore opens or creates a persistent vector DB at the given directory path.
func OpenStore(path string) (*Store, error) {
	if err := os.MkdirAll(path, 0755); err != nil {
		return nil, err
	}
	db, err := chromem.NewPersistentDB(path, false)
	if err != nil {
		return nil, err
	}
	col, err := db.GetOrCreateCollection(collectionName, nil, nil)
	if err != nil {
		return nil, err
	}
	s := &Store{
		db:         db,
		collection: col,
		metaPath:   filepath.Join(path, "meta.json"),
		meta:       make(map[string]string),
	}
	s.loadMeta()
	return s, nil
}

func (s *Store) loadMeta() {
	data, err := os.ReadFile(s.metaPath)
	if err != nil {
		return // file doesn't exist yet — fine
	}
	_ = json.Unmarshal(data, &s.meta)
}

func (s *Store) saveMeta() error {
	data, err := json.Marshal(s.meta)
	if err != nil {
		return err
	}
	return os.WriteFile(s.metaPath, data, 0644)
}

// GetModified returns the stored modified timestamp for a doc ID (empty if not indexed).
func (s *Store) GetModified(id string) string {
	return s.meta[id]
}

// AllIndexedIDs returns all document IDs currently tracked in the index.
func (s *Store) AllIndexedIDs() []string {
	ids := make([]string, 0, len(s.meta))
	for id := range s.meta {
		ids = append(ids, id)
	}
	return ids
}

// Upsert stores a document embedding, replacing any previous version.
func (s *Store) Upsert(ctx context.Context, id, title, content, modified, created string, embedding []float32) error {
	// Delete existing entry if present (chromem errors on duplicate IDs)
	if _, exists := s.meta[id]; exists {
		_ = s.collection.Delete(ctx, nil, nil, id)
	}
	doc := chromem.Document{
		ID:        id,
		Content:   content,
		Embedding: embedding,
		Metadata:  map[string]string{"title": title, "modified": modified, "created": created},
	}
	if err := s.collection.AddDocuments(ctx, []chromem.Document{doc}, 1); err != nil {
		return err
	}
	s.meta[id] = modified
	return s.saveMeta()
}

// Remove deletes a document from the index.
func (s *Store) Remove(ctx context.Context, id string) error {
	if err := s.collection.Delete(ctx, nil, nil, id); err != nil {
		return err
	}
	delete(s.meta, id)
	return s.saveMeta()
}

// Reset drops and recreates the collection and clears the meta.
func (s *Store) Reset() error {
	s.db.DeleteCollection(collectionName)
	col, err := s.db.CreateCollection(collectionName, nil, nil)
	if err != nil {
		return err
	}
	s.collection = col
	s.meta = make(map[string]string)
	return s.saveMeta()
}

// Search returns the top-k most similar documents by cosine similarity.
func (s *Store) Search(ctx context.Context, queryEmbedding []float32, k int) ([]chromem.Result, error) {
	return s.collection.QueryEmbedding(ctx, queryEmbedding, k, nil, nil)
}

// Count returns the number of documents in the index.
func (s *Store) Count() int {
	return s.collection.Count()
}
