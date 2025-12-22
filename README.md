# Tensorlake MCP Server

An MCP (Model Context Protocol) server that provides access to Tensorlake's document parsing capabilities.

## Features

- **Document Upload**: Upload documents from URLs, local file paths, or data URIs
- **Document Parsing**: Parse documents into structured data using Tensorlake's AI-powered parsing engine
- **Document Management**: List and delete documents in your session
- **MCP Resources**: Access parsed documents as MCP resources for seamless integration

## Prerequisites

- A Tensorlake API key (sign up at [Tensorlake](https://tensorlake.ai))
- An MCP-compatible host application (e.g., [Claude Desktop](https://claude.ai/download), [Cursor](https://cursor.com), or other MCP hosts)

## Installation

### Install using `go` install

```bash
go install github.com/sixt/tensorlake-mcp@latest
```

Locate the binary in your `GOPATH/bin` directory or use the `where` command to find it. For example:

```bash
$ where tensorlake-mcp

/Users/<username>/go/bin/tensorlake-mcp
```

### Building from Source

```bash
# Clone the repository
git clone <repository-url>
cd tensorlake-mcp

# Build the server
go build -o tensorlake-mcp .
```

The binary will be created as `tensorlake-mcp` in the current directory.

## Configuration

The server requires the following environment variables:

- **`TENSORLAKE_API_KEY`** (required): Your Tensorlake API key
- **`TENSORLAKE_API_BASE_URL`** (optional): The base URL for the Tensorlake API. Defaults to `https://api.tensorlake.ai/documents/v2`

## Setup

The MCP configuration is:

```json
{
  "mcpServers": {
    "tensorlake-mcp": {
      "command": "/absolute/path/to/tensorlake-mcp",
      "env": {
        "TENSORLAKE_API_KEY": "your-api-key-here"
      }
    }
  }
}
```

See these articles for setting up the MCP server in your host application:
- [Claude Desktop](https://docs.anthropic.com/en/docs/mcp-servers/mcp-servers-claude-desktop)
- [Claude Code](https://code.claude.com/docs/en/mcp)
- [Cursor](https://cursor.com/docs/context/mcp)
- [Other MCP Clients](https://modelcontextprotocol.io/clients)

## Usage

Once configured, the MCP server provides the following tools:

### `upload_document`
Upload a document from a URL, local path, or data URI.

**Parameters:**
- `url` (string, required): The URL of the document to upload
  - URL: `https://example.com/document.pdf`
  - Local file: `file:///path/to/document.pdf`
  - Data URI: `data:application/pdf;base64,...`

**Returns:** A `document_id` to be used in subsequent operations

### `parse_document`
Parse an uploaded document into structured data.

**Parameters:**
- `document_id` (string, required): The ID returned from `upload_document`
- `parse_id` (string, optional): The parse ID to check status or get results
- `sync` (boolean, optional): If true, wait for parsing to complete. If false, start parsing in the background

**Returns:** Parse ID, status, and results (if completed)

### `list_documents`
List all documents in the current session.

**Returns:** A list of all uploaded documents with their metadata

### `delete_document`
Delete a document from Tensorlake.

**Parameters:**
- `document_id` (string, required): The ID of the document to delete

### Resources

The server also exposes a `tensorlake://documents` resource that provides access to all documents and their metadata.

## Example Interaction

```
User: Upload and parse this PDF document at https://example.com/invoice.pdf

AI: [Uses upload_document tool]
    [Uses parse_document tool with sync=true]
    Here's the parsed content from your invoice...
```

## Development

### Running in Development

```bash
# Set environment variables
export TENSORLAKE_API_KEY="your-api-key"

# Run the server
go build && ./tensorlake-mcp
```

### Testing with MCP Inspector

You can test the server using the [MCP Inspector](https://github.com/modelcontextprotocol/inspector):

```bash
npx @modelcontextprotocol/inspector /absolute/path/to/tensorlake-mcp
```

## License

Copyright 2025 SIXT SE. Licensed under the Apache License, Version 2.0. See [LICENSE](LICENSE) for details.

<a href="https://www.sixt.com">
    <picture>
        <source media="(prefers-color-scheme: dark)" srcset=".github/sixt_dark.png">
        <source media="(prefers-color-scheme: light)" srcset=".github/sixt_light.png">
        <img width="100px" alt="Sixt logo" src=".github/sixt_dark.png">
    </picture>
</a>

