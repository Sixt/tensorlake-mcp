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
	"bytes"
	"context"
	"encoding/base64"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/google/uuid"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/sixt/tensorlake-mcp/internal/mimetype"
)

type UploadInput struct {
	Source      string `json:"source"`
	Destination string `json:"destination,omitempty"`
}

type UploadOutput struct {
	Path string `json:"path"`
	Size int64  `json:"size"`
}

func (s *server) Upload(ctx context.Context, req *mcp.CallToolRequest, in *UploadInput) (*mcp.CallToolResult, any, error) {
	sandboxID, err := s.ensureSandbox(ctx)
	if err != nil {
		return newToolResultError(fmt.Errorf("failed to ensure sandbox: %w", err))
	}

	var reader io.Reader
	var fileName string
	var size int64

	switch {
	case strings.HasPrefix(in.Source, "http://") || strings.HasPrefix(in.Source, "https://"):
		body, name, err := downloadFile(ctx, in.Source, "")
		if err != nil {
			return newToolResultError(fmt.Errorf("failed to download: %w", err))
		}
		defer body.Close()
		// Buffer to get size.
		data, err := io.ReadAll(body)
		if err != nil {
			return newToolResultError(fmt.Errorf("failed to read download: %w", err))
		}
		reader = bytes.NewReader(data)
		fileName = name
		size = int64(len(data))

	case strings.HasPrefix(in.Source, "data:"):
		raw, ext, err := parseDataURI(in.Source)
		if err != nil {
			return newToolResultError(fmt.Errorf("failed to parse data URI: %w", err))
		}
		fileName = uuid.New().String() + ext
		reader = bytes.NewReader(raw)
		size = int64(len(raw))

	case strings.HasPrefix(in.Source, "file://"):
		localPath := strings.TrimPrefix(in.Source, "file://")
		f, err := os.Open(localPath)
		if err != nil {
			return newToolResultError(fmt.Errorf("failed to open local file: %w", err))
		}
		defer f.Close()
		data, err := io.ReadAll(f)
		if err != nil {
			return newToolResultError(fmt.Errorf("failed to read local file: %w", err))
		}
		reader = bytes.NewReader(data)
		fileName = filepath.Base(localPath)
		size = int64(len(data))

	default:
		return newToolResultError(fmt.Errorf("invalid source: must start with http://, https://, data:, or file://"))
	}

	dest := in.Destination
	if dest == "" {
		dest = "/data/" + fileName
	}

	if err := s.tl.WriteSandboxFile(ctx, sandboxID, dest, reader); err != nil {
		return newToolResultError(fmt.Errorf("failed to write to sandbox: %w", err))
	}

	return newToolResultJSON(&UploadOutput{Path: dest, Size: size})
}

// parseDataURI parses a data URI and returns the decoded bytes and a file extension.
// Supports: data:text/plain,hello  data:application/pdf;base64,iVBOR...  data:raw content
func parseDataURI(uri string) ([]byte, string, error) {
	rest := strings.TrimPrefix(uri, "data:")

	// Check for standard data URI format: [mediatype][;base64],data
	if meta, payload, ok := strings.Cut(rest, ","); ok {

		isBase64 := strings.HasSuffix(meta, ";base64")
		mediaType := strings.TrimSuffix(meta, ";base64")

		ext := mimetype.ExtensionFromMediaType(mediaType)

		if isBase64 {
			// Try standard, then raw (no padding), then URL-safe variants.
			decoders := []*base64.Encoding{
				base64.StdEncoding,
				base64.RawStdEncoding,
				base64.URLEncoding,
				base64.RawURLEncoding,
			}
			for _, dec := range decoders {
				decoded, err := dec.DecodeString(payload)
				if err == nil {
					return decoded, ext, nil
				}
			}
			return nil, "", fmt.Errorf("invalid base64 data in data URI")
		}

		return []byte(payload), ext, nil
	}

	// No comma — treat entire rest as raw text content.
	return []byte(rest), ".txt", nil
}
