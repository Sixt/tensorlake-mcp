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
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/sixt/tensorlake-go"
	"github.com/sixt/tensorlake-mcp/internal/mimetype"
)

type server struct {
	tl        *tensorlake.Client
	sandboxID string
	sandboxMu sync.Mutex
}

func newServer() *server {
	return &server{
		tl: tensorlake.NewClient(
			tensorlake.WithBaseURL(tlAPIBaseURL),
			tensorlake.WithAPIKey(tlAPIKey),
			tensorlake.WithSandboxAPIBaseURL(tlSandboxAPIBaseURL),
			tensorlake.WithSandboxProxyBaseURL(tlSandboxProxyBaseURL),
		),
	}
}

// sessionFilePath returns a deterministic temp file path for persisting the sandbox ID.
// The path is keyed by API key hash so different accounts don't collide.
func sessionFilePath() string {
	h := sha256.Sum256([]byte(tlAPIKey))
	return filepath.Join(os.TempDir(), fmt.Sprintf("tensorlake-mcp-session-%x", h[:8]))
}

// loadPersistedSandbox tries to restore a sandbox ID from disk and validates it is still running.
func (s *server) loadPersistedSandbox(ctx context.Context) (string, bool) {
	// Check env var override first.
	if id := os.Getenv("TENSORLAKE_SANDBOX_ID"); id != "" {
		info, err := s.tl.GetSandbox(ctx, id)
		if err == nil && info.Status == tensorlake.SandboxStatusRunning {
			slog.Info("reusing sandbox from TENSORLAKE_SANDBOX_ID", "sandbox_id", id)
			return id, true
		}
		slog.Warn("TENSORLAKE_SANDBOX_ID sandbox not running, creating new one", "sandbox_id", id)
	}

	// Try temp file.
	data, err := os.ReadFile(sessionFilePath())
	if err != nil {
		return "", false
	}
	id := strings.TrimSpace(string(data))
	if id == "" {
		return "", false
	}

	info, err := s.tl.GetSandbox(ctx, id)
	if err != nil || info.Status != tensorlake.SandboxStatusRunning {
		slog.Info("persisted sandbox no longer running, creating new one", "sandbox_id", id)
		os.Remove(sessionFilePath())
		return "", false
	}

	slog.Info("reusing persisted sandbox", "sandbox_id", id)
	return id, true
}

// persistSandboxID writes the sandbox ID to disk.
func persistSandboxID(id string) {
	if err := os.WriteFile(sessionFilePath(), []byte(id), 0o600); err != nil {
		slog.Warn("failed to persist sandbox ID", "error", err)
	}
}

// ensureSandbox lazily creates a sandbox on first call and returns its ID.
// It first tries to restore a persisted session from a prior server run.
func (s *server) ensureSandbox(ctx context.Context) (string, error) {
	s.sandboxMu.Lock()
	defer s.sandboxMu.Unlock()

	if s.sandboxID != "" {
		return s.sandboxID, nil
	}

	// Try to restore a persisted sandbox.
	if id, ok := s.loadPersistedSandbox(ctx); ok {
		s.sandboxID = id
		return id, nil
	}

	timeout := int64(tlSandboxTimeoutSecs)
	resp, err := s.tl.CreateSandbox(ctx, &tensorlake.CreateSandboxRequest{
		TimeoutSecs: &timeout,
	})
	if err != nil {
		return "", fmt.Errorf("failed to create sandbox: %w", err)
	}

	// Poll until sandbox is running.
	deadline := time.Now().Add(60 * time.Second)
	for {
		info, err := s.tl.GetSandbox(ctx, resp.SandboxId)
		if err != nil {
			return "", fmt.Errorf("failed to get sandbox status: %w", err)
		}
		if info.Status == tensorlake.SandboxStatusRunning {
			break
		}
		if info.Status == tensorlake.SandboxStatusTerminated {
			return "", fmt.Errorf("sandbox terminated unexpectedly")
		}
		if time.Now().After(deadline) {
			return "", fmt.Errorf("sandbox failed to start within 60s (status: %s)", info.Status)
		}
		time.Sleep(500 * time.Millisecond)
	}

	slog.Info("sandbox created", "sandbox_id", resp.SandboxId)
	s.sandboxID = resp.SandboxId
	persistSandboxID(s.sandboxID)

	// Create /data directory for the default workspace.
	if err := s.tl.WriteSandboxFile(ctx, s.sandboxID, "/data/.keep", strings.NewReader("")); err != nil {
		slog.Warn("failed to create /data directory", "error", err)
	}

	return s.sandboxID, nil
}

const maxOutputBytes = 100 * 1024 // 100KB

func truncateOutput(s string) string {
	if len(s) <= maxOutputBytes {
		return s
	}
	half := maxOutputBytes / 2
	head := s[:half]
	tail := s[len(s)-half:]
	omitted := len(s) - maxOutputBytes
	return head + fmt.Sprintf("\n\n... [%d bytes truncated] ...\n\n", omitted) + tail
}

