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
	"context"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type FileWriteInput struct {
	Path    string `json:"path"`
	Content string `json:"content"`
}

func (s *server) FileWrite(ctx context.Context, req *mcp.CallToolRequest, in *FileWriteInput) (*mcp.CallToolResult, any, error) {
	sandboxID, err := s.ensureSandbox(ctx)
	if err != nil {
		return newToolResultError(fmt.Errorf("failed to ensure sandbox: %w", err))
	}

	// Ensure parent directory exists.
	if dir := filepath.Dir(in.Path); dir != "." && dir != "/" {
		s.runCommand(ctx, fmt.Sprintf("mkdir -p %s", shellQuote(dir, true)), 10, "/")
	}

	if err := s.tl.WriteSandboxFile(ctx, sandboxID, in.Path, strings.NewReader(in.Content)); err != nil {
		return newToolResultError(fmt.Errorf("failed to write file: %w", err))
	}

	return newToolResultText(fmt.Sprintf("Wrote %d bytes to %s", len(in.Content), in.Path))
}
