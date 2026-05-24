package scanner

import (
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/ulf/paperless-aissist/internal/config"
	"github.com/ulf/paperless-aissist/internal/logger"
	"github.com/ulf/paperless-aissist/internal/openai"
	"github.com/ulf/paperless-aissist/internal/paperless"
	"github.com/ulf/paperless-aissist/internal/rag"
	"github.com/ulf/paperless-aissist/internal/types"
	"github.com/ulf/paperless-aissist/internal/validate"
)

type Status struct {
	Running         bool
	LastRun         *time.Time
	LastCount       int
	LastErrors      []string
	NextRun         *time.Time
	IntervalMinutes int
	Total           int
	Done            int
}

var (
	mu     sync.Mutex
	status Status
	stopCh chan struct{}
)

func GetStatus() types.ScanStatus {
	mu.Lock()
	defer mu.Unlock()
	s := types.ScanStatus{
		Running:         status.Running,
		LastCount:       status.LastCount,
		LastErrors:      status.LastErrors,
		IntervalMinutes: status.IntervalMinutes,
		Total:           status.Total,
		Done:            status.Done,
	}
	if status.LastRun != nil {
		t := status.LastRun.UTC().Format(time.RFC3339)
		s.LastRun = &t
	}
	if status.NextRun != nil {
		t := status.NextRun.UTC().Format(time.RFC3339)
		s.NextRun = &t
	}
	return s
}

func IsRunning() bool {
	mu.Lock()
	defer mu.Unlock()
	return status.Running
}

// Start launches the scanner loop. Call Stop first to restart.
func Start(intervalMinutes int) {
	Stop()
	if intervalMinutes <= 0 {
		return
	}
	mu.Lock()
	stopCh = make(chan struct{})
	status.IntervalMinutes = intervalMinutes
	mu.Unlock()

	go func() {
		ticker := time.NewTicker(time.Duration(intervalMinutes) * time.Minute)
		defer ticker.Stop()
		// Run immediately on start
		RunOnce()
		for {
			next := time.Now().Add(time.Duration(intervalMinutes) * time.Minute)
			mu.Lock()
			status.NextRun = &next
			mu.Unlock()
			select {
			case <-ticker.C:
				RunOnce()
			case <-stopCh:
				return
			}
		}
	}()
}

// Stop halts the scanner loop.
func Stop() {
	mu.Lock()
	defer mu.Unlock()
	if stopCh != nil {
		close(stopCh)
		stopCh = nil
	}
	status.NextRun = nil
}

