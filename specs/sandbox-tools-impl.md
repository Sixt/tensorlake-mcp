# Sandbox-Based MCP Tools — Implementation

Implements the design in [sandbox-tools-design.md](sandbox-tools-design.md).

## File Organization

```
main.go             Server init, env vars, tool registration
server.go           server struct, ensureSandbox, runCommand, CleanupSession, helpers
tool_bash.go        bash tool
tool_file_read.go   file_read tool
tool_file_edit.go   file_edit tool
tool_grep.go        grep tool
tool_glob.go        glob tool
tool_upload.go      upload tool
tool_parse.go       parse tool
tools_test.go       Integration tests
internal/mimetype/  Unchanged
```

Deleted: `resources.go`, `tools.go` (split into above files).

## Environment Variables

```
TENSORLAKE_API_KEY                     required
TENSORLAKE_API_BASE_URL                optional   default: https://api.tensorlake.ai/documents/v2
TENSORLAKE_SANDBOX_API_BASE_URL        optional   default: https://api.tensorlake.ai/sandboxes
TENSORLAKE_SANDBOX_PROXY_BASE_URL      optional   default: https://sandbox.tensorlake.ai
TENSORLAKE_SANDBOX_TIMEOUT_SECS        optional   default: 3600
TENSORLAKE_MCP_LOG_LEVEL               optional   default: debug
```

## server.go

### server struct

```go
type server struct {
    tl        *tensorlake.Client
    sandboxID string
    sandboxMu sync.Mutex
}
```

### ensureSandbox

```go
func (s *server) ensureSandbox(ctx context.Context) (string, error)
```

- Lock `sandboxMu`
- If `sandboxID` already set, return it
- `CreateSandbox(ctx, &CreateSandboxRequest{TimeoutSecs: ptr(int64(timeout))})`
- Poll `GetSandbox` every 500ms until `Status == SandboxStatusRunning`
  (60s total timeout)
- Set `s.sandboxID`, return

### runCommand

```go
func (s *server) runCommand(ctx context.Context, command string, timeoutSec int, workingDir string) (exitCode int, stdout, stderr string, err error)
```

- Call `ensureSandbox`
- Create timeout context from `timeoutSec` (default 30)
- `StartProcess(ctx, sandboxID, &StartProcessRequest{Command: "bash", Args: ["-c", command], WorkingDir: workingDir, StdoutMode: "capture", StderrMode: "capture"})`
- Collect output via `FollowProcessOutput` (SSE iterator), accumulate
  stdout/stderr lines by `event.Stream`
- Fallback: poll `GetProcess` every 250ms until not running, then
  `GetProcessStdout` + `GetProcessStderr`
- On context timeout: `KillProcess`, return timeout error
- `GetProcess` for final exit code
- Truncate stdout/stderr independently at 100KB (first 40KB + last 40KB
  with `... [N bytes truncated] ...` marker)

### CleanupSession

```go
func (s *server) CleanupSession(ctx context.Context) {
    s.sandboxMu.Lock()
    defer s.sandboxMu.Unlock()
    if s.sandboxID != "" {
        s.tl.DeleteSandbox(ctx, s.sandboxID)
        s.sandboxID = ""
    }
}
```

### Helpers (carried from current tools.go)

- `newToolResultJSON[T](data T) (*mcp.CallToolResult, any, error)`
- `newToolResultError(err error) (*mcp.CallToolResult, any, error)`
- `sendProgress(ctx, req, progress, total, message)`
- `downloadFile(ctx, url, authToken) (io.ReadCloser, string, error)`
- `truncateOutput(s string) string`

## main.go

- Add sandbox env vars, pass to client via `WithSandboxAPIBaseURL`,
  `WithSandboxProxyBaseURL`
- Move API key check to `main()` (already done)
- Remove `HasResources`, `CompletionHandler`, `initializeDocumentResources`
- Register 7 tools with `mcp.AddTool`
- Update server instructions string

## tool_bash.go

```go
type BashInput struct {
    Command    string `json:"command"`
    TimeoutSec int    `json:"timeout_sec,omitempty"`
    WorkingDir string `json:"working_dir,omitempty"`
}

type BashOutput struct {
    ExitCode int    `json:"exit_code"`
    Stdout   string `json:"stdout"`
    Stderr   string `json:"stderr"`
}

func (s *server) Bash(ctx, req, in *BashInput) (*mcp.CallToolResult, any, error)
```

Delegates to `s.runCommand(ctx, in.Command, in.TimeoutSec, in.WorkingDir)`.
Returns `BashOutput` as JSON.

## tool_file_read.go

```go
type FileReadInput struct {
    Path   string `json:"path"`
    Offset int    `json:"offset,omitempty"`
    Limit  int    `json:"limit,omitempty"`
}

func (s *server) FileRead(ctx, req, in *FileReadInput) (*mcp.CallToolResult, any, error)
```

- `ensureSandbox`
- `ReadSandboxFile(ctx, sandboxID, path)` → `[]byte`
- Split into lines, apply offset/limit (default limit 2000)
- Format with line numbers: `%6d\t%s`
- Return as text content (not JSON)

## tool_file_edit.go

```go
type FileEditInput struct {
    Path      string `json:"path"`
    OldString string `json:"old_string"`
    NewString string `json:"new_string"`
}

func (s *server) FileEdit(ctx, req, in *FileEditInput) (*mcp.CallToolResult, any, error)
```

