package imageconv

import (
	"bytes"
	"image/jpeg"

	"golang.org/x/image/webp"
)

// WebPToJPEG decodes a WebP image and re-encodes it as JPEG at the given quality (1-100).
func WebPToJPEG(data []byte, quality int) ([]byte, error) {
	img, err := webp.Decode(bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, img, &jpeg.Options{Quality: quality}); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}
