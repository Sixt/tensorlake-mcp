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
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/sixt/tensorlake-go"
	"github.com/sixt/tensorlake-mcp/internal/mimetype"
)

type server struct {
	tl *tensorlake.Client
}

func newServer() *server {
	return &server{tl: tensorlake.NewClient(tensorlake.WithBaseURL(tlAPIBaseURL), tensorlake.WithAPIKey(tlAPIKey))}
}

func (s *server) ListDocuments(ctx context.Context, req *mcp.CallToolRequest, _ any) (*mcp.CallToolResult, any, error) {
	documents := make([]*FileInfo, 0)
	files.Range(func(key string, value *FileInfo) bool {
		documents = append(documents, value)
		return true
	})
	return newToolResultJSON(documents)
}

type UploadDocumentInput struct {
	URL string `json:"url"`
}

type UploadDocumentOutput struct {
	DocumentId       string              `json:"document_id"`
	DocumentName     string              `json:"document_name"`
	DocumentMimeType tensorlake.MimeType `json:"document_mime_type"`
	DocumentSize     int64               `json:"document_size"`
	ChecksumSHA256   string              `json:"checksum_sha256,omitempty"`
	CreatedAt        string              `json:"created_at"`
}

// UploadDocument uploads a document from a URL, local path, or data URI to Tensorlake and returns the document ID and creation time.
// Returns the document Id and creation time.
func (s *server) UploadDocument(ctx context.Context, req *mcp.CallToolRequest, in *UploadDocumentInput) (*mcp.CallToolResult, any, error) {

	// Check input URL is either:
	// 1. a URL
	// 2. a data URI
	// 3. a local file path
	// If it is not one of these, return an error.
	if !strings.HasPrefix(in.URL, "http") &&
		!strings.HasPrefix(in.URL, "data:") &&
		!strings.HasPrefix(in.URL, "file:") {
		return newToolResultError(fmt.Errorf("invalid URL: %s", in.URL))
	}

	if strings.HasPrefix(in.URL, "http") {
		return s.uploadDocumentFromURL(ctx, req, in)
	}

	if strings.HasPrefix(in.URL, "data:") {
		return s.uploadDocumentFromDataURI(ctx, req, in)
	}

	if strings.HasPrefix(in.URL, "file:") {
		return s.uploadDocumentFromLocalPath(ctx, req, in)
	}

	return newToolResultError(fmt.Errorf("invalid URL: %s", in.URL))
}

func (s *server) uploadDocumentFromURL(ctx context.Context, _ *mcp.CallToolRequest, in *UploadDocumentInput) (*mcp.CallToolResult, any, error) {
	// Download the document and pipe it to the upload document API.
	//
	// Note that this is actually a pipe operation because the downloadFile
	// function returns a ReadCloser.
	fileBody, fileName, err := downloadFile(ctx, in.URL, "") // TODO: auth token?
	if err != nil {
		return newToolResultError(fmt.Errorf("failed to download document: %w", err))
	}
	defer fileBody.Close()

	// Upload the document.
	r, err := s.tl.UploadFile(ctx, &tensorlake.UploadFileRequest{
		FileBytes: fileBody,
		FileName:  fileName,
	})
	if err != nil {
		return newToolResultError(fmt.Errorf("failed to upload document: %w", err))
	}

	m, err := s.tl.GetFileMetadata(ctx, r.FileId)
	if err != nil {
		return newToolResultError(fmt.Errorf("failed to get document metadata: %w", err))
	}

	files.Store(r.FileId, &FileInfo{
		FileId:         r.FileId,
		FileName:       m.FileName,
		MimeType:       m.MimeType,
		FileSize:       m.FileSize,
		ChecksumSHA256: m.ChecksumSHA256,
		CreatedAt:      r.CreatedAt.Format(time.RFC3339),
	})
	return newToolResultJSON(&UploadDocumentOutput{
		DocumentId:       r.FileId,
		DocumentName:     m.FileName,
		DocumentMimeType: m.MimeType,
		DocumentSize:     m.FileSize,
		ChecksumSHA256:   m.ChecksumSHA256,
		CreatedAt:        r.CreatedAt.Format(time.RFC3339),
	})
}

