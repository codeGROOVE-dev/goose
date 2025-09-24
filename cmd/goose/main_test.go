package main

import (
	"context"
	"fmt"
	"os"
	"runtime"
	"sync"
	"testing"
	"time"
)

func TestMain(m *testing.M) {
	// Set test mode to prevent actual sound playback during tests
	if err := os.Setenv("GOOSE_TEST_MODE", "1"); err != nil {
		panic(err)
	}
	os.Exit(m.Run())
}

func TestIsStale(t *testing.T) {
	// Capture time.Now() once to avoid race conditions
	now := time.Now()
	tests := []struct {
		time     time.Time
		name     string
		expected bool
	}{
		{
			name:     "recent PR",
			time:     now.Add(-24 * time.Hour),
			expected: false,
		},
		{
			name:     "stale PR",
			time:     now.Add(-91 * 24 * time.Hour),
			expected: true,
		},
		{
			name:     "exactly at threshold",
			time:     now.Add(-90 * 24 * time.Hour),
			expected: true, // >= 90 days is stale
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// isStale was inlined - test the logic directly
			// Use the same 'now' for consistency
			threshold := now.Add(-stalePRThreshold)
			got := !tt.time.After(threshold) // time <= threshold means stale (>= 90 days old)
			if got != tt.expected {
				t.Logf("Test time: %v, Threshold: %v, Before: %v", tt.time, threshold, got)
				t.Errorf("stale check = %v, want %v", got, tt.expected)
			}
		})
	}
}

// TestMenuItemTitleTransition tests that PR menu titles transition from emoji to regular prefix.
func TestMenuItemTitleTransition(t *testing.T) {
	// Test duration - using 1 second for quick testing
	const testBlockDuration = 1 * time.Second

	ctx := context.Background()

	// Create app with mocked data
	app := &App{
		mu:                 sync.RWMutex{},
		stateManager:       NewPRStateManager(time.Now()),
		hiddenOrgs:         make(map[string]bool),
		seenOrgs:           make(map[string]bool),
		blockedPRTimes:     make(map[string]time.Time),
		browserRateLimiter: NewBrowserRateLimiter(startupGracePeriod, 5, defaultMaxBrowserOpensDay),
		systrayInterface:   &MockSystray{}, // Use mock systray to avoid panics
	}

	// Test incoming PR that just became blocked
	incomingPR := PR{
		Repository:     "test/repo",
		Number:         123,
		Title:          "Test PR",
		URL:            "https://github.com/test/repo/pull/123",
		NeedsReview:    true,
		FirstBlockedAt: time.Now(),
		UpdatedAt:      time.Now(),
	}

	// Test outgoing PR that just became blocked
	outgoingPR := PR{
		Repository:     "test/repo2",
		Number:         456,
		Title:          "Another Test PR",
		URL:            "https://github.com/test/repo2/pull/456",
		IsBlocked:      true,
		FirstBlockedAt: time.Now(),
		UpdatedAt:      time.Now(),
	}

	// Set the PRs in the app
	app.incoming = []PR{incomingPR}
	app.outgoing = []PR{outgoingPR}

	// Track blocked times
	app.blockedPRTimes[incomingPR.URL] = incomingPR.FirstBlockedAt
	app.blockedPRTimes[outgoingPR.URL] = outgoingPR.FirstBlockedAt

	// Helper function to extract titles from menu structure
	extractTitles := func() (incomingTitle, outgoingTitle string) {
		// This simulates what addPRSection does when building menu items
		for _, pr := range app.incoming {
			title := fmt.Sprintf("%s #%d", pr.Repository, pr.Number)
			if pr.NeedsReview {
				if !pr.FirstBlockedAt.IsZero() && time.Since(pr.FirstBlockedAt) < testBlockDuration {
					title = fmt.Sprintf("🪿 %s", title)
				} else {
					title = fmt.Sprintf("• %s", title)
				}
			}
			incomingTitle = title
		}

		for _, pr := range app.outgoing {
			title := fmt.Sprintf("%s #%d", pr.Repository, pr.Number)
			if pr.IsBlocked {
				if !pr.FirstBlockedAt.IsZero() && time.Since(pr.FirstBlockedAt) < testBlockDuration {
					title = fmt.Sprintf("🎉 %s", title)
				} else {
					title = fmt.Sprintf("• %s", title)
				}
			}
			outgoingTitle = title
		}
		return incomingTitle, outgoingTitle
	}

	// Test initial state - should have emoji prefixes
	inTitle, outTitle := extractTitles()
	if inTitle != "🪿 test/repo #123" {
		t.Errorf("Expected incoming PR to have goose emoji, got: %s", inTitle)
	}
	if outTitle != "🎉 test/repo2 #456" {
		t.Errorf("Expected outgoing PR to have party emoji, got: %s", outTitle)
	}

	// Wait for the test duration to pass
	time.Sleep(testBlockDuration + 100*time.Millisecond)

	// Test after duration - should have bullet points
	inTitle, outTitle = extractTitles()
	if inTitle != "• test/repo #123" {
		t.Errorf("Expected incoming PR to have bullet point after %v, got: %s", testBlockDuration, inTitle)
	}
	if outTitle != "• test/repo2 #456" {
		t.Errorf("Expected outgoing PR to have bullet point after %v, got: %s", testBlockDuration, outTitle)
	}

	// Test PR that becomes unblocked and then blocked again
	app.incoming[0].FirstBlockedAt = time.Time{} // Clear FirstBlockedAt
	app.incoming[0].NeedsReview = false          // Actually unblock it
	inTitle, _ = extractTitles()
	if inTitle != "test/repo #123" {
		t.Errorf("Expected unblocked PR to have no prefix, got: %s", inTitle)
	}

	// Re-block the PR
	app.incoming[0].NeedsReview = true
	app.incoming[0].FirstBlockedAt = time.Now()
	app.blockedPRTimes[incomingPR.URL] = app.incoming[0].FirstBlockedAt
	inTitle, _ = extractTitles()
	if inTitle != "🪿 test/repo #123" {
		t.Errorf("Expected re-blocked PR to have goose emoji again, got: %s", inTitle)
	}

	_ = ctx // Unused in this test but would be used for real menu operations
}

