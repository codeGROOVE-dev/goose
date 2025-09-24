package main

import (
	_ "embed"
	"log/slog"
	"os"
	"path/filepath"
)

// Embed icon files at compile time for better distribution
//
//go:embed icons/goose.png
var iconGoose []byte

//go:embed icons/popper.png
var iconPopper []byte

//go:embed icons/smiling-face.png
var iconSmiling []byte

//go:embed icons/lock.png
var iconLock []byte

//go:embed icons/warning.png
var iconWarning []byte

// IconType represents different icon states
type IconType int

const (
	IconSmiling IconType = iota // No blocked PRs
	IconGoose                   // Incoming PRs blocked
	IconPopper                  // Outgoing PRs blocked
	IconBoth                    // Both incoming and outgoing blocked
	IconWarning                 // General error/warning
	IconLock                    // Authentication error
)

// getIcon returns the icon bytes for the given type
func getIcon(iconType IconType) []byte {
	switch iconType {
	case IconGoose:
		return iconGoose
	case IconPopper:
		return iconPopper
	case IconSmiling:
		return iconSmiling
	case IconWarning:
		return iconWarning
	case IconLock:
		return iconLock
	case IconBoth:
		// For both, we'll use the goose icon as primary
		return iconGoose
	default:
		return iconSmiling
	}
}

// loadIconFromFile loads an icon from the filesystem (fallback if embed fails)
func loadIconFromFile(filename string) []byte {
	iconPath := filepath.Join("icons", filename)
	data, err := os.ReadFile(iconPath)
	if err != nil {
		slog.Warn("Failed to load icon file", "path", iconPath, "error", err)
		return nil
	}
	return data
}

// setTrayIcon updates the system tray icon based on PR counts
func (app *App) setTrayIcon(iconType IconType) {
	iconBytes := getIcon(iconType)
	if iconBytes == nil || len(iconBytes) == 0 {
		slog.Warn("Icon bytes are empty, skipping icon update", "type", iconType)
		return
	}

	app.systrayInterface.SetIcon(iconBytes)
	slog.Debug("[TRAY] Setting icon", "type", iconType)
}