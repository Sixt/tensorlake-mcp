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
	"testing"
)

func TestDetectFromContent_PDF(t *testing.T) {
	tests := []struct {
		name     string
		content  []byte
		wantMime string
		wantExt  string
	}{
		{
			name:     "Standard PDF",
			content:  []byte("%PDF-1.4\n"),
			wantMime: "application/pdf",
			wantExt:  ".pdf",
		},
		{
			name:     "PDF with version",
			content:  []byte("%PDF-1.7\n"),
			wantMime: "application/pdf",
			wantExt:  ".pdf",
		},
		{
			name:     "PDF with minimal header",
			content:  []byte("%PDF"),
			wantMime: "application/pdf",
			wantExt:  ".pdf",
		},
		{
			name:     "PDF with more content",
			content:  []byte("%PDF-1.4\n%This is a PDF file\n"),
			wantMime: "application/pdf",
			wantExt:  ".pdf",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotMime, gotExt := DetectFromContent(tt.content)
			if gotMime != tt.wantMime {
				t.Errorf("DetectFromContent() mimeType = %v, want %v", gotMime, tt.wantMime)
			}
			if gotExt != tt.wantExt {
				t.Errorf("DetectFromContent() extension = %v, want %v", gotExt, tt.wantExt)
			}
		})
	}
}

func TestDetectFromContent_Images(t *testing.T) {
	tests := []struct {
		name     string
		content  []byte
		wantMime string
		wantExt  string
	}{
		{
			name:     "PNG image",
			content:  []byte{0x89, 0x50, 0x4E, 0x47, 0x0D, 0x0A, 0x1A, 0x0A},
			wantMime: "image/png",
			wantExt:  ".png",
		},
		{
			name:     "JPEG image",
			content:  []byte{0xFF, 0xD8, 0xFF, 0xE0},
			wantMime: "image/jpeg",
			wantExt:  ".jpg",
		},
		{
			name:     "GIF87a",
			content:  []byte("GIF87a"),
			wantMime: "image/gif",
			wantExt:  ".gif",
		},
		{
			name:     "GIF89a",
			content:  []byte("GIF89a"),
			wantMime: "image/gif",
			wantExt:  ".gif",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotMime, gotExt := DetectFromContent(tt.content)
			if gotMime != tt.wantMime {
				t.Errorf("DetectFromContent() mimeType = %v, want %v", gotMime, tt.wantMime)
			}
			if gotExt != tt.wantExt {
				t.Errorf("DetectFromContent() extension = %v, want %v", gotExt, tt.wantExt)
			}
		})
	}
}

func TestDetectFromContent_OfficeDocuments(t *testing.T) {
	tests := []struct {
		name     string
		content  []byte
		wantMime string
		wantExt  string
	}{
		{
			name:     "DOCX file",
			content:  []byte{0x50, 0x4B, 0x03, 0x04, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 'w', 'o', 'r', 'd', '/'},
			wantMime: "application/vnd.openxmlformats-officedocument.wordprocessingml.document",
			wantExt:  ".docx",
		},
		{
			name:     "XLSX file",
			content:  []byte{0x50, 0x4B, 0x03, 0x04, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 'x', 'l', '/'},
			wantMime: "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet",
			wantExt:  ".xlsx",
		},
		{
			name:     "PPTX file",
			content:  []byte{0x50, 0x4B, 0x03, 0x04, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 'p', 'p', 't', '/'},
			wantMime: "application/vnd.openxmlformats-officedocument.presentationml.presentation",
			wantExt:  ".pptx",
		},
		{
			name:     "Generic ZIP file",
			content:  []byte{0x50, 0x4B, 0x03, 0x04},
			wantMime: "application/zip",
			wantExt:  ".zip",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotMime, gotExt := DetectFromContent(tt.content)
			if gotMime != tt.wantMime {
				t.Errorf("DetectFromContent() mimeType = %v, want %v", gotMime, tt.wantMime)
			}
			if gotExt != tt.wantExt {
				t.Errorf("DetectFromContent() extension = %v, want %v", gotExt, tt.wantExt)
			}
		})
	}
}

