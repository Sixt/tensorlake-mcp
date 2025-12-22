// Copyright 2025 SIXT SE
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//      http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package mimetype

import (
	"bytes"
	"fmt"
	"io"
	"net/http"
	"strings"
)

// DetectExtensionFromContentType detects the file extension from the Content-Type header.
// If the extension is not found, it tries to detect the extension from the first 512 bytes of the file content.
// Returns the file extension and an error if the extension is not found.
func DetectExtensionFromContentType(resp *http.Response) (string, error) {
	var detectedExt string

	// First, try Content-Type header
	contentType := resp.Header.Get("Content-Type")
	if contentType != "" {
		detectedExt = ExtensionFromContentType(contentType)
	}

	// If Content-Type didn't help, try content-based detection
	if detectedExt == "" {
		// Read first 512 bytes to detect MIME type
		// We need to read from resp.Body, which will consume those bytes
		// Then we'll reconstruct the stream with MultiReader
		peekBuffer := make([]byte, 512)
		n, err := resp.Body.Read(peekBuffer)
		if err != nil && err != io.EOF {
			resp.Body.Close()
			return "", fmt.Errorf("failed to peek file content: %w", err)
		}

		if n > 0 {
			_, detectedExt = DetectFromContent(peekBuffer[:n])
		}

		// Reconstruct the stream: combine the peeked bytes with the remaining stream
		if n > 0 {
			// resp.Body still has the remaining bytes after the first n bytes
			// Create a new reader that combines the buffer with the remaining stream
			resp.Body = io.NopCloser(io.MultiReader(bytes.NewReader(peekBuffer[:n]), resp.Body))
		}
	}

	return detectedExt, nil
}

// DetectFromContent detects MIME type from file content by checking magic numbers.
// Returns the MIME type and corresponding file extension.
// If detection fails, returns empty strings.
func DetectFromContent(header []byte) (mimeType string, extension string) {
	if len(header) < 4 {
		return "", ""
	}

	// PDF: starts with %PDF (standard PDF files start with "%PDF" at byte 0)
	if len(header) >= 4 && string(header[0:4]) == "%PDF" {
		return "application/pdf", ".pdf"
	}

	// ZIP-based formats (DOCX, XLSX, PPTX): start with PK (ZIP signature)
	if len(header) >= 2 && header[0] == 0x50 && header[1] == 0x4B {
		// Check for Office Open XML formats
		if len(header) >= 30 {
			// Look for specific patterns in ZIP central directory
			headerStr := string(header)
			switch {
			case strings.Contains(headerStr, "word/"):
				return "application/vnd.openxmlformats-officedocument.wordprocessingml.document", ".docx"
			case strings.Contains(headerStr, "xl/"):
				return "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet", ".xlsx"
			case strings.Contains(headerStr, "ppt/"):
				return "application/vnd.openxmlformats-officedocument.presentationml.presentation", ".pptx"
			}
		}
		// Default to ZIP if we can't determine
		return "application/zip", ".zip"
	}

	// PNG: starts with PNG signature
	if len(header) >= 8 && header[0] == 0x89 && header[1] == 0x50 &&
		header[2] == 0x4E && header[3] == 0x47 &&
		header[4] == 0x0D && header[5] == 0x0A &&
		header[6] == 0x1A && header[7] == 0x0A {
		return "image/png", ".png"
	}

	// JPEG: starts with FF D8
	if len(header) >= 2 && header[0] == 0xFF && header[1] == 0xD8 {
		return "image/jpeg", ".jpg"
	}

	// GIF: starts with GIF87a or GIF89a
	if len(header) >= 6 && string(header[0:3]) == "GIF" {
		return "image/gif", ".gif"
	}

	// XML/HTML: starts with <?xml or <html
	if len(header) >= 5 {
		headerStr := strings.ToLower(string(header[:min(100, len(header))]))
		if strings.HasPrefix(headerStr, "<?xml") {
			return "application/xml", ".xml"
		}
		if strings.HasPrefix(headerStr, "<html") || strings.HasPrefix(headerStr, "<!doctype html") {
			return "text/html", ".html"
		}
	}

	// JSON: starts with { or [
	if len(header) >= 1 && (header[0] == '{' || header[0] == '[') {
		return "application/json", ".json"
	}

	// Plain text: check if it's mostly printable ASCII
	if isText(header) {
		return "text/plain", ".txt"
	}

	return "", ""
}

// isText checks if the content appears to be plain text.
func isText(data []byte) bool {
	if len(data) == 0 {
		return false
	}
	nonText := 0
	for i := 0; i < min(512, len(data)); i++ {
		if data[i] < 0x20 && data[i] != 0x09 && data[i] != 0x0A && data[i] != 0x0D {
			nonText++
		}
	}
	// If more than 30% are non-text characters, it's probably binary
	return float64(nonText)/float64(min(512, len(data))) < 0.3
}
