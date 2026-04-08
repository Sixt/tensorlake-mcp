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
	"encoding/json"
	"os"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/sixt/tensorlake-go"
)

func initTestServer(t *testing.T) *server {
	t.Helper()
	apiKey := os.Getenv("TENSORLAKE_API_KEY")
	if apiKey == "" {
		t.Skip("TENSORLAKE_API_KEY must be set")
	}
	baseURL := cmp.Or(os.Getenv("TENSORLAKE_API_BASE_URL"), "https://api.tensorlake.ai/documents/v2")
	sandboxAPI := cmp.Or(os.Getenv("TENSORLAKE_SANDBOX_API_BASE_URL"), tensorlake.SandboxAPIBaseURL)
	sandboxProxy := cmp.Or(os.Getenv("TENSORLAKE_SANDBOX_PROXY_BASE_URL"), tensorlake.DefaultSandboxProxyBaseURL)
	return &server{
		tl: tensorlake.NewClient(
			tensorlake.WithBaseURL(baseURL),
			tensorlake.WithAPIKey(apiKey),
			tensorlake.WithSandboxAPIBaseURL(sandboxAPI),
			tensorlake.WithSandboxProxyBaseURL(sandboxProxy),
		),
	}
}

func extractText(t *testing.T, result *mcp.CallToolResult) string {
	t.Helper()
	b, _ := json.Marshal(result.Content[0])
	var raw map[string]any
	json.Unmarshal(b, &raw)
	return raw["text"].(string)
}

func unmarshalToolResult[T any](t *testing.T, result *mcp.CallToolResult) T {
	t.Helper()
	b, _ := json.Marshal(result.Content[0])
	var raw map[string]any
	json.Unmarshal(b, &raw)
	text := raw["text"].(string)
	var out T
	if err := json.Unmarshal([]byte(text), &out); err != nil {
		t.Fatalf("failed to unmarshal tool result: %v", err)
	}
	return out
}

func TestSandboxLifecycle(t *testing.T) {
	s := initTestServer(t)
	ctx := context.Background()
	defer s.CleanupSession(ctx)

	// First call creates sandbox.
	id1, err := s.ensureSandbox(ctx)
	if err != nil {
		t.Fatalf("ensureSandbox: %v", err)
	}
	if id1 == "" {
		t.Fatal("expected non-empty sandbox ID")
	}

	// Second call returns the same ID.
	id2, err := s.ensureSandbox(ctx)
	if err != nil {
		t.Fatalf("ensureSandbox (second): %v", err)
	}
	if id1 != id2 {
		t.Fatalf("expected same sandbox ID, got %q and %q", id1, id2)
	}
}

func TestBash(t *testing.T) {
	s := initTestServer(t)
	ctx := context.Background()
	defer s.CleanupSession(ctx)

	result, _, err := s.Bash(ctx, &mcp.CallToolRequest{}, &BashInput{
		Command: "echo hello",
	})
	if err != nil {
		t.Fatalf("Bash error: %v", err)
	}
	if result.IsError {
		t.Fatalf("Bash tool error: %v", result.Content)
	}
	out := unmarshalToolResult[BashOutput](t, result)
	if out.ExitCode != 0 {
		t.Errorf("expected exit code 0, got %d", out.ExitCode)
	}
	if out.Stdout != "hello" {
		t.Errorf("expected stdout 'hello', got %q", out.Stdout)
	}
}

func TestBashTimeout(t *testing.T) {
	s := initTestServer(t)
	ctx := context.Background()
	defer s.CleanupSession(ctx)

	result, _, err := s.Bash(ctx, &mcp.CallToolRequest{}, &BashInput{
		Command:    "sleep 60",
		TimeoutSec: 2,
	})
	if err != nil {
		t.Fatalf("Bash error: %v", err)
	}
	// Should have timed out.
	out := unmarshalToolResult[BashOutput](t, result)
	if !out.TimedOut {
		t.Error("expected timed_out to be true")
	}
}

