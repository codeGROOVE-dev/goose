// Package main - ui.go handles system tray UI and menu management.
package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/url"
	"os/exec"
	"runtime"
	"sort"
	"strings"
	"time"

	"github.com/energye/systray" // needed for MenuItem type
)

// Ensure systray package is used.
var _ *systray.MenuItem = nil

// openURL safely opens a URL in the default browser after validation.
func openURL(ctx context.Context, rawURL string) error {
	// Parse and validate the URL
	u, err := url.Parse(rawURL)
	if err != nil {
		return fmt.Errorf("parse url: %w", err)
	}

	// Security validation: scheme, host whitelist, no userinfo
	if u.Scheme != "https" {
		return fmt.Errorf("invalid url scheme: %s (only https allowed)", u.Scheme)
	}

	allowedHosts := map[string]bool{
		"github.com":               true,
		"www.github.com":           true,
		"dash.ready-to-review.dev": true,
	}
	if !allowedHosts[u.Host] {
		return fmt.Errorf("invalid host: %s", u.Host)
	}

	if u.User != nil {
		return errors.New("URLs with user info are not allowed")
	}

	// Add goose=1 parameter to track source for GitHub and dash URLs
	if u.Host == "github.com" || u.Host == "www.github.com" || u.Host == "dash.ready-to-review.dev" {
		q := u.Query()
		q.Set("goose", "1")
		u.RawQuery = q.Encode()
		rawURL = u.String()
	}

	// Execute the open command based on OS
	// Use safer methods that don't invoke shell interpretation
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		// Use open command with explicit arguments to prevent injection
		slog.Debug("Executing command", "command", "/usr/bin/open", "url", rawURL)
		cmd = exec.CommandContext(ctx, "/usr/bin/open", "-u", rawURL)
	case "windows":
		// Use rundll32 to open URL safely without cmd shell
		slog.Debug("Executing command", "command", "rundll32.exe url.dll,FileProtocolHandler", "url", rawURL)
		cmd = exec.CommandContext(ctx, "rundll32.exe", "url.dll,FileProtocolHandler", rawURL)
	case "linux":
		// Use xdg-open with full path
		slog.Debug("Executing command", "command", "/usr/bin/xdg-open", "url", rawURL)
		cmd = exec.CommandContext(ctx, "/usr/bin/xdg-open", rawURL)
	default:
		// Try xdg-open for other Unix-like systems
		slog.Debug("Executing command", "command", "/usr/bin/xdg-open", "url", rawURL)
		cmd = exec.CommandContext(ctx, "/usr/bin/xdg-open", rawURL)
	}

	if err := cmd.Start(); err != nil {
		return fmt.Errorf("open url: %w", err)
	}

	// Don't wait for the command to finish - browser launch is fire-and-forget
	// The OS will handle the browser process lifecycle

	return nil
}

// PRCounts represents PR count information.
type PRCounts struct {
	IncomingTotal   int
	IncomingBlocked int
	OutgoingTotal   int
	OutgoingBlocked int
}