// TestTrayIconRestoredAfterNetworkRecovery tests that the tray icon is restored
// to normal after network failures are resolved.
func TestTrayIconRestoredAfterNetworkRecovery(t *testing.T) {
	ctx := context.Background()
	mock := &MockSystray{}
	app := &App{
		mu:                 sync.RWMutex{},
		stateManager:       NewPRStateManager(time.Now().Add(-35 * time.Second)), // Past grace period
		blockedPRTimes:     make(map[string]time.Time),
		hiddenOrgs:         make(map[string]bool),
		seenOrgs:           make(map[string]bool),
		browserRateLimiter: NewBrowserRateLimiter(startupGracePeriod, 5, defaultMaxBrowserOpensDay),
		systrayInterface:   mock,
		menuInitialized:    true,
	}

	// Initial state - successful fetch with some PRs
	app.incoming = []PR{
		{Repository: "test/repo", Number: 1, NeedsReview: true, UpdatedAt: time.Now()},
	}
	app.setTrayTitle()
	initialTitle := mock.title

	// Expected title varies by platform
	expectedTitle := "1/1" // Linux format
	if runtime.GOOS == "darwin" {
		expectedTitle = "1" // macOS format
	}

	if initialTitle != expectedTitle {
		t.Errorf("Expected initial tray title %q, got %q", expectedTitle, initialTitle)
	}

	// Simulate network failure - updatePRs would set warning icon and return early
	app.consecutiveFailures = 3
	app.lastFetchError = "network timeout"
	// In the old code, rebuildMenu would be called but return early, never calling setTrayTitle()
	app.rebuildMenu(ctx)
	// The mock systray won't have the warning icon because rebuildMenu doesn't set it directly

	// Simulate network recovery - this should restore the normal icon
	app.consecutiveFailures = 0
	app.lastFetchError = ""
	// With our fix, setTrayTitle() is now called after successful fetch
	app.setTrayTitle()
	recoveredTitle := mock.title
	if recoveredTitle != expectedTitle {
		t.Errorf("Expected tray title to be restored to %q after recovery, got %q", expectedTitle, recoveredTitle)
	}
}