func TestBashNonZeroExit(t *testing.T) {
	s := initTestServer(t)
	ctx := context.Background()
	defer s.CleanupSession(ctx)

	result, _, err := s.Bash(ctx, &mcp.CallToolRequest{}, &BashInput{
		Command: "exit 42",
	})
	if err != nil {
		t.Fatalf("Bash error: %v", err)
	}
	out := unmarshalToolResult[BashOutput](t, result)
	if out.ExitCode != 42 {
		t.Errorf("expected exit code 42, got %d", out.ExitCode)
	}
}

func TestBashStderr(t *testing.T) {
	s := initTestServer(t)
	ctx := context.Background()
	defer s.CleanupSession(ctx)

	result, _, err := s.Bash(ctx, &mcp.CallToolRequest{}, &BashInput{
		Command: "echo oops >&2",
	})
	if err != nil {
		t.Fatalf("Bash error: %v", err)
	}
	out := unmarshalToolResult[BashOutput](t, result)
	if out.Stderr != "oops" {
		t.Errorf("expected stderr 'oops', got %q", out.Stderr)
	}
}

func TestBashRunInBackground(t *testing.T) {
	s := initTestServer(t)
	ctx := context.Background()
	defer s.CleanupSession(ctx)

	// Start a background command.
	result, _, err := s.Bash(ctx, &mcp.CallToolRequest{}, &BashInput{
		Command:         "echo background_done",
		RunInBackground: true,
	})
	if err != nil {
		t.Fatalf("Bash background error: %v", err)
	}
	if result.IsError {
		t.Fatalf("Bash background tool error: %v", result.Content)
	}

	bgResult := unmarshalToolResult[map[string]string](t, result)
	pid := bgResult["process_id"]
	if pid == "" {
		t.Fatal("expected process_id")
	}

	// Poll for result (should be fast).
	var statusResult *mcp.CallToolResult
	for range 20 {
		statusResult, _, err = s.BashStatus(ctx, &mcp.CallToolRequest{}, &BashStatusInput{
			ProcessID: pid,
		})
		if err != nil {
			t.Fatalf("BashStatus error: %v", err)
		}
		text := extractText(t, statusResult)
		if strings.Contains(text, "background_done") {
			break
		}
	}

	out := unmarshalToolResult[BashOutput](t, statusResult)
	if out.Stdout != "background_done" {
		t.Errorf("expected 'background_done', got %q", out.Stdout)
	}
}

func TestBashStatusUnknown(t *testing.T) {
	s := initTestServer(t)
	ctx := context.Background()

	result, _, err := s.BashStatus(ctx, &mcp.CallToolRequest{}, &BashStatusInput{
		ProcessID: "nonexistent",
	})
	if err != nil {
		t.Fatalf("BashStatus error: %v", err)
	}
	if !result.IsError {
		t.Fatal("expected error for unknown process_id")
	}
}

func TestBashWorkingDir(t *testing.T) {
	s := initTestServer(t)
	ctx := context.Background()
	defer s.CleanupSession(ctx)

	// Create a directory first.
	s.Bash(ctx, &mcp.CallToolRequest{}, &BashInput{
		Command: "mkdir -p /data/wdtest",
	})

	result, _, err := s.Bash(ctx, &mcp.CallToolRequest{}, &BashInput{
		Command:    "pwd",
		WorkingDir: "/data/wdtest",
	})
	if err != nil {
		t.Fatalf("Bash error: %v", err)
	}
	out := unmarshalToolResult[BashOutput](t, result)
	if out.Stdout != "/data/wdtest" {
		t.Errorf("expected working dir /data/wdtest, got %q", out.Stdout)
	}
}