// downloadFile downloads a file from a URL with optional authorization.
// Returns the response body, filename (with extension if needed), and any error.
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

	// Try to determine filename from Content-Disposition header or URL
	fileName := filepath.Base(url)
	if cd := resp.Header.Get("Content-Disposition"); cd != "" {
		if strings.HasPrefix(cd, "attachment; filename=") {
			fileName = strings.Trim(strings.TrimPrefix(cd, "attachment; filename="), "\"")
		}
	}

	// If filename doesn't have an extension, try to add one
	if filepath.Ext(fileName) == "" {
		detectedExt, err := mimetype.DetectExtensionFromContentType(resp)
		if err != nil {
			return nil, "", fmt.Errorf("failed to detect extension: %w", err)
		}

		// Add extension if we detected one
		if detectedExt != "" {
			fileName = fileName + detectedExt
		}
	}

	return resp.Body, fileName, nil
}

func (s *server) uploadDocumentFromDataURI(ctx context.Context, _ *mcp.CallToolRequest, in *UploadDocumentInput) (*mcp.CallToolResult, any, error) {
	// If Data URI, convert it to a base64 encoded string and upload it.
	// Convert Data URI to base64 encoded string.
	base64String := strings.TrimPrefix(in.URL, "data:")
	base64String = strings.TrimPrefix(base64String, ";base64,")
	base64String = strings.TrimPrefix(base64String, "base64,")
	base64String = strings.TrimSpace(base64String)
	fileBody := io.NopCloser(strings.NewReader(base64String))

	// Detect mimetype
	peekBuffer := make([]byte, 512)
	n, err := fileBody.Read(peekBuffer)
	if err != nil && err != io.EOF {
		return newToolResultError(fmt.Errorf("failed to peek file content: %w", err))
	}
	// reset the file body
	_, extension := mimetype.DetectFromContent(peekBuffer[:n])
	fileName := uuid.New().String() + extension
	fileBody = io.NopCloser(strings.NewReader(base64String))

	slog.Info("uploading document from Data URI", "document_name", fileName, "document_size", len(base64String))

	// Upload the file
	r, err := s.tl.UploadFile(ctx, &tensorlake.UploadFileRequest{
		FileBytes: fileBody,
		FileName:  fileName,
	})
	if err != nil {
		return newToolResultError(fmt.Errorf("failed to upload document: %w", err))
	}

	slog.Info("document uplaod completed", "document_id", r.FileId)

	m, err := s.tl.GetFileMetadata(ctx, r.FileId)
	if err != nil {
		return newToolResultError(fmt.Errorf("failed to get document metadata: %w", err))
	}
	files.Store(r.FileId, &FileInfo{
		FileId:         r.FileId,
		FileName:       m.FileName,
		MimeType:       m.MimeType,
		FileSize:       m.FileSize,
		ChecksumSHA256: m.ChecksumSHA256,
		CreatedAt:      r.CreatedAt.Format(time.RFC3339),
	})

	return newToolResultJSON(&UploadDocumentOutput{
		DocumentId:       r.FileId,
		DocumentName:     m.FileName,
		DocumentMimeType: m.MimeType,
		DocumentSize:     m.FileSize,
		ChecksumSHA256:   m.ChecksumSHA256,
		CreatedAt:        r.CreatedAt.Format(time.RFC3339),
	})
}

func (s *server) uploadDocumentFromLocalPath(ctx context.Context, _ *mcp.CallToolRequest, in *UploadDocumentInput) (*mcp.CallToolResult, any, error) {
	// Open the document and pipe it to the upload document API.
	localPath := strings.TrimPrefix(in.URL, "file://")
	file, err := os.Open(localPath)
	if err != nil {
		return newToolResultError(fmt.Errorf("failed to open document: %w", err))
	}
	defer file.Close()

	// Upload the document.
	r, err := s.tl.UploadFile(ctx, &tensorlake.UploadFileRequest{
		FileBytes: file,
		FileName:  filepath.Base(in.URL),
	})
	if err != nil {
		return newToolResultError(fmt.Errorf("failed to upload document: %w", err))
	}

	slog.Info("document uplaod completed", "document_id", r.FileId)

	m, err := s.tl.GetFileMetadata(ctx, r.FileId)
	if err != nil {
		return newToolResultError(fmt.Errorf("failed to get document metadata: %w", err))
	}

	files.Store(r.FileId, &FileInfo{
		FileId:         r.FileId,
		FileName:       m.FileName,
		MimeType:       m.MimeType,
		FileSize:       m.FileSize,
		ChecksumSHA256: m.ChecksumSHA256,
		CreatedAt:      r.CreatedAt.Format(time.RFC3339),
	})
	return newToolResultJSON(&UploadDocumentOutput{
		DocumentId:       r.FileId,
		DocumentName:     m.FileName,
		DocumentMimeType: m.MimeType,
		DocumentSize:     m.FileSize,
		ChecksumSHA256:   m.ChecksumSHA256,
		CreatedAt:        r.CreatedAt.Format(time.RFC3339),
	})
}

