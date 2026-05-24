package rag

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/ulf/paperless-aissist/internal/config"
	"github.com/ulf/paperless-aissist/internal/logger"
	"github.com/ulf/paperless-aissist/internal/openai"
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
	oc := openai.New(cfg.OpenAIBaseURL, cfg.OpenAIModel, cfg.OpenAIAPIKey)

	docs, err := pc.ListAllDocuments()
	if err != nil {
		logger.Error("RAG indexer: failed to fetch documents", err.Error())
		return
	}

	tags, err := pc.ListTags()
	if err != nil {
		logger.Warn("RAG indexer: failed to fetch tags", err.Error())
	}
	correspondents, err := pc.ListCorrespondents()
	if err != nil {
		logger.Warn("RAG indexer: failed to fetch correspondents", err.Error())
	}
	docTypes, err := pc.ListDocumentTypes()
	if err != nil {
		logger.Warn("RAG indexer: failed to fetch document types", err.Error())
	}

	tagNames := make(map[int]string, len(tags))
	for _, t := range tags {
		tagNames[t.ID] = t.Name
	}
	correspondentNames := make(map[int]string, len(correspondents))
	for _, c := range correspondents {
		correspondentNames[c.ID] = c.Name
	}
	docTypeNames := make(map[int]string, len(docTypes))
	for _, dt := range docTypes {
		docTypeNames[dt.ID] = dt.Name
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
		text := buildIndexText(doc, tagNames, correspondentNames, docTypeNames)
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
func buildIndexText(doc types.PaperlessDocument, tagNames, correspondentNames, docTypeNames map[int]string) string {
	content := doc.Content
	if len(content) > maxContentBytes {
		content = content[:maxContentBytes]
	}

	meta := "Title: " + doc.Title
	if doc.Created != "" {
		meta += "\nDate: " + doc.Created
	}
	if doc.Correspondent != nil {
		if name, ok := correspondentNames[*doc.Correspondent]; ok {
			meta += "\nCorrespondent: " + name
		}
	}
	if doc.DocumentType != nil {
		if name, ok := docTypeNames[*doc.DocumentType]; ok {
			meta += "\nDocument Type: " + name
		}
	}
	if len(doc.Tags) > 0 {
		var tagList []string
		for _, id := range doc.Tags {
			if name, ok := tagNames[id]; ok {
				tagList = append(tagList, name)
			}
		}
		if len(tagList) > 0 {
			meta += "\nTags: " + strings.Join(tagList, ", ")
		}
	}

	return meta + "\n\n" + content
}

// SearchResult is a single hit from the vector index.
type SearchResult struct {
	DocID   int     `json:"doc_id"`
	Title   string  `json:"title"`
	Score   float64 `json:"score"`
	Snippet string  `json:"snippet"`
}

// SearchText embeds query and returns the top-k most similar documents.
func (idx *Indexer) SearchText(ctx context.Context, baseURL, embedModel, apiKey, query string, k int) ([]SearchResult, error) {
	oc := openai.New(baseURL, "", apiKey)
	emb, err := oc.Embed(embedModel, query)
	if err != nil {
		return nil, fmt.Errorf("embed query: %w", err)
	}
	hits, err := idx.store.Search(ctx, emb, k)
	if err != nil {
		return nil, fmt.Errorf("vector search: %w", err)
	}
	results := make([]SearchResult, 0, len(hits))
	for _, h := range hits {
		id, _ := strconv.Atoi(h.ID)
		snippet := h.Content
		if len(snippet) > 400 {
			snippet = snippet[:400] + "…"
		}
		results = append(results, SearchResult{
			DocID:   id,
			Title:   h.Metadata["title"],
			Score:   float64(h.Similarity),
			Snippet: snippet,
		})
	}
	return results, nil
}