// countPRs counts the number of PRs that need review/are blocked.
func (app *App) countPRs() PRCounts {
	app.mu.RLock()
	defer app.mu.RUnlock()

	var incomingCount, incomingBlocked, outgoingCount, outgoingBlocked int

	// Pre-calculate stale threshold to avoid repeated time calculations
	now := time.Now()
	staleThreshold := now.Add(-stalePRThreshold)

	slog.Info("[MENU] Counting incoming PRs", "total_incoming", len(app.incoming))
	filteredIncoming := 0
	for i := range app.incoming {
		// Check if org is hidden
		org := extractOrgFromRepo(app.incoming[i].Repository)
		if org != "" && app.hiddenOrgs[org] {
			filteredIncoming++
			continue
		}

		if !app.hideStaleIncoming || app.incoming[i].UpdatedAt.After(staleThreshold) {
			incomingCount++
			if app.incoming[i].NeedsReview {
				incomingBlocked++
			}
		} else {
			filteredIncoming++
		}
	}
	slog.Info("[MENU] Incoming PR count results",
		"total_before_filter", len(app.incoming),
		"total_after_filter", incomingCount,
		"filtered_out", filteredIncoming,
		"blocked_count", incomingBlocked)

	slog.Info("[MENU] Counting outgoing PRs",
		"total_outgoing", len(app.outgoing),
		"hideStaleIncoming", app.hideStaleIncoming,
		"staleThreshold", staleThreshold.Format(time.RFC3339))
	for i := range app.outgoing {
		pr := app.outgoing[i]
		// Check if org is hidden
		org := extractOrgFromRepo(pr.Repository)
		hiddenByOrg := org != "" && app.hiddenOrgs[org]
		isStale := pr.UpdatedAt.Before(staleThreshold)

		// Log every PR with its filtering status
		slog.Info("[MENU] Processing outgoing PR",
			"repo", pr.Repository,
			"number", pr.Number,
			"org", org,
			"hidden_org", hiddenByOrg,
			"updated_at", pr.UpdatedAt.Format(time.RFC3339),
			"is_stale", isStale,
			"hideStale_enabled", app.hideStaleIncoming,
			"blocked", pr.IsBlocked,
			"url", pr.URL)

		if hiddenByOrg {
			slog.Info("[MENU] ❌ Filtering out outgoing PR (hidden org)",
				"repo", pr.Repository, "number", pr.Number,
				"org", org, "url", pr.URL)
			continue
		}

		if !app.hideStaleIncoming || !isStale {
			outgoingCount++
			if pr.IsBlocked {
				outgoingBlocked++
			}
			slog.Info("[MENU] ✅ Including outgoing PR in count",
				"repo", pr.Repository, "number", pr.Number,
				"blocked", pr.IsBlocked, "url", pr.URL)
		} else {
			slog.Info("[MENU] ❌ Filtering out outgoing PR (stale)",
				"repo", pr.Repository, "number", pr.Number,
				"updated_at", pr.UpdatedAt.Format(time.RFC3339),
				"url", pr.URL)
		}
	}
	slog.Info("[MENU] Outgoing PR count results",
		"total_before_filter", len(app.outgoing),
		"total_after_filter", outgoingCount,
		"blocked_count", outgoingBlocked)
	return PRCounts{
		IncomingTotal:   incomingCount,
		IncomingBlocked: incomingBlocked,
		OutgoingTotal:   outgoingCount,
		OutgoingBlocked: outgoingBlocked,
	}
}

// setTrayTitle updates the system tray title and icon based on PR counts.
func (app *App) setTrayTitle() {
	counts := app.countPRs()

	// Set title and icon based on PR state
	var title string
	var iconType IconType

	// On Linux, always show counts if there are any PRs
	// This helps since not all desktop environments show the text
	if runtime.GOOS == "linux" && (counts.IncomingTotal > 0 || counts.OutgoingTotal > 0) {
		// Show blocked/total format for better visibility
		if counts.IncomingBlocked > 0 && counts.OutgoingBlocked > 0 {
			title = fmt.Sprintf("%d/%d • %d/%d", counts.IncomingBlocked, counts.IncomingTotal, counts.OutgoingBlocked, counts.OutgoingTotal)
			iconType = IconBoth
		} else if counts.IncomingBlocked > 0 {
			title = fmt.Sprintf("%d/%d", counts.IncomingBlocked, counts.IncomingTotal)
			iconType = IconGoose
		} else if counts.OutgoingBlocked > 0 {
			title = fmt.Sprintf("%d/%d", counts.OutgoingBlocked, counts.OutgoingTotal)
			iconType = IconPopper
		} else {
			// No blocked PRs but there are PRs
			title = fmt.Sprintf("0/%d", counts.IncomingTotal+counts.OutgoingTotal)
			iconType = IconSmiling
		}
	} else {
		// Original behavior for other platforms
		switch {
		case counts.IncomingBlocked == 0 && counts.OutgoingBlocked == 0:
			title = ""
			iconType = IconSmiling
		case counts.IncomingBlocked > 0 && counts.OutgoingBlocked > 0:
			title = fmt.Sprintf("%d / %d", counts.IncomingBlocked, counts.OutgoingBlocked)
			iconType = IconBoth
		case counts.IncomingBlocked > 0:
			title = fmt.Sprintf("%d", counts.IncomingBlocked)
			iconType = IconGoose
		default:
			title = fmt.Sprintf("%d", counts.OutgoingBlocked)
			iconType = IconPopper
		}
	}

	// Log title change with detailed counts
	slog.Info("[TRAY] Setting title and icon",
		"os", runtime.GOOS,
		"title", title,
		"icon", iconType,
		"incoming_total", counts.IncomingTotal,
		"incoming_blocked", counts.IncomingBlocked,
		"outgoing_total", counts.OutgoingTotal,
		"outgoing_blocked", counts.OutgoingBlocked)
	app.systrayInterface.SetTitle(title)
	app.setTrayIcon(iconType)
}

