package detector

import (
	"bytes"
	"io"
	"mime"
	"net/http"
	"path/filepath"
	"strings"

	"github.com/h2non/filetype"
)

type FileInfo struct {
	MIME      string
	Extension string
	Size      int64
}

// Detect reads up to the first 8192 bytes from reader without exhausting it,
// checking magic bytes with h2non/filetype and http.DetectContentType fallback.
func Detect(r io.Reader, filename string) (*FileInfo, io.Reader, error) {
	buffer := make([]byte, 8192)
	n, err := io.ReadFull(r, buffer)
	if err != nil && err != io.EOF && err != io.ErrUnexpectedEOF {
		return nil, nil, err
	}

	validBytes := buffer[:n]
	combinedReader := io.MultiReader(bytes.NewReader(validBytes), r)

	detectedMime := ""
	detectedExt := ""

	// 1. Try h2non/filetype (magic bytes signature matching)
	kind, err := filetype.Match(validBytes)
	if err == nil && kind != filetype.Unknown {
		detectedMime = kind.MIME.Value
		detectedExt = kind.Extension
	}

	// 2. Fallback to http.DetectContentType
	if detectedMime == "" || detectedMime == "application/octet-stream" {
		httpMime := http.DetectContentType(validBytes)
		if httpMime != "" && httpMime != "application/octet-stream" {
			detectedMime = httpMime
		}
	}

	// 3. Fallback based on filename extension if still unknown
	ext := strings.TrimPrefix(filepath.Ext(filename), ".")
	if detectedExt == "" && ext != "" {
		detectedExt = strings.ToLower(ext)
	}

	if detectedMime == "" || detectedMime == "application/octet-stream" {
		if ext != "" {
			if m := mime.TypeByExtension("." + ext); m != "" {
				detectedMime = m
			}
		}
	}

	if detectedMime == "" {
		detectedMime = "application/octet-stream"
	}
	if detectedExt == "" {
		detectedExt = "bin"
	}

	return &FileInfo{
		MIME:      detectedMime,
		Extension: strings.ToLower(detectedExt),
	}, combinedReader, nil
}