func TestFileReadWrite(t *testing.T) {
	s := initTestServer(t)
	ctx := context.Background()
	defer s.CleanupSession(ctx)

	// Write a file via bash.
	result, _, err := s.Bash(ctx, &mcp.CallToolRequest{}, &BashInput{
		Command: `printf 'line1\nline2\nline3\nline4\nline5' > /data/test.txt`,
	})
	if err != nil {
		t.Fatalf("Bash write error: %v", err)
	}
	if result.IsError {
		t.Fatalf("Bash write tool error: %v", result.Content)
	}

	// Read it back with offset/limit.
	result, _, err = s.FileRead(ctx, &mcp.CallToolRequest{}, &FileReadInput{
		Path:   "/data/test.txt",
		Offset: 1,
		Limit:  2,
	})
	if err != nil {
		t.Fatalf("FileRead error: %v", err)
	}
	if result.IsError {
		t.Fatalf("FileRead tool error: %v", result.Content)
	}
	// Should contain lines 2 and 3.
	b, _ := json.Marshal(result.Content[0])
	var raw map[string]any
	json.Unmarshal(b, &raw)
	text := raw["text"].(string)
	if len(text) == 0 {
		t.Fatal("expected non-empty file content")
	}
	t.Logf("FileRead output:\n%s", text)
}

func TestFileReadNonExistent(t *testing.T) {
	s := initTestServer(t)
	ctx := context.Background()
	defer s.CleanupSession(ctx)

	result, _, err := s.FileRead(ctx, &mcp.CallToolRequest{}, &FileReadInput{
		Path: "/data/does_not_exist.txt",
	})
	if err != nil {
		t.Fatalf("FileRead error: %v", err)
	}
	if !result.IsError {
		t.Fatal("expected tool error for non-existent file")
	}
}

func TestFileReadBinaryDetection(t *testing.T) {
	s := initTestServer(t)
	ctx := context.Background()
	defer s.CleanupSession(ctx)

	// Write a binary file with null bytes.
	s.Bash(ctx, &mcp.CallToolRequest{}, &BashInput{
		Command: `printf '\x00\x01\x02\x03' > /data/binary.dat`,
	})

	result, _, err := s.FileRead(ctx, &mcp.CallToolRequest{}, &FileReadInput{
		Path: "/data/binary.dat",
	})
	if err != nil {
		t.Fatalf("FileRead error: %v", err)
	}
	if !result.IsError {
		t.Fatal("expected error for binary file")
	}
}

func TestFileReadImage(t *testing.T) {
	s := initTestServer(t)
	ctx := context.Background()
	defer s.CleanupSession(ctx)

	// Create a minimal 1x1 PNG in the sandbox.
	s.Bash(ctx, &mcp.CallToolRequest{}, &BashInput{
		Command: `printf '\x89PNG\r\n\x1a\n' > /data/test.png`,
	})

	result, _, err := s.FileRead(ctx, &mcp.CallToolRequest{}, &FileReadInput{
		Path: "/data/test.png",
	})
	if err != nil {
		t.Fatalf("FileRead error: %v", err)
	}
	if result.IsError {
		t.Fatalf("FileRead tool error: %v", result.Content)
	}

	// Result should be ImageContent, not TextContent.
	b, _ := json.Marshal(result.Content[0])
	var raw map[string]any
	json.Unmarshal(b, &raw)
	if raw["type"] != "image" {
		t.Errorf("expected image content type, got %v", raw["type"])
	}
}

func TestFileReadDefaults(t *testing.T) {
	s := initTestServer(t)
	ctx := context.Background()
	defer s.CleanupSession(ctx)

	// Write a file.
	s.Bash(ctx, &mcp.CallToolRequest{}, &BashInput{
		Command: `printf 'a\nb\nc' > /data/defaults.txt`,
	})

	// Read with default offset/limit (0, 2000).
	result, _, err := s.FileRead(ctx, &mcp.CallToolRequest{}, &FileReadInput{
		Path: "/data/defaults.txt",
	})
	if err != nil {
		t.Fatalf("FileRead error: %v", err)
	}
	if result.IsError {
		t.Fatalf("FileRead tool error: %v", result.Content)
	}

	b, _ := json.Marshal(result.Content[0])
	var raw map[string]any
	json.Unmarshal(b, &raw)
	text := raw["text"].(string)
	// Should have all 3 lines with line numbers.
	if !strings.Contains(text, "1\ta") || !strings.Contains(text, "3\tc") {
		t.Errorf("expected all lines with line numbers, got:\n%s", text)
	}
}

