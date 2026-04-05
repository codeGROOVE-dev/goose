package main

import (
	"strings"
	"testing"
)

// URL validation tests are in pkg/safebrowse/safebrowse_test.go
// This file only contains tests for goose-specific security functions

func TestValidateGitHubUsername(t *testing.T) {
	tests := []struct {
		name     string
		username string
		wantErr  bool
	}{
		// Valid usernames
		{
			name:     "simple username",
			username: "user",
			wantErr:  false,
		},
		{
			name:     "username with hyphen",
			username: "user-name",
			wantErr:  false,
		},
		{
			name:     "username with numbers",
			username: "user123",
			wantErr:  false,
		},
		{
			name:     "single character",
			username: "a",
			wantErr:  false,
		},
		{
			name:     "max length username",
			username: strings.Repeat("a", 39),
			wantErr:  false,
		},

		// Invalid usernames
		{
			name:     "empty string",
			username: "",
			wantErr:  true,
		},
		{
			name:     "username starting with hyphen",
			username: "-user",
			wantErr:  true,
		},
		{
			name:     "username ending with hyphen",
			username: "user-",
			wantErr:  true,
		},
		{
			name:     "username with double hyphen",
			username: "user--name",
			wantErr:  false, // GitHub allows this
		},
		{
			name:     "username too long",
			username: strings.Repeat("a", 40),
			wantErr:  true,
		},
		{
			name:     "username with underscore",
			username: "user_name",
			wantErr:  true,
		},
		{
			name:     "username with dot",
			username: "user.name",
			wantErr:  true,
		},
		{
			name:     "username with space",
			username: "user name",
			wantErr:  true,
		},
		{
			name:     "username with special chars",
			username: "user@name",
			wantErr:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateGitHubUsername(tt.username)
			if (err != nil) != tt.wantErr {
				t.Errorf("validateGitHubUsername() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestValidateGitHubToken(t *testing.T) {
	tests := []struct {
		name    string
		token   string
		wantErr bool
	}{
		// Valid tokens
		{
			name:    "classic token (40 hex chars)",
			token:   "abcdef0123456789abcdef0123456789abcdef01",
			wantErr: false,
		},
		{
			name:    "personal access token (ghp_)",
			token:   "ghp_" + strings.Repeat("a", 36),
			wantErr: false,
		},
		{
			name:    "server token (ghs_)",
			token:   "ghs_" + strings.Repeat("A", 36),
			wantErr: false,
		},
		{
			name:    "refresh token (ghr_)",
			token:   "ghr_" + strings.Repeat("1", 36),
			wantErr: false,
		},
		{
			name:    "OAuth token (gho_)",
			token:   "gho_" + strings.Repeat("z", 36),
			wantErr: false,
		},
		{
			name:    "user-to-server token (ghu_)",
			token:   "ghu_" + strings.Repeat("Z", 36),
			wantErr: false,
		},
		{
			name:    "fine-grained PAT",
			token:   "github_pat_" + strings.Repeat("a", 22) + "_" + strings.Repeat("b", 59),
			wantErr: false,
		},

		// Invalid tokens
		{
			name:    "fine-grained PAT",
			token:   "github_pat_" + strings.Repeat("a", 82),
			wantErr: true,
		},
		{
			name:    "empty string",
			token:   "",
			wantErr: true,
		},
		{
			name:    "too short",
			token:   "short",
			wantErr: true,
		},
		{
			name:    "too long",
			token:   strings.Repeat("a", 300),
			wantErr: true,
		},
		{
			name:    "placeholder: your_token",
			token:   "your_token_here_aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
			wantErr: true,
		},
		{
			name:    "placeholder: xxx",
			token:   "xxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxx",
			wantErr: true,
		},
		{
			name:    "placeholder with dots",
			token:   "...aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
			wantErr: true,
		},
		{
			name:    "invalid format",
			token:   "invalid_format_token_here_aaaaaaaaaaaaaaaa",
			wantErr: true,
		},
		{
			name:    "classic token too short",
			token:   "abcdef0123456789abcdef0123456789abcdef0",
			wantErr: true,
		},
		{
			name:    "ghp_ token too short",
			token:   "ghp_" + strings.Repeat("a", 35),
			wantErr: true,
		},
		{
			name:    "fine-grained PAT too short",
			token:   "github_pat_" + strings.Repeat("a", 81),
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateGitHubToken(tt.token)
			if (err != nil) != tt.wantErr {
				t.Errorf("validateGitHubToken() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestSanitizeForLog(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		wantHide bool // true if sensitive data should be redacted
	}{
		{
			name:     "classic token redacted",
			input:    "token: abcdef0123456789abcdef0123456789abcdef01",
			wantHide: true,
		},
		{
			name:     "ghp_ token redacted",
			input:    "Authorization: ghp_" + strings.Repeat("a", 36),
			wantHide: true,
		},
		{
			name:     "fine-grained PAT redacted",
			input:    "token=github_pat_" + strings.Repeat("a", 22) + "_" + strings.Repeat("b", 59),
			wantHide: true,
		},
		{
			name:     "bearer token redacted",
			input:    "Bearer abc123xyz",
			wantHide: true,
		},
		{
			name:     "authorization header redacted",
			input:    "Authorization: Bearer token123",
			wantHide: true,
		},
		{
			name:     "normal text not redacted",
			input:    "This is just a normal log message",
			wantHide: false,
		},
		{
			name:     "URL not redacted",
			input:    "https://github.com/owner/repo/pull/123",
			wantHide: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := sanitizeForLog(tt.input)

			if tt.wantHide {
				// Should contain REDACTED marker
				if !strings.Contains(result, "[REDACTED") {
					t.Errorf("sanitizeForLog() = %v, should contain redaction marker", result)
				}
				// Should not contain original sensitive data patterns
				if strings.Contains(result, "ghp_") || strings.Contains(result, "github_pat_") {
					t.Errorf("sanitizeForLog() = %v, still contains sensitive pattern", result)
				}
			} else if result != tt.input {
				// Should be unchanged
				t.Errorf("sanitizeForLog() = %v, want %v", result, tt.input)
			}
		})
	}
}