// addPRSection adds a section of PRs to the menu.
func (app *App) addPRSection(ctx context.Context, prs []PR, sectionTitle string, blockedCount int) {
	slog.Debug("[MENU] addPRSection called",
		"section", sectionTitle,
		"pr_count", len(prs),
		"blocked_count", blockedCount)
	if len(prs) == 0 {
		slog.Debug("[MENU] No PRs to add in section", "section", sectionTitle)
		return
	}

	// Add header
	headerText := fmt.Sprintf("%s — %d blocked on you", sectionTitle, blockedCount)
	// Create section header
	header := app.systrayInterface.AddMenuItem(headerText, "")
	header.Disable()

	// Sort PRs with blocked ones first - inline for simplicity
	sortedPRs := make([]PR, len(prs))
	copy(sortedPRs, prs)
	sort.SliceStable(sortedPRs, func(i, j int) bool {
		// First priority: blocked status
		if sortedPRs[i].NeedsReview != sortedPRs[j].NeedsReview {
			return sortedPRs[i].NeedsReview // true (blocked) comes before false
		}
		if sortedPRs[i].IsBlocked != sortedPRs[j].IsBlocked {
			return sortedPRs[i].IsBlocked // true (blocked) comes before false
		}
		// Second priority: more recent PRs first
		return sortedPRs[i].UpdatedAt.After(sortedPRs[j].UpdatedAt)
	})

	// Get hidden orgs with proper locking
	app.mu.RLock()
	hiddenOrgs := make(map[string]bool)
	for org, hidden := range app.hiddenOrgs {
		hiddenOrgs[org] = hidden
	}
	hideStale := app.hideStaleIncoming
	app.mu.RUnlock()

	// Add PR items in sorted order
	itemsAdded := 0
	for prIndex := range sortedPRs {
		// Apply filters
		// Skip PRs from hidden orgs
		org := extractOrgFromRepo(sortedPRs[prIndex].Repository)
		if org != "" && hiddenOrgs[org] {
			slog.Debug("[MENU] Skipping PR in addPRSection (hidden org)",
				"section", sectionTitle,
				"repo", sortedPRs[prIndex].Repository,
				"number", sortedPRs[prIndex].Number,
				"org", org)
			continue
		}

		// Skip stale PRs if configured
		if hideStale && sortedPRs[prIndex].UpdatedAt.Before(time.Now().Add(-stalePRThreshold)) {
			slog.Debug("[MENU] Skipping PR in addPRSection (stale)",
				"section", sectionTitle,
				"repo", sortedPRs[prIndex].Repository,
				"number", sortedPRs[prIndex].Number,
				"updated_at", sortedPRs[prIndex].UpdatedAt)
			continue
		}

		title := fmt.Sprintf("%s #%d", sortedPRs[prIndex].Repository, sortedPRs[prIndex].Number)

		// Add action code if present
		if sortedPRs[prIndex].ActionKind != "" {
			title = fmt.Sprintf("%s — %s", title, sortedPRs[prIndex].ActionKind)
		}

		// Add bullet point or emoji based on PR status
		if sortedPRs[prIndex].NeedsReview || sortedPRs[prIndex].IsBlocked {
			// Get the blocked time from state manager
			prState, hasState := app.stateManager.PRState(sortedPRs[prIndex].URL)

			// Show emoji for PRs blocked within the last 5 minutes (but only for real state transitions, not initial discoveries)
			if hasState && !prState.FirstBlockedAt.IsZero() && time.Since(prState.FirstBlockedAt) < blockedPRIconDuration && !prState.IsInitialDiscovery {
				timeSinceBlocked := time.Since(prState.FirstBlockedAt)
				// Use party popper for outgoing PRs, goose for incoming PRs
				if sectionTitle == "Outgoing" {
					title = fmt.Sprintf("🎉 %s", title)
					slog.Info("[MENU] Adding party popper to outgoing PR",
						"repo", sortedPRs[prIndex].Repository,
						"number", sortedPRs[prIndex].Number,
						"url", sortedPRs[prIndex].URL,
						"firstBlockedAt", prState.FirstBlockedAt.Format(time.RFC3339),
						"blocked_ago", timeSinceBlocked.Round(time.Second),
						"remaining", (blockedPRIconDuration - timeSinceBlocked).Round(time.Second))
				} else {
					title = fmt.Sprintf("🪿 %s", title)
					slog.Debug("[MENU] Adding goose to incoming PR",
						"url", sortedPRs[prIndex].URL,
						"blocked_ago", timeSinceBlocked,
						"remaining", blockedPRIconDuration-timeSinceBlocked)
				}
			} else {
				// Use block/square icon for blocked PRs
				title = fmt.Sprintf("■ %s", title)
				// Log when we transition from emoji to block icon
				if hasState && !prState.FirstBlockedAt.IsZero() {
					timeSinceBlocked := time.Since(prState.FirstBlockedAt)
					if sectionTitle == "Outgoing" {
						slog.Debug("[MENU] Removing party popper from outgoing PR",
							"url", sortedPRs[prIndex].URL,
							"blocked_ago", timeSinceBlocked,
							"duration", blockedPRIconDuration)
					} else {
						slog.Debug("[MENU] Removing goose from incoming PR",
							"url", sortedPRs[prIndex].URL,
							"blocked_ago", timeSinceBlocked,
							"duration", blockedPRIconDuration)
					}
				}
			}
		} else if sortedPRs[prIndex].ActionKind != "" {
			// PR has an action but isn't blocked - add bullet to indicate it could use input
			title = fmt.Sprintf("• %s", title)
		}
		// Format age inline for tooltip
		duration := time.Since(sortedPRs[prIndex].UpdatedAt)
		var age string
		switch {
		case duration < time.Hour:
			age = fmt.Sprintf("%dm", int(duration.Minutes()))
		case duration < 24*time.Hour:
			age = fmt.Sprintf("%dh", int(duration.Hours()))
		case duration < 30*24*time.Hour:
			age = fmt.Sprintf("%dd", int(duration.Hours()/24))
		case duration < 365*24*time.Hour:
			age = fmt.Sprintf("%dmo", int(duration.Hours()/(24*30)))
		default:
			age = sortedPRs[prIndex].UpdatedAt.Format("2006")
		}
		tooltip := fmt.Sprintf("%s (%s)", sortedPRs[prIndex].Title, age)
		// Add action reason for blocked PRs
		if (sortedPRs[prIndex].NeedsReview || sortedPRs[prIndex].IsBlocked) && sortedPRs[prIndex].ActionReason != "" {
			tooltip = fmt.Sprintf("%s - %s", tooltip, sortedPRs[prIndex].ActionReason)
		}

		// Create PR menu item
		itemsAdded++
		slog.Debug("[MENU] Adding PR to menu",
			"section", sectionTitle,
			"title", title,
			"repo", sortedPRs[prIndex].Repository,
			"number", sortedPRs[prIndex].Number,
			"url", sortedPRs[prIndex].URL,
			"blocked", sortedPRs[prIndex].NeedsReview || sortedPRs[prIndex].IsBlocked)
		item := app.systrayInterface.AddMenuItem(title, tooltip)

		// Capture URL to avoid loop variable capture bug
		prURL := sortedPRs[prIndex].URL
		item.Click(func() {
			if err := openURL(ctx, prURL); err != nil {
				slog.Error("failed to open url", "error", err)
			}
		})
	}
	slog.Info("[MENU] Added PR section",
		"section", sectionTitle,
		"items_added", itemsAdded,
		"filtered_out", len(sortedPRs)-itemsAdded)
}