func TestFileEdit(t *testing.T) {
	s := initTestServer(t)
	ctx := context.Background()
	defer s.CleanupSession(ctx)

	// Create a file.
	result, _, err := s.FileEdit(ctx, &mcp.CallToolRequest{}, &FileEditInput{
		Path:      "/data/edit_test.txt",
		OldString: "",
		NewString: "hello world\nfoo bar\n",
	})
	if err != nil {
		t.Fatalf("FileEdit create error: %v", err)
	}
	if result.IsError {
		t.Fatalf("FileEdit create tool error: %v", result.Content)
	}

	// Edit it.
	result, _, err = s.FileEdit(ctx, &mcp.CallToolRequest{}, &FileEditInput{
		Path:      "/data/edit_test.txt",
		OldString: "foo bar",
		NewString: "baz qux",
	})
	if err != nil {
		t.Fatalf("FileEdit error: %v", err)
	}
	if result.IsError {
		t.Fatalf("FileEdit tool error: %v", result.Content)
	}

	// Verify via bash.
	result, _, err = s.Bash(ctx, &mcp.CallToolRequest{}, &BashInput{
		Command: "cat /data/edit_test.txt",
	})
	if err != nil {
		t.Fatalf("Bash verify error: %v", err)
	}
	out := unmarshalToolResult[BashOutput](t, result)
	if out.Stdout != "hello world\nbaz qux" {
		t.Errorf("expected edited content, got %q", out.Stdout)
	}
}

func TestFileWrite(t *testing.T) {
	s := initTestServer(t)
	ctx := context.Background()
	defer s.CleanupSession(ctx)

	result, _, err := s.FileWrite(ctx, &mcp.CallToolRequest{}, &FileWriteInput{
		Path:    "/data/written.txt",
		Content: "hello from file_write",
	})
	if err != nil {
		t.Fatalf("FileWrite error: %v", err)
	}
	if result.IsError {
		t.Fatalf("FileWrite tool error: %v", result.Content)
	}

	// Verify content.
	bashResult, _, _ := s.Bash(ctx, &mcp.CallToolRequest{}, &BashInput{
		Command: "cat /data/written.txt",
	})
	out := unmarshalToolResult[BashOutput](t, bashResult)
	if out.Stdout != "hello from file_write" {
		t.Errorf("expected written content, got %q", out.Stdout)
	}
}

func TestFileWriteOverwrite(t *testing.T) {
	s := initTestServer(t)
	ctx := context.Background()
	defer s.CleanupSession(ctx)

	// Write initial content.
	s.FileWrite(ctx, &mcp.CallToolRequest{}, &FileWriteInput{
		Path:    "/data/overwrite.txt",
		Content: "original",
	})

	// Overwrite.
	result, _, err := s.FileWrite(ctx, &mcp.CallToolRequest{}, &FileWriteInput{
		Path:    "/data/overwrite.txt",
		Content: "replaced",
	})
	if err != nil {
		t.Fatalf("FileWrite error: %v", err)
	}
	if result.IsError {
		t.Fatalf("FileWrite tool error: %v", result.Content)
	}

	bashResult, _, _ := s.Bash(ctx, &mcp.CallToolRequest{}, &BashInput{
		Command: "cat /data/overwrite.txt",
	})
	out := unmarshalToolResult[BashOutput](t, bashResult)
	if out.Stdout != "replaced" {
		t.Errorf("expected overwritten content, got %q", out.Stdout)
	}
}

func TestFileEditAmbiguous(t *testing.T) {
	s := initTestServer(t)
	ctx := context.Background()
	defer s.CleanupSession(ctx)

	// Create a file with duplicated text.
	s.FileEdit(ctx, &mcp.CallToolRequest{}, &FileEditInput{
		Path:      "/data/ambiguous.txt",
		OldString: "",
		NewString: "abc\nabc\n",
	})

	result, _, err := s.FileEdit(ctx, &mcp.CallToolRequest{}, &FileEditInput{
		Path:      "/data/ambiguous.txt",
		OldString: "abc",
		NewString: "xyz",
	})
	if err != nil {
		t.Fatalf("FileEdit error: %v", err)
	}
	if !result.IsError {
		t.Fatal("expected tool error for ambiguous old_string")
	}
}

