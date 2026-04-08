package rag

import (
	"context"
	"fmt"
	"strconv"
	"sync"
	"time"

	"github.com/ulf/paperless-aissist/internal/config"
	"github.com/ulf/paperless-aissist/internal/logger"
	"github.com/ulf/paperless-aissist/internal/ollama"
	"github.com/ulf/paperless-aissist/internal/paperless"
	"github.com/ulf/paperless-aissist/internal/types"
)

const maxContentBytes = 6000 // bge-m3 supports 8192 tokens; mxbai-embed-large ~1200

var globalIndexer *Indexer

// Init creates the global indexer and opens the persistent vector store.
func Init(indexPath string) error {
	var err error
	globalIndexer, err = newIndexer(indexPath)
	return err
}

// Global returns the package-level indexer (must call Init first).
func Global() *Indexer { return globalIndexer }

// Indexer manages the RAG vector index and background indexing state.
type Indexer struct {
	mu     sync.Mutex
	status types.IndexStatus
	store  *Store
}

func newIndexer(indexPath string) (*Indexer, error) {
	store, err := OpenStore(indexPath)
	if err != nil {
		return nil, err
	}
	return &Indexer{store: store}, nil
}

// GetStatus returns a copy of the current indexing status (thread-safe).
func (idx *Indexer) GetStatus() types.IndexStatus {
	idx.mu.Lock()
	defer idx.mu.Unlock()
	s := idx.status
	s.DocCount = idx.store.Count()
	return s
}

// IsRunning reports whether indexing is currently in progress.
func (idx *Indexer) IsRunning() bool {
	idx.mu.Lock()
	defer idx.mu.Unlock()
	return idx.status.Running
}

// RunOnce indexes documents. If full=true, resets the store first (complete rebuild).
// If full=false, only processes new/changed docs and removes deleted ones.
// Safe to call in a goroutine; a second call while running is a no-op.
func (idx *Indexer) RunOnce(full bool) {
	idx.mu.Lock()
	if idx.status.Running {
		idx.mu.Unlock()
		return
	}
	idx.status.Running = true
	idx.status.Done = 0
	idx.status.Errors = 0
	idx.status.Total = 0
	idx.mu.Unlock()

	defer func() {
		idx.mu.Lock()
		idx.status.Running = false
		now := time.Now().UTC().Format(time.RFC3339)
		idx.status.LastRun = &now
		idx.mu.Unlock()
	}()

	cfg, err := config.Load()
	if err != nil {
		logger.Error("RAG indexer: config load failed", err.Error())
		return
	}

	pc := paperless.New(cfg.PaperlessURL, cfg.PaperlessToken, cfg.StrictMetadata)
	oc := ollama.New(cfg.OllamaHost, cfg.OllamaModel)

	docs, err := pc.ListAllDocuments()
	if err != nil {
		logger.Error("RAG indexer: failed to fetch documents", err.Error())
		return
	}

	if full {
		logger.Info(fmt.Sprintf("RAG indexer: full re-index of %d documents", len(docs)))
		if err := idx.store.Reset(); err != nil {
			logger.Error("RAG indexer: failed to reset store", err.Error())
			return
		}
	} else {
		logger.Info(fmt.Sprintf("RAG indexer: incremental sync, %d documents in paperless", len(docs)))
	}

	// Build set of current paperless IDs for deletion detection
	paperlessIDs := make(map[string]bool, len(docs))
	for _, doc := range docs {
		paperlessIDs[strconv.Itoa(doc.ID)] = true
	}

	// Determine what needs to be done
	var toIndex []types.PaperlessDocument
	for _, doc := range docs {
		id := strconv.Itoa(doc.ID)
		if full || idx.store.GetModified(id) != doc.Modified {
			toIndex = append(toIndex, doc)
		}
	}

	// Remove deleted documents (only in incremental mode)
	if !full {
		ctx := context.Background()
		for _, id := range idx.store.AllIndexedIDs() {
			if !paperlessIDs[id] {
				if err := idx.store.Remove(ctx, id); err != nil {
					logger.Warn(fmt.Sprintf("RAG indexer: failed to remove deleted doc %s", id), err.Error())
				} else {
					logger.Info(fmt.Sprintf("RAG indexer: removed deleted doc %s", id))
				}
			}
		}
	}

	idx.mu.Lock()
	idx.status.Total = len(toIndex)
	idx.mu.Unlock()

	if len(toIndex) == 0 {
		logger.Info("RAG indexer: everything up to date, nothing to do")
		return
	}

	logger.Info(fmt.Sprintf("RAG indexer: embedding %d documents", len(toIndex)))

	ctx := context.Background()
	for _, doc := range toIndex {
		text := buildIndexText(doc)
		emb, err := oc.Embed(cfg.EmbedModel, text)
		if err != nil {
			logger.Error(fmt.Sprintf("RAG indexer: embed doc %d failed", doc.ID), err.Error())
			idx.mu.Lock()
			idx.status.Errors++
			idx.mu.Unlock()
			continue
		}
		id := strconv.Itoa(doc.ID)
		if err := idx.store.Upsert(ctx, id, doc.Title, text, doc.Modified, emb); err != nil {
			logger.Error(fmt.Sprintf("RAG indexer: store doc %d failed", doc.ID), err.Error())
			idx.mu.Lock()
			idx.status.Errors++
			idx.mu.Unlock()
			continue
		}
		idx.mu.Lock()
		idx.status.Done++
		idx.mu.Unlock()
	}

	logger.Info(fmt.Sprintf("RAG indexer: done, %d indexed, %d errors",
		idx.status.Done, idx.status.Errors))
}

// buildIndexText creates the text to embed for a document.
func buildIndexText(doc types.PaperlessDocument) string {
	content := doc.Content
	if len(content) > maxContentBytes {
		content = content[:maxContentBytes]
	}
	return "Title: " + doc.Title + "\n\n" + content
}
