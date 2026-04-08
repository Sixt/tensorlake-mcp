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
	"bytes"
	"context"
	"fmt"
	"log/slog"
	"path/filepath"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/sixt/tensorlake-go"
)

type ParseInput struct {
	Path       string `json:"path"`
	OutputPath string `json:"output_path,omitempty"`
}

type ParseOutput struct {
	OutputPath string `json:"output_path"`
	Pages      int    `json:"pages"`
	Size       int    `json:"size"`
}

func (s *server) Parse(ctx context.Context, req *mcp.CallToolRequest, in *ParseInput) (*mcp.CallToolResult, any, error) {
	sandboxID, err := s.ensureSandbox(ctx)
	if err != nil {
		return newToolResultError(fmt.Errorf("failed to ensure sandbox: %w", err))
	}

	s.sendProgress(ctx, req, 0, 5, "Reading file from sandbox...")

	// 1. Read file from sandbox.
	content, err := s.tl.ReadSandboxFile(ctx, sandboxID, in.Path)
	if err != nil {
		return newToolResultError(fmt.Errorf("failed to read file from sandbox: %w", err))
	}

	s.sendProgress(ctx, req, 1, 5, "Uploading to tensorlake...")

	// 2. Upload to tensorlake.
	uploadResp, err := s.tl.UploadFile(ctx, &tensorlake.UploadFileRequest{
		FileBytes: bytes.NewReader(content),
		FileName:  filepath.Base(in.Path),
	})
	if err != nil {
		return newToolResultError(fmt.Errorf("failed to upload to tensorlake: %w", err))
	}

	s.sendProgress(ctx, req, 2, 5, "Parsing document...")

	// 3. Parse.
	parseResp, err := s.tl.ParseDocument(ctx, &tensorlake.ParseDocumentRequest{
		FileSource: tensorlake.FileSource{FileId: uploadResp.FileId},
		ParsingOptions: &tensorlake.ParsingOptions{
			TableOutputMode:    tensorlake.TableOutputModeMarkdown,
			TableParsingFormat: tensorlake.TableParsingFormatVLM,
			ChunkingStrategy:   tensorlake.ChunkingStrategyNone,
		},
	})
	if err != nil {
		return newToolResultError(fmt.Errorf("failed to start parse: %w", err))
	}

	// 4. Wait for result via SSE.
	result, err := s.tl.GetParseResult(ctx, parseResp.ParseId,
		tensorlake.WithSSE(true),
		tensorlake.WithOnUpdate(func(name tensorlake.ParseEventName, _ *tensorlake.ParseResult) {
			switch name {
			case tensorlake.SSEEventParseQueued:
				s.sendProgress(ctx, req, 3, 5, "Parse job queued...")
			case tensorlake.SSEEventParseUpdate:
				s.sendProgress(ctx, req, 3, 5, "Parsing in progress...")
			case tensorlake.SSEEventParseDone:
				s.sendProgress(ctx, req, 4, 5, "Parse complete, writing results...")
			}
		}),
	)
	if err != nil {
		return newToolResultError(fmt.Errorf("failed to get parse result: %w", err))
	}

	// Assemble parsed content from chunks.
	var parsed strings.Builder
	for _, chunk := range result.Chunks {
		parsed.WriteString(chunk.Content)
		parsed.WriteString("\n")
	}

	// 5. Determine output path and write back to sandbox.
	outputPath := in.OutputPath
	if outputPath == "" {
		base := filepath.Base(in.Path)
		ext := filepath.Ext(base)
		name := strings.TrimSuffix(base, ext)
		outputPath = "/data/parsed/" + name + ".md"
	}

	// Ensure parent directory exists.
	dir := filepath.Dir(outputPath)
	s.runCommand(ctx, fmt.Sprintf("mkdir -p %s", shellQuote(dir, true)), 10, "/")

	parsedContent := parsed.String()
	if err := s.tl.WriteSandboxFile(ctx, sandboxID, outputPath, strings.NewReader(parsedContent)); err != nil {
		return newToolResultError(fmt.Errorf("failed to write parsed result: %w", err))
	}

	s.sendProgress(ctx, req, 5, 5, "Done")

	// 6. Clean up transient tensorlake resources.
	if err := s.tl.DeleteParseJob(ctx, parseResp.ParseId); err != nil {
		slog.Error("failed to clean up parse job", "parse_id", parseResp.ParseId, "error", err)
	}
	if err := s.tl.DeleteFile(ctx, uploadResp.FileId); err != nil {
		slog.Error("failed to clean up uploaded file", "file_id", uploadResp.FileId, "error", err)
	}

	return newToolResultJSON(&ParseOutput{
		OutputPath: outputPath,
		Pages:      result.TotalPages,
		Size:       len(parsedContent),
	})
}
