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

type UploadDocumentInput struct {
	URL string `json:"url"`
}

type UploadDocumentOutput struct {
	DocumentId string `json:"document_id"`
	CreatedAt  string `json:"created_at"`
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

	o := &UploadDocumentOutput{DocumentId: r.FileId, CreatedAt: r.CreatedAt.Format(time.RFC3339)}
	return newToolResultJSON(o)
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

	o := &UploadDocumentOutput{DocumentId: r.FileId, CreatedAt: r.CreatedAt.Format(time.RFC3339)}
	return newToolResultJSON(o)
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

	o := &UploadDocumentOutput{DocumentId: r.FileId, CreatedAt: r.CreatedAt.Format(time.RFC3339)}
	return newToolResultJSON(o)
}

type DeleteDocumentInput struct {
	DocumentId string `json:"document_id"`
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