// generateMenuTitles generates the list of menu item titles that would be shown
// without actually building the UI. Used for change detection.
func (app *App) generateMenuTitles() []string {
	var titles []string

	// Check for auth error first
	if app.authError != "" {
		titles = append(titles,
			"⚠️ Authentication Error",
			app.authError,
			"To fix this issue:",
			"1. Install GitHub CLI: brew install gh",
			"2. Run: gh auth login",
			"3. Or set GITHUB_TOKEN environment variable",
			"Quit")
		return titles
	}

	app.mu.RLock()
	incoming := make([]PR, len(app.incoming))
	copy(incoming, app.incoming)
	outgoing := make([]PR, len(app.outgoing))
	copy(outgoing, app.outgoing)
	hiddenOrgs := make(map[string]bool)
	for org, hidden := range app.hiddenOrgs {
		hiddenOrgs[org] = hidden
	}
	hideStale := app.hideStaleIncoming
	app.mu.RUnlock()

	// Add common menu items
	titles = append(titles, "Web Dashboard")

	// Generate PR section titles
	if len(incoming) == 0 && len(outgoing) == 0 {
		titles = append(titles, "No pull requests")
	} else {
		// Add incoming PR titles
		if len(incoming) > 0 {
			titles = append(titles, "📥 Incoming PRs")
			titles = append(titles, app.generatePRSectionTitles(incoming, "Incoming", hiddenOrgs, hideStale)...)
		}

		// Add outgoing PR titles
		if len(outgoing) > 0 {
			titles = append(titles, "📤 Outgoing PRs")
			titles = append(titles, app.generatePRSectionTitles(outgoing, "Outgoing", hiddenOrgs, hideStale)...)
		}
	}

	// Add settings menu items
	titles = append(titles,
		"⚙️ Settings",
		"Hide Stale Incoming PRs",
		"Honks enabled",
		"Auto-open in Browser",
		"Hidden Organizations",
		"Quit")

	return titles
}