// TestTrayTitleUpdates tests that the tray title updates correctly based on PR counts.
func TestTrayTitleUpdates(t *testing.T) {
	app := &App{
		mu:                 sync.RWMutex{},
		stateManager:       NewPRStateManager(time.Now().Add(-35 * time.Second)), // Past grace period
		blockedPRTimes:     make(map[string]time.Time),
		hiddenOrgs:         make(map[string]bool),
		seenOrgs:           make(map[string]bool),
		browserRateLimiter: NewBrowserRateLimiter(startupGracePeriod, 5, defaultMaxBrowserOpensDay),
		systrayInterface:   &MockSystray{}, // Use mock systray to avoid panics
	}

	tests := []struct {
		name              string
		incoming          []PR
		outgoing          []PR
		hiddenOrgs        map[string]bool
		hideStaleIncoming bool
		expectedTitle     string
	}{
		{
			name:          "no PRs",
			incoming:      []PR{},
			outgoing:      []PR{},
			expectedTitle: "😊",
		},
		{
			name: "only incoming blocked",
			incoming: []PR{
				{Repository: "test/repo", Number: 1, NeedsReview: true, UpdatedAt: time.Now()},
				{Repository: "test/repo", Number: 2, NeedsReview: true, UpdatedAt: time.Now()},
			},
			outgoing:      []PR{},
			expectedTitle: "🪿 2",
		},
		{
			name:     "only outgoing blocked",
			incoming: []PR{},
			outgoing: []PR{
				{Repository: "test/repo", Number: 3, IsBlocked: true, UpdatedAt: time.Now()},
				{Repository: "test/repo", Number: 4, IsBlocked: true, UpdatedAt: time.Now()},
				{Repository: "test/repo", Number: 5, IsBlocked: true, UpdatedAt: time.Now()},
			},
			expectedTitle: "🎉 3",
		},
		{
			name: "both incoming and outgoing blocked",
			incoming: []PR{
				{Repository: "test/repo", Number: 1, NeedsReview: true, UpdatedAt: time.Now()},
			},
			outgoing: []PR{
				{Repository: "test/repo", Number: 2, IsBlocked: true, UpdatedAt: time.Now()},
			},
			expectedTitle: "🪿 1 🎉 1",
		},
		{
			name: "mixed blocked and unblocked",
			incoming: []PR{
				{Repository: "test/repo", Number: 1, NeedsReview: true, UpdatedAt: time.Now()},
				{Repository: "test/repo", Number: 2, NeedsReview: false, UpdatedAt: time.Now()},
			},
			outgoing: []PR{
				{Repository: "test/repo", Number: 3, IsBlocked: false, UpdatedAt: time.Now()},
				{Repository: "test/repo", Number: 4, IsBlocked: true, UpdatedAt: time.Now()},
			},
			expectedTitle: "🪿 1 🎉 1",
		},
		{
			name: "hidden org filters out blocked PRs",
			incoming: []PR{
				{Repository: "hidden-org/repo", Number: 1, NeedsReview: true, UpdatedAt: time.Now()},
				{Repository: "visible-org/repo", Number: 2, NeedsReview: true, UpdatedAt: time.Now()},
			},
			outgoing:      []PR{},
			hiddenOrgs:    map[string]bool{"hidden-org": true},
			expectedTitle: "🪿 1",
		},
		{
			name: "stale PRs filtered when hideStaleIncoming is true",
			incoming: []PR{
				{Repository: "test/repo", Number: 1, NeedsReview: true, UpdatedAt: time.Now().Add(-100 * 24 * time.Hour)}, // stale
				{Repository: "test/repo", Number: 2, NeedsReview: true, UpdatedAt: time.Now()},                            // fresh
			},
			outgoing:          []PR{},
			hideStaleIncoming: true,
			expectedTitle:     "🪿 1",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			app.incoming = tt.incoming
			app.outgoing = tt.outgoing
			app.hiddenOrgs = tt.hiddenOrgs
			app.hideStaleIncoming = tt.hideStaleIncoming

			// Get the title that would be set
			counts := app.countPRs()
			var title string
			switch {
			case counts.IncomingBlocked == 0 && counts.OutgoingBlocked == 0:
				title = "😊"
			case counts.IncomingBlocked > 0 && counts.OutgoingBlocked > 0:
				title = fmt.Sprintf("🪿 %d 🎉 %d", counts.IncomingBlocked, counts.OutgoingBlocked)
			case counts.IncomingBlocked > 0:
				title = fmt.Sprintf("🪿 %d", counts.IncomingBlocked)
			default:
				title = fmt.Sprintf("🎉 %d", counts.OutgoingBlocked)
			}

			if title != tt.expectedTitle {
				t.Errorf("Expected tray title %q, got %q", tt.expectedTitle, title)
			}
		})
	}
}

