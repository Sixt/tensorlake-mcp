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
	"log/slog"
	"sync"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type BashInput struct {
	Command         string `json:"command"`
	TimeoutSec      int    `json:"timeout_sec,omitempty"`
	WorkingDir      string `json:"working_dir,omitempty"`
	Description     string `json:"description,omitempty"`
	RunInBackground bool   `json:"run_in_background,omitempty"`
}

type BashOutput struct {
	ExitCode int    `json:"exit_code"`
	Stdout   string `json:"stdout"`
	Stderr   string `json:"stderr"`
	TimedOut bool   `json:"timed_out,omitempty"`
}

// bgProcess holds the result of a background command.
type bgProcess struct {
	done   chan struct{}
	result BashOutput
	err    error
}

var (
	bgMu        sync.Mutex
	bgProcesses = map[string]*bgProcess{}
	bgCounter   int
)

func (s *server) Bash(ctx context.Context, req *mcp.CallToolRequest, in *BashInput) (*mcp.CallToolResult, any, error) {
	if in.Description != "" {
		slog.Info("bash", "description", in.Description, "command", in.Command)
	}

	if in.RunInBackground {
		return s.bashBackground(ctx, req, in)
	}

	// Send progress notifications during long-running commands.
	progress := func(elapsed, total int) {
		s.sendProgress(ctx, req, float64(elapsed), float64(total*4), "Running command...")
	}

	exitCode, stdout, stderr, err := s.runCommand(ctx, in.Command, in.TimeoutSec, in.WorkingDir, progress)
	if err != nil {
		return newToolResultJSON(&BashOutput{
			ExitCode: exitCode,
			Stdout:   stdout,
			Stderr:   stderr,
			TimedOut: true,
		})
	}
	return newToolResultJSON(&BashOutput{
		ExitCode: exitCode,
		Stdout:   stdout,
		Stderr:   stderr,
	})
}

func (s *server) bashBackground(ctx context.Context, _ *mcp.CallToolRequest, in *BashInput) (*mcp.CallToolResult, any, error) {
	// Ensure sandbox exists before spawning goroutine (needs the caller's context).
	if _, err := s.ensureSandbox(ctx); err != nil {
		return newToolResultError(fmt.Errorf("failed to ensure sandbox: %w", err))
	}

	bgMu.Lock()
	bgCounter++
	id := fmt.Sprintf("bg-%d", bgCounter)
	proc := &bgProcess{done: make(chan struct{})}
	bgProcesses[id] = proc
	bgMu.Unlock()

	go func() {
		defer close(proc.done)
		// Use a background context — the original request context will be cancelled.
		bgCtx := context.Background()
		exitCode, stdout, stderr, err := s.runCommand(bgCtx, in.Command, in.TimeoutSec, in.WorkingDir)
		proc.err = err
		if err != nil {
			proc.result = BashOutput{ExitCode: exitCode, Stdout: stdout, Stderr: stderr, TimedOut: true}
		} else {
			proc.result = BashOutput{ExitCode: exitCode, Stdout: stdout, Stderr: stderr}
		}
	}()

	return newToolResultJSON(map[string]string{
		"process_id": id,
		"status":     "running",
		"message":    fmt.Sprintf("Command started in background. Use bash_status with process_id %q to check results.", id),
	})
}

type BashStatusInput struct {
	ProcessID string `json:"process_id"`
}

func (s *server) BashStatus(ctx context.Context, req *mcp.CallToolRequest, in *BashStatusInput) (*mcp.CallToolResult, any, error) {
	bgMu.Lock()
	proc, ok := bgProcesses[in.ProcessID]
	bgMu.Unlock()

	if !ok {
		return newToolResultError(fmt.Errorf("unknown process_id: %s", in.ProcessID))
	}

	select {
	case <-proc.done:
		// Clean up.
		bgMu.Lock()
		delete(bgProcesses, in.ProcessID)
		bgMu.Unlock()
		return newToolResultJSON(&proc.result)
	default:
		return newToolResultJSON(map[string]string{
			"process_id": in.ProcessID,
			"status":     "running",
		})
	}
}