// generatePRSectionTitles generates the titles for a specific PR section.
func (app *App) generatePRSectionTitles(prs []PR, sectionTitle string, hiddenOrgs map[string]bool, hideStale bool) []string {
	var titles []string

	// Sort PRs by UpdatedAt (most recent first)
	sortedPRs := make([]PR, len(prs))
	copy(sortedPRs, prs)
	sort.Slice(sortedPRs, func(i, j int) bool {
		return sortedPRs[i].UpdatedAt.After(sortedPRs[j].UpdatedAt)
	})

	for prIndex := range sortedPRs {
		// Apply filters (same logic as in addPRSection)
		org := extractOrgFromRepo(sortedPRs[prIndex].Repository)
		if org != "" && hiddenOrgs[org] {
			continue
		}

		if hideStale && sortedPRs[prIndex].UpdatedAt.Before(time.Now().Add(-stalePRThreshold)) {
			continue
		}

		title := fmt.Sprintf("%s #%d", sortedPRs[prIndex].Repository, sortedPRs[prIndex].Number)

		// Add action code if present
		if sortedPRs[prIndex].ActionKind != "" {
			title = fmt.Sprintf("%s — %s", title, sortedPRs[prIndex].ActionKind)
		}

		// Add bullet point or emoji for blocked PRs (same logic as in addPRSection)
		if sortedPRs[prIndex].NeedsReview || sortedPRs[prIndex].IsBlocked {
			prState, hasState := app.stateManager.PRState(sortedPRs[prIndex].URL)

			if hasState && !prState.FirstBlockedAt.IsZero() && time.Since(prState.FirstBlockedAt) < blockedPRIconDuration && !prState.IsInitialDiscovery {
				timeSinceBlocked := time.Since(prState.FirstBlockedAt)
				if sectionTitle == "Outgoing" {
					title = fmt.Sprintf("🎉 %s", title)
					slog.Info("[MENU] Adding party popper to outgoing PR in generateMenuTitles",
						"repo", sortedPRs[prIndex].Repository,
						"number", sortedPRs[prIndex].Number,
						"url", sortedPRs[prIndex].URL,
						"firstBlockedAt", prState.FirstBlockedAt.Format(time.RFC3339),
						"blocked_ago", timeSinceBlocked.Round(time.Second),
						"remaining", (blockedPRIconDuration - timeSinceBlocked).Round(time.Second))
				} else {
					title = fmt.Sprintf("🪿 %s", title)
					slog.Debug("[MENU] Adding goose to incoming PR in generateMenuTitles",
						"url", sortedPRs[prIndex].URL,
						"blocked_ago", timeSinceBlocked,
						"remaining", blockedPRIconDuration-timeSinceBlocked)
				}
			} else {
				// Use block/square icon for blocked PRs
				title = fmt.Sprintf("■ %s", title)
				// Log when we use block icon instead of emoji
				if hasState && !prState.FirstBlockedAt.IsZero() {
					timeSinceBlocked := time.Since(prState.FirstBlockedAt)
					if sectionTitle == "Outgoing" {
						slog.Debug("[MENU] Using block icon instead of party popper in generateMenuTitles",
							"url", sortedPRs[prIndex].URL,
							"blocked_ago", timeSinceBlocked.Round(time.Second),
							"icon_duration", blockedPRIconDuration)
					}
				} else if !hasState {
					slog.Debug("[MENU] No state found for blocked PR, using block icon",
						"url", sortedPRs[prIndex].URL,
						"repo", sortedPRs[prIndex].Repository,
						"number", sortedPRs[prIndex].Number)
				}
			}
		} else if sortedPRs[prIndex].ActionKind != "" {
			// PR has an action but isn't blocked - add bullet to indicate it could use input
			title = fmt.Sprintf("• %s", title)
		}

		titles = append(titles, title)
	}

	return titles
}