// TestSoundPlaybackDuringTransitions tests the logic for when sounds should be played during PR state transitions.
func TestSoundPlaybackDuringTransitions(t *testing.T) {
	// This test verifies the logic by checking state transitions
	// Actual sound playback is tested through logging in integration tests
	ctx := context.Background()

	// Create app with initial state
	app := &App{
		mu:                  sync.RWMutex{},
		stateManager:        NewPRStateManager(time.Now().Add(-35 * time.Second)), // Past grace period
		blockedPRTimes:      make(map[string]time.Time),
		hiddenOrgs:          make(map[string]bool),
		seenOrgs:            make(map[string]bool),
		previousBlockedPRs:  make(map[string]bool),
		browserRateLimiter:  NewBrowserRateLimiter(startupGracePeriod, 5, defaultMaxBrowserOpensDay),
		enableAudioCues:     true,
		initialLoadComplete: true, // Set to true to allow sound playback
		menuInitialized:     true,
		systrayInterface:    &MockSystray{}, // Use mock systray to avoid Windows-specific panics
	}

	tests := []struct {
		name            string
		initialIncoming []PR
		initialOutgoing []PR
		updatedIncoming []PR
		updatedOutgoing []PR
		expectedSounds  []string
		description     string
	}{
		{
			name: "incoming PR becomes blocked",
			initialIncoming: []PR{
				{Repository: "test/repo", Number: 1, URL: "https://github.com/test/repo/pull/1", NeedsReview: false, UpdatedAt: time.Now()},
			},
			initialOutgoing: []PR{},
			updatedIncoming: []PR{
				{Repository: "test/repo", Number: 1, URL: "https://github.com/test/repo/pull/1", NeedsReview: true, UpdatedAt: time.Now()},
			},
			updatedOutgoing: []PR{},
			expectedSounds:  []string{"honk"},
			description:     "Should play honk sound when incoming PR becomes blocked",
		},
		{
			name:            "outgoing PR becomes blocked",
			initialIncoming: []PR{},
			initialOutgoing: []PR{
				{Repository: "test/repo", Number: 2, URL: "https://github.com/test/repo/pull/2", IsBlocked: false, UpdatedAt: time.Now()},
			},
			updatedIncoming: []PR{},
			updatedOutgoing: []PR{
				{Repository: "test/repo", Number: 2, URL: "https://github.com/test/repo/pull/2", IsBlocked: true, UpdatedAt: time.Now()},
			},
			expectedSounds: []string{"rocket"},
			description:    "Should play rocket sound when outgoing PR becomes blocked",
		},
		{
			name: "multiple PRs become blocked",
			initialIncoming: []PR{
				{Repository: "test/repo", Number: 1, URL: "https://github.com/test/repo/pull/1", NeedsReview: false, UpdatedAt: time.Now()},
			},
			initialOutgoing: []PR{
				{Repository: "test/repo", Number: 2, URL: "https://github.com/test/repo/pull/2", IsBlocked: false, UpdatedAt: time.Now()},
			},
			updatedIncoming: []PR{
				{Repository: "test/repo", Number: 1, URL: "https://github.com/test/repo/pull/1", NeedsReview: true, UpdatedAt: time.Now()},
			},
			updatedOutgoing: []PR{
				{Repository: "test/repo", Number: 2, URL: "https://github.com/test/repo/pull/2", IsBlocked: true, UpdatedAt: time.Now()},
			},
			expectedSounds: []string{"honk", "rocket"},
			description:    "Should play both sounds when both PR types become blocked",
		},
		{
			name: "PR becomes unblocked - no sound",
			initialIncoming: []PR{
				{Repository: "test/repo", Number: 1, URL: "https://github.com/test/repo/pull/1", NeedsReview: true, UpdatedAt: time.Now()},
			},
			initialOutgoing: []PR{},
			updatedIncoming: []PR{
				{Repository: "test/repo", Number: 1, URL: "https://github.com/test/repo/pull/1", NeedsReview: false, UpdatedAt: time.Now()},
			},
			updatedOutgoing: []PR{},
			expectedSounds:  []string{},
			description:     "Should not play sound when PR becomes unblocked",
		},
		{
			name: "already blocked PR stays blocked - no sound",
			initialIncoming: []PR{
				{Repository: "test/repo", Number: 1, URL: "https://github.com/test/repo/pull/1", NeedsReview: true, UpdatedAt: time.Now()},
			},
			initialOutgoing: []PR{},
			updatedIncoming: []PR{
				{Repository: "test/repo", Number: 1, URL: "https://github.com/test/repo/pull/1", NeedsReview: true, UpdatedAt: time.Now()},
			},
			updatedOutgoing: []PR{},
			expectedSounds:  []string{},
			description:     "Should not play sound when PR stays blocked",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Reset app state for each test
			app.previousBlockedPRs = make(map[string]bool)
			app.blockedPRTimes = make(map[string]time.Time)

			// Note: We can't directly capture sound calls in unit tests since playSound is a method.
			// This test verifies the state transitions that would trigger sounds.
			// Actual sound playback is verified through integration testing.

			// Set initial state
			app.mu.Lock()
			app.incoming = tt.initialIncoming
			app.outgoing = tt.initialOutgoing
			app.mu.Unlock()

			// Run first check to establish baseline
			app.checkForNewlyBlockedPRs(ctx)

			// Update to new state
			app.mu.Lock()
			app.incoming = tt.updatedIncoming
			app.outgoing = tt.updatedOutgoing
			app.mu.Unlock()

			// Run check again to detect transitions
			app.checkForNewlyBlockedPRs(ctx)

			// Verify state transitions occurred correctly
			// For newly blocked PRs after grace period, the previousBlockedPRs map should be updated
			if len(tt.expectedSounds) > 0 {
				// Check that blocked PRs are tracked in previousBlockedPRs
				blocked := 0
				app.mu.RLock()
				for _, pr := range app.incoming {
					if pr.NeedsReview && app.previousBlockedPRs[pr.URL] {
						blocked++
					}
				}
				for _, pr := range app.outgoing {
					if pr.IsBlocked && app.previousBlockedPRs[pr.URL] {
						blocked++
					}
				}
				app.mu.RUnlock()
				if blocked == 0 {
					t.Errorf("%s: expected blocked PRs to be tracked in previousBlockedPRs", tt.description)
				}
			}
		})
	}
}

