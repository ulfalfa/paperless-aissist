package paperless

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/ulf/paperless-aissist/internal/types"
)

// Client is a paperless-ngx REST API client.
type Client struct {
	baseURL string
	token   string
	http    *http.Client
	strict  bool
}

func New(baseURL, token string, strict bool) *Client {
	return &Client{
		baseURL: strings.TrimRight(baseURL, "/"),
		token:   token,
		http:    &http.Client{},
		strict:  strict,
	}
}

func (c *Client) get(path string, out any) error {
	req, err := http.NewRequest("GET", c.baseURL+path, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Token "+c.token)
	resp, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("GET %s: %d %s", path, resp.StatusCode, string(body))
	}
	return json.NewDecoder(resp.Body).Decode(out)
}

func (c *Client) patch(path string, payload any) error {
	data, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	req, err := http.NewRequest("PATCH", c.baseURL+path, bytes.NewReader(data))
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Token "+c.token)
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("PATCH %s: %d %s", path, resp.StatusCode, string(body))
	}
	return nil
}

func (c *Client) delete(path string) error {
	req, err := http.NewRequest("DELETE", c.baseURL+path, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Token "+c.token)
	resp, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("DELETE %s: %d %s", path, resp.StatusCode, string(body))
	}
	return nil
}

func (c *Client) post(path string, payload any, out any) error {
	data, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	req, err := http.NewRequest("POST", c.baseURL+path, bytes.NewReader(data))
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Token "+c.token)
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("POST %s: %d %s", path, resp.StatusCode, string(body))
	}
	if out != nil {
		return json.NewDecoder(resp.Body).Decode(out)
	}
	return nil
}

type listResponse[T any] struct {
	Results []T     `json:"results"`
	Next    *string `json:"next"`
}

func fetchAll[T any](c *Client, path string) ([]T, error) {
	var all []T
	next := c.baseURL + path + "?page_size=500"
	for next != "" {
		req, err := http.NewRequest("GET", next, nil)
		if err != nil {
			return nil, err
		}
		req.Header.Set("Authorization", "Token "+c.token)
		resp, err := c.http.Do(req)
		if err != nil {
			return nil, err
		}
		defer resp.Body.Close()
		var page listResponse[T]
		if err := json.NewDecoder(resp.Body).Decode(&page); err != nil {
			return nil, err
		}
		all = append(all, page.Results...)
		if page.Next != nil {
			next = *page.Next
		} else {
			next = ""
		}
	}
	return all, nil
}

func (c *Client) GetDocument(id int) (*types.PaperlessDocument, error) {
	var doc types.PaperlessDocument
	err := c.get(fmt.Sprintf("/api/documents/%d/", id), &doc)
	return &doc, err
}

func (c *Client) UpdateDocument(id int, update types.DocumentUpdate) error {
	return c.patch(fmt.Sprintf("/api/documents/%d/", id), update)
}

func (c *Client) ListTags() ([]types.PaperlessItem, error) {
	return fetchAll[types.PaperlessItem](c, "/api/tags/")
}

func (c *Client) ListCorrespondents() ([]types.PaperlessItem, error) {
	return fetchAll[types.PaperlessItem](c, "/api/correspondents/")
}

func (c *Client) ListDocumentTypes() ([]types.PaperlessItem, error) {
	return fetchAll[types.PaperlessItem](c, "/api/document_types/")
}

func (c *Client) SearchDocuments(query string, pageSize int) ([]types.PaperlessDocument, error) {
	q := url.QueryEscape(query)
	path := fmt.Sprintf("/api/documents/?query=%s&page_size=%d", q, pageSize)
	return fetchAll[types.PaperlessDocument](c, path)
}

func (c *Client) ListDocumentsByTag(tagID int) ([]types.PaperlessDocument, error) {
	path := fmt.Sprintf("/api/documents/?tags__id=%d&page_size=100", tagID)
	return fetchAll[types.PaperlessDocument](c, path)
}

func (c *Client) ListDocumentsByType(typeID int) ([]types.PaperlessDocument, error) {
	path := fmt.Sprintf("/api/documents/?document_type__id=%d&page_size=100", typeID)
	return fetchAll[types.PaperlessDocument](c, path)
}

func (c *Client) ListDocumentsByCorrespondent(correspondentID int) ([]types.PaperlessDocument, error) {
	path := fmt.Sprintf("/api/documents/?correspondent__id=%d&page_size=100", correspondentID)
	return fetchAll[types.PaperlessDocument](c, path)
}

// ListAllDocuments returns every document from paperless-ngx (all pages).
func (c *Client) ListAllDocuments() ([]types.PaperlessDocument, error) {
	return fetchAll[types.PaperlessDocument](c, "/api/documents/")
}

// CountDocuments returns the total number of documents in paperless-ngx.
func (c *Client) CountDocuments() (int, error) {
	var page struct {
		Count int `json:"count"`
	}
	if err := c.get("/api/documents/?page_size=1", &page); err != nil {
		return 0, err
	}
	return page.Count, nil
}

