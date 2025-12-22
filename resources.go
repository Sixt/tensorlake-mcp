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
	"log/slog"
	"net/url"

	"github.com/go4org/hashtriemap"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/sixt/tensorlake-go"
)

type FileInfo struct {
	FileId         string                    `json:"file_id"`
	FileName       string                    `json:"file_name"`
	MimeType       tensorlake.MimeType       `json:"mime_type"`
	FileSize       int64                     `json:"file_size"`
	ChecksumSHA256 string                    `json:"checksum_sha256,omitempty"`
	CreatedAt      string                    `json:"created_at,omitempty"`
	Labels         map[string]string         `json:"labels,omitempty"`
	ParseJobs      []*tensorlake.ParseResult `json:"parse_jobs,omitempty"`
}

var (
	files hashtriemap.HashTrieMap[string, *FileInfo]
)

func (s *server) initializeDocumentResources(ctx context.Context) {
	// Iterate all parse jobs. This way we get all parsed results and their files if any.
	for parseJob, err := range s.tl.IterParseJobs(ctx, 100) {
		if err != nil {
			slog.Error("failed to iterate parse jobs", "error", err)
			break
		}

		// Correlate parse jobs and their documents.
		r, err := s.tl.GetParseResult(ctx, parseJob.ParseId, tensorlake.WithOptions(true))
		if err != nil {
			continue
		}
		if r.Options == nil {
			continue
		}

		fileId := r.Options.FileId
		if fileId == "" {
			continue
		}

		m, err := s.tl.GetFileMetadata(ctx, fileId)
		if err != nil {
			continue
		}

		finfo := &FileInfo{
			FileId:         fileId,
			FileName:       m.FileName,
			MimeType:       m.MimeType,
			FileSize:       m.FileSize,
			ChecksumSHA256: m.ChecksumSHA256,
			CreatedAt:      m.CreatedAt,
			Labels:         m.Labels,
			ParseJobs:      []*tensorlake.ParseResult{r},
		}

		info, ok := files.Load(fileId)
		if !ok {
			files.Store(fileId, finfo)
		} else {
			info.ParseJobs = append(info.ParseJobs, r)
			files.Store(fileId, info)
		}
	}
}

// DocumentResources handles resource requests for document metadata and parse results.
// The URI is of the form "tensorlake://documents".
func (s *server) DocumentResources(ctx context.Context, req *mcp.ReadResourceRequest) (*mcp.ReadResourceResult, error) {
	u, err := url.Parse(req.Params.URI)
	if err != nil {
		return nil, fmt.Errorf("invalid tensorlake resource URI: %s", req.Params.URI)
	}
	if u.Scheme != "tensorlake" {
		return nil, fmt.Errorf("invalid tensorlake resource URI scheme: %s", u.Scheme)
	}
	if u.Host != "documents" {
		return nil, fmt.Errorf("invalid tensorlake resource URI host: %s", u.Host)
	}

	// List all documents

	ff := make([]*FileInfo, 0)
	files.Range(func(key string, value *FileInfo) bool {
		ff = append(ff, value)
		return true
	})
	data, err := json.MarshalIndent(ff, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("failed to marshal documents: %w", err)
	}
	return &mcp.ReadResourceResult{
		Contents: []*mcp.ResourceContents{
			{
				URI:      req.Params.URI,
				MIMEType: "application/json",
				Text:     string(data),
			},
		},
	}, nil
}
