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
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type FileEditInput struct {
	Path       string `json:"path"`
	OldString  string `json:"old_string"`
	NewString  string `json:"new_string"`
	ReplaceAll bool   `json:"replace_all,omitempty"`
}

func (s *server) FileEdit(ctx context.Context, req *mcp.CallToolRequest, in *FileEditInput) (*mcp.CallToolResult, any, error) {
	sandboxID, err := s.ensureSandbox(ctx)
	if err != nil {
		return newToolResultError(fmt.Errorf("failed to ensure sandbox: %w", err))
	}

	content, err := s.tl.ReadSandboxFile(ctx, sandboxID, in.Path)
	if err != nil {
		// If file doesn't exist and old_string is empty, create a new file.
		if in.OldString == "" {
			if writeErr := s.tl.WriteSandboxFile(ctx, sandboxID, in.Path, strings.NewReader(in.NewString)); writeErr != nil {
				return newToolResultError(fmt.Errorf("failed to create file: %w", writeErr))
			}
			return newToolResultText(fmt.Sprintf("Created %s", in.Path))
		}
		return newToolResultError(fmt.Errorf("failed to read file: %w", err))
	}

	text := string(content)
	count := strings.Count(text, in.OldString)
	if count == 0 {
		return newToolResultError(fmt.Errorf("old_string not found in %s", in.Path))
	}
	if count > 1 && !in.ReplaceAll {
		return newToolResultError(fmt.Errorf("old_string is ambiguous (found %d occurrences in %s), provide more context or set replace_all to true", count, in.Path))
	}

	n := 1
	if in.ReplaceAll {
		n = -1
	}
	newText := strings.Replace(text, in.OldString, in.NewString, n)
	if err := s.tl.WriteSandboxFile(ctx, sandboxID, in.Path, strings.NewReader(newText)); err != nil {
		return newToolResultError(fmt.Errorf("failed to write file: %w", err))
	}

	return newToolResultText(fmt.Sprintf("Edited %s", in.Path))
}
