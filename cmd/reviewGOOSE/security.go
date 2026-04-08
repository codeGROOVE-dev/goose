package main

import (
	"errors"
	"fmt"
	"regexp"
	"strings"
)

const (
	// Security constants.
	minTokenLength   = 40   // GitHub tokens are at least 40 chars
	maxTokenLength   = 255  // Reasonable upper bound
	maxUsernameLen   = 39   // GitHub username max length
	maxURLLength     = 2048 // Maximum URL length
	minPrintableChar = 0x20 // Minimum printable character
	deleteChar       = 0x7F // Delete character
)

var (
	// githubUsernameRegex validates GitHub usernames.
	// GitHub usernames can only contain alphanumeric characters and hyphens,
	// cannot start or end with hyphen, and max 39 characters.
	githubUsernameRegex = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9-]{0,37}[a-zA-Z0-9]$|^[a-zA-Z0-9]$`)

	// githubTokenRegex validates GitHub token format.
	// Classic tokens: 40 hex chars.
	// New tokens: ghp_ (personal), ghs_ (server), ghr_ (refresh), gho_ (OAuth), ghu_ (user-to-server) followed by base62 chars.
	// Fine-grained tokens: github_pat_ followed by 22 base62 chars, underscore, and 59 base62 chars.
	githubTokenRegex = regexp.MustCompile(`^[a-f0-9]{40}$|^gh[psoru]_[A-Za-z0-9]{36,251}$|^github_pat_[a-zA-Z0-9]{22}_[a-zA-Z0-9]{59}$`)
)

// validateGitHubUsername validates a GitHub username.
func validateGitHubUsername(username string) error {
	if username == "" {
		return errors.New("username cannot be empty")
	}
	if len(username) > maxUsernameLen {
		return fmt.Errorf("username too long: %d > %d", len(username), maxUsernameLen)
	}
	if !githubUsernameRegex.MatchString(username) {
		return fmt.Errorf("invalid GitHub username format: %s", username)
	}
	return nil
}

// validateGitHubToken performs basic validation on a GitHub token.
func validateGitHubToken(token string) error {
	if token == "" {
		return errors.New("token cannot be empty")
	}

	tokenLen := len(token)
	if tokenLen < minTokenLength {
		return fmt.Errorf("token too short: %d < %d", tokenLen, minTokenLength)
	}
	if tokenLen > maxTokenLength {
		return fmt.Errorf("token too long: %d > %d", tokenLen, maxTokenLength)
	}

	// Check for common placeholder values
	if strings.Contains(strings.ToLower(token), "your_token") ||
		strings.Contains(strings.ToLower(token), "xxx") ||
		strings.Contains(token, "...") {
		return errors.New("token appears to be a placeholder")
	}

	if !githubTokenRegex.MatchString(token) {
		return errors.New("token does not match expected GitHub token format")
	}

	return nil
}

// sanitizeForLog removes sensitive information from strings before logging.
func sanitizeForLog(s string) string {
	// Redact tokens (both classic 40-char hex and new format)
	// Classic tokens
	s = regexp.MustCompile(`\b[a-f0-9]{40}\b`).ReplaceAllString(s, "[REDACTED-TOKEN]")
	// New format tokens (ghp_, ghs_, ghr_, gho_, ghu_)
	s = regexp.MustCompile(`\bgh[psoru]_[A-Za-z0-9]{36,251}\b`).ReplaceAllString(s, "[REDACTED-TOKEN]")
	// Fine-grained personal access tokens
	s = regexp.MustCompile(`\bgithub_pat_[a-zA-Z0-9]{22}_[a-zA-Z0-9]{59}\b`).ReplaceAllString(s, "[REDACTED-TOKEN]")
	// Bearer tokens in headers
	s = regexp.MustCompile(`Bearer [A-Za-z0-9_\-.]+`).ReplaceAllString(s, "Bearer [REDACTED]")
	// Authorization headers
	s = regexp.MustCompile(`Authorization: \S+`).ReplaceAllString(s, "Authorization: [REDACTED]")

	return s
}