// rebuildMenu completely rebuilds the menu from scratch.
func (app *App) rebuildMenu(ctx context.Context) {
	// Prevent concurrent menu rebuilds
	app.menuMutex.Lock()
	defer app.menuMutex.Unlock()

	// Rebuild entire menu
	slog.Info("[MENU] Starting rebuildMenu", "os", runtime.GOOS)

	// Clear all existing menu items
	app.systrayInterface.ResetMenu()
	slog.Info("[MENU] Called ResetMenu")

	// On Linux, add a small delay to ensure DBus properly processes the reset
	// This helps prevent menu item duplication
	if runtime.GOOS == "linux" {
		time.Sleep(50 * time.Millisecond)
	}

	// Check for errors (auth or connection failures)
	app.mu.RLock()
	authError := app.authError
	failureCount := app.consecutiveFailures
	lastFetchError := app.lastFetchError
	app.mu.RUnlock()

	// Show auth error if present
	if authError != "" {
		// Show authentication error message
		errorTitle := app.systrayInterface.AddMenuItem("⚠️ Authentication Error", "")
		errorTitle.Disable()

		app.systrayInterface.AddSeparator()

		// Add error details
		errorMsg := app.systrayInterface.AddMenuItem(authError, "Click to see setup instructions")
		errorMsg.Click(func() {
			if err := openURL(ctx, "https://cli.github.com/manual/gh_auth_login"); err != nil {
				slog.Error("failed to open setup instructions", "error", err)
			}
		})

		app.systrayInterface.AddSeparator()

		// Add setup instructions
		setupInstr := app.systrayInterface.AddMenuItem("To fix this issue:", "")
		setupInstr.Disable()

		option1 := app.systrayInterface.AddMenuItem("1. Install GitHub CLI: brew install gh", "")
		option1.Disable()

		option2 := app.systrayInterface.AddMenuItem("2. Run: gh auth login", "")
		option2.Disable()

		option3 := app.systrayInterface.AddMenuItem("3. Or set GITHUB_TOKEN environment variable", "")
		option3.Disable()

		app.systrayInterface.AddSeparator()

		// Add quit option
		quitItem := app.systrayInterface.AddMenuItem("Quit", "")
		quitItem.Click(func() {
			app.systrayInterface.Quit()
		})

		return
	}

	// Show connection error if we have consecutive failures
	if failureCount > 0 && lastFetchError != "" {
		var errorMsg string
		switch {
		case failureCount == 1:
			errorMsg = "⚠️ Connection Error"
		case failureCount <= 3:
			errorMsg = fmt.Sprintf("⚠️ Connection Issues (%d failures)", failureCount)
		case failureCount <= 10:
			errorMsg = "❌ Multiple Connection Failures"
		default:
			errorMsg = "💀 Service Degraded"
		}

		errorTitle := app.systrayInterface.AddMenuItem(errorMsg, "")
		errorTitle.Disable()

		// Determine hostname and error type
		hostname := "api.github.com"
		for _, h := range []struct{ match, host string }{
			{"ready-to-review.dev", "dash.ready-to-review.dev"},
			{"api.github.com", "api.github.com"},
			{"github.com", "github.com"},
		} {
			if strings.Contains(lastFetchError, h.match) {
				hostname = h.host
				break
			}
		}

		errorType := "Connection failed"
		for _, e := range []struct{ match, errType string }{
			{"timeout", "Request timeout"},
			{"context deadline", "Request timeout (context deadline)"},
			{"rate limit", "Rate limit exceeded"},
			{"401", "Authentication failed"},
			{"unauthorized", "Authentication failed"},
			{"403", "Access forbidden"},
			{"forbidden", "Access forbidden"},
			{"404", "Resource not found"},
			{"connection refused", "Connection refused"},
			{"no such host", "DNS resolution failed"},
			{"TLS", "TLS/Certificate error"},
			{"x509", "TLS/Certificate error"},
		} {
			if strings.Contains(lastFetchError, e.match) {
				errorType = e.errType
				break
			}
		}

		// Show technical details
		techDetails := app.systrayInterface.AddMenuItem(fmt.Sprintf("Host: %s", hostname), "")
		techDetails.Disable()

		errorTypeItem := app.systrayInterface.AddMenuItem(fmt.Sprintf("Error: %s", errorType), "")
		errorTypeItem.Disable()

		// Show truncated raw error for debugging (max 80 chars)
		rawError := lastFetchError
		if len(rawError) > 80 {
			rawError = rawError[:77] + "..."
		}
		rawErrorItem := app.systrayInterface.AddMenuItem(fmt.Sprintf("Details: %s", rawError), "Click to copy full error")
		rawErrorItem.Click(func() {
			// Would need clipboard support to implement copy
			slog.Info("Full error", "error", lastFetchError)
		})

		app.systrayInterface.AddSeparator()
	}

	// Update tray title
	app.setTrayTitle()

	// Dashboard at the top
	// Add Web Dashboard link
	dashboardItem := app.systrayInterface.AddMenuItem("Web Dashboard", "")
	dashboardItem.Click(func() {
		if err := openURL(ctx, "https://dash.ready-to-review.dev/"); err != nil {
			slog.Error("failed to open dashboard", "error", err)
		}
	})

	app.systrayInterface.AddSeparator()

	// Get PR counts
	counts := app.countPRs()

	// Handle "No pull requests" case
	if counts.IncomingTotal == 0 && counts.OutgoingTotal == 0 {
		// No PRs to display
		noPRs := app.systrayInterface.AddMenuItem("No pull requests", "")
		noPRs.Disable()
	} else {
		// Incoming section
		if counts.IncomingTotal > 0 {
			app.mu.RLock()
			incoming := app.incoming
			app.mu.RUnlock()
			app.addPRSection(ctx, incoming, "Incoming", counts.IncomingBlocked)
		}

		app.systrayInterface.AddSeparator()

		// Outgoing section
		slog.Info("[MENU] Building outgoing section",
			"total_count", counts.OutgoingTotal,
			"blocked_count", counts.OutgoingBlocked)
		if counts.OutgoingTotal > 0 {
			app.mu.RLock()
			outgoing := app.outgoing
			app.mu.RUnlock()
			slog.Debug("[MENU] Outgoing PRs to add", "count", len(outgoing))
			app.addPRSection(ctx, outgoing, "Outgoing", counts.OutgoingBlocked)
		} else {
			slog.Info("[MENU] No outgoing PRs to display after filtering")
		}
	}

	// Add static items at the end
	app.addStaticMenuItems(ctx)

	// Menu rebuild complete
}