func TestFileEditReplaceAll(t *testing.T) {
	s := initTestServer(t)
	ctx := context.Background()
	defer s.CleanupSession(ctx)

	// Create a file with duplicated text.
	s.FileEdit(ctx, &mcp.CallToolRequest{}, &FileEditInput{
		Path:      "/data/replaceall.txt",
		OldString: "",
		NewString: "abc\nabc\nabc\n",
	})

	result, _, err := s.FileEdit(ctx, &mcp.CallToolRequest{}, &FileEditInput{
		Path:       "/data/replaceall.txt",
		OldString:  "abc",
		NewString:  "xyz",
		ReplaceAll: true,
	})
	if err != nil {
		t.Fatalf("FileEdit error: %v", err)
	}
	if result.IsError {
		t.Fatalf("FileEdit tool error: %v", result.Content)
	}

	// Verify all occurrences replaced.
	bashResult, _, _ := s.Bash(ctx, &mcp.CallToolRequest{}, &BashInput{
		Command: "cat /data/replaceall.txt",
	})
	out := unmarshalToolResult[BashOutput](t, bashResult)
	if out.Stdout != "xyz\nxyz\nxyz" {
		t.Errorf("expected all replaced, got %q", out.Stdout)
	}
}

func TestFileEditNotFound(t *testing.T) {
	s := initTestServer(t)
	ctx := context.Background()
	defer s.CleanupSession(ctx)

	// Create a file, then try to replace text that doesn't exist.
	s.FileEdit(ctx, &mcp.CallToolRequest{}, &FileEditInput{
		Path:      "/data/notfound.txt",
		OldString: "",
		NewString: "hello\n",
	})

	result, _, err := s.FileEdit(ctx, &mcp.CallToolRequest{}, &FileEditInput{
		Path:      "/data/notfound.txt",
		OldString: "nonexistent text",
		NewString: "replacement",
	})
	if err != nil {
		t.Fatalf("FileEdit error: %v", err)
	}
	if !result.IsError {
		t.Fatal("expected tool error for old_string not found")
	}
}

func TestGrep(t *testing.T) {
	s := initTestServer(t)
	ctx := context.Background()
	defer s.CleanupSession(ctx)

	// Create test files.
	s.Bash(ctx, &mcp.CallToolRequest{}, &BashInput{
		Command: `mkdir -p /data/grep_test && echo 'hello world' > /data/grep_test/a.txt && echo 'goodbye world' > /data/grep_test/b.txt`,
	})

	result, _, err := s.Grep(ctx, &mcp.CallToolRequest{}, &GrepInput{
		Pattern: "hello",
		Path:    "/data/grep_test",
	})
	if err != nil {
		t.Fatalf("Grep error: %v", err)
	}
	if result.IsError {
		t.Fatalf("Grep tool error: %v", result.Content)
	}
	b, _ := json.Marshal(result.Content[0])
	var raw map[string]any
	json.Unmarshal(b, &raw)
	text := raw["text"].(string)
	if text == "No matches found." {
		t.Error("expected matches")
	}
	t.Logf("Grep output: %s", text)
}

func TestGrepNoMatch(t *testing.T) {
	s := initTestServer(t)
	ctx := context.Background()
	defer s.CleanupSession(ctx)

	s.Bash(ctx, &mcp.CallToolRequest{}, &BashInput{
		Command: `mkdir -p /data/grep_none && echo 'hello' > /data/grep_none/a.txt`,
	})

	result, _, err := s.Grep(ctx, &mcp.CallToolRequest{}, &GrepInput{
		Pattern: "zzzzz_no_match",
		Path:    "/data/grep_none",
	})
	if err != nil {
		t.Fatalf("Grep error: %v", err)
	}
	text := extractText(t, result)
	if text != "No matches found." {
		t.Errorf("expected 'No matches found.', got %q", text)
	}
}

