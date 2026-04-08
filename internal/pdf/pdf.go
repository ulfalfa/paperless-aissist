package pdf

import (
	"bytes"
	"strings"

	"github.com/ledongthuc/pdf"
)

// ExtractText reads a PDF file and returns its text content.
func ExtractText(path string) (string, error) {
	f, r, err := pdf.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()

	var buf bytes.Buffer
	for i := 1; i <= r.NumPage(); i++ {
		page := r.Page(i)
		if page.V.IsNull() {
			continue
		}
		text, err := page.GetPlainText(nil)
		if err != nil {
			continue
		}
		buf.WriteString(text)
		buf.WriteString("\n")
	}
	return strings.TrimSpace(buf.String()), nil
}
