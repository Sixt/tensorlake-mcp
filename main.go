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

package main

import (
	"cmp"
	"context"
	"log/slog"
	"os"
	"strconv"

	"github.com/google/jsonschema-go/jsonschema"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/sixt/tensorlake-go"
)

const (
	serverName    = "tensorlake-mcp"
	serverVersion = "v0.3.0"
)

var (
	logLevel              = os.Getenv("TENSORLAKE_MCP_LOG_LEVEL")
	tlAPIBaseURL          = os.Getenv("TENSORLAKE_API_BASE_URL")
	tlAPIKey              = os.Getenv("TENSORLAKE_API_KEY")
	tlSandboxAPIBaseURL   = os.Getenv("TENSORLAKE_SANDBOX_API_BASE_URL")
	tlSandboxProxyBaseURL = os.Getenv("TENSORLAKE_SANDBOX_PROXY_BASE_URL")
	tlSandboxTimeoutSecs  int
)

func init() {
	logLevel = cmp.Or(logLevel, "debug")

	slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stderr, &slog.HandlerOptions{
		Level: func() slog.Level {
			switch logLevel {
			case "debug":
				return slog.LevelDebug
			case "info":
				return slog.LevelInfo
			case "warn":
				return slog.LevelWarn
			default:
				return slog.LevelInfo
			}
		}(),
	})))

	tlAPIBaseURL = cmp.Or(tlAPIBaseURL, "https://api.tensorlake.ai/documents/v2")
	tlSandboxAPIBaseURL = cmp.Or(tlSandboxAPIBaseURL, tensorlake.SandboxAPIBaseURL)
	tlSandboxProxyBaseURL = cmp.Or(tlSandboxProxyBaseURL, tensorlake.DefaultSandboxProxyBaseURL)

	tlSandboxTimeoutSecs = 3600
	if v := os.Getenv("TENSORLAKE_SANDBOX_TIMEOUT_SECS"); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			tlSandboxTimeoutSecs = n
		}
	}
}