type DeleteDocumentInput struct {
	DocumentId string `json:"document_id"`
}

// DeleteFile deletes a file from Tensorlake.
func (s *server) DeleteDocument(ctx context.Context, _ *mcp.CallToolRequest, in *DeleteDocumentInput) (*mcp.CallToolResult, any, error) {
	err := s.tl.DeleteFile(ctx, in.DocumentId)
	if err != nil {
		return newToolResultError(fmt.Errorf("failed to delete document: %w", err))
	}

	// Delete all relevant parse jobs.
	if info, ok := files.Load(in.DocumentId); ok {
		for _, parseJob := range info.ParseJobs {
			err := s.tl.DeleteParseJob(ctx, parseJob.ParseId)
			if err != nil {
				slog.Error("failed to delete parse job", "parse_id", parseJob.ParseId, "error", err)
			}
		}
	}

	files.Delete(in.DocumentId)
	return newToolResultJSON(fmt.Sprintf("Document (%s) deleted successfully", in.DocumentId))
}

// ParseDocumentInput represents the input for the parse_document tool.
type ParseDocumentInput struct {
	DocumentId string `json:"document_id,omitempty"`
	ParseId    string `json:"parse_id,omitempty"`
	Sync       bool   `json:"sync,omitempty"`
}

// ParseDocumentOutput represents the output from the extract_text tool.
type ParseDocumentOutput struct {
	DocumentId string                 `json:"document_id"`
	ParseID    string                 `json:"parse_id"`
	Status     tensorlake.ParseStatus `json:"status"`
	Message    string                 `json:"message"`
	Result     string                 `json:"result,omitempty"`
	CreatedAt  string                 `json:"created_at"` // RFC3339 timestamp
}

// ParseDocument handles the parse_document tool call.
func (s *server) ParseDocument(ctx context.Context, req *mcp.CallToolRequest, in *ParseDocumentInput) (*mcp.CallToolResult, any, error) {
	// Fast path: if parse ID is provided, check the status and return the results.
	if in.ParseId != "" {
		return s.fetchParseResult(ctx, req, in.DocumentId, in.ParseId, in.Sync)
	}

	// If both document ID and parse ID are empty, throw an error.
	if in.DocumentId == "" {
		return newToolResultError(errors.New("either 'document_id' or 'parse_id' must be provided"))
	}
	s.sendProgress(ctx, req, 0, 4, "Starting parse job...")

	// Start parse job.
	pr, err := s.tl.ParseDocument(ctx, &tensorlake.ParseDocumentRequest{
		FileSource: tensorlake.FileSource{FileId: in.DocumentId},
		ParsingOptions: &tensorlake.ParsingOptions{
			TableOutputMode:    tensorlake.TableOutputModeMarkdown,
			TableParsingFormat: tensorlake.TableParsingFormatVLM,
			// Do not chunk the document so that we can parse the whole document at once.
			ChunkingStrategy: tensorlake.ChunkingStrategyNone,
		},
	})
	if err != nil {
		return newToolResultError(fmt.Errorf("failed to start parse job: %w", err))
	}

	// If sync is true, poll for completion with exponential backoff
	if in.Sync {
		return s.fetchParseResult(ctx, req, in.DocumentId, pr.ParseId, true)
	}

	return newToolResultJSON(&ParseDocumentOutput{
		DocumentId: in.DocumentId,
		ParseID:    pr.ParseId,
		Status:     tensorlake.ParseStatusPending,
		Message:    fmt.Sprintf("Parse job started (ID: %s)", pr.ParseId),
		CreatedAt:  time.Now().Format(time.RFC3339),
	})
}