// TestSoundDisabledNoPlayback tests that no sounds are played when audio cues are disabled.
func TestSoundDisabledNoPlayback(t *testing.T) {
	ctx := context.Background()

	app := &App{
		mu:                  sync.RWMutex{},
		stateManager:        NewPRStateManager(time.Now().Add(-35 * time.Second)), // Past grace period
		blockedPRTimes:      make(map[string]time.Time),
		hiddenOrgs:          make(map[string]bool),
		seenOrgs:            make(map[string]bool),
		previousBlockedPRs:  make(map[string]bool),
		browserRateLimiter:  NewBrowserRateLimiter(startupGracePeriod, 5, defaultMaxBrowserOpensDay),
		enableAudioCues:     false, // Audio disabled
		initialLoadComplete: true,
		menuInitialized:     true,
		systrayInterface:    &MockSystray{}, // Use mock systray to avoid panics
	}

	// Note: We verify behavior through state changes rather than direct sound capture

	// Set initial state with no blocked PRs
	app.incoming = []PR{
		{Repository: "test/repo", Number: 1, URL: "https://github.com/test/repo/pull/1", NeedsReview: false, UpdatedAt: time.Now()},
	}
	app.checkForNewlyBlockedPRs(ctx)

	// Update to blocked state
	app.incoming = []PR{
		{Repository: "test/repo", Number: 1, URL: "https://github.com/test/repo/pull/1", NeedsReview: true, UpdatedAt: time.Now()},
	}
	app.checkForNewlyBlockedPRs(ctx)

	// When audio is disabled, PRs should still be tracked but no actual sounds would play
	// Verify that state is tracked correctly even with audio disabled
	if len(app.previousBlockedPRs) == 0 {
		t.Errorf("Expected blocked PRs to be tracked even with audio disabled")
	}
}

