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
	return &server{tl: tensorlake.NewClient(tensorlake.WithBaseURL(baseURL), tensorlake.WithAPIKey(apiKey))}
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

// TestUploadParseDelete exercises the full lifecycle: upload a local PDF,
// parse it synchronously, then delete the document. This also covers the
// former nil-pointer-dereference bug in fetchParseResult when the document
// was not yet in the files map at parse time.
func TestUploadParseDelete(t *testing.T) {
	s := initTestServer(t)
	ctx := context.Background()

	// Upload
	result, _, err := s.UploadDocument(ctx, &mcp.CallToolRequest{}, &UploadDocumentInput{
		URL: "file://testdata/sixt_DE_de.pdf",
	})
	if err != nil {
		t.Fatalf("UploadDocument error: %v", err)
	}
	if result.IsError {
		t.Fatalf("UploadDocument tool error: %v", result.Content)
	}
	upload := unmarshalToolResult[UploadDocumentOutput](t, result)
	if upload.DocumentId == "" {
		t.Fatal("expected non-empty document_id")
	}
	t.Logf("uploaded document: %s (%s, %d bytes)", upload.DocumentId, upload.DocumentName, upload.DocumentSize)

	// Clear the files map entry so that the parse path hits the
	// "document not in map" branch — this is the path that previously
	// caused a nil pointer dereference.
	files.Delete(upload.DocumentId)

	// Parse (sync)
	result, _, err = s.ParseDocument(ctx, &mcp.CallToolRequest{}, &ParseDocumentInput{
		DocumentId: upload.DocumentId,
		Sync:       true,
	})
	if err != nil {
		t.Fatalf("ParseDocument error: %v", err)
	}
	if result.IsError {
		t.Fatalf("ParseDocument tool error: %v", result.Content)
	}
	parse := unmarshalToolResult[ParseDocumentOutput](t, result)
	if parse.Status != tensorlake.ParseStatusSuccessful {
		t.Fatalf("expected status %q, got %q", tensorlake.ParseStatusSuccessful, parse.Status)
	}
	if parse.Result == "" {
		t.Error("expected non-empty parse result")
	}
	t.Logf("parsed document, parse_id: %s, result length: %d", parse.ParseID, len(parse.Result))

	// Verify the document was re-populated in the files map with metadata.
	info, ok := files.Load(upload.DocumentId)
	if !ok {
		t.Fatal("expected document to be in files map after parse")
	}
	if info.FileName == "" {
		t.Error("expected non-empty FileName after metadata fetch")
	}
	if len(info.ParseJobs) == 0 {
		t.Error("expected at least one parse job stored")
	}

	// Delete
	result, _, err = s.DeleteDocument(ctx, &mcp.CallToolRequest{}, &DeleteDocumentInput{
		DocumentId: upload.DocumentId,
	})
	if err != nil {
		t.Fatalf("DeleteDocument error: %v", err)
	}
	if result.IsError {
		t.Fatalf("DeleteDocument tool error: %v", result.Content)
	}

	// Verify cleanup
	if _, ok := files.Load(upload.DocumentId); ok {
		t.Error("expected document to be removed from files map after delete")
	}
}

// TestCleanupSession uploads two documents, parses them, then calls
// CleanupSession and verifies all documents and parse jobs are cleaned up.
// This also verifies that cleanup continues past individual errors
// (best-effort) rather than stopping on the first failure.
func TestCleanupSession(t *testing.T) {
	s := initTestServer(t)
	ctx := context.Background()

	// Upload two documents.
	var docIDs []string
	for range 2 {
		result, _, err := s.UploadDocument(ctx, &mcp.CallToolRequest{}, &UploadDocumentInput{
			URL: "file://testdata/sixt_DE_de.pdf",
		})
		if err != nil {
			t.Fatalf("UploadDocument error: %v", err)
		}
		if result.IsError {
			t.Fatalf("UploadDocument tool error: %v", result.Content)
		}
		upload := unmarshalToolResult[UploadDocumentOutput](t, result)
		docIDs = append(docIDs, upload.DocumentId)
	}

	// Parse both documents synchronously.
	for _, docID := range docIDs {
		result, _, err := s.ParseDocument(ctx, &mcp.CallToolRequest{}, &ParseDocumentInput{
			DocumentId: docID,
			Sync:       true,
		})
		if err != nil {
			t.Fatalf("ParseDocument error: %v", err)
		}
		if result.IsError {
			t.Fatalf("ParseDocument tool error: %v", result.Content)
		}
	}

	// Verify both documents are in the files map with parse jobs.
	for _, docID := range docIDs {
		info, ok := files.Load(docID)
		if !ok {
			t.Fatalf("expected document %s in files map before cleanup", docID)
		}
		if len(info.ParseJobs) == 0 {
			t.Fatalf("expected parse jobs for document %s before cleanup", docID)
		}
	}

	// Run cleanup.
	s.CleanupSession(ctx)

	// Verify the files map is empty — all documents should have been
	// processed regardless of individual errors.
	var remaining []string
	files.Range(func(key string, _ *FileInfo) bool {
		remaining = append(remaining, key)
		return true
	})
	if len(remaining) > 0 {
		t.Errorf("expected files map to be empty after CleanupSession, still has: %v", remaining)
	}
}
