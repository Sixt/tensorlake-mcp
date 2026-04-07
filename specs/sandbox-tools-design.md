# Sandbox-Based MCP Tools — Design

## Problem

MCP's request-response protocol is poorly suited for large documents. When
`parse_document` returns a 200-page PDF as markdown, the entire content flows
through MCP into the LLM context — wasting tokens and exceeding context limits.

The LLM cannot meaningfully consume megabytes of parsed text. It needs to
operate on documents selectively, not ingest them wholesale.

## Key Insight

The sandbox separates the data plane from the control plane. Documents and
parsed output live in the sandbox filesystem. MCP carries only small, targeted
command outputs. The LLM interacts with documents the same way a developer
does — through bash, grep, file reads — not by loading everything into memory.

```
Current (broken for large files):

  upload → parse → [huge text through MCP] → LLM tries to read it all

Proposed:

  upload → parse → results stay in sandbox filesystem
                   → LLM writes targeted commands to query/extract
                   → only relevant output comes back through MCP
```

## Design Principles

1. **Sandbox is hidden infrastructure.** The LLM never sees sandbox IDs,
   lifecycle operations, or cloud compute vocabulary. It sees a filesystem
   and a shell.

2. **Standard agent primitives.** Five of seven tools (bash, file_read,
   file_edit, grep, glob) mirror what LLMs already know from coding agents.
   Zero new concepts to learn.

3. **Synchronous and bounded.** Every tool call returns a finite result in
   bounded time. No streaming, no polling, no process IDs exposed.

4. **Output-truncated.** Large outputs are truncated to protect context
   windows. The LLM can always narrow its query instead.

5. **Explicit composition.** The LLM chains tool calls (upload → parse →
   grep → read) rather than specifying complex compound operations. This
   lets it inspect intermediate results and adjust.

## Tool Surface

Seven tools total.

### Agent primitives (5)

| Tool | Purpose |
|------|---------|
| `bash` | Run a shell command. Returns exit code + stdout + stderr. Timeout-bounded (default 30s). |
| `file_read` | Read a file with offset/limit. Line-numbered output (cat -n format). |
| `file_edit` | Edit a file via exact string replacement. Must match exactly once. |
| `grep` | Search file contents by regex. Returns filename:line:content. |
| `glob` | Find files by pattern. Returns matching paths. |

### Tensorlake-specific (2)

| Tool | Purpose |
|------|---------|
| `upload` | Put an external file into the sandbox (from URL, local path, or data URI). |
| `parse` | Parse a document in the sandbox via tensorlake AI. Results written back to sandbox filesystem. |

### Tool schemas

**bash**
```
Input:  command (string, required), timeout_sec (int, optional, default 30),
        working_dir (string, optional, default "/data")
Output: { exit_code, stdout, stderr }
```

**file_read**
```
Input:  path (string, required), offset (int, optional, 0-based line),
        limit (int, optional, default 2000)
Output: Text with line numbers
```

**file_edit**
```
Input:  path (string, required), old_string (string, required),
        new_string (string, required)
Output: Success message with context
```

**grep**
```
Input:  pattern (string, required), path (string, optional, default "/data"),
        glob (string, optional, file filter)
Output: Matching lines (filename:line_number:content). Truncated at 200 matches.
```

**glob**
```
Input:  pattern (string, required), path (string, optional, default "/data")
Output: File paths, one per line. Truncated at 500 entries.
```

**upload**
```
Input:  source (string, required: URL / file:// / data: URI),
        destination (string, optional, default /data/<filename>)
Output: { path, size }
```

**parse**
```
Input:  path (string, required: path in sandbox),
        output_path (string, optional, default /data/parsed/<basename>.md)
Output: { output_path, pages, size }
```

## Sandbox Lifecycle

- **Lazy creation**: Sandbox created on first tool call. LLM is unaware.
- **Session-scoped**: One sandbox per MCP session.
- **Auto-cleanup**: Sandbox deleted on session end.
- **Auto-timeout**: Sandbox terminates after 1 hour (configurable).

## Data Flow: parse

The `parse` tool is the bridge between the sandbox filesystem and tensorlake's
parsing API:

```
sandbox filesystem          tensorlake API            sandbox filesystem
     /data/doc.pdf  ──────→  UploadFile  ──→  ParseDocument  ──→  /data/parsed/doc.md
                              (transient)       (transient)
```

The file passes through tensorlake transiently for AI parsing. Both the
uploaded file and parse job are cleaned up afterward. The authoritative copy
of input and output lives in the sandbox.

## Conventions

- Default working directory: `/data`
- Parse output directory: `/data/parsed/`
- All paths are absolute within the sandbox

## What's Excluded

| API | Reason |
|-----|--------|
| PTY terminals | WebSocket binary protocol; MCP is request-response |
| Process streaming | Used internally, not exposed as tool |
| stdin write | Implies incremental process interaction across turns |
| Suspend / resume | Requires state beyond conversation lifetime |
| Signal / kill | Timeout handling covers this internally |
| Snapshot | No user story yet for cross-session persistence |
| List sandboxes | One sandbox per session; nothing to list |

These can be revisited if MCP gains streaming support or if cross-session
persistence becomes a requirement.

## Example Session

```
agent: upload(source: "https://example.com/contract.pdf")
       → { path: "/data/contract.pdf", size: 2048576 }

agent: parse(path: "/data/contract.pdf")
       → { output_path: "/data/parsed/contract.md", pages: 84, size: 312400 }

agent: bash(command: "wc -l /data/parsed/contract.md")
       → { exit_code: 0, stdout: "4521 /data/parsed/contract.md" }

agent: grep(pattern: "termination clause", path: "/data/parsed")
       → /data/parsed/contract.md:1203:## Termination Clause
         /data/parsed/contract.md:1204:Either party may terminate...

agent: file_read(path: "/data/parsed/contract.md", offset: 1200, limit: 30)
       → (30 lines of targeted content around the termination clause)

agent: bash(command: "python3 -c \"import json; ...\"")
       → (structured extraction result)
```