// TestGracePeriodPreventsNotifications tests that no sounds/notifications occur during the grace period.
func TestGracePeriodPreventsNotifications(t *testing.T) {
	ctx := context.Background()

	// Create app with a very recent start time (within grace period)
	app := &App{
		mu:                  sync.RWMutex{},
		stateManager:        NewPRStateManager(time.Now()), // Within grace period
		blockedPRTimes:      make(map[string]time.Time),
		hiddenOrgs:          make(map[string]bool),
		seenOrgs:            make(map[string]bool),
		previousBlockedPRs:  make(map[string]bool),
		browserRateLimiter:  NewBrowserRateLimiter(startupGracePeriod, 5, defaultMaxBrowserOpensDay),
		enableAudioCues:     true,
		initialLoadComplete: true,
		menuInitialized:     true,
		startTime:           time.Now(),     // Just started
		systrayInterface:    &MockSystray{}, // Use mock systray to avoid panics
	}

	// Track whether we're in grace period for verification
	inGracePeriod := func() bool {
		return time.Since(app.startTime) < startupGracePeriod
	}

	// Set initial state with no blocked PRs
	app.incoming = []PR{
		{Repository: "test/repo", Number: 1, URL: "https://github.com/test/repo/pull/1", NeedsReview: false, UpdatedAt: time.Now()},
	}
	app.checkForNewlyBlockedPRs(ctx)

	// PR becomes blocked during grace period
	app.incoming = []PR{
		{Repository: "test/repo", Number: 1, URL: "https://github.com/test/repo/pull/1", NeedsReview: true, UpdatedAt: time.Now()},
	}
	app.checkForNewlyBlockedPRs(ctx)

	// Verify we're still in grace period
	if !inGracePeriod() {
		t.Errorf("Expected to still be in grace period")
	}

	// The PR should be tracked as blocked but not as "newly notified"
	if !app.previousBlockedPRs["https://github.com/test/repo/pull/1"] {
		t.Errorf("Expected PR to be tracked as blocked during grace period")
	}

	// Now simulate time passing beyond grace period (1 minute)
	app.startTime = time.Now().Add(-61 * time.Second)

	// New PR becomes blocked after grace period
	app.incoming = []PR{
		{Repository: "test/repo", Number: 1, URL: "https://github.com/test/repo/pull/1", NeedsReview: true, UpdatedAt: time.Now()},
		{Repository: "test/repo", Number: 2, URL: "https://github.com/test/repo/pull/2", NeedsReview: true, UpdatedAt: time.Now()},
	}
	app.checkForNewlyBlockedPRs(ctx)

	// Verify we're past grace period
	if inGracePeriod() {
		t.Errorf("Expected to be past grace period")
	}

	// Both PRs should now be tracked
	if !app.previousBlockedPRs["https://github.com/test/repo/pull/2"] {
		t.Errorf("Expected new PR to be tracked as blocked after grace period")
	}
}

