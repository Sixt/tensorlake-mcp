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
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type GlobInput struct {
	Pattern string `json:"pattern"`
	Path    string `json:"path,omitempty"`
}

func (s *server) Glob(ctx context.Context, req *mcp.CallToolRequest, in *GlobInput) (*mcp.CallToolResult, any, error) {
	path := cmp.Or(in.Path, "/data")

	// Use find with a shell-safe pattern.
	// For ** patterns (recursive), strip ** prefix and use find with -name on the file part.
	// For simple patterns, use find -name directly.
	pattern := in.Pattern
	namePattern := pattern

	// Handle **/ prefix: just search recursively with the basename pattern.
	if idx := strings.LastIndex(pattern, "**/"); idx >= 0 {
		namePattern = pattern[idx+3:]
	}

	// Build find command with printf for mtime sorting.
	// Output format: mtime_epoch<tab>path, then sort numerically descending, then cut the mtime.
	cmd := fmt.Sprintf(
		"find %s -name %s -type f -printf '%%T@\\t%%p\\n' 2>/dev/null | sort -t$'\\t' -k1 -rn | head -500 | cut -f2",
		shellQuote(path, true),
		shellQuote(namePattern, true),
	)

	_, stdout, stderr, err := s.runCommand(ctx, cmd, 30, "/")
	if err != nil {
		return newToolResultError(err)
	}

	// Fallback: if -printf is not supported (e.g., busybox find), use plain find.
	if stdout == "" && strings.Contains(stderr, "printf") {
		cmd = fmt.Sprintf("find %s -name %s -type f | head -500",
			shellQuote(path, true),
			shellQuote(namePattern, true),
		)
		_, stdout, _, err = s.runCommand(ctx, cmd, 30, "/")
		if err != nil {
			return newToolResultError(err)
		}
	}

	if stdout == "" {
		return newToolResultText("No files found.")
	}

	lines := strings.Split(strings.TrimSpace(stdout), "\n")
	if len(lines) == 500 {
		stdout = stdout + "\n\n[Truncated at 500 entries]"
	}
	return newToolResultText(stdout)
}