// fetchParseResult retrieves and formats the parse result for a given parse Id.
func (s *server) fetchParseResult(ctx context.Context, req *mcp.CallToolRequest, documentId, parseId string, sync bool) (*mcp.CallToolResult, any, error) {
	// Fast path: If parse ID is in the parses map, return the results.
	if pr, ok := files.Load(documentId); ok {
		if len(pr.ParseJobs) > 0 {
			return newToolResultJSON(&ParseDocumentOutput{
				DocumentId: documentId,
				ParseID:    parseId,
				Status:     tensorlake.ParseStatusSuccessful,
				Result:     pr.ParseJobs[0].Chunks[0].Content,
				Message:    fmt.Sprintf("Parse job done (ID: %s)", parseId),
				CreatedAt:  pr.CreatedAt,
			})
		}
	}

	// Slow path: Poll for the parse result.
	r, err := s.tl.GetParseResult(ctx, parseId, tensorlake.WithSSE(sync), tensorlake.WithOnUpdate(func(name tensorlake.ParseEventName, result *tensorlake.ParseResult) {
		switch name {
		case tensorlake.SSEEventParseQueued:
			s.sendProgress(ctx, req, 1, 4, "Parse job queued")
		case tensorlake.SSEEventParseUpdate:
			s.sendProgress(ctx, req, 2, 4, "Parse job updated")
		case tensorlake.SSEEventParseDone:
			s.sendProgress(ctx, req, 3, 4, "Parse job completed")
		case tensorlake.SSEEventParseFailed:
			s.sendProgress(ctx, req, 3, 4, "Parse job failed")
		}
	}))
	if err != nil {
		return newToolResultError(fmt.Errorf("failed to get parse result: %w", err))
	}

	slog.Info("parse result fetched", "parse_id", r.ParseId, "status", r.Status, "results", r)
	o := ParseDocumentOutput{
		ParseID:   r.ParseId,
		Status:    r.Status,
		Message:   fmt.Sprintf("Parse job done (ID: %s)", r.ParseId),
		CreatedAt: r.CreatedAt,
	}
	// TODO: allow structured data output.
	if len(r.Chunks) > 0 {
		o.Result = r.Chunks[0].Content
	}

	// Store the parse result in the parses map.
	info, ok := files.Load(documentId)
	if !ok {
		files.Store(documentId, &FileInfo{
			FileId:         documentId,
			FileName:       info.FileName,
			MimeType:       info.MimeType,
			FileSize:       info.FileSize,
			ChecksumSHA256: info.ChecksumSHA256,
			CreatedAt:      info.CreatedAt,
			ParseJobs:      []*tensorlake.ParseResult{r},
		})
	} else {
		info.ParseJobs = append(info.ParseJobs, r)
		files.Store(documentId, info)
	}
	return newToolResultJSON(&o)
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
	}) // Ignore error for non-critical notifications
}

func newToolResultJSON[T any](data T) (*mcp.CallToolResult, any, error) {
	b, err := json.Marshal(data)
	if err != nil {
		return newToolResultError(fmt.Errorf("unable to marshal JSON: %w", err))
	}
	return &mcp.CallToolResult{
		Content: []mcp.Content{
			&mcp.TextContent{
				Text: string(b),
			},
		},
	}, nil, nil
}

func newToolResultError(err error) (*mcp.CallToolResult, any, error) {
	return &mcp.CallToolResult{
		IsError: true,
		Content: []mcp.Content{
			&mcp.TextContent{
				Text: err.Error(),
			},
		},
	}, nil, nil
}

func (s *server) CleanupSession(ctx context.Context) {
	files.Range(func(key string, value *FileInfo) bool {
		err := s.tl.DeleteFile(ctx, key)
		if err != nil {
			slog.Error("failed to delete document", "document_id", key, "error", err)
			return false
		}
		slog.Info("document deleted", "document_id", key)

		for _, parseJob := range value.ParseJobs {
			err := s.tl.DeleteParseJob(ctx, parseJob.ParseId)
			if err != nil {
				slog.Error("failed to delete parse job", "parse_id", parseJob.ParseId, "error", err)
				return false
			}
			slog.Info("parse job deleted", "parse_id", parseJob.ParseId)
		}

		return true
	})
}
