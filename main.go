package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"

	"github.com/joho/godotenv"
	"github.com/ulf/paperless-aissist/internal/auth"
	"github.com/ulf/paperless-aissist/internal/config"
	"github.com/ulf/paperless-aissist/internal/logger"
	"github.com/ulf/paperless-aissist/internal/ollama"
	"github.com/ulf/paperless-aissist/internal/paperless"
	pdf "github.com/ulf/paperless-aissist/internal/pdf"
	"github.com/ulf/paperless-aissist/internal/promptlog"
	"github.com/ulf/paperless-aissist/internal/rag"
	"github.com/ulf/paperless-aissist/internal/scanner"
	"github.com/ulf/paperless-aissist/internal/types"
	"github.com/ulf/paperless-aissist/internal/validate"
	"github.com/ulf/paperless-aissist/internal/web"
)

// version is set via -ldflags="-X main.version=vX.Y.Z" at build time.
var version = "dev"

func main() {
	_ = godotenv.Load(".env")

	flag.Usage = func() {
		fmt.Fprintf(os.Stderr, "paperless-aissist %s — AI metadata extraction for paperless-ngx\n\n", version)
		fmt.Fprintf(os.Stderr, "Usage:\n")
		fmt.Fprintf(os.Stderr, "  paperless-aissist --server              Start web server\n")
		fmt.Fprintf(os.Stderr, "  paperless-aissist --pdf <file.pdf>      Analyze local PDF\n")
		fmt.Fprintf(os.Stderr, "  paperless-aissist --id <N>              Fetch and analyze document\n")
		fmt.Fprintf(os.Stderr, "  paperless-aissist --id <N> --dry-run    Analyze without writing\n")
		fmt.Fprintf(os.Stderr, "  paperless-aissist --version             Print version\n")
	}

	serverCmd := flag.Bool("server", false, "Run HTTP server")
	docID := flag.Int("id", 0, "Analyze a paperless-ngx document by ID")
	dryRun := flag.Bool("dry-run", false, "Analyze without writing back to paperless-ngx")
	pdfPath := flag.String("pdf", "", "Analyze a local PDF file (prints JSON, no write)")
	versionCmd := flag.Bool("version", false, "Print version and exit")
	flag.Parse()

	if *versionCmd {
		fmt.Println(version)
		return
	}

	// If no flag given but a positional argument exists, treat as pdf path
	if *pdfPath == "" && flag.NArg() > 0 {
		p := flag.Arg(0)
		pdfPath = &p
	}

	switch {
	case *serverCmd:
		runServer()
	case *pdfPath != "":
		runPDF(*pdfPath)
	case *docID > 0:
		runDocument(*docID, *dryRun)
	default:
		flag.Usage()
		os.Exit(1)
	}
}

func runServer() {
	cfg, err := config.Load()
	if err != nil {
		log.Fatal("config:", err)
	}
	if cfg.ScanInterval > 0 {
		scanner.Start(cfg.ScanInterval)
	}
	if err := rag.Init(cfg.IndexPath); err != nil {
		log.Fatal("rag init:", err)
	}
	if cfg.LogPrompts {
		promptlog.Init(cfg.IndexPath)
		logger.Info("Prompt logging enabled", cfg.IndexPath+"/prompts.jsonl")
	}
	am, err := auth.New(cfg.IndexPath + "/credentials.json")
	if err != nil {
		log.Fatal("auth init:", err)
	}
	logger.Info("Server starting", "port "+cfg.Port)
	log.Printf("paperless-aissist listening on :%s", cfg.Port)
	web.SetVersion(version)
	if err := http.ListenAndServe(":"+cfg.Port, web.NewRouter(am, cfg.MCPToken)); err != nil {
		log.Fatal(err)
	}
}

func runPDF(path string) {
	cfg, err := config.Load()
	if err != nil {
		log.Fatal("config:", err)
	}
	text, err := pdf.ExtractText(path)
	if err != nil {
		log.Fatal("pdf:", err)
	}
	result, err := analyze(cfg, text)
	if err != nil {
		log.Fatal("analyze:", err)
	}
	json.NewEncoder(os.Stdout).Encode(result)
}

func runDocument(id int, dryRun bool) {
	cfg, err := config.Load()
	if err != nil {
		log.Fatal("config:", err)
	}
	pc := paperless.New(cfg.PaperlessURL, cfg.PaperlessToken, cfg.StrictMetadata)
	doc, err := pc.GetDocument(id)
	if err != nil {
		log.Fatal("fetch:", err)
	}

	result, err := analyzeWithHints(cfg, pc, doc)
	if err != nil {
		log.Fatal("analyze:", err)
	}

	if dryRun {
		json.NewEncoder(os.Stdout).Encode(result)
		return
	}

	// Resolve metadata IDs and write back
	tags, _ := pc.ListTags()
	correspondents, _ := pc.ListCorrespondents()
	docTypes, _ := pc.ListDocumentTypes()

	var tagIDs []int
	for _, name := range result.Tags {
		tid, err := pc.ResolveOrCreate("tag", name, tags)
		if err == nil && tid != nil {
			tagIDs = append(tagIDs, *tid)
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
	created := doc.Created
	if result.CreatedDate != nil {
		created = *result.CreatedDate + "T00:00:00Z"
	}
	update := types.DocumentUpdate{
		Title:         result.Title,
		Created:       created,
		Correspondent: corrID,
		DocumentType:  dtID,
		Tags:          tagIDs,
	}
	if err := pc.UpdateDocument(id, update); err != nil {
		log.Fatal("update:", err)
	}
	fmt.Printf("Updated document %d: %s\n", id, result.Title)
}

func analyze(cfg *types.AppConfig, text string) (*types.AnalysisResult, error) {
	oc := ollama.New(cfg.OllamaHost, cfg.OllamaModel)
	promptTemplate, err := config.LoadPrompt(cfg.PromptFile)
	if err != nil {
		return nil, err
	}
	prompt := ollama.BuildPrompt(promptTemplate, text, types.Hints{})
	result, _, err := oc.Analyze(prompt, types.ModelOptions{})
	if err != nil {
		return nil, err
	}
	return validate.Apply(result, cfg.AITag), nil
}

func analyzeWithHints(cfg *types.AppConfig, pc *paperless.Client, doc *types.PaperlessDocument) (*types.AnalysisResult, error) {
	oc := ollama.New(cfg.OllamaHost, cfg.OllamaModel)
	promptTemplate, err := config.LoadPrompt(cfg.PromptFile)
	if err != nil {
		return nil, err
	}
	tags, _ := pc.ListTags()
	correspondents, _ := pc.ListCorrespondents()
	docTypes, _ := pc.ListDocumentTypes()
	hints := types.Hints{
		Tags:           namesOf(tags),
		Correspondents: namesOf(correspondents),
		DocumentTypes:  namesOf(docTypes),
	}
	prompt := ollama.BuildPrompt(promptTemplate, doc.Content, hints)
	result, _, err := oc.Analyze(prompt, types.ModelOptions{})
	if err != nil {
		return nil, err
	}
	return validate.Apply(result, cfg.AITag), nil
}

func namesOf(items []types.PaperlessItem) []string {
	names := make([]string, len(items))
	for i, item := range items {
		names[i] = item.Name
	}
	return names
}