func TestGrepWithGlob(t *testing.T) {
	s := initTestServer(t)
	ctx := context.Background()
	defer s.CleanupSession(ctx)

	s.Bash(ctx, &mcp.CallToolRequest{}, &BashInput{
		Command: `mkdir -p /data/grep_glob && echo 'target' > /data/grep_glob/a.txt && echo 'target' > /data/grep_glob/b.py`,
	})

	result, _, err := s.Grep(ctx, &mcp.CallToolRequest{}, &GrepInput{
		Pattern: "target",
		Path:    "/data/grep_glob",
		Glob:    "*.txt",
	})
	if err != nil {
		t.Fatalf("Grep error: %v", err)
	}
	text := extractText(t, result)
	if !strings.Contains(text, "a.txt") {
		t.Errorf("expected match in a.txt, got %q", text)
	}
	if strings.Contains(text, "b.py") {
		t.Errorf("did not expect match in b.py, got %q", text)
	}
}

func TestGlob(t *testing.T) {
	s := initTestServer(t)
	ctx := context.Background()
	defer s.CleanupSession(ctx)

	// Create test files.
	s.Bash(ctx, &mcp.CallToolRequest{}, &BashInput{
		Command: `mkdir -p /data/glob_test/sub && touch /data/glob_test/a.txt /data/glob_test/b.py /data/glob_test/sub/c.txt`,
	})

	result, _, err := s.Glob(ctx, &mcp.CallToolRequest{}, &GlobInput{
		Pattern: "*.txt",
		Path:    "/data/glob_test",
	})
	if err != nil {
		t.Fatalf("Glob error: %v", err)
	}
	if result.IsError {
		t.Fatalf("Glob tool error: %v", result.Content)
	}
	text := extractText(t, result)
	if text == "No files found." {
		t.Error("expected files")
	}
	t.Logf("Glob output: %s", text)
}

func TestGrepFilesWithMatches(t *testing.T) {
	s := initTestServer(t)
	ctx := context.Background()
	defer s.CleanupSession(ctx)

	s.Bash(ctx, &mcp.CallToolRequest{}, &BashInput{
		Command: `mkdir -p /data/grep_mode && echo 'hello' > /data/grep_mode/a.txt && echo 'hello' > /data/grep_mode/b.txt`,
	})

	result, _, err := s.Grep(ctx, &mcp.CallToolRequest{}, &GrepInput{
		Pattern:    "hello",
		Path:       "/data/grep_mode",
		OutputMode: "files_with_matches",
	})
	if err != nil {
		t.Fatalf("Grep error: %v", err)
	}
	text := extractText(t, result)
	if !strings.Contains(text, "a.txt") || !strings.Contains(text, "b.txt") {
		t.Errorf("expected both files listed, got %q", text)
	}
	// Should not contain line content, just paths.
	if strings.Contains(text, ":hello") {
		t.Errorf("files_with_matches should not contain line content, got %q", text)
	}
}

func TestGrepCount(t *testing.T) {
	s := initTestServer(t)
	ctx := context.Background()
	defer s.CleanupSession(ctx)

	s.Bash(ctx, &mcp.CallToolRequest{}, &BashInput{
		Command: `mkdir -p /data/grep_count && printf 'aa\naa\nbb\n' > /data/grep_count/a.txt`,
	})

	result, _, err := s.Grep(ctx, &mcp.CallToolRequest{}, &GrepInput{
		Pattern:    "aa",
		Path:       "/data/grep_count",
		OutputMode: "count",
	})
	if err != nil {
		t.Fatalf("Grep error: %v", err)
	}
	text := extractText(t, result)
	if !strings.Contains(text, ":2") {
		t.Errorf("expected count of 2, got %q", text)
	}
}

func TestGrepIgnoreCase(t *testing.T) {
	s := initTestServer(t)
	ctx := context.Background()
	defer s.CleanupSession(ctx)

	s.Bash(ctx, &mcp.CallToolRequest{}, &BashInput{
		Command: `mkdir -p /data/grep_icase && echo 'Hello World' > /data/grep_icase/a.txt`,
	})

	result, _, err := s.Grep(ctx, &mcp.CallToolRequest{}, &GrepInput{
		Pattern:    "hello",
		Path:       "/data/grep_icase",
		IgnoreCase: true,
	})
	if err != nil {
		t.Fatalf("Grep error: %v", err)
	}
	text := extractText(t, result)
	if text == "No matches found." {
		t.Error("expected case-insensitive match")
	}
}