- `ensureSandbox`
- `ReadSandboxFile` → get content as string
- If `old_string` empty and file doesn't exist: create file with `new_string`
- `strings.Count(content, oldString)`: must be exactly 1, else error
- `strings.Replace(content, oldString, newString, 1)`
- `WriteSandboxFile`
- Return success message

## tool_grep.go

```go
type GrepInput struct {
    Pattern string `json:"pattern"`
    Path    string `json:"path,omitempty"`
    Glob    string `json:"glob,omitempty"`
}

func (s *server) Grep(ctx, req, in *GrepInput) (*mcp.CallToolResult, any, error)
```

- Build command: `grep -rn --include='<glob>' '<pattern>' <path>`
- `runCommand(ctx, cmd, 30, "/")`
- Return stdout as text content
- Truncate at 200 matches

## tool_glob.go

```go
type GlobInput struct {
    Pattern string `json:"pattern"`
    Path    string `json:"path,omitempty"`
}

func (s *server) Glob(ctx, req, in *GlobInput) (*mcp.CallToolResult, any, error)
```

- Build command: `find <path> -name '<pattern>' | head -500`
- `runCommand(ctx, cmd, 30, "/")`
- Return stdout as text content

## tool_upload.go

```go
type UploadInput struct {
    Source      string `json:"source"`
    Destination string `json:"destination,omitempty"`
}

type UploadOutput struct {
    Path string `json:"path"`
    Size int64  `json:"size"`
}

func (s *server) Upload(ctx, req, in *UploadInput) (*mcp.CallToolResult, any, error)
```

- `ensureSandbox`
- Detect source type by prefix (`http`, `data:`, `file://`)
- For URLs: reuse `downloadFile` → get reader + filename
- For data URIs: decode, detect mimetype (reuse `internal/mimetype`)
- For local files: `os.Open`
- Determine destination: `cmp.Or(in.Destination, "/data/"+filename)`
- `WriteSandboxFile(ctx, sandboxID, destination, reader)`
- Return `UploadOutput`

## tool_parse.go

```go
type ParseInput struct {
    Path       string `json:"path"`
    OutputPath string `json:"output_path,omitempty"`
}

type ParseOutput struct {
    OutputPath string `json:"output_path"`
    Pages      int    `json:"pages"`
    Size       int    `json:"size"`
}

func (s *server) Parse(ctx, req, in *ParseInput) (*mcp.CallToolResult, any, error)
```

1. `ensureSandbox`
2. `ReadSandboxFile(ctx, sandboxID, in.Path)` → content bytes
3. `s.tl.UploadFile(ctx, &UploadFileRequest{FileBytes: reader, FileName: basename})`
4. `s.tl.ParseDocument(ctx, &ParseDocumentRequest{...})` with current options
5. `s.tl.GetParseResult(ctx, parseId, WithSSE(true), WithOnUpdate(...))` —
   send progress notifications
6. Determine output path: `cmp.Or(in.OutputPath, "/data/parsed/"+basename+".md")`
7. Ensure parent dir: `runCommand(ctx, "mkdir -p "+dir, 10, "/")`
8. `WriteSandboxFile(ctx, sandboxID, outputPath, result)`
9. Cleanup: `DeleteParseJob` + `DeleteFile` (transient tensorlake resources)
10. Return `ParseOutput`

## Tests (tools_test.go)

All integration tests. Skip when `TENSORLAKE_API_KEY` is unset.

| Test | What it covers |
|------|---------------|
| `TestSandboxLifecycle` | `ensureSandbox` creates sandbox, returns same ID on second call, `CleanupSession` deletes it |
| `TestBash` | Run `echo hello`, verify stdout, exit code 0 |
| `TestBashTimeout` | Run `sleep 60` with 2s timeout, verify timeout error |
| `TestFileReadWrite` | bash writes file, `FileRead` reads it back with offset/limit |
| `TestFileEdit` | Write file, edit via `FileEdit`, verify replacement |
| `TestGrep` | Write files, grep for pattern, verify matches |
| `TestGlob` | Write files in subdirs, glob pattern, verify paths |
| `TestUploadAndParse` | Upload testdata PDF, parse, verify output file in sandbox |

Each test calls `defer s.CleanupSession(ctx)`.

## Implementation Order

1. `server.go` — struct, `ensureSandbox`, `runCommand`, `CleanupSession`, helpers
2. `main.go` — env vars, client construction, tool registration
3. `tool_bash.go` — first testable tool, validates sandbox lifecycle end-to-end
4. `tool_file_read.go`
5. `tool_file_edit.go`
6. `tool_grep.go` + `tool_glob.go`
7. `tool_upload.go`
8. `tool_parse.go`
9. Delete `resources.go`
10. `tools_test.go` — rewrite integration tests

## Verification

1. `go build ./...` passes
2. `go test ./...` passes (requires `TENSORLAKE_API_KEY`)
3. Manual test via MCP inspector or Claude Desktop:
   - `upload(source: "file://testdata/sixt_DE_de.pdf")`
   - `parse(path: "/data/sixt_DE_de.pdf")`
   - `bash(command: "wc -l /data/parsed/sixt_DE_de.md")`
   - `grep(pattern: "SIXT", path: "/data/parsed")`
   - `file_read(path: "/data/parsed/sixt_DE_de.md", limit: 20)`