// RunOnce performs a single scan pass.
func RunOnce() {
	mu.Lock()
	if status.Running {
		mu.Unlock()
		return
	}
	status.Running = true
	status.Total = 0
	status.Done = 0
	mu.Unlock()

	defer func() {
		mu.Lock()
		status.Running = false
		now := time.Now()
		status.LastRun = &now
		mu.Unlock()
	}()

	logger.Info("Scanner: starting scan")

	cfg, err := config.Load()
	if err != nil || cfg.ScanTag == "" {
		logger.Warn("Scanner: no SCAN_TAG configured, skipping")
		return
	}

	pc := paperless.New(cfg.PaperlessURL, cfg.PaperlessToken, cfg.StrictMetadata)
	oc := openai.New(cfg.OpenAIBaseURL, cfg.OpenAIModel, cfg.OpenAIAPIKey)

	// Find tag ID for SCAN_TAG
	tags, err := pc.ListTags()
	if err != nil {
		logger.Error("Scanner: failed to list tags", err.Error())
		return
	}
	var scanTagID *int
	for _, t := range tags {
		if strings.EqualFold(t.Name, cfg.ScanTag) {
			id := t.ID
			scanTagID = &id
			break
		}
	}
	if scanTagID == nil {
		logger.Info("Scanner: SCAN_TAG not found, nothing to do", cfg.ScanTag)
		return
	}

	docs, err := pc.ListDocumentsByTag(*scanTagID)
	if err != nil {
		logger.Error("Scanner: failed to list documents", err.Error())
		return
	}
	if len(docs) == 0 {
		logger.Info("Scanner: no documents with SCAN_TAG", cfg.ScanTag)
		mu.Lock()
		status.LastCount = 0
		status.LastErrors = nil
		mu.Unlock()
		return
	}

	logger.Info(fmt.Sprintf("Scanner: found %d document(s)", len(docs)))
	mu.Lock()
	status.Total = len(docs)
	mu.Unlock()

	promptTemplate, err := config.LoadPrompt(cfg.PromptFile)
	if err != nil {
		logger.Error("Scanner: failed to load prompt", err.Error())
		return
	}

	correspondents, _ := pc.ListCorrespondents()
	docTypes, _ := pc.ListDocumentTypes()

	hints := types.Hints{
		Correspondents: namesOf(correspondents),
		DocumentTypes:  namesOf(docTypes),
		Tags:           namesOf(tags),
	}

	var errs []string
	count := 0
	for _, doc := range docs {
		prompt := openai.BuildPrompt(promptTemplate, doc.Content, hints)
		result, _, err := oc.Analyze(prompt, types.ModelOptions{})
		if err != nil {
			msg := fmt.Sprintf("doc %d: %s", doc.ID, err.Error())
			errs = append(errs, msg)
			logger.Error("Scanner: analysis failed", msg)
			continue
		}

		result = validate.Apply(result, cfg.AITag)

		// Build update: remove scan tag, resolve metadata IDs
		tagIDs, err := resolveTagIDs(pc, result.Tags, tags)
		if err != nil {
			errs = append(errs, fmt.Sprintf("doc %d tags: %s", doc.ID, err.Error()))
		}
		// Remove scan tag from tag list
		var filteredTags []int
		for _, id := range tagIDs {
			if id != *scanTagID {
				filteredTags = append(filteredTags, id)
			}
		}

		var corrID *int
		if result.Correspondent != nil {
			corrID, _ = pc.ResolveOrCreate("correspondent", *result.Correspondent, correspondents)
		}
		var dtID *int
		if result.DocumentType != nil {
			dtID, _ = pc.ResolveOrCreate("document_type", *result.DocumentType, docTypes)
		}

		created := ""
		if result.CreatedDate != nil {
			created = *result.CreatedDate + "T00:00:00Z"
		} else {
			created = doc.Created
		}

		update := types.DocumentUpdate{
			Title:         result.Title,
			Created:       created,
			Correspondent: corrID,
			DocumentType:  dtID,
			Tags:          filteredTags,
		}
		if err := pc.UpdateDocument(doc.ID, update); err != nil {
			errs = append(errs, fmt.Sprintf("doc %d update: %s", doc.ID, err.Error()))
			logger.Error("Scanner: update failed", fmt.Sprintf("doc %d: %s", doc.ID, err.Error()))
			continue
		}
		logger.Info(fmt.Sprintf("Scanner: updated doc %d — %s", doc.ID, result.Title))
		count++
		mu.Lock()
		status.Done = count
		mu.Unlock()
	}

	mu.Lock()
	status.LastCount = count
	status.LastErrors = errs
	mu.Unlock()
	logger.Info(fmt.Sprintf("Scanner: done, %d updated, %d errors", count, len(errs)))

	if count > 0 {
		logger.Info("Scanner: triggering RAG index sync for new documents")
		go rag.Global().RunOnce(false)
	}
}

func namesOf(items []types.PaperlessItem) []string {
	names := make([]string, len(items))
	for i, item := range items {
		names[i] = item.Name
	}
	return names
}

func resolveTagIDs(pc *paperless.Client, tagNames []string, existing []types.PaperlessItem) ([]int, error) {
	var ids []int
	for _, name := range tagNames {
		id, err := pc.ResolveOrCreate("tag", name, existing)
		if err != nil {
			return ids, err
		}
		if id != nil {
			ids = append(ids, *id)
		}
	}
	return ids, nil
}
