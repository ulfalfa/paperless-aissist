package validate

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/ulf/paperless-aissist/internal/types"
)

var dateRe = regexp.MustCompile(`^\d{4}-\d{2}-\d{2}$`)

// Apply sanitizes an AnalysisResult and injects the AI tag.
func Apply(r *types.AnalysisResult, aiTag string) *types.AnalysisResult {
	if r == nil {
		return &types.AnalysisResult{}
	}

	// Trim title
	r.Title = strings.TrimSpace(r.Title)

	// Validate / normalize date
	r.CreatedDate = normalizeDate(r.CreatedDate)

	// Trim correspondent and document type
	if r.Correspondent != nil {
		s := strings.TrimSpace(*r.Correspondent)
		if s == "" {
			r.Correspondent = nil
		} else {
			r.Correspondent = &s
		}
	}
	if r.DocumentType != nil {
		s := strings.TrimSpace(*r.DocumentType)
		if s == "" {
			r.DocumentType = nil
		} else {
			r.DocumentType = &s
		}
	}

	// Normalize tags and inject AI tag
	seen := map[string]bool{}
	var tags []string
	for _, t := range r.Tags {
		t = strings.TrimSpace(t)
		if t == "" {
			continue
		}
		low := strings.ToLower(t)
		if !seen[low] {
			seen[low] = true
			tags = append(tags, t)
		}
	}
	if aiTag != "" {
		low := strings.ToLower(aiTag)
		if !seen[low] {
			tags = append(tags, aiTag)
		}
	}
	r.Tags = tags

	return r
}

func normalizeDate(d *string) *string {
	if d == nil {
		return nil
	}
	s := strings.TrimSpace(*d)
	if s == "" || s == "null" {
		return nil
	}
	if !dateRe.MatchString(s) {
		return nil
	}
	// Fix overflow dates (e.g. Feb 30 → Mar 1)
	parts := strings.Split(s, "-")
	year, _ := strconv.Atoi(parts[0])
	month, _ := strconv.Atoi(parts[1])
	day, _ := strconv.Atoi(parts[2])
	t := time.Date(year, time.Month(month), day, 0, 0, 0, 0, time.UTC)
	fixed := fmt.Sprintf("%04d-%02d-%02d", t.Year(), int(t.Month()), t.Day())
	return &fixed
}