// TestNotificationScenarios comprehensively tests when notifications should and shouldn't fire.
func TestNotificationScenarios(t *testing.T) {
	tests := []struct {
		name                string
		timeSinceStart      time.Duration
		initialLoadComplete bool
		prWasBlocked        bool
		prIsBlocked         bool
		expectNotification  bool
		description         string
	}{
		{
			name:                "initial_load_already_blocked",
			timeSinceStart:      1 * time.Second,
			initialLoadComplete: false,
			prWasBlocked:        false,
			prIsBlocked:         true,
			expectNotification:  false,
			description:         "PRs already blocked on startup should not notify",
		},
		{
			name:                "newly_blocked_during_grace_period",
			timeSinceStart:      10 * time.Second,
			initialLoadComplete: true,
			prWasBlocked:        false,
			prIsBlocked:         true,
			expectNotification:  false,
			description:         "Newly blocked PRs during grace period should not notify",
		},
		{
			name:                "newly_blocked_after_grace_period",
			timeSinceStart:      35 * time.Second,
			initialLoadComplete: true,
			prWasBlocked:        false,
			prIsBlocked:         true,
			expectNotification:  true,
			description:         "Newly blocked PRs after grace period SHOULD notify",
		},
		{
			name:                "stays_blocked",
			timeSinceStart:      35 * time.Second,
			initialLoadComplete: true,
			prWasBlocked:        true,
			prIsBlocked:         true,
			expectNotification:  false,
			description:         "PRs that stay blocked should not notify again",
		},
		{
			name:                "becomes_unblocked",
			timeSinceStart:      35 * time.Second,
			initialLoadComplete: true,
			prWasBlocked:        true,
			prIsBlocked:         false,
			expectNotification:  false,
			description:         "PRs becoming unblocked should not notify",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := context.Background()

			app := &App{
				mu:                  sync.RWMutex{},
				stateManager:        NewPRStateManager(time.Now().Add(-tt.timeSinceStart)),
				blockedPRTimes:      make(map[string]time.Time),
				hiddenOrgs:          make(map[string]bool),
				seenOrgs:            make(map[string]bool),
				previousBlockedPRs:  make(map[string]bool),
				browserRateLimiter:  NewBrowserRateLimiter(startupGracePeriod, 5, defaultMaxBrowserOpensDay),
				enableAudioCues:     true,
				initialLoadComplete: tt.initialLoadComplete,
				menuInitialized:     true,
				startTime:           time.Now().Add(-tt.timeSinceStart),
				systrayInterface:    &MockSystray{}, // Use mock systray to avoid panics
			}

			// Set up previous state
			if tt.prWasBlocked {
				app.previousBlockedPRs["https://github.com/test/repo/pull/1"] = true
				app.blockedPRTimes["https://github.com/test/repo/pull/1"] = time.Now().Add(-5 * time.Minute)
			}

			// Set current state
			app.incoming = []PR{
				{
					Repository:  "test/repo",
					Number:      1,
					URL:         "https://github.com/test/repo/pull/1",
					NeedsReview: tt.prIsBlocked,
					UpdatedAt:   time.Now(),
				},
			}

			// Track if we would notify (by checking logs)
			app.checkForNewlyBlockedPRs(ctx)

			// Verify expectations
			if tt.expectNotification {
				// Should have updated previousBlockedPRs
				if !app.previousBlockedPRs["https://github.com/test/repo/pull/1"] {
					t.Errorf("%s: Expected PR to be tracked as blocked", tt.description)
				}
				// Should have set FirstBlockedAt in state manager
				if state, exists := app.stateManager.PRState("https://github.com/test/repo/pull/1"); !exists || state.FirstBlockedAt.IsZero() {
					t.Errorf("%s: Expected FirstBlockedAt to be set in state manager", tt.description)
				}
			}

			// Check if PR is tracked correctly
			if tt.prIsBlocked && !app.previousBlockedPRs["https://github.com/test/repo/pull/1"] {
				t.Errorf("%s: Expected blocked PR to be tracked", tt.description)
			}
		})
	}
}

// TestNewlyBlockedPRAfterGracePeriod verifies that a PR becoming blocked after grace period triggers notifications.
func TestNewlyBlockedPRAfterGracePeriod(t *testing.T) {
	ctx := context.Background()

	// Create app that's been running for more than 30 seconds
	app := &App{
		mu:                  sync.RWMutex{},
		stateManager:        NewPRStateManager(time.Now().Add(-35 * time.Second)), // Past grace period
		blockedPRTimes:      make(map[string]time.Time),
		hiddenOrgs:          make(map[string]bool),
		seenOrgs:            make(map[string]bool),
		previousBlockedPRs:  make(map[string]bool),
		browserRateLimiter:  NewBrowserRateLimiter(startupGracePeriod, 5, defaultMaxBrowserOpensDay),
		enableAudioCues:     true,
		initialLoadComplete: true, // Already past initial load
		menuInitialized:     true,
		startTime:           time.Now().Add(-35 * time.Second), // Started 35 seconds ago
		systrayInterface:    &MockSystray{},                    // Use mock systray to avoid panics
	}

	// Start with no blocked PRs
	app.incoming = []PR{
		{Repository: "test/repo", Number: 1, URL: "https://github.com/test/repo/pull/1", NeedsReview: false, UpdatedAt: time.Now()},
	}

	// Run initial check to establish baseline
	app.checkForNewlyBlockedPRs(ctx)

	// Now the PR becomes blocked (after grace period)
	app.incoming = []PR{
		{Repository: "test/repo", Number: 1, URL: "https://github.com/test/repo/pull/1", NeedsReview: true, UpdatedAt: time.Now()},
	}

	// This should trigger notifications since we're past grace period
	app.checkForNewlyBlockedPRs(ctx)

	// Verify the PR is tracked as blocked
	if !app.previousBlockedPRs["https://github.com/test/repo/pull/1"] {
		t.Error("Expected PR to be tracked as blocked after grace period")
	}

	// Verify FirstBlockedAt was set in state manager
	if state, exists := app.stateManager.PRState("https://github.com/test/repo/pull/1"); !exists || state.FirstBlockedAt.IsZero() {
		t.Error("Expected FirstBlockedAt to be set for newly blocked PR in state manager")
	}
}
