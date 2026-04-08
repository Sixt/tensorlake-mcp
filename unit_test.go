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
	"strings"
	"testing"
)

func TestTruncateOutput(t *testing.T) {
	// Short string: no truncation.
	short := "hello"
	if got := truncateOutput(short); got != short {
		t.Errorf("expected no truncation, got %q", got)
	}

	// Exactly at limit: no truncation.
	exact := strings.Repeat("x", maxOutputBytes)
	if got := truncateOutput(exact); got != exact {
		t.Error("expected no truncation at exact limit")
	}

	// Over limit: should truncate with head/tail and omitted count.
	over := strings.Repeat("a", maxOutputBytes+100)
	got := truncateOutput(over)
	if len(got) >= len(over) {
		t.Error("expected truncated output to be shorter")
	}
	if !strings.Contains(got, "bytes truncated") {
		t.Error("expected truncation marker")
	}
	// Head should start with 'a', tail should end with 'a'.
	if got[0] != 'a' || got[len(got)-1] != 'a' {
		t.Error("expected head and tail to be preserved")
	}
}

func TestIsBinary(t *testing.T) {
	tests := []struct {
		name   string
		data   []byte
		binary bool
	}{
		{"empty", []byte{}, false},
		{"text", []byte("hello world\n"), false},
		{"null byte", []byte("hel\x00lo"), true},
		{"pure binary", []byte{0, 1, 2, 3}, true},
		{"tabs and newlines", []byte("a\tb\nc\r\n"), false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := isBinary(tt.data); got != tt.binary {
				t.Errorf("isBinary(%q) = %v, want %v", tt.data, got, tt.binary)
			}
		})
	}
}

func TestShellQuote(t *testing.T) {
	tests := []struct {
		input   string
		include bool
		want    string
	}{
		{"hello", true, "'hello'"},
		{"it's", true, "'it'\\''s'"},
		{"hello", false, ""},
		{"", true, "''"},
		{"a b c", true, "'a b c'"},
		{"$(rm -rf /)", true, "'$(rm -rf /)'"},
		{"`cmd`", true, "'`cmd`'"},
	}
	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := shellQuote(tt.input, tt.include)
			if got != tt.want {
				t.Errorf("shellQuote(%q, %v) = %q, want %q", tt.input, tt.include, got, tt.want)
			}
		})
	}
}

func TestParseDataURI(t *testing.T) {
	tests := []struct {
		name    string
		uri     string
		wantErr bool
		wantExt string
		wantStr string // expected content as string (for text payloads)
	}{
		{
			name:    "plain text",
			uri:     "data:text/plain,hello world",
			wantExt: ".txt",
			wantStr: "hello world",
		},
		{
			name:    "base64 text",
			uri:     "data:text/plain;base64,aGVsbG8=",
			wantExt: ".txt",
			wantStr: "hello",
		},
		{
			name:    "json",
			uri:     "data:application/json,{\"key\":\"value\"}",
			wantExt: ".json",
			wantStr: `{"key":"value"}`,
		},
		{
			name:    "raw content no comma",
			uri:     "data:just raw text",
			wantExt: ".txt",
			wantStr: "just raw text",
		},
		{
			name:    "pdf base64",
			uri:     "data:application/pdf;base64,JVBERi0=",
			wantExt: ".pdf",
			wantStr: "%PDF-",
		},
		{
			name:    "invalid base64",
			uri:     "data:text/plain;base64,!!!invalid!!!",
			wantErr: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			data, ext, err := parseDataURI(tt.uri)
			if tt.wantErr {
				if err == nil {
					t.Fatal("expected error")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if ext != tt.wantExt {
				t.Errorf("extension = %q, want %q", ext, tt.wantExt)
			}
			if tt.wantStr != "" && string(data) != tt.wantStr {
				t.Errorf("content = %q, want %q", string(data), tt.wantStr)
			}
		})
	}
}
