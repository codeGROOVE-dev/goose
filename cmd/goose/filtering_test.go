package main

import (
	"testing"
	"time"
)

// TestCountPRsWithHiddenOrgs tests that PRs from hidden orgs are not counted
func TestCountPRsWithHiddenOrgs(t *testing.T) {
	app := &App{
		incoming: []PR{
			{Repository: "org1/repo1", NeedsReview: true, UpdatedAt: time.Now()},
			{Repository: "org2/repo2", NeedsReview: true, UpdatedAt: time.Now()},
			{Repository: "org3/repo3", NeedsReview: true, UpdatedAt: time.Now()},
		},
		outgoing: []PR{
			{Repository: "org1/repo4", IsBlocked: true, UpdatedAt: time.Now()},
			{Repository: "org2/repo5", IsBlocked: true, UpdatedAt: time.Now()},
		},
		hiddenOrgs: map[string]bool{
			"org2": true, // Hide org2
		},
		hideStaleIncoming: false,
	}

	counts := app.countPRs()

	// Should only count PRs from org1 and org3, not org2
	if counts.IncomingTotal != 2 {
		t.Errorf("IncomingTotal = %d, want 2 (org2 should be hidden)", counts.IncomingTotal)
	}
	if counts.IncomingBlocked != 2 {
		t.Errorf("IncomingBlocked = %d, want 2 (org2 should be hidden)", counts.IncomingBlocked)
	}
	if counts.OutgoingTotal != 1 {
		t.Errorf("OutgoingTotal = %d, want 1 (org2 should be hidden)", counts.OutgoingTotal)
	}
	if counts.OutgoingBlocked != 1 {
		t.Errorf("OutgoingBlocked = %d, want 1 (org2 should be hidden)", counts.OutgoingBlocked)
	}
}

// TestCountPRsWithStalePRs tests that stale PRs are not counted when hideStaleIncoming is true
func TestCountPRsWithStalePRs(t *testing.T) {
	now := time.Now()
	staleTime := now.Add(-100 * 24 * time.Hour) // 100 days ago
	recentTime := now.Add(-1 * time.Hour)       // 1 hour ago

	app := &App{
		incoming: []PR{
			{Repository: "org1/repo1", NeedsReview: true, UpdatedAt: staleTime},
			{Repository: "org1/repo2", NeedsReview: true, UpdatedAt: recentTime},
			{Repository: "org2/repo3", NeedsReview: false, UpdatedAt: staleTime},
		},
		outgoing: []PR{
			{Repository: "org1/repo4", IsBlocked: true, UpdatedAt: staleTime},
			{Repository: "org1/repo5", IsBlocked: true, UpdatedAt: recentTime},
		},
		hiddenOrgs:        map[string]bool{},
		hideStaleIncoming: true, // Hide stale PRs
	}

	counts := app.countPRs()

	// Should only count recent PRs
	if counts.IncomingTotal != 1 {
		t.Errorf("IncomingTotal = %d, want 1 (stale PRs should be hidden)", counts.IncomingTotal)
	}
	if counts.IncomingBlocked != 1 {
		t.Errorf("IncomingBlocked = %d, want 1 (stale PRs should be hidden)", counts.IncomingBlocked)
	}
	if counts.OutgoingTotal != 1 {
		t.Errorf("OutgoingTotal = %d, want 1 (stale PRs should be hidden)", counts.OutgoingTotal)
	}
	if counts.OutgoingBlocked != 1 {
		t.Errorf("OutgoingBlocked = %d, want 1 (stale PRs should be hidden)", counts.OutgoingBlocked)
	}
}

// TestCountPRsWithBothFilters tests that both filters work together
func TestCountPRsWithBothFilters(t *testing.T) {
	now := time.Now()
	staleTime := now.Add(-100 * 24 * time.Hour)
	recentTime := now.Add(-1 * time.Hour)

	app := &App{
		incoming: []PR{
			{Repository: "org1/repo1", NeedsReview: true, UpdatedAt: recentTime},  // Should be counted
			{Repository: "org2/repo2", NeedsReview: true, UpdatedAt: recentTime},  // Hidden org
			{Repository: "org3/repo3", NeedsReview: true, UpdatedAt: staleTime},   // Stale
			{Repository: "org1/repo4", NeedsReview: false, UpdatedAt: recentTime}, // Not blocked
		},
		outgoing: []PR{
			{Repository: "org1/repo5", IsBlocked: true, UpdatedAt: recentTime}, // Should be counted
			{Repository: "org2/repo6", IsBlocked: true, UpdatedAt: recentTime}, // Hidden org
			{Repository: "org3/repo7", IsBlocked: true, UpdatedAt: staleTime},  // Stale
		},
		hiddenOrgs: map[string]bool{
			"org2": true,
		},
		hideStaleIncoming: true,
	}

	counts := app.countPRs()

	// Should only count org1/repo1 (incoming) and org1/repo5 (outgoing)
	if counts.IncomingTotal != 2 {
		t.Errorf("IncomingTotal = %d, want 2", counts.IncomingTotal)
	}
	if counts.IncomingBlocked != 1 {
		t.Errorf("IncomingBlocked = %d, want 1", counts.IncomingBlocked)
	}
	if counts.OutgoingTotal != 1 {
		t.Errorf("OutgoingTotal = %d, want 1", counts.OutgoingTotal)
	}
	if counts.OutgoingBlocked != 1 {
		t.Errorf("OutgoingBlocked = %d, want 1", counts.OutgoingBlocked)
	}
}

// TestExtractOrgFromRepo tests the org extraction function
func TestExtractOrgFromRepo(t *testing.T) {
	tests := []struct {
		repo string
		name string
		want string
	}{
		{
			name: "standard repo path",
			repo: "microsoft/vscode",
			want: "microsoft",
		},
		{
			name: "single segment",
			repo: "justarepo",
			want: "justarepo",
		},
		{
			name: "empty string",
			repo: "",
			want: "",
		},
		{
			name: "nested path",
			repo: "org/repo/subpath",
			want: "org",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := extractOrgFromRepo(tt.repo); got != tt.want {
				t.Errorf("extractOrgFromRepo(%q) = %q, want %q", tt.repo, got, tt.want)
			}
		})
	}
}
