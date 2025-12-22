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

// Package mimetype provides functions for detecting the MIME type and
// extension of a file.
package mimetype

import (
	"mime"
	"strings"
)

// ExtensionFromContentType gets a file extension from a Content-Type header.
// It first checks a manual mapping for common MIME types (to ensure consistent
// extensions), then falls back to the standard mime.ExtensionsByType.
// Returns empty string if no extension can be determined.
func ExtensionFromContentType(contentType string) string {
	if contentType == "" {
		return ""
	}

	// Remove any parameters (e.g., "application/pdf; charset=utf-8" -> "application/pdf")
	if idx := strings.Index(contentType, ";"); idx != -1 {
		contentType = strings.TrimSpace(contentType[:idx])
	}

	// Manual mapping for common MIME types (preferred for consistency)
	mimeToExt := map[string]string{
		// Text
		"text/plain":      ".txt",
		"text/html":       ".html",
		"text/csv":        ".csv",
		"text/markdown":   ".md",
		"text/x-markdown": ".md",
		"text/xml":        ".xml",
		"text/yaml":       ".yaml",
		"text/x-yaml":     ".yaml",
		"text/css":        ".css",
		"text/javascript": ".js",

		// Images
		"image/jpeg":    ".jpg",
		"image/png":     ".png",
		"image/gif":     ".gif",
		"image/tiff":    ".tiff",
		"image/webp":    ".webp",
		"image/bmp":     ".bmp",
		"image/svg+xml": ".svg",
		"image/heic":    ".heic",
		"image/heif":    ".heif",

		// Application / structured
		"application/json":             ".json",
		"application/xml":              ".xml",
		"application/pdf":              ".pdf",
		"application/rtf":              ".rtf",
		"application/zip":              ".zip",
		"application/x-tar":            ".tar",
		"application/gzip":             ".gz",
		"application/x-7z-compressed":  ".7z",
		"application/x-rar-compressed": ".rar",

		// Office – Microsoft
		"application/msword": ".doc",
		"application/vnd.openxmlformats-officedocument.wordprocessingml.document": ".docx",
		"application/vnd.ms-excel":                                                  ".xls",
		"application/vnd.ms-excel.sheet.macroenabled.12":                            ".xlsm",
		"application/vnd.openxmlformats-officedocument.spreadsheetml.sheet":         ".xlsx",
		"application/vnd.ms-powerpoint":                                             ".ppt",
		"application/vnd.ms-powerpoint.presentation.macroenabled.12":                ".pptm",
		"application/vnd.openxmlformats-officedocument.presentationml.presentation": ".pptx",
		"application/vnd.ms-excel.addin.macroenabled.12":                            ".xlam",
		"application/vnd.ms-excel.template.macroenabled.12":                         ".xltm",

		// Office – Apple
		"application/vnd.apple.keynote": ".key",
		"application/vnd.apple.pages":   ".pages",
		"application/vnd.apple.numbers": ".numbers",

		// Audio
		"audio/mpeg": ".mp3",
		"audio/wav":  ".wav",
		"audio/ogg":  ".ogg",
		"audio/flac": ".flac",
		"audio/aac":  ".aac",
		"audio/mp4":  ".m4a",

		// Video
		"video/mp4":        ".mp4",
		"video/mpeg":       ".mpeg",
		"video/quicktime":  ".mov",
		"video/x-msvideo":  ".avi",
		"video/x-matroska": ".mkv",
		"video/webm":       ".webm",

		// Fonts
		"font/ttf":   ".ttf",
		"font/otf":   ".otf",
		"font/woff":  ".woff",
		"font/woff2": ".woff2",

		// Binaries / misc
		"application/octet-stream": ".bin",
		"application/x-sh":         ".sh",
		"application/x-executable": ".exe",
	}

	if ext, ok := mimeToExt[contentType]; ok {
		return ext
	}

	// Fallback: try standard mime package for other types
	exts, err := mime.ExtensionsByType(contentType)
	if err == nil && len(exts) > 0 {
		return exts[0]
	}
	return ""
}