func main() {
	if tlAPIKey == "" {
		slog.Error("TENSORLAKE_API_KEY environment variable is required")
		os.Exit(1)
	}

	impl := mcp.NewServer(&mcp.Implementation{
		Name:    serverName,
		Version: serverVersion,
	}, &mcp.ServerOptions{
		Instructions: "Tensorlake MCP server provides a cloud sandbox environment for document processing. " +
			"Upload documents, parse them with advanced AI, and use standard tools (bash, file_read, file_edit, grep, glob) " +
			"to work with the results. All files live in the sandbox filesystem under /data.\n\n" +
			"IMPORTANT: Prefer dedicated tools over bash for file operations:\n" +
			" - To read files: use file_read (NOT cat/head/tail via bash)\n" +
			" - To edit files: use file_edit (NOT sed/awk via bash)\n" +
			" - To search file contents: use grep (NOT grep via bash)\n" +
			" - To find files by name: use glob (NOT find/ls via bash)\n" +
			"Reserve bash for system commands, installing packages, running scripts, and operations " +
			"that the dedicated tools cannot handle.",
		HasTools: true,
	})

	ctx := context.Background()
	s := newServer()
	defer s.CleanupSession(ctx)

	mcp.AddTool(impl, &mcp.Tool{
		Name: "bash",
		Description: "Execute a shell command in the cloud sandbox and return its exit code, stdout, and stderr.\n\n" +
			"IMPORTANT: Do NOT use bash when a dedicated tool can do the job:\n" +
			" - File search: use glob (NOT find or ls)\n" +
			" - Content search: use grep (NOT grep/rg via bash)\n" +
			" - Read files: use file_read (NOT cat/head/tail)\n" +
			" - Edit files: use file_edit (NOT sed/awk)\n\n" +
			"Use bash for: installing packages, running scripts, compiling code, " +
			"git operations, and other system commands that dedicated tools cannot handle.\n\n" +
			"Tips:\n" +
			" - Chain dependent commands with '&&'. Use ';' only when you don't care if earlier commands fail.\n" +
			" - Use absolute paths. The default working directory is /data.\n" +
			" - If a command produces no output, check stderr in the response.",
		InputSchema: &jsonschema.Schema{
			Type: "object",
			Properties: map[string]*jsonschema.Schema{
				"command":           {Type: "string", Description: "The shell command to execute."},
				"timeout_sec":       {Type: "integer", Description: "Timeout in seconds (max 300). Default 30."},
				"working_dir":       {Type: "string", Description: "Working directory. Default /data."},
				"description":       {Type: "string", Description: "Human-readable description of what this command does."},
				"run_in_background": {Type: "boolean", Description: "Run the command in the background. Returns a process_id immediately. Use bash_status to check results."},
			},
			Required: []string{"command"},
		},
	}, s.Bash)

	mcp.AddTool(impl, &mcp.Tool{
		Name: "bash_status",
		Description: "Check the status or retrieve results of a background bash command.\n\n" +
			"Pass the process_id returned by bash with run_in_background=true. " +
			"Returns the command output if complete, or status 'running' if still in progress.",
		InputSchema: &jsonschema.Schema{
			Type: "object",
			Properties: map[string]*jsonschema.Schema{
				"process_id": {Type: "string", Description: "The process ID returned by a background bash command."},
			},
			Required: []string{"process_id"},
		},
	}, s.BashStatus)

	mcp.AddTool(impl, &mcp.Tool{
		Name: "file_read",
		Description: "Read a file from the sandbox filesystem. Returns content with line numbers (cat -n format).\n\n" +
			"Usage:\n" +
			" - Use this tool instead of cat/head/tail via bash.\n" +
			" - By default reads up to 2000 lines from the start of the file.\n" +
			" - For large files, use offset and limit to read specific sections.\n" +
			" - When you already know which part of the file you need, only read that part.\n" +
			" - Always read a file before editing it with file_edit.\n" +
			" - Image files (PNG, JPG, GIF, etc.) are returned as base64-encoded image content.\n" +
			" - PDF files: uses pdftotext if available, with optional page ranges. If not available, use the parse tool.\n" +
			" - Binary files will return an error with the detected content type.",
		InputSchema: &jsonschema.Schema{
			Type: "object",
			Properties: map[string]*jsonschema.Schema{
				"path":   {Type: "string", Description: "Absolute path to the file in the sandbox."},
				"offset": {Type: "integer", Description: "0-based line offset to start reading from. Default 0."},
				"limit":  {Type: "integer", Description: "Number of lines to read. Default 2000."},
				"pages":  {Type: "string", Description: "Page range for PDF files (e.g. '1-5', '3'). Only applies to PDFs."},
			},
			Required: []string{"path"},
		},
	}, s.FileRead)

	mcp.AddTool(impl, &mcp.Tool{
		Name: "file_edit",
		Description: "Edit a file in the sandbox using exact string replacement.\n\n" +
			"Usage:\n" +
			" - Use this tool instead of sed/awk via bash.\n" +
			" - Always read the file with file_read first before editing.\n" +
			" - old_string must match exactly once in the file. If it matches multiple times, " +
			"provide more surrounding context to make it unique.\n" +
			" - Preserve the exact indentation (tabs/spaces) from the file.\n" +
			" - To create a new file: set old_string to empty string. Only works if the file does not exist.\n" +
			" - To append to a file: use the last line(s) as old_string and include them plus new content in new_string.",
		InputSchema: &jsonschema.Schema{
			Type: "object",
			Properties: map[string]*jsonschema.Schema{
				"path":        {Type: "string", Description: "Absolute path to the file in the sandbox."},
				"old_string":  {Type: "string", Description: "The exact text to find and replace. Must appear exactly once unless replace_all is true. Empty string to create a new file."},
				"new_string":  {Type: "string", Description: "The replacement text."},
				"replace_all": {Type: "boolean", Description: "Replace all occurrences of old_string. Default false."},
			},
			Required: []string{"path", "old_string", "new_string"},
		},
	}, s.FileEdit)

	mcp.AddTool(impl, &mcp.Tool{
		Name: "file_write",
		Description: "Write a file to the sandbox filesystem. Creates the file if it doesn't exist, or overwrites it entirely.\n\n" +
			"Usage:\n" +
			" - Use this tool to create new files or completely rewrite existing ones.\n" +
			" - For partial edits (replacing specific text), prefer file_edit instead.\n" +
			" - Parent directories are created automatically.",
		InputSchema: &jsonschema.Schema{
			Type: "object",
			Properties: map[string]*jsonschema.Schema{
				"path":    {Type: "string", Description: "Absolute path to the file in the sandbox."},
				"content": {Type: "string", Description: "The full content to write to the file."},
			},
			Required: []string{"path", "content"},
		},
	}, s.FileWrite)

	mcp.AddTool(impl, &mcp.Tool{
		Name: "grep",
		Description: "Search file contents in the sandbox by regex pattern. Returns matching lines with filename and line numbers.\n\n" +
			"Usage:\n" +
			" - ALWAYS use this tool for content search. Do NOT run grep/rg via bash.\n" +
			" - Supports regex syntax (e.g., 'log.*Error', 'function\\s+\\w+').\n" +
			" - Filter files with the glob parameter (e.g., '*.py', '*.go').\n" +
			" - Results are truncated at 200 matches. Use the glob filter to narrow results.\n" +
			" - Default search path is /data.",
		InputSchema: &jsonschema.Schema{
			Type: "object",
			Properties: map[string]*jsonschema.Schema{
				"pattern":     {Type: "string", Description: "Regex pattern to search for."},
				"path":        {Type: "string", Description: "Directory or file to search in. Default /data."},
				"glob":        {Type: "string", Description: "File glob filter, e.g. '*.py', '*.txt'."},
				"output_mode": {Type: "string", Description: "Output mode: 'content' (matching lines with line numbers, default), 'files_with_matches' (file paths only), 'count' (match counts per file)."},
				"before":      {Type: "integer", Description: "Number of lines to show before each match (-B). Only for output_mode 'content'."},
				"after":       {Type: "integer", Description: "Number of lines to show after each match (-A). Only for output_mode 'content'."},
				"context":     {Type: "integer", Description: "Number of lines to show before and after each match (-C). Only for output_mode 'content'."},
				"ignore_case": {Type: "boolean", Description: "Case insensitive search."},
				"head_limit":  {Type: "integer", Description: "Limit output to first N lines. Default 200."},
			},
			Required: []string{"pattern"},
		},
	}, s.Grep)

	mcp.AddTool(impl, &mcp.Tool{
		Name: "glob",
		Description: "Find files by name pattern in the sandbox. Returns matching file paths.\n\n" +
			"Usage:\n" +
			" - Use this tool instead of find/ls via bash when searching for files.\n" +
			" - Supports glob patterns like '*.pdf', '*.py', 'report*'.\n" +
			" - Results are truncated at 500 entries.\n" +
			" - Default search path is /data.",
		InputSchema: &jsonschema.Schema{
			Type: "object",
			Properties: map[string]*jsonschema.Schema{
				"pattern": {Type: "string", Description: "Glob pattern to match files against, e.g. '*.pdf', '*.py'."},
				"path":    {Type: "string", Description: "Base directory to search in. Default /data."},
			},
			Required: []string{"pattern"},
		},
	}, s.Glob)

	mcp.AddTool(impl, &mcp.Tool{
		Name: "upload",
		Description: "Upload a file into the sandbox filesystem from an external source.\n\n" +
			"Supported sources:\n" +
			" - HTTP/HTTPS URL: 'https://example.com/file.pdf'\n" +
			" - Local file: 'file:///path/to/local/file.pdf'\n" +
			" - Data URI: 'data:raw content here'\n\n" +
			"The file is written to the sandbox filesystem. Default destination is /data/<filename>.",
		InputSchema: &jsonschema.Schema{
			Type: "object",
			Properties: map[string]*jsonschema.Schema{
				"source":      {Type: "string", Description: "URL (http/https), local path (file://), or data URI (data:) of the file to upload."},
				"destination": {Type: "string", Description: "Destination path in the sandbox. Default: /data/<filename>."},
			},
			Required: []string{"source"},
		},
	}, s.Upload)

	mcp.AddTool(impl, &mcp.Tool{
		Name: "parse",
		Description: "Parse a document in the sandbox using Tensorlake AI. Extracts text, tables, and structure " +
			"from PDFs and other documents, then writes the result as markdown.\n\n" +
			"Usage:\n" +
			" - Upload the document first with the upload tool.\n" +
			" - The parsed markdown is written to /data/parsed/<basename>.md by default.\n" +
			" - After parsing, use file_read to inspect the result.",
		InputSchema: &jsonschema.Schema{
			Type: "object",
			Properties: map[string]*jsonschema.Schema{
				"path":        {Type: "string", Description: "Path to the document in the sandbox."},
				"output_path": {Type: "string", Description: "Where to write the parsed result. Default: /data/parsed/<basename>.md."},
			},
			Required: []string{"path"},
		},
	}, s.Parse)

	if err := impl.Run(ctx, &mcp.StdioTransport{}); err != nil {
		slog.Error("failed to run tensorlake-mcp", "error", err)
	}
}
