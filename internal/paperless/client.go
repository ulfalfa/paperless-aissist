package paperless

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"

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