// runCommand executes a shell command in the sandbox and returns its output.
// onProgress is called periodically while the command runs (can be nil).
func (s *server) runCommand(ctx context.Context, command string, timeoutSec int, workingDir string, onProgress ...func(elapsed, total int)) (exitCode int, stdout, stderr string, err error) {
	sandboxID, err := s.ensureSandbox(ctx)
	if err != nil {
		return -1, "", "", err
	}

	var progressFn func(elapsed, total int)
	if len(onProgress) > 0 {
		progressFn = onProgress[0]
	}

	timeout := cmp.Or(timeoutSec, 30)
	ctx, cancel := context.WithTimeout(ctx, time.Duration(timeout)*time.Second)
	defer cancel()

	// Retry process start — the sandbox container may still be initializing.
	var proc *tensorlake.ProcessInfo
	for range 5 {
		req := &tensorlake.StartProcessRequest{
			Command:    "/bin/sh",
			Args:       []string{"-c", command},
			StdoutMode: tensorlake.OutputModeCapture,
			StderrMode: tensorlake.OutputModeCapture,
		}
		if wd := cmp.Or(workingDir, ""); wd != "" {
			req.WorkingDir = wd
		}
		proc, err = s.tl.StartProcess(ctx, sandboxID, req)
		if err == nil {
			break
		}
		time.Sleep(500 * time.Millisecond)
	}
	if err != nil {
		return -1, "", "", fmt.Errorf("failed to start process: %w", err)
	}

	// Poll until process exits, sending progress notifications if provided.
	elapsed := 0
	for {
		info, err := s.tl.GetProcess(ctx, sandboxID, proc.PID)
		if err != nil {
			return -1, "", "", fmt.Errorf("failed to get process status: %w", err)
		}
		if info.Status != tensorlake.ProcessStatusRunning {
			break
		}
		if progressFn != nil {
			elapsed++
			progressFn(elapsed, timeout)
		}
		select {
		case <-ctx.Done():
			_ = s.tl.KillProcess(context.Background(), sandboxID, proc.PID)
			return -1, "", "", fmt.Errorf("command timed out after %ds", timeout)
		case <-time.After(250 * time.Millisecond):
		}
	}

	// Collect output.
	stdoutResp, err := s.tl.GetProcessStdout(ctx, sandboxID, proc.PID)
	if err != nil {
		return -1, "", "", fmt.Errorf("failed to get stdout: %w", err)
	}
	stderrResp, err := s.tl.GetProcessStderr(ctx, sandboxID, proc.PID)
	if err != nil {
		return -1, "", "", fmt.Errorf("failed to get stderr: %w", err)
	}

	// Get final exit code.
	info, err := s.tl.GetProcess(ctx, sandboxID, proc.PID)
	if err != nil {
		return -1, truncateOutput(strings.Join(stdoutResp.Lines, "\n")),
			truncateOutput(strings.Join(stderrResp.Lines, "\n")), err
	}

	ec := 0
	if info.ExitCode != nil {
		ec = int(*info.ExitCode)
	}
	return ec, truncateOutput(strings.Join(stdoutResp.Lines, "\n")),
		truncateOutput(strings.Join(stderrResp.Lines, "\n")), nil
}

// CleanupSession deletes the sandbox and removes the persisted session file.
func (s *server) CleanupSession(ctx context.Context) {
	s.sandboxMu.Lock()
	defer s.sandboxMu.Unlock()
	if s.sandboxID != "" {
		if err := s.tl.DeleteSandbox(ctx, s.sandboxID); err != nil {
			slog.Error("failed to delete sandbox", "sandbox_id", s.sandboxID, "error", err)
		} else {
			slog.Info("sandbox deleted", "sandbox_id", s.sandboxID)
		}
		os.Remove(sessionFilePath())
		s.sandboxID = ""
	}
}

// sendProgress sends a progress notification if a progress token is available.
func (s *server) sendProgress(ctx context.Context, req *mcp.CallToolRequest, progress float64, total float64, message string) {
	if req == nil || req.Session == nil {
		return
	}
	_ = req.Session.NotifyProgress(ctx, &mcp.ProgressNotificationParams{
		ProgressToken: req.Params.GetProgressToken(),
		Progress:      progress,
		Total:         total,
		Message:       message,
	})
}

func newToolResultJSON[T any](data T) (*mcp.CallToolResult, any, error) {
	b, err := json.Marshal(data)
	if err != nil {
		return newToolResultError(fmt.Errorf("unable to marshal JSON: %w", err))
	}
	return &mcp.CallToolResult{
		Content: []mcp.Content{
			&mcp.TextContent{Text: string(b)},
		},
	}, nil, nil
}

func newToolResultText(text string) (*mcp.CallToolResult, any, error) {
	return &mcp.CallToolResult{
		Content: []mcp.Content{
			&mcp.TextContent{Text: text},
		},
	}, nil, nil
}

func newToolResultError(err error) (*mcp.CallToolResult, any, error) {
	return &mcp.CallToolResult{
		IsError: true,
		Content: []mcp.Content{
			&mcp.TextContent{Text: err.Error()},
		},
	}, nil, nil
}

// downloadFile downloads a file from a URL with optional authorization.
func downloadFile(ctx context.Context, url, authToken string) (io.ReadCloser, string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, "", fmt.Errorf("failed to create request: %w", err)
	}
	if authToken != "" {
		req.Header.Set("Authorization", "Bearer "+authToken)
	}

	client := &http.Client{Timeout: 5 * time.Minute}
	resp, err := client.Do(req)
	if err != nil {
		return nil, "", fmt.Errorf("failed to download file: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		resp.Body.Close()
		return nil, "", fmt.Errorf("unexpected status code %d", resp.StatusCode)
	}

	fileName := filepath.Base(url)
	if cd := resp.Header.Get("Content-Disposition"); cd != "" {
		if name, ok := strings.CutPrefix(cd, "attachment; filename="); ok {
			fileName = strings.Trim(name, "\"")
		}
	}
	if filepath.Ext(fileName) == "" {
		detectedExt, err := mimetype.DetectExtensionFromContentType(resp)
		if err != nil {
			return nil, "", fmt.Errorf("failed to detect extension: %w", err)
		}
		if detectedExt != "" {
			fileName = fileName + detectedExt
		}
	}

	return resp.Body, fileName, nil
}