func TestGrepContext(t *testing.T) {
	s := initTestServer(t)
	ctx := context.Background()
	defer s.CleanupSession(ctx)

	s.Bash(ctx, &mcp.CallToolRequest{}, &BashInput{
		Command: `mkdir -p /data/grep_ctx && printf 'before\ntarget\nafter\n' > /data/grep_ctx/a.txt`,
	})

	result, _, err := s.Grep(ctx, &mcp.CallToolRequest{}, &GrepInput{
		Pattern: "target",
		Path:    "/data/grep_ctx",
		Before:  1,
		After:   1,
	})
	if err != nil {
		t.Fatalf("Grep error: %v", err)
	}
	text := extractText(t, result)
	if !strings.Contains(text, "before") || !strings.Contains(text, "after") {
		t.Errorf("expected context lines, got %q", text)
	}
}

func TestGlobNoMatch(t *testing.T) {
	s := initTestServer(t)
	ctx := context.Background()
	defer s.CleanupSession(ctx)

	s.Bash(ctx, &mcp.CallToolRequest{}, &BashInput{
		Command: `mkdir -p /data/glob_none && touch /data/glob_none/a.txt`,
	})

	result, _, err := s.Glob(ctx, &mcp.CallToolRequest{}, &GlobInput{
		Pattern: "*.xyz",
		Path:    "/data/glob_none",
	})
	if err != nil {
		t.Fatalf("Glob error: %v", err)
	}
	text := extractText(t, result)
	if text != "No files found." {
		t.Errorf("expected 'No files found.', got %q", text)
	}
}

func TestGlobRecursive(t *testing.T) {
	s := initTestServer(t)
	ctx := context.Background()
	defer s.CleanupSession(ctx)

	s.Bash(ctx, &mcp.CallToolRequest{}, &BashInput{
		Command: `mkdir -p /data/glob_rec/a/b && touch /data/glob_rec/top.txt /data/glob_rec/a/mid.txt /data/glob_rec/a/b/deep.txt /data/glob_rec/a/b/deep.py`,
	})

	result, _, err := s.Glob(ctx, &mcp.CallToolRequest{}, &GlobInput{
		Pattern: "**/*.txt",
		Path:    "/data/glob_rec",
	})
	if err != nil {
		t.Fatalf("Glob error: %v", err)
	}
	text := extractText(t, result)
	if !strings.Contains(text, "top.txt") || !strings.Contains(text, "mid.txt") || !strings.Contains(text, "deep.txt") {
		t.Errorf("expected all .txt files recursively, got %q", text)
	}
	if strings.Contains(text, "deep.py") {
		t.Errorf("should not include .py files, got %q", text)
	}
}

func TestUploadAndParse(t *testing.T) {
	s := initTestServer(t)
	ctx := context.Background()
	defer s.CleanupSession(ctx)

	// Upload.
	result, _, err := s.Upload(ctx, &mcp.CallToolRequest{}, &UploadInput{
		Source: "file://testdata/sixt_DE_de.pdf",
	})
	if err != nil {
		t.Fatalf("Upload error: %v", err)
	}
	if result.IsError {
		t.Fatalf("Upload tool error: %v", result.Content)
	}
	upload := unmarshalToolResult[UploadOutput](t, result)
	if upload.Path == "" {
		t.Fatal("expected non-empty path")
	}
	t.Logf("uploaded to: %s (%d bytes)", upload.Path, upload.Size)

	// Parse.
	result, _, err = s.Parse(ctx, &mcp.CallToolRequest{}, &ParseInput{
		Path: upload.Path,
	})
	if err != nil {
		t.Fatalf("Parse error: %v", err)
	}
	if result.IsError {
		t.Fatalf("Parse tool error: %v", result.Content)
	}
	parse := unmarshalToolResult[ParseOutput](t, result)
	if parse.OutputPath == "" {
		t.Fatal("expected non-empty output path")
	}
	if parse.Size == 0 {
		t.Error("expected non-zero parsed size")
	}
	t.Logf("parsed to: %s (%d pages, %d bytes)", parse.OutputPath, parse.Pages, parse.Size)

	// Verify parsed file exists via bash.
	result, _, err = s.Bash(ctx, &mcp.CallToolRequest{}, &BashInput{
		Command: "wc -l " + parse.OutputPath,
	})
	if err != nil {
		t.Fatalf("Bash verify error: %v", err)
	}
	out := unmarshalToolResult[BashOutput](t, result)
	if out.ExitCode != 0 {
		t.Errorf("expected exit code 0, got %d (stderr: %s)", out.ExitCode, out.Stderr)
	}
	t.Logf("parsed file line count: %s", out.Stdout)
}