// FindOptions holds filter parameters for FindDocuments.
type FindOptions struct {
	Query           string
	CorrespondentID *int
	DocumentTypeID  *int
	TagIDs          []int
	CreatedFrom     string // YYYY-MM-DD
	CreatedTo       string // YYYY-MM-DD
	Ordering        string // default "-created"
	PageSize        int    // default 50
}

// FindDocuments fetches the first page of documents matching the given filters.
// Unlike ListDocumentsByX methods, this does NOT follow pagination — it returns
// at most PageSize results to keep latency predictable for agent use.
func (c *Client) FindDocuments(opts FindOptions) ([]types.PaperlessDocument, error) {
	params := url.Values{}
	if opts.Query != "" {
		params.Set("query", opts.Query)
	}
	if opts.CorrespondentID != nil {
		params.Set("correspondent__id", strconv.Itoa(*opts.CorrespondentID))
	}
	if opts.DocumentTypeID != nil {
		params.Set("document_type__id", strconv.Itoa(*opts.DocumentTypeID))
	}
	if len(opts.TagIDs) > 0 {
		parts := make([]string, len(opts.TagIDs))
		for i, t := range opts.TagIDs {
			parts[i] = strconv.Itoa(t)
		}
		params.Set("tags__id__in", strings.Join(parts, ","))
	}
	if opts.CreatedFrom != "" {
		params.Set("created__date__gte", opts.CreatedFrom)
	}
	if opts.CreatedTo != "" {
		params.Set("created__date__lte", opts.CreatedTo)
	}
	ordering := opts.Ordering
	if ordering == "" {
		ordering = "-created"
	}
	params.Set("ordering", ordering)
	pageSize := opts.PageSize
	if pageSize <= 0 {
		pageSize = 50
	}
	params.Set("page_size", strconv.Itoa(pageSize))

	fullURL := c.baseURL + "/api/documents/?" + params.Encode()
	req, err := http.NewRequest("GET", fullURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Token "+c.token)
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("find_documents: %d %s", resp.StatusCode, string(body))
	}
	var page listResponse[types.PaperlessDocument]
	if err := json.NewDecoder(resp.Body).Decode(&page); err != nil {
		return nil, err
	}
	return page.Results, nil
}

// GetDocumentDownload fetches the original binary file for a document.
// Returns the file bytes, Content-Type, and filename (from Content-Disposition).
func (c *Client) GetDocumentDownload(id int) ([]byte, string, string, error) {
	path := fmt.Sprintf("/api/documents/%d/download/", id)
	req, err := http.NewRequest("GET", c.baseURL+path, nil)
	if err != nil {
		return nil, "", "", err
	}
	req.Header.Set("Authorization", "Token "+c.token)
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, "", "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		body, _ := io.ReadAll(resp.Body)
		return nil, "", "", fmt.Errorf("download %d: %d %s", id, resp.StatusCode, string(body))
	}
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, "", "", err
	}
	ct := resp.Header.Get("Content-Type")
	if ct == "" {
		ct = "application/pdf"
	}
	filename := filenameFromContentDisposition(resp.Header.Get("Content-Disposition"))
	if filename == "" {
		filename = fmt.Sprintf("document_%d.pdf", id)
	}
	return data, ct, filename, nil
}

// filenameFromContentDisposition extracts the filename from a Content-Disposition header value.
// Handles both plain and RFC 5987 encoded filenames.
func filenameFromContentDisposition(cd string) string {
	// Try RFC 5987 filename* first (e.g. filename*=UTF-8''foo%20bar.pdf)
	if idx := strings.Index(cd, "filename*="); idx >= 0 {
		val := cd[idx+len("filename*="):]
		if semi := strings.IndexByte(val, ';'); semi >= 0 {
			val = val[:semi]
		}
		val = strings.TrimSpace(val)
		// Strip encoding prefix (e.g. UTF-8'')
		if apos := strings.LastIndex(val, "'"); apos >= 0 {
			val = val[apos+1:]
		}
		if unescaped, err := url.PathUnescape(val); err == nil && unescaped != "" {
			return unescaped
		}
	}
	// Fall back to plain filename= (quoted or unquoted)
	if idx := strings.Index(cd, "filename="); idx >= 0 {
		val := cd[idx+len("filename="):]
		if semi := strings.IndexByte(val, ';'); semi >= 0 {
			val = val[:semi]
		}
		val = strings.TrimSpace(val)
		val = strings.Trim(val, `"`)
		return val
	}
	return ""
}

// AddTags adds tag IDs to a document without removing existing tags. Idempotent.
func (c *Client) AddTags(id int, tagIDs []int) error {
	doc, err := c.GetDocument(id)
	if err != nil {
		return err
	}
	existing := make(map[int]bool, len(doc.Tags))
	for _, t := range doc.Tags {
		existing[t] = true
	}
	for _, t := range tagIDs {
		existing[t] = true
	}
	merged := make([]int, 0, len(existing))
	for t := range existing {
		merged = append(merged, t)
	}
	return c.UpdateDocument(id, types.DocumentUpdate{
		Title:         doc.Title,
		Created:       doc.Created,
		Correspondent: doc.Correspondent,
		DocumentType:  doc.DocumentType,
		Tags:          merged,
	})
}

