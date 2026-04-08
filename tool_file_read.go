// Copyright 2026 SIXT SE
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

package main

import (
	"cmp"
	"context"
	"fmt"
	"net/http"
	"path/filepath"
	"slices"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type FileReadInput struct {
	Path   string `json:"path"`
	Offset int    `json:"offset,omitempty"`
	Limit  int    `json:"limit,omitempty"`
	Pages  string `json:"pages,omitempty"` // PDF page range, e.g. "1-5"
}

// imageExtensions are file extensions we return as base64 image content.
var imageExtensions = map[string]string{
	".png":  "image/png",
	".jpg":  "image/jpeg",
	".jpeg": "image/jpeg",
	".gif":  "image/gif",
	".webp": "image/webp",
	".svg":  "image/svg+xml",
	".bmp":  "image/bmp",
	".ico":  "image/x-icon",
}

func (s *server) FileRead(ctx context.Context, req *mcp.CallToolRequest, in *FileReadInput) (*mcp.CallToolResult, any, error) {
	sandboxID, err := s.ensureSandbox(ctx)
	if err != nil {
		return newToolResultError(fmt.Errorf("failed to ensure sandbox: %w", err))
	}

	ext := strings.ToLower(filepath.Ext(in.Path))

	// Handle image files: return base64-encoded content.
	if mimeType, ok := imageExtensions[ext]; ok {
		return s.readImage(ctx, sandboxID, in.Path, mimeType)
	}

	// Handle PDF files: extract text via pdftotext if available, otherwise return info.
	if ext == ".pdf" {
		return s.readPDF(ctx, in)
	}

	// Default: text file.
	return s.readText(ctx, sandboxID, in)
}

func (s *server) readText(ctx context.Context, sandboxID string, in *FileReadInput) (*mcp.CallToolResult, any, error) {
	content, err := s.tl.ReadSandboxFile(ctx, sandboxID, in.Path)
	if err != nil {
		return newToolResultError(fmt.Errorf("failed to read file: %w", err))
	}

	// Detect binary content.
	if isBinary(content) {
		return newToolResultError(fmt.Errorf("file %s appears to be binary (%s). Use bash to inspect it, or upload tool to retrieve it", in.Path, http.DetectContentType(content)))
	}

	lines := strings.Split(string(content), "\n")
	totalLines := len(lines)
	limit := cmp.Or(in.Limit, 2000)
	offset := in.Offset

	offset = min(offset, totalLines)
	end := min(offset+limit, totalLines)
	lines = lines[offset:end]

	var b strings.Builder
	for i, line := range lines {
		fmt.Fprintf(&b, "%6d\t%s\n", offset+i+1, line)
	}

	if totalLines > end {
		fmt.Fprintf(&b, "\n[File has %d total lines. Showing lines %d-%d. Use offset/limit to read more.]", totalLines, offset+1, end)
	}

	return newToolResultText(b.String())
}

func (s *server) readImage(ctx context.Context, sandboxID, path, mimeType string) (*mcp.CallToolResult, any, error) {
	content, err := s.tl.ReadSandboxFile(ctx, sandboxID, path)
	if err != nil {
		return newToolResultError(fmt.Errorf("failed to read file: %w", err))
	}

	return &mcp.CallToolResult{
		Content: []mcp.Content{
			&mcp.ImageContent{
				MIMEType: mimeType,
				Data:     content,
			},
		},
	}, nil, nil
}

func (s *server) readPDF(ctx context.Context, in *FileReadInput) (*mcp.CallToolResult, any, error) {
	// Try pdftotext in the sandbox. If not available, return a helpful message.
	cmd := "which pdftotext >/dev/null 2>&1 && echo found || echo missing"
	_, stdout, _, err := s.runCommand(ctx, cmd, 10, "/")
	if err != nil || strings.TrimSpace(stdout) != "found" {
		return newToolResultText(fmt.Sprintf(
			"File %s is a PDF. pdftotext is not available in the sandbox.\n"+
				"Use the parse tool to extract text from this PDF.", in.Path))
	}

	// Build pdftotext command with optional page range.
	pdfCmd := "pdftotext"
	if in.Pages != "" {
		parts := strings.SplitN(in.Pages, "-", 2)
		if len(parts) == 2 {
			pdfCmd += fmt.Sprintf(" -f %s -l %s", parts[0], parts[1])
		} else {
			pdfCmd += fmt.Sprintf(" -f %s -l %s", parts[0], parts[0])
		}
	}
	pdfCmd += fmt.Sprintf(" %s -", shellQuote(in.Path, true))

	_, textOut, _, err := s.runCommand(ctx, pdfCmd, 30, "/")
	if err != nil {
		return newToolResultError(fmt.Errorf("pdftotext failed: %w", err))
	}

	if textOut == "" {
		return newToolResultText(fmt.Sprintf("PDF %s produced no text output. It may be image-based. Use the parse tool instead.", in.Path))
	}
	return newToolResultText(textOut)
}

// isBinary checks if content looks like binary data by scanning for null bytes.
func isBinary(data []byte) bool {
	// Check up to the first 8KB.
	check := data
	if len(check) > 8192 {
		check = check[:8192]
	}
	return slices.Contains(check, 0)
}