// addStaticMenuItems adds the static menu items (hide stale, start at login, quit).
func (app *App) addStaticMenuItems(ctx context.Context) {
	// Add static menu items

	app.systrayInterface.AddSeparator()

	// Hide orgs submenu
	// Add 'Hide orgs' submenu
	hideOrgsMenu := app.systrayInterface.AddMenuItem("Hide orgs", "Select organizations to hide PRs from")

	// Get combined list of seen orgs and hidden orgs
	app.mu.RLock()
	orgSet := make(map[string]bool)
	// Add all seen orgs
	for org := range app.seenOrgs {
		orgSet[org] = true
	}
	// Add all hidden orgs (in case they're not in seenOrgs yet)
	for org := range app.hiddenOrgs {
		orgSet[org] = true
	}
	// Convert to sorted slice
	orgs := make([]string, 0, len(orgSet))
	for org := range orgSet {
		orgs = append(orgs, org)
	}
	hiddenOrgs := make(map[string]bool)
	for org, hidden := range app.hiddenOrgs {
		hiddenOrgs[org] = hidden
	}
	app.mu.RUnlock()

	sort.Strings(orgs)

	if len(orgs) == 0 {
		noOrgsItem := hideOrgsMenu.AddSubMenuItem("No organizations found", "")
		noOrgsItem.Disable()
	} else {
		// Add checkbox items for each org
		for _, org := range orgs {
			orgName := org // Capture for closure
			// Add text checkmark for Linux
			var orgText string
			if hiddenOrgs[orgName] {
				orgText = "✓ " + orgName
			} else {
				orgText = orgName
			}
			orgItem := hideOrgsMenu.AddSubMenuItem(orgText, "")

			// Check if org is currently hidden (for non-Linux)
			if runtime.GOOS != "linux" && hiddenOrgs[orgName] {
				orgItem.Check()
			}

			orgItem.Click(func() {
				app.mu.Lock()
				if app.hiddenOrgs[orgName] {
					delete(app.hiddenOrgs, orgName)
					if runtime.GOOS != "linux" {
						orgItem.Uncheck()
					}
					slog.Info("[SETTINGS] Unhiding org", "org", orgName)
				} else {
					app.hiddenOrgs[orgName] = true
					if runtime.GOOS != "linux" {
						orgItem.Check()
					}
					slog.Info("[SETTINGS] Hiding org", "org", orgName)
				}
				app.mu.Unlock()

				// Save settings
				app.saveSettings()

				// Note: Menu needs manual refresh on Linux to see changes
			})
		}
	}

	// Hide stale PRs
	// Add 'Hide stale PRs' option with text checkmark for Linux
	var hideStaleText string
	if app.hideStaleIncoming {
		hideStaleText = "✓ Hide stale PRs (>90 days)"
	} else {
		hideStaleText = "Hide stale PRs (>90 days)"
	}
	hideStaleItem := app.systrayInterface.AddMenuItem(hideStaleText, "")
	if runtime.GOOS != "linux" && app.hideStaleIncoming {
		hideStaleItem.Check()
	}
	hideStaleItem.Click(func() {
		app.mu.Lock()
		app.hideStaleIncoming = !app.hideStaleIncoming
		hideStale := app.hideStaleIncoming
		app.mu.Unlock()

		if runtime.GOOS != "linux" {
			if hideStale {
				hideStaleItem.Check()
			} else {
				hideStaleItem.Uncheck()
			}
		}

		// Save settings to disk
		app.saveSettings()

		// Note: Menu needs manual refresh on Linux to see changes
		slog.Info("[SETTINGS] Hide stale PRs toggled", "enabled", hideStale)
	})

	// Add login item option (macOS only)
	addLoginItemUI(ctx, app)

	// Audio cues
	// Add 'Audio cues' option with text checkmark for Linux
	app.mu.RLock()
	var audioText string
	if app.enableAudioCues {
		audioText = "✓ Honks enabled"
	} else {
		audioText = "Honks enabled"
	}
	enableAudioCues := app.enableAudioCues
	app.mu.RUnlock()
	audioItem := app.systrayInterface.AddMenuItem(audioText, "Play sounds for notifications")
	if runtime.GOOS != "linux" && enableAudioCues {
		audioItem.Check()
	}
	audioItem.Click(func() {
		app.mu.Lock()
		app.enableAudioCues = !app.enableAudioCues
		enabled := app.enableAudioCues
		app.mu.Unlock()

		if runtime.GOOS != "linux" {
			if enabled {
				audioItem.Check()
			} else {
				audioItem.Uncheck()
			}
		}

		slog.Info("[SETTINGS] Audio cues toggled", "enabled", enabled)

		// Save settings to disk
		app.saveSettings()
	})

	// Auto-open blocked PRs in browser
	// Add 'Auto-open PRs' option with text checkmark for Linux
	app.mu.RLock()
	var autoText string
	if app.enableAutoBrowser {
		autoText = "✓ Auto-open incoming PRs"
	} else {
		autoText = "Auto-open incoming PRs"
	}
	enableAutoBrowser := app.enableAutoBrowser
	app.mu.RUnlock()
	autoOpenItem := app.systrayInterface.AddMenuItem(autoText, "Automatically open newly blocked PRs in browser (rate limited)")
	if runtime.GOOS != "linux" && enableAutoBrowser {
		autoOpenItem.Check()
	}
	autoOpenItem.Click(func() {
		app.mu.Lock()
		app.enableAutoBrowser = !app.enableAutoBrowser
		enabled := app.enableAutoBrowser
		// Reset rate limiter when toggling the feature
		if !enabled {
			app.browserRateLimiter.Reset()
		}
		app.mu.Unlock()

		if runtime.GOOS != "linux" {
			if enabled {
				autoOpenItem.Check()
			} else {
				autoOpenItem.Uncheck()
			}
		}

		slog.Info("[SETTINGS] Auto-open blocked PRs toggled", "enabled", enabled)

		// Save settings to disk
		app.saveSettings()
	})

	// Quit
	// Add 'Quit' option
	quitItem := app.systrayInterface.AddMenuItem("Quit", "")
	quitItem.Click(func() {
		slog.Info("Quit requested by user")
		app.systrayInterface.Quit()
	})
}