func TestUploadDataURI(t *testing.T) {
	s := initTestServer(t)
	ctx := context.Background()
	defer s.CleanupSession(ctx)

	result, _, err := s.Upload(ctx, &mcp.CallToolRequest{}, &UploadInput{
		Source:      "data:hello sandbox",
		Destination: "/data/data_uri.txt",
	})
	if err != nil {
		t.Fatalf("Upload error: %v", err)
	}
	if result.IsError {
		t.Fatalf("Upload tool error: %v", result.Content)
	}
	out := unmarshalToolResult[UploadOutput](t, result)
	if out.Path != "/data/data_uri.txt" {
		t.Errorf("expected path /data/data_uri.txt, got %q", out.Path)
	}

	// Verify content.
	bashResult, _, _ := s.Bash(ctx, &mcp.CallToolRequest{}, &BashInput{
		Command: "cat /data/data_uri.txt",
	})
	bashOut := unmarshalToolResult[BashOutput](t, bashResult)
	if bashOut.Stdout != "hello sandbox" {
		t.Errorf("expected content 'hello sandbox', got %q", bashOut.Stdout)
	}
}

func TestUploadInvalidSource(t *testing.T) {
	s := initTestServer(t)
	ctx := context.Background()
	defer s.CleanupSession(ctx)

	result, _, err := s.Upload(ctx, &mcp.CallToolRequest{}, &UploadInput{
		Source: "ftp://example.com/file.txt",
	})
	if err != nil {
		t.Fatalf("Upload error: %v", err)
	}
	if !result.IsError {
		t.Fatal("expected tool error for invalid source scheme")
	}
}

func TestUploadLocalFileNotFound(t *testing.T) {
	s := initTestServer(t)
	ctx := context.Background()
	defer s.CleanupSession(ctx)

	result, _, err := s.Upload(ctx, &mcp.CallToolRequest{}, &UploadInput{
		Source: "file:///nonexistent/path/file.txt",
	})
	if err != nil {
		t.Fatalf("Upload error: %v", err)
	}
	if !result.IsError {
		t.Fatal("expected tool error for non-existent local file")
	}
}

func TestUploadCustomDestination(t *testing.T) {
	s := initTestServer(t)
	ctx := context.Background()
	defer s.CleanupSession(ctx)

	result, _, err := s.Upload(ctx, &mcp.CallToolRequest{}, &UploadInput{
		Source:      "file://testdata/sixt_DE_de.pdf",
		Destination: "/data/custom/renamed.pdf",
	})
	if err != nil {
		t.Fatalf("Upload error: %v", err)
	}
	if result.IsError {
		t.Fatalf("Upload tool error: %v", result.Content)
	}
	out := unmarshalToolResult[UploadOutput](t, result)
	if out.Path != "/data/custom/renamed.pdf" {
		t.Errorf("expected path /data/custom/renamed.pdf, got %q", out.Path)
	}
}

func TestParseNonExistent(t *testing.T) {
	s := initTestServer(t)
	ctx := context.Background()
	defer s.CleanupSession(ctx)

	result, _, err := s.Parse(ctx, &mcp.CallToolRequest{}, &ParseInput{
		Path: "/data/nonexistent.pdf",
	})
	if err != nil {
		t.Fatalf("Parse error: %v", err)
	}
	if !result.IsError {
		t.Fatal("expected tool error for non-existent file")
	}
}
