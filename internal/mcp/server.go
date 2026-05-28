package mcp

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	mcpsdk "github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
	"github.com/ulf/paperless-aissist/internal/config"
	"github.com/ulf/paperless-aissist/internal/imageconv"
	"github.com/ulf/paperless-aissist/internal/paperless"
	"github.com/ulf/paperless-aissist/internal/rag"
	"github.com/ulf/paperless-aissist/internal/signedurl"
	"github.com/ulf/paperless-aissist/internal/tools"
	"github.com/ulf/paperless-aissist/internal/types"
)

func NewHTTPHandler() http.Handler {
	s := server.NewMCPServer("paperless-aissist", "0.1.0")

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
		mcpsdk.WithDescription("Retrieve full details and content of a specific document by its numeric ID. Use this immediately when a document ID is known."),
		mcpsdk.WithNumber("id", mcpsdk.Required(), mcpsdk.Description("Document ID")),
	), func(ctx context.Context, req mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
		args := req.GetArguments()
		b, _ := json.Marshal(map[string]any{"id": args["id"]})
		return runTool("get_document", b)
	})

	// search_documents
	s.AddTool(mcpsdk.NewTool("search_documents",
		mcpsdk.WithDescription("Search paperless-ngx by keyword, exact title, or person/company name. Use for exact name lookups or when the user mentions a specific organization."),
		mcpsdk.WithString("query", mcpsdk.Required(), mcpsdk.Description("Search query")),
		mcpsdk.WithNumber("page_size", mcpsdk.Description("Max results (default 10)")),
	), func(ctx context.Context, req mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
		args := req.GetArguments()
		b, _ := json.Marshal(map[string]any{"query": args["query"], "page_size": args["page_size"]})
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

	// download_document
	s.AddTool(mcpsdk.NewTool("download_document",
		mcpsdk.WithDescription("Download the original file of a document. "+
			"By default returns a short-lived signed URL (15 min) to avoid large MCP payloads. "+
			"Set inline=true to also receive the full base64-encoded content."),
		mcpsdk.WithNumber("id", mcpsdk.Required(), mcpsdk.Description("Document ID")),
		mcpsdk.WithBoolean("inline", mcpsdk.Description("Also include base64 content in response (default false)")),
	), func(ctx context.Context, req mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
		args := req.GetArguments()
		id := int(args["id"].(float64))
		inline, _ := args["inline"].(bool)
		cfg, _ := config.Load()
		pc := paperless.New(cfg.PaperlessURL, cfg.PaperlessToken, cfg.StrictMetadata)

		secret, err := signedurl.GetSecret(cfg.IndexPath)
		if err != nil {
			return mcpsdk.NewToolResultError("secret error: " + err.Error()), nil
		}
		token := signedurl.Sign(id, "pdf", 15*time.Minute, secret)

		type downloadResult struct {
			Filename string `json:"filename"`
			MimeType string `json:"mime_type"`
			URL      string `json:"url,omitempty"`
			Content  string `json:"content,omitempty"`
		}
		res := downloadResult{}

		if cfg.FileURLBase != "" {
			res.URL = cfg.FileURLBase + "/files/" + token
		}

		if inline || cfg.FileURLBase == "" {
			data, contentType, filename, err := pc.GetDocumentDownload(id)
			if err != nil {
				return mcpsdk.NewToolResultError(err.Error()), nil
			}
			res.Filename = filename
			res.MimeType = contentType
			res.Content = base64.StdEncoding.EncodeToString(data)
		} else {
			// Fetch only metadata (filename, mime_type) without downloading the full PDF
			doc, err := pc.GetDocument(id)
			if err != nil {
				return mcpsdk.NewToolResultError(err.Error()), nil
			}
			res.Filename = doc.Title + ".pdf"
			res.MimeType = "application/pdf"
		}

		return mcpsdk.NewToolResultStructured(res, res.Filename), nil
	})

	// document_preview
	s.AddTool(mcpsdk.NewTool("document_preview",
		mcpsdk.WithDescription("Download a document with its thumbnail preview. "+
			"By default returns short-lived signed URLs (15 min) for both PDF and JPEG preview. "+
			"Set inline=true to also receive base64-encoded content and preview image. "+
			"Preview image is JPEG (converted from paperless-ngx WebP thumbnail)."),
		mcpsdk.WithNumber("doc_id", mcpsdk.Required(), mcpsdk.Description("Document ID")),
		mcpsdk.WithBoolean("inline", mcpsdk.Description("Also include base64 content and preview in response (default false)")),
	), func(ctx context.Context, req mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
		args := req.GetArguments()
		id := int(args["doc_id"].(float64))
		inline, _ := args["inline"].(bool)
		cfg, _ := config.Load()
		pc := paperless.New(cfg.PaperlessURL, cfg.PaperlessToken, cfg.StrictMetadata)

		secret, err := signedurl.GetSecret(cfg.IndexPath)
		if err != nil {
			return mcpsdk.NewToolResultError("secret error: " + err.Error()), nil
		}
		pdfToken := signedurl.Sign(id, "pdf", 15*time.Minute, secret)
		previewToken := signedurl.Sign(id, "preview", 15*time.Minute, secret)

		type previewResult struct {
			Filename        string `json:"filename"`
			MimeType        string `json:"mime_type"`
			URL             string `json:"url,omitempty"`
			PreviewURL      string `json:"preview_url,omitempty"`
			PreviewMimeType string `json:"preview_mime_type"`
			Content         string `json:"content,omitempty"`
			Preview         string `json:"preview,omitempty"`
		}
		res := previewResult{PreviewMimeType: "image/jpeg"}

		if cfg.FileURLBase != "" {
			res.URL = cfg.FileURLBase + "/files/" + pdfToken
			res.PreviewURL = cfg.FileURLBase + "/files/" + previewToken
		}

		if inline || cfg.FileURLBase == "" {
			pdfData, pdfMime, filename, err := pc.GetDocumentDownload(id)
			if err != nil {
				return mcpsdk.NewToolResultError(err.Error()), nil
			}
			thumbData, _, err := pc.GetDocumentThumb(id)
			if err != nil {
				return mcpsdk.NewToolResultError(err.Error()), nil
			}
			jpegData, err := imageconv.WebPToJPEG(thumbData, 85)
			if err != nil {
				return mcpsdk.NewToolResultError("preview conversion: " + err.Error()), nil
			}
			res.Filename = filename
			res.MimeType = pdfMime
			res.Content = base64.StdEncoding.EncodeToString(pdfData)
			res.Preview = base64.StdEncoding.EncodeToString(jpegData)
		} else {
			doc, err := pc.GetDocument(id)
			if err != nil {
				return mcpsdk.NewToolResultError(err.Error()), nil
			}
			res.Filename = doc.Title + ".pdf"
			res.MimeType = "application/pdf"
		}

		return mcpsdk.NewToolResultStructured(res, res.Filename), nil
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

	// search_documents_semantic
	s.AddTool(mcpsdk.NewTool("search_documents_semantic",
		mcpsdk.WithDescription("Search the document archive by content similarity (vector search). "+
			"Before writing the query, infer what type of document would contain the answer — for example: "+
			"a question about a bank transfer implies 'Kontoauszug', taxes imply 'Steuerbescheid', "+
			"insurance implies 'Police' or 'Rechnung', employment implies 'Arbeitsvertrag' or 'Zeugnis'. "+
			"Include the inferred document type AND any person/company names in the query string. "+
			"Example: for 'Wann habe ich an Müller überwiesen?' use query 'Überweisung Müller Kontoauszug'. "+
			"Default sort is by content similarity. Use sort_by='date' for 'aktuellste/letzte/neueste X' questions — re-sorts the top results by document creation date (newest first)."),
		mcpsdk.WithString("query", mcpsdk.Required(), mcpsdk.Description("Descriptive semantic search query")),
		mcpsdk.WithNumber("max_docs", mcpsdk.Description("Max results (default 10)")),
		mcpsdk.WithString("sort_by", mcpsdk.Description("Sort order: 'relevance' (default, by similarity) or 'date' (newest document first)")),
	), func(ctx context.Context, req mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
		args := req.GetArguments()
		query, _ := args["query"].(string)
		k := 10
		if n, ok := args["max_docs"].(float64); ok && n > 0 {
			k = int(n)
		}
		sortBy, _ := args["sort_by"].(string)
		idx := rag.Global()
		if idx == nil {
			return mcpsdk.NewToolResultError("RAG index not initialized"), nil
		}
		cfg, _ := config.Load()
		results, err := idx.SearchText(ctx, cfg.OpenAIBaseURL, cfg.EmbedModel, cfg.OpenAIAPIKey, query, k, sortBy)
		if err != nil {
			return mcpsdk.NewToolResultError(err.Error()), nil
		}
		return jsonResult(results)
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
