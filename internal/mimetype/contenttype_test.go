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
	"strings"
	"testing"
)

func TestExtensionFromContentType(t *testing.T) {
	tests := []struct {
		name        string
		contentType string
		want        string
	}{
		{
			name:        "PDF",
			contentType: "application/pdf",
			want:        ".pdf",
		},
		{
			name:        "PDF with charset",
			contentType: "application/pdf; charset=utf-8",
			want:        ".pdf",
		},
		{
			name:        "DOCX",
			contentType: "application/vnd.openxmlformats-officedocument.wordprocessingml.document",
			want:        ".docx",
		},
		{
			name:        "XLSX",
			contentType: "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet",
			want:        ".xlsx",
		},
		{
			name:        "PPTX",
			contentType: "application/vnd.openxmlformats-officedocument.presentationml.presentation",
			want:        ".pptx",
		},
		{
			name:        "Plain text",
			contentType: "text/plain",
			want:        ".txt",
		},
		{
			name:        "HTML",
			contentType: "text/html",
			want:        ".html",
		},
		{
			name:        "CSV",
			contentType: "text/csv",
			want:        ".csv",
		},
		{
			name:        "JPEG",
			contentType: "image/jpeg",
			want:        ".jpg",
		},
		{
			name:        "PNG",
			contentType: "image/png",
			want:        ".png",
		},
		{
			name:        "GIF",
			contentType: "image/gif",
			want:        ".gif",
		},
		{
			name:        "JSON",
			contentType: "application/json",
			want:        ".json",
		},
		{
			name:        "XML",
			contentType: "application/xml",
			want:        ".xml",
		},
		{
			name:        "Empty string",
			contentType: "",
			want:        "",
		},
		{
			name:        "Unknown MIME type",
			contentType: "application/unknown",
			want:        "",
		},
		{
			name:        "MIME type with multiple parameters",
			contentType: "application/pdf; charset=utf-8; boundary=something",
			want:        ".pdf",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ExtensionFromContentType(tt.contentType)
			if got != tt.want {
				t.Errorf("ExtensionFromContentType(%q) = %v, want %v", tt.contentType, got, tt.want)
			}
		})
	}
}

func TestExtensionFromContentType_StandardMimeTypes(t *testing.T) {
	// Test that standard mime types work through the mime package
	// Note: Some MIME types may not have extensions in all systems
	tests := []struct {
		name        string
		contentType string
	}{
		{
			name:        "JavaScript",
			contentType: "application/javascript",
		},
		{
			name:        "CSS",
			contentType: "text/css",
		},
		{
			name:        "Markdown",
			contentType: "text/markdown",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ExtensionFromContentType(tt.contentType)
			// If an extension is returned, it should start with a dot
			if got != "" && !strings.HasPrefix(got, ".") {
				t.Errorf("ExtensionFromContentType(%q) returned %q, expected extension starting with '.'", tt.contentType, got)
			}
			// It's okay if some types don't have extensions (system-dependent)
		})
	}
}