// RemoveTags removes tag IDs from a document. Idempotent — no error if tag not present.
func (c *Client) RemoveTags(id int, tagIDs []int) error {
	doc, err := c.GetDocument(id)
	if err != nil {
		return err
	}
	remove := make(map[int]bool, len(tagIDs))
	for _, t := range tagIDs {
		remove[t] = true
	}
	var remaining []int
	for _, t := range doc.Tags {
		if !remove[t] {
			remaining = append(remaining, t)
		}
	}
	return c.UpdateDocument(id, types.DocumentUpdate{
		Title:         doc.Title,
		Created:       doc.Created,
		Correspondent: doc.Correspondent,
		DocumentType:  doc.DocumentType,
		Tags:          remaining,
	})
}

// UploadOptions holds optional metadata for a document upload.
type UploadOptions struct {
	Title           string
	CorrespondentID *int
	DocumentTypeID  *int
	TagIDs          []int
	Created         string // YYYY-MM-DD
}

// UploadDocument uploads a document via POST /api/documents/post_document/ and
// returns the task ID assigned by paperless-ngx for background processing.
func (c *Client) UploadDocument(filename string, data []byte, opts UploadOptions) (string, error) {
	var body bytes.Buffer
	w := multipart.NewWriter(&body)

	part, err := w.CreateFormFile("document", filename)
	if err != nil {
		return "", err
	}
	if _, err := part.Write(data); err != nil {
		return "", err
	}
	if opts.Title != "" {
		_ = w.WriteField("title", opts.Title)
	}
	if opts.CorrespondentID != nil {
		_ = w.WriteField("correspondent", strconv.Itoa(*opts.CorrespondentID))
	}
	if opts.DocumentTypeID != nil {
		_ = w.WriteField("document_type", strconv.Itoa(*opts.DocumentTypeID))
	}
	for _, id := range opts.TagIDs {
		_ = w.WriteField("tags", strconv.Itoa(id))
	}
	if opts.Created != "" {
		_ = w.WriteField("created", opts.Created)
	}
	w.Close()

	req, err := http.NewRequest("POST", c.baseURL+"/api/documents/post_document/", &body)
	if err != nil {
		return "", err
	}
	req.Header.Set("Authorization", "Token "+c.token)
	req.Header.Set("Content-Type", w.FormDataContentType())
	resp, err := c.http.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	respBody, _ := io.ReadAll(resp.Body)
	if resp.StatusCode >= 400 {
		return "", fmt.Errorf("upload failed: %d %s", resp.StatusCode, string(respBody))
	}
	// paperless returns the task ID as a JSON-quoted string: "\"<uuid>\""
	taskID := strings.Trim(strings.TrimSpace(string(respBody)), "\"")
	return taskID, nil
}

type taskResult struct {
	TaskID string `json:"task_id"`
	Status string `json:"status"` // PENDING, STARTED, SUCCESS, FAILURE
	Result string `json:"result"`
}

// WaitForTask polls /api/tasks/ until the task reaches a terminal state or the
// timeout elapses. Returns status ("SUCCESS", "FAILURE", "TIMEOUT") and result string.
func (c *Client) WaitForTask(taskID string, timeoutSecs int) (status, result string, err error) {
	deadline := time.Now().Add(time.Duration(timeoutSecs) * time.Second)
	for time.Now().Before(deadline) {
		var tasks []taskResult
		path := "/api/tasks/?task_id=" + url.QueryEscape(taskID)
		if ferr := c.get(path, &tasks); ferr != nil {
			return "", "", ferr
		}
		if len(tasks) > 0 {
			t := tasks[0]
			if t.Status == "SUCCESS" || t.Status == "FAILURE" {
				return t.Status, t.Result, nil
			}
		}
		time.Sleep(2 * time.Second)
	}
	return "TIMEOUT", "", nil
}

// ResolveOrCreate finds an item by name (case-insensitive) or creates it if missing.
// Returns nil if strict mode is on and the item doesn't exist.
func (c *Client) ResolveOrCreate(kind, name string, existing []types.PaperlessItem) (*int, error) {
	nameLower := strings.ToLower(strings.TrimSpace(name))
	for _, item := range existing {
		if strings.ToLower(item.Name) == nameLower {
			id := item.ID
			return &id, nil
		}
	}
	if c.strict {
		return nil, nil
	}
	// Create new
	var endpoint string
	switch kind {
	case "tag":
		endpoint = "/api/tags/"
	case "correspondent":
		endpoint = "/api/correspondents/"
	case "document_type":
		endpoint = "/api/document_types/"
	default:
		return nil, fmt.Errorf("unknown kind: %s", kind)
	}
	payload := map[string]string{"name": name}
	var created types.PaperlessItem
	err := c.post(endpoint, payload, &created)
	if err != nil {
		// Might be a race condition — re-fetch
		items, ferr := fetchAll[types.PaperlessItem](c, endpoint)
		if ferr != nil {
			return nil, err
		}
		for _, item := range items {
			if strings.ToLower(item.Name) == nameLower {
				id := item.ID
				return &id, nil
			}
		}
		return nil, err
	}
	return &created.ID, nil
}