func TestDetectFromContent_TextFormats(t *testing.T) {
	tests := []struct {
		name     string
		content  []byte
		wantMime string
		wantExt  string
	}{
		{
			name:     "XML file",
			content:  []byte("<?xml version=\"1.0\"?>"),
			wantMime: "application/xml",
			wantExt:  ".xml",
		},
		{
			name:     "HTML file",
			content:  []byte("<html><head></head><body></body></html>"),
			wantMime: "text/html",
			wantExt:  ".html",
		},
		{
			name:     "HTML with DOCTYPE",
			content:  []byte("<!DOCTYPE html><html>"),
			wantMime: "text/html",
			wantExt:  ".html",
		},
		{
			name:     "JSON object",
			content:  []byte(`{"key": "value"}`),
			wantMime: "application/json",
			wantExt:  ".json",
		},
		{
			name:     "JSON array",
			content:  []byte(`[1, 2, 3]`),
			wantMime: "application/json",
			wantExt:  ".json",
		},
		{
			name:     "Plain text",
			content:  []byte("This is plain text content with only printable ASCII characters."),
			wantMime: "text/plain",
			wantExt:  ".txt",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotMime, gotExt := DetectFromContent(tt.content)
			if gotMime != tt.wantMime {
				t.Errorf("DetectFromContent() mimeType = %v, want %v", gotMime, tt.wantMime)
			}
			if gotExt != tt.wantExt {
				t.Errorf("DetectFromContent() extension = %v, want %v", gotExt, tt.wantExt)
			}
		})
	}
}

func TestDetectFromContent_EdgeCases(t *testing.T) {
	tests := []struct {
		name     string
		content  []byte
		wantMime string
		wantExt  string
	}{
		{
			name:     "Empty content",
			content:  []byte{},
			wantMime: "",
			wantExt:  "",
		},
		{
			name:     "Too short content",
			content:  []byte{0x50},
			wantMime: "",
			wantExt:  "",
		},
		{
			name:     "Unknown binary format",
			content:  []byte{0x00, 0x01, 0x02, 0x03, 0x04, 0x05},
			wantMime: "",
			wantExt:  "",
		},
		{
			name:     "Text with many control characters",
			content:  []byte{0x00, 0x01, 0x02, 0x03, 0x04, 0x05, 0x06, 0x07, 0x08, 0x09, 0x0A, 0x0B, 0x0C, 0x0D, 0x0E, 0x0F},
			wantMime: "",
			wantExt:  "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotMime, gotExt := DetectFromContent(tt.content)
			if gotMime != tt.wantMime {
				t.Errorf("DetectFromContent() mimeType = %v, want %v", gotMime, tt.wantMime)
			}
			if gotExt != tt.wantExt {
				t.Errorf("DetectFromContent() extension = %v, want %v", gotExt, tt.wantExt)
			}
		})
	}
}

func TestDetectFromContent_RealWorldExamples(t *testing.T) {
	tests := []struct {
		name     string
		content  []byte
		wantMime string
		wantExt  string
	}{
		{
			name:     "arXiv paper PDF (2310.04621)",
			content:  []byte("%PDF-1.4\n%âãÏÓ\n"),
			wantMime: "application/pdf",
			wantExt:  ".pdf",
		},
		{
			name:     "Minimal valid PDF",
			content:  []byte("%PDF"),
			wantMime: "application/pdf",
			wantExt:  ".pdf",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotMime, gotExt := DetectFromContent(tt.content)
			if gotMime != tt.wantMime {
				t.Errorf("DetectFromContent() mimeType = %v, want %v", gotMime, tt.wantMime)
			}
			if gotExt != tt.wantExt {
				t.Errorf("DetectFromContent() extension = %v, want %v", gotExt, tt.wantExt)
			}
		})
	}
}
