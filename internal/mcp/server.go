package mcp

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	mcpsdk "github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
	"github.com/ulf/paperless-aissist/internal/config"
	"github.com/ulf/paperless-aissist/internal/imageconv"
	"github.com/ulf/paperless-aissist/internal/paperless"
	"github.com/ulf/paperless-aissist/internal/rag"
	"github.com/ulf/paperless-aissist/internal/tools"
	"github.com/ulf/paperless-aissist/internal/types"
)

func NewHTTPHandler() http.Handler {
	s := server.NewMCPServer("paperless-aissist", "0.1.0",
		server.WithResourceCapabilities(false, false),
	)

	// Resource template: paperless://document/{id}/pdf
	s.AddResourceTemplate(
		mcpsdk.NewResourceTemplate(
			"paperless://document/{id}/pdf",
			"Document PDF",
			mcpsdk.WithTemplateDescription("Full PDF file for a paperless-ngx document"),
			mcpsdk.WithTemplateMIMEType("application/pdf"),
		),
		func(ctx context.Context, req mcpsdk.ReadResourceRequest) ([]mcpsdk.ResourceContents, error) {
			parts := strings.Split(req.Params.URI, "/")
			// "paperless://document/42/pdf" → ["paperless:", "", "document", "42", "pdf"]
			if len(parts) < 5 {
				return nil, fmt.Errorf("invalid resource URI: %s", req.Params.URI)
			}
			id, err := strconv.Atoi(parts[3])
			if err != nil {
				return nil, fmt.Errorf("invalid document ID in URI: %s", req.Params.URI)
			}
			cfg, _ := config.Load()
			pc := paperless.New(cfg.PaperlessURL, cfg.PaperlessToken, cfg.StrictMetadata)
			data, contentType, _, err := pc.GetDocumentDownload(id)
			if err != nil {
				return nil, err
			}
			return []mcpsdk.ResourceContents{
				mcpsdk.BlobResourceContents{
					URI:      req.Params.URI,
					MIMEType: contentType,
					Blob:     base64.StdEncoding.EncodeToString(data),
				},
			}, nil
		},
	)

	// Resource template: paperless://document/{id}/preview
	s.AddResourceTemplate(
		mcpsdk.NewResourceTemplate(
			"paperless://document/{id}/preview",
			"Document Preview",
			mcpsdk.WithTemplateDescription("JPEG thumbnail preview for a paperless-ngx document"),
			mcpsdk.WithTemplateMIMEType("image/jpeg"),
		),
		func(ctx context.Context, req mcpsdk.ReadResourceRequest) ([]mcpsdk.ResourceContents, error) {
			parts := strings.Split(req.Params.URI, "/")
			if len(parts) < 5 {
				return nil, fmt.Errorf("invalid resource URI: %s", req.Params.URI)
			}
			id, err := strconv.Atoi(parts[3])
			if err != nil {
				return nil, fmt.Errorf("invalid document ID in URI: %s", req.Params.URI)
			}
			cfg, _ := config.Load()
			pc := paperless.New(cfg.PaperlessURL, cfg.PaperlessToken, cfg.StrictMetadata)
			thumbData, _, err := pc.GetDocumentThumb(id)
			if err != nil {
				return nil, err
			}
			jpegData, err := imageconv.WebPToJPEG(thumbData, 85)
			if err != nil {
				return nil, err
			}
			return []mcpsdk.ResourceContents{
				mcpsdk.BlobResourceContents{
					URI:      req.Params.URI,
					MIMEType: "image/jpeg",
					Blob:     base64.StdEncoding.EncodeToString(jpegData),
				},
			}, nil
		},
	)

	// helper: run a tool via the shared tools package and return JSON
	runTool := func(name string, args json.RawMessage) (*mcpsdk.CallToolResult, error) {
		cfg, _ := config.Load()
		pc := paperless.New(cfg.PaperlessURL, cfg.PaperlessToken, cfg.StrictMetadata)
		res, err := tools.Execute(name, args, pc, cfg.PaperlessURL)
		if err != nil {
			return mcpsdk.NewToolResultError(err.Error()), nil
		}
		return jsonResult(res.Data)
	}

	// list_tags
	s.AddTool(mcpsdk.NewTool("list_tags",
		mcpsdk.WithDescription("List all tags in the paperless-ngx archive with their document counts."),
	), func(ctx context.Context, req mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
		return runTool("list_tags", nil)
	})

	// list_correspondents
	s.AddTool(mcpsdk.NewTool("list_correspondents",
		mcpsdk.WithDescription("List all correspondents (senders/recipients) in the paperless-ngx archive."),
	), func(ctx context.Context, req mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
		return runTool("list_correspondents", nil)
	})

	// list_document_types
	s.AddTool(mcpsdk.NewTool("list_document_types",
		mcpsdk.WithDescription("List all document types in the paperless-ngx archive."),
	), func(ctx context.Context, req mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
		return runTool("list_document_types", nil)
	})

	// count_documents
	s.AddTool(mcpsdk.NewTool("count_documents",
		mcpsdk.WithDescription("Return the total number of documents stored in the archive."),
	), func(ctx context.Context, req mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
		return runTool("count_documents", nil)
	})

	// get_document
	s.AddTool(mcpsdk.NewTool("get_document",
		mcpsdk.WithDescription("Get a document by ID. Returns metadata (title, correspondent, document type, tags, dates) "+
			"and MCP resource URIs for fetching the PDF (resource_uri) and JPEG thumbnail (preview_uri) via resources/read. "+
			"Set include_content=true to also return the full OCR text (can be very large — only use when text analysis is required)."),
		mcpsdk.WithNumber("id", mcpsdk.Required(), mcpsdk.Description("Document ID")),
		mcpsdk.WithBoolean("include_content", mcpsdk.Description("Include full OCR text in response (default false)")),
	), func(ctx context.Context, req mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
		args := req.GetArguments()
		id := int(args["id"].(float64))
		includeContent, _ := args["include_content"].(bool)
		cfg, _ := config.Load()
		pc := paperless.New(cfg.PaperlessURL, cfg.PaperlessToken, cfg.StrictMetadata)
		doc, err := pc.GetDocument(id)
		if err != nil {
			return mcpsdk.NewToolResultError(err.Error()), nil
		}
		type docMeta struct {
			ID            int    `json:"id"`
			Title         string `json:"title"`
			Created       string `json:"created"`
			Modified      string `json:"modified"`
			Correspondent *int   `json:"correspondent"`
			DocumentType  *int   `json:"document_type"`
			Tags          []int  `json:"tags"`
			ResourceURI   string `json:"resource_uri"`
			PreviewURI    string `json:"preview_uri"`
			Content       string `json:"content,omitempty"`
		}
		res := docMeta{
			ID:            doc.ID,
			Title:         doc.Title,
			Created:       doc.Created,
			Modified:      doc.Modified,
			Correspondent: doc.Correspondent,
			DocumentType:  doc.DocumentType,
			Tags:          doc.Tags,
			ResourceURI:   fmt.Sprintf("paperless://document/%d/pdf", id),
			PreviewURI:    fmt.Sprintf("paperless://document/%d/preview", id),
		}
		if includeContent {
			res.Content = doc.Content
		}
		return mcpsdk.NewToolResultStructured(res, doc.Title), nil
	})

	// search_documents (unified: semantic by default, keyword with semantic=false)
	s.AddTool(mcpsdk.NewTool("search_documents",
		mcpsdk.WithDescription("Search the document archive. Uses semantic (vector) search by default — finds documents by meaning, not just exact words. "+
			"Infer the document type in your query for best results (e.g. 'Überweisung Müller Kontoauszug' instead of just 'Müller'). "+
			"Use sort_by='date' for 'latest/newest X' questions. "+
			"Set semantic=false for exact keyword or title matching."),
		mcpsdk.WithString("query", mcpsdk.Required(), mcpsdk.Description("Search query")),
		mcpsdk.WithBoolean("semantic", mcpsdk.Description("Use semantic/vector search (default true). Set to false for exact keyword matching.")),
		mcpsdk.WithNumber("max_results", mcpsdk.Description("Max results (default 10)")),
		mcpsdk.WithString("sort_by", mcpsdk.Description("Semantic only: 'relevance' (default) or 'date' (newest first)")),
	), func(ctx context.Context, req mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
		args := req.GetArguments()
		query, _ := args["query"].(string)
		semantic := true
		if s, ok := args["semantic"].(bool); ok {
			semantic = s
		}
		maxResults := 10
		if n, ok := args["max_results"].(float64); ok && n > 0 {
			maxResults = int(n)
		}
		if semantic {
			sortBy, _ := args["sort_by"].(string)
			idx := rag.Global()
			if idx == nil {
				return mcpsdk.NewToolResultError("RAG index not initialized — semantic search unavailable"), nil
			}
			cfg, _ := config.Load()
			results, err := idx.SearchText(ctx, cfg.OpenAIBaseURL, cfg.EmbedModel, cfg.OpenAIAPIKey, query, maxResults, sortBy)
			if err != nil {
				return mcpsdk.NewToolResultError(err.Error()), nil
			}
			return jsonResult(results)
		}
		b, _ := json.Marshal(map[string]any{"query": query, "page_size": maxResults})
		return runTool("search_documents", b)
	})

	// list_documents_by_type
	s.AddTool(mcpsdk.NewTool("list_documents_by_type",
		mcpsdk.WithDescription("List all documents with a given document type (e.g. 'Zeugnis', 'Rechnung', 'Kontoauszug')."),
		mcpsdk.WithString("type_name", mcpsdk.Required(), mcpsdk.Description("Document type name (case-insensitive)")),
	), func(ctx context.Context, req mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
		args := req.GetArguments()
		b, _ := json.Marshal(map[string]any{"type_name": args["type_name"]})
		return runTool("list_documents_by_type", b)
	})

	// list_documents_by_correspondent
	s.AddTool(mcpsdk.NewTool("list_documents_by_correspondent",
		mcpsdk.WithDescription("List all documents from a specific correspondent (sender/recipient). Case-insensitive name match."),
		mcpsdk.WithString("correspondent_name", mcpsdk.Required(), mcpsdk.Description("Correspondent name (case-insensitive)")),
	), func(ctx context.Context, req mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
		args := req.GetArguments()
		b, _ := json.Marshal(map[string]any{"correspondent_name": args["correspondent_name"]})
		return runTool("list_documents_by_correspondent", b)
	})

	// list_documents_by_tag
	s.AddTool(mcpsdk.NewTool("list_documents_by_tag",
		mcpsdk.WithDescription("List all documents with a specific tag. Use when the user asks for documents with a given label or category."),
		mcpsdk.WithString("tag_name", mcpsdk.Required(), mcpsdk.Description("Tag name (case-insensitive)")),
	), func(ctx context.Context, req mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
		args := req.GetArguments()
		b, _ := json.Marshal(map[string]any{"tag_name": args["tag_name"]})
		return runTool("list_documents_by_tag", b)
	})

	// find_documents
	s.AddTool(mcpsdk.NewTool("find_documents",
		mcpsdk.WithDescription("Find documents using combined filters: correspondent, document type, tags, date range, and optional freetext query. "+
			"Best choice for RECENCY questions ('die letzte Rechnung von Telekom', 'letzter Werkstattbesuch Galaxy', 'aktuellste Unterlagen von X'): "+
			"set correspondent_name or document_type_name, results are sorted newest-first by default — always complete, never misses a new document. "+
			"All parameters are optional; omit those not needed."),
		mcpsdk.WithString("query", mcpsdk.Description("Optional freetext search")),
		mcpsdk.WithString("correspondent_name", mcpsdk.Description("Filter by correspondent name (case-insensitive)")),
		mcpsdk.WithString("document_type_name", mcpsdk.Description("Filter by document type (case-insensitive)")),
		mcpsdk.WithString("created_from", mcpsdk.Description("Earliest creation date inclusive (YYYY-MM-DD)")),
		mcpsdk.WithString("created_to", mcpsdk.Description("Latest creation date inclusive (YYYY-MM-DD)")),
		mcpsdk.WithString("ordering", mcpsdk.Description("Sort order: '-created' newest first (default), 'created' oldest first")),
		mcpsdk.WithNumber("limit", mcpsdk.Description("Max results (default 50)")),
	), func(ctx context.Context, req mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
		args := req.GetArguments()
		b, _ := json.Marshal(map[string]any{
			"query":             args["query"],
			"correspondent_name": args["correspondent_name"],
			"document_type_name": args["document_type_name"],
			"created_from":      args["created_from"],
			"created_to":        args["created_to"],
			"ordering":          args["ordering"],
			"limit":             args["limit"],
		})
		return runTool("find_documents", b)
	})

	// list_recent_documents
	s.AddTool(mcpsdk.NewTool("list_recent_documents",
		mcpsdk.WithDescription("List the most recently created documents across the entire archive, without a topic filter. "+
			"Use for 'what came in recently', 'what's new this week', 'what was added this month'. "+
			"NOT for 'the latest invoice from X' or 'last visit to Y' — use find_documents with correspondent_name for those."),
		mcpsdk.WithNumber("limit", mcpsdk.Description("Max results (default 10)")),
		mcpsdk.WithString("since", mcpsdk.Description("Only include documents created on or after this date (YYYY-MM-DD)")),
	), func(ctx context.Context, req mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
		args := req.GetArguments()
		b, _ := json.Marshal(map[string]any{"limit": args["limit"], "since": args["since"]})
		return runTool("list_recent_documents", b)
	})

	// add_tag_to_document
	s.AddTool(mcpsdk.NewTool("add_tag_to_document",
		mcpsdk.WithDescription("Add a tag to a document. Set create_if_missing=true to create the tag if it doesn't exist yet."),
		mcpsdk.WithNumber("id", mcpsdk.Required(), mcpsdk.Description("Document ID")),
		mcpsdk.WithString("tag_name", mcpsdk.Required(), mcpsdk.Description("Tag name to add")),
		mcpsdk.WithBoolean("create_if_missing", mcpsdk.Description("Create the tag if it does not exist (default false)")),
	), func(ctx context.Context, req mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
		args := req.GetArguments()
		b, _ := json.Marshal(map[string]any{
			"id":                args["id"],
			"tag_name":          args["tag_name"],
			"create_if_missing": args["create_if_missing"],
		})
		cfg, _ := config.Load()
		pc := paperless.New(cfg.PaperlessURL, cfg.PaperlessToken, cfg.StrictMetadata)
		tr, err := tools.Execute("add_tag_to_document", b, pc, cfg.PaperlessURL)
		if err != nil {
			return mcpsdk.NewToolResultError(err.Error()), nil
		}
		return mcpsdk.NewToolResultText(tr.Text), nil
	})

	// remove_tag_from_document
	s.AddTool(mcpsdk.NewTool("remove_tag_from_document",
		mcpsdk.WithDescription("Remove a tag from a document. Idempotent — no error if the tag is not present on the document."),
		mcpsdk.WithNumber("id", mcpsdk.Required(), mcpsdk.Description("Document ID")),
		mcpsdk.WithString("tag_name", mcpsdk.Required(), mcpsdk.Description("Tag name to remove")),
	), func(ctx context.Context, req mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
		args := req.GetArguments()
		b, _ := json.Marshal(map[string]any{"id": args["id"], "tag_name": args["tag_name"]})
		cfg, _ := config.Load()
		pc := paperless.New(cfg.PaperlessURL, cfg.PaperlessToken, cfg.StrictMetadata)
		tr, err := tools.Execute("remove_tag_from_document", b, pc, cfg.PaperlessURL)
		if err != nil {
			return mcpsdk.NewToolResultError(err.Error()), nil
		}
		return mcpsdk.NewToolResultText(tr.Text), nil
	})

	// upload_document
	s.AddTool(mcpsdk.NewTool("upload_document",
		mcpsdk.WithDescription("Upload a new document to the paperless-ngx archive. "+
			"File must be provided as a base64-encoded string. "+
			"Optionally set title, correspondent, document type, tags, and creation date. "+
			"Waits up to 60 seconds for processing by default."),
		mcpsdk.WithString("filename", mcpsdk.Required(), mcpsdk.Description("Original filename including extension (e.g. 'invoice.pdf')")),
		mcpsdk.WithString("base64_content", mcpsdk.Required(), mcpsdk.Description("Base64-encoded file content")),
		mcpsdk.WithString("title", mcpsdk.Description("Document title (optional)")),
		mcpsdk.WithString("correspondent_name", mcpsdk.Description("Correspondent name — created if missing (optional)")),
		mcpsdk.WithString("document_type_name", mcpsdk.Description("Document type name — created if missing (optional)")),
		mcpsdk.WithString("created", mcpsdk.Description("Document creation date (YYYY-MM-DD, optional)")),
		mcpsdk.WithBoolean("wait", mcpsdk.Description("Wait for processing to complete (default true)")),
	), func(ctx context.Context, req mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
		args := req.GetArguments()
		b, _ := json.Marshal(map[string]any{
			"filename":          args["filename"],
			"base64_content":    args["base64_content"],
			"title":             args["title"],
			"correspondent_name": args["correspondent_name"],
			"document_type_name": args["document_type_name"],
			"created":           args["created"],
			"wait":              args["wait"],
		})
		return runTool("upload_document", b)
	})

	// update_document
	s.AddTool(mcpsdk.NewTool("update_document",
		mcpsdk.WithDescription("Update a document's metadata in paperless-ngx"),
		mcpsdk.WithNumber("id", mcpsdk.Required(), mcpsdk.Description("Document ID")),
		mcpsdk.WithString("title", mcpsdk.Description("Document title")),
		mcpsdk.WithString("correspondent", mcpsdk.Description("Correspondent name")),
		mcpsdk.WithString("document_type", mcpsdk.Description("Document type name")),
		mcpsdk.WithString("created_date", mcpsdk.Description("Created date (YYYY-MM-DD)")),
	), func(ctx context.Context, req mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
		args := req.GetArguments()
		id := int(args["id"].(float64))
		cfg, _ := config.Load()
		pc := paperless.New(cfg.PaperlessURL, cfg.PaperlessToken, cfg.StrictMetadata)

		doc, err := pc.GetDocument(id)
		if err != nil {
			return mcpsdk.NewToolResultError(err.Error()), nil
		}

		title := doc.Title
		if t, ok := args["title"].(string); ok && t != "" {
			title = t
		}
		created := doc.Created
		if d, ok := args["created_date"].(string); ok && d != "" {
			created = d + "T00:00:00Z"
		}

		correspondents, _ := pc.ListCorrespondents()
		docTypes, _ := pc.ListDocumentTypes()

		var corrID *int
		if c, ok := args["correspondent"].(string); ok && c != "" {
			corrID, _ = pc.ResolveOrCreate("correspondent", c, correspondents)
		}
		var dtID *int
		if d, ok := args["document_type"].(string); ok && d != "" {
			dtID, _ = pc.ResolveOrCreate("document_type", d, docTypes)
		}

		update := types.DocumentUpdate{
			Title:         title,
			Created:       created,
			Correspondent: corrID,
			DocumentType:  dtID,
			Tags:          doc.Tags,
		}
		if err := pc.UpdateDocument(id, update); err != nil {
			return mcpsdk.NewToolResultError(err.Error()), nil
		}
		return mcpsdk.NewToolResultText(fmt.Sprintf("Document %d updated successfully", id)), nil
	})

	return server.NewStreamableHTTPServer(s, server.WithStateLess(true))
}

func namesOf(items []types.PaperlessItem) []string {
	names := make([]string, len(items))
	for i, item := range items {
		names[i] = item.Name
	}
	return names
}

func jsonResult(v any) (*mcpsdk.CallToolResult, error) {
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return mcpsdk.NewToolResultError(err.Error()), nil
	}
	return mcpsdk.NewToolResultText(string(data)), nil
}

func applyAndSave(pc *paperless.Client, doc *types.PaperlessDocument, result *types.AnalysisResult, tags, correspondents, docTypes []types.PaperlessItem) error {
	var tagIDs []int
	for _, name := range result.Tags {
		id, err := pc.ResolveOrCreate("tag", name, tags)
		if err == nil && id != nil {
			tagIDs = append(tagIDs, *id)
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
	return pc.UpdateDocument(doc.ID, update)
}
