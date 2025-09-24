// Package main - notifications.go provides simplified notification handling.
package main

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/gen2brain/beeep"
)

// processNotifications handles notifications for newly blocked PRs using the state manager.
func (app *App) processNotifications(ctx context.Context) {
	slog.Debug("[NOTIFY] Processing notifications...")

	// Get the list of PRs that need notifications
	app.mu.RLock()
	hiddenOrgs := make(map[string]bool)
	for org, hidden := range app.hiddenOrgs {
		hiddenOrgs[org] = hidden
	}
	incoming := make([]PR, len(app.incoming))
	copy(incoming, app.incoming)
	outgoing := make([]PR, len(app.outgoing))
	copy(outgoing, app.outgoing)
	app.mu.RUnlock()

	// Determine if this is the initial discovery
	isInitialDiscovery := !app.hasPerformedInitialDiscovery

	// Let the state manager figure out what needs notifications
	toNotify := app.stateManager.UpdatePRs(incoming, outgoing, hiddenOrgs, isInitialDiscovery)

	// Mark that we've performed initial discovery
	if isInitialDiscovery {
		app.hasPerformedInitialDiscovery = true
		slog.Info("[STATE] Initial discovery completed", "incoming_count", len(incoming), "outgoing_count", len(outgoing))
	}

	// Update deprecated fields for test compatibility
	app.mu.Lock()
	app.previousBlockedPRs = make(map[string]bool)
	app.blockedPRTimes = make(map[string]time.Time)
	states := app.stateManager.BlockedPRs()
	for url, state := range states {
		app.previousBlockedPRs[url] = true
		app.blockedPRTimes[url] = state.FirstBlockedAt
	}
	app.mu.Unlock()

	if len(toNotify) == 0 {
		slog.Debug("[NOTIFY] No PRs need notifications")
		return
	}

	slog.Info("[NOTIFY] PRs need notifications", "count", len(toNotify))

	// Process notifications in a goroutine to avoid blocking the UI thread
	go func() {
		// Send notifications for each PR
		playedHonk := false
		playedRocket := false

		for i := range toNotify {
			pr := toNotify[i]
			isIncoming := false
			// Check if it's in the incoming list
			for j := range incoming {
				if incoming[j].URL == pr.URL {
					isIncoming = true
					break
				}
			}

			// Send notification
			if isIncoming {
				app.sendPRNotification(ctx, pr, "PR Blocked on You 🪿", "honk", &playedHonk)
			} else {
				// Add delay between different sound types in goroutine to avoid blocking
				if playedHonk && !playedRocket {
					time.Sleep(2 * time.Second)
				}
				app.sendPRNotification(ctx, pr, "Your PR is Blocked 🚀", "rocket", &playedRocket)
			}

			// Auto-open if enabled
			if app.enableAutoBrowser && time.Since(app.startTime) > startupGracePeriod {
				app.tryAutoOpenPR(ctx, pr, app.enableAutoBrowser, app.startTime)
			}
		}

	}()

	// Update menu immediately after sending notifications
	// This needs to happen in the main thread to show the party popper emoji
	if len(toNotify) > 0 {
		slog.Info("[FLOW] Updating menu after sending notifications", "notified_count", len(toNotify))
		app.updateMenu(ctx)
		slog.Info("[FLOW] Menu update after notifications completed")
	}
}

// sendPRNotification sends a notification for a single PR.
func (app *App) sendPRNotification(ctx context.Context, pr PR, title string, soundType string, playedSound *bool) {
	message := fmt.Sprintf("%s #%d: %s", pr.Repository, pr.Number, pr.Title)

	// Send desktop notification in a goroutine to avoid blocking
	go func() {
		if err := beeep.Notify(title, message, ""); err != nil {
			slog.Error("[NOTIFY] Failed to send notification", "url", pr.URL, "error", err)
		}
	}()

	// Play sound (only once per type per cycle) - already async in playSound
	if !*playedSound {
		slog.Debug("[NOTIFY] Playing sound for PR", "soundType", soundType, "repo", pr.Repository, "number", pr.Number)
		app.playSound(ctx, soundType)
		*playedSound = true
	}
}

// updatePRStatesAndNotify is the simplified replacement for checkForNewlyBlockedPRs.
func (app *App) updatePRStatesAndNotify(ctx context.Context) {
	// Simple and clear: just process notifications
	app.processNotifications(ctx)
}
