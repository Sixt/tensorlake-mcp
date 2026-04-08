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

type GrepInput struct {
	Pattern    string `json:"pattern"`
	Path       string `json:"path,omitempty"`
	Glob       string `json:"glob,omitempty"`
	OutputMode string `json:"output_mode,omitempty"` // "content" (default), "files_with_matches", "count"
	Before     int    `json:"before,omitempty"`       // -B context lines
	After      int    `json:"after,omitempty"`        // -A context lines
	Context    int    `json:"context,omitempty"`       // -C context lines
	IgnoreCase bool   `json:"ignore_case,omitempty"`  // -i
	HeadLimit  int    `json:"head_limit,omitempty"`   // truncate output
}

func (s *server) Grep(ctx context.Context, req *mcp.CallToolRequest, in *GrepInput) (*mcp.CallToolResult, any, error) {
	path := cmp.Or(in.Path, "/data")
	mode := cmp.Or(in.OutputMode, "content")
	headLimit := cmp.Or(in.HeadLimit, 200)

	// Build grep command as a proper argument list to avoid shell injection.
	// We use printf '%s\0' for each arg piped to xargs -0 to avoid any quoting issues.
	var args []string
	args = append(args, "-r") // recursive

	switch mode {
	case "files_with_matches":
		args = append(args, "-l")
	case "count":
		args = append(args, "-c")
	default: // "content"
		args = append(args, "-n") // line numbers
	}

	if in.IgnoreCase {
		args = append(args, "-i")
	}
	if in.Before > 0 {
		args = append(args, fmt.Sprintf("-B%d", in.Before))
	}
	if in.After > 0 {
		args = append(args, fmt.Sprintf("-A%d", in.After))
	}
	if in.Context > 0 {
		args = append(args, fmt.Sprintf("-C%d", in.Context))
	}
	if in.Glob != "" {
		args = append(args, "--include="+in.Glob)
	}

	args = append(args, "--", in.Pattern, path)

	// Build a command that passes args safely via a shell array.
	// We use printf to inject each argument null-terminated, then xargs -0 grep.
	var cmd strings.Builder
	cmd.WriteString("printf '%s\\0'")
	for _, arg := range args {
		cmd.WriteString(" ")
		cmd.WriteString(shellQuote(arg, true))
	}
	cmd.WriteString(" | xargs -0 grep; true") // || true: exit 0 on no match

	_, stdout, _, err := s.runCommand(ctx, cmd.String(), 30, "/")
	if err != nil {
		return newToolResultError(err)
	}

	if stdout == "" {
		return newToolResultText("No matches found.")
	}

	// Truncate output.
	lines := strings.Split(stdout, "\n")
	total := len(lines)
	if total > headLimit {
		stdout = strings.Join(lines[:headLimit], "\n") + fmt.Sprintf("\n\n[Truncated: showing %d of %d lines]", headLimit, total)
	}

	return newToolResultText(stdout)
}

func shellQuote(s string, include bool) string {
	if !include {
		return ""
	}
	return "'" + strings.ReplaceAll(s, "'", "'\\''") + "'"
}
