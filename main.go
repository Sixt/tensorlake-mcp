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

	"github.com/google/jsonschema-go/jsonschema"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

const (
	serverName    = "tensorlake-mcp"
	serverVersion = "v0.1.0"
)

var (
	logLevel     = os.Getenv("TENSORLAKE_MCP_LOG_LEVEL") // Optional, debug, info, warn, error, default to debug.
	tlAPIBaseURL = os.Getenv("TENSORLAKE_API_BASE_URL")  // Optional, default to https://api.tensorlake.com/documents/v2
	tlAPIKey     = os.Getenv("TENSORLAKE_API_KEY")       // Required
)

func init() {
	logLevel = cmp.Or(logLevel, "debug") // default to debug

	// Setup the default logger be a json logger.
	slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
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
	if tlAPIKey == "" {
		slog.Error("TENSORLAKE_API_KEY environment variable is required")
		os.Exit(1)
	}
}

func main() {
	impl := mcp.NewServer(&mcp.Implementation{
		Name:    serverName,
		Version: serverVersion,
	}, &mcp.ServerOptions{
		Instructions: "Tensorlake MCP server provides advanced document parsing capabilities. It allows uploading files to Tensorlake and parsing them into structured data. Users can interactively refine the parsing schema and get the parsing results as MCP resources or retrieve the results as multi-modal content.",
		HasTools:     true,
		HasResources: true,
		CompletionHandler: func(ctx context.Context, cr *mcp.CompleteRequest) (*mcp.CompleteResult, error) {
			slog.Info("completion request", "request", cr)
			return nil, nil
		},
	})

	s := newServer()

	// Notes: We word the tool names using "document" instead of "file" to avoid confusion with the file tool which
	// is already spreaded everywhere in LLM host applications. For instance, Claude or Cursor both have their own file tool.

	mcp.AddTool(impl, &mcp.Tool{
		Name:        "upload_document",
		Description: "Upload a document from a URL, local path, or data URI to Tensorlake and obtain a document_id to be used later in other processing/parsing steps.",
		InputSchema: &jsonschema.Schema{
			Type: "object",
			Properties: map[string]*jsonschema.Schema{
				"url": {
					Type:        "string",
					Description: "The URL of the document to upload. Example: 'https://example.com/document.pdf', or 'data:application/pdf;base64,...', or 'file:///path/to/local/document.pdf'",
				},
			},
			Required: []string{"url"},
		},
	}, s.UploadDocument)

	mcp.AddTool(impl, &mcp.Tool{
		Name:        "delete_document",
		Description: "Delete a document from Tensorlake.",
		InputSchema: &jsonschema.Schema{
			Type: "object",
			Properties: map[string]*jsonschema.Schema{
				"document_id": {
					Type:        "string",
					Description: "The ID of the document to delete. Example: 'file_1234567890'. This is the document_id returned by the upload_document tool.",
				},
			},
			Required: []string{"document_id"},
		},
	}, s.DeleteDocument)

	mcp.AddTool(impl, &mcp.Tool{
		Name:        "parse_document",
		Description: "Parse a document and obtain the parsed outcome. The tool returns the parse_id, the status of the parse job, and the results of the parse job if the background job is completed. The parse job is started in the background if sync is false, otherwise, the tool will wait for the parse job to complete and return the results.",
		InputSchema: &jsonschema.Schema{
			Type: "object",
			Properties: map[string]*jsonschema.Schema{
				"document_id": {
					Type:        "string",
					Description: "The document Id to start parsing. Example: 'file_1234567890'. This is the document_id returned by the upload_document tool.",
				},
				"parse_id": {
					Type:        "string",
					Description: "The parse ID to check the status or get the results. Example: 'parse_1234567890'. If provided, the tool will check the status or get the results of the parse job.",
				},
				"sync": {
					Type:        "boolean",
					Description: "If true, wait for parsing to complete before returning results. If false, the tool will return the parse_id and wait for the parse job to complete in the background.",
				},
				// TODO: extend parsing options.
			},
		},
	}, s.ParseDocument)

	if err := impl.Run(context.Background(), &mcp.StdioTransport{}); err != nil {
		slog.Error("failed to run tensorlake-mcp", "error", err)
	}
}
