// PicoClaw - Ultra-lightweight personal AI agent
// Inspired by and based on nanobot: https://github.com/HKUDS/nanobot
// License: MIT
//
// Copyright (c) 2026 PicoClaw contributors

package heartbeat

import (
	"context"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/andre25costa-code/kuromatsu"
	"github.com/andre25costa-code/kuromatsu/pkg/bus"
	"github.com/andre25costa-code/kuromatsu/pkg/constants"
	"github.com/andre25costa-code/kuromatsu/pkg/fileutil"
	"github.com/andre25costa-code/kuromatsu/pkg/logger"
	"github.com/andre25costa-code/kuromatsu/pkg/state"
	"github.com/andre25costa-code/kuromatsu/pkg/tools"
)

const (
	minIntervalMinutes     = 5
	defaultIntervalMinutes = 30
	userTasksMarker        = "Add your heartbeat tasks below this line:"
)

// HeartbeatHandler is the function type for handling heartbeat.
// It returns a ToolResult that can indicate async operations.
// channel and chatID are derived from the last active user channel.
type HeartbeatHandler func(prompt, channel, chatID string) *tools.ToolResult

// HeartbeatService manages periodic heartbeat checks
type HeartbeatService struct {
	workspace string
	bus       *bus.MessageBus
	state     *state.Manager
	handler   HeartbeatHandler
	interval  time.Duration
	enabled   bool
	mu        sync.RWMutex
	stopChan  chan struct{}

	// shouldSkip is the Trilho C runstate hook (ADR-016 point 7,
	// FR-017/AC-017-2): checked at the top of executeHeartbeat. Installed
	// only when runstate.enabled=true and
	// runstate.skip_heartbeat_when_busy is on (gateway.go, C2) -- nil
	// (the default) means every heartbeat runs exactly as it always has,
	// with zero risk of ever pulling in a real evaluation (AC-017-7).
	shouldSkip func() bool

	// now is the clock buildPrompt stamps the prompt with; time.Now
	// outside tests.
	now func() time.Time
}

// NewHeartbeatService creates a new heartbeat service
func NewHeartbeatService(workspace string, intervalMinutes int, enabled bool) *HeartbeatService {
	// Apply minimum interval
	if intervalMinutes < minIntervalMinutes && intervalMinutes != 0 {
		intervalMinutes = minIntervalMinutes
	}

	if intervalMinutes == 0 {
		intervalMinutes = defaultIntervalMinutes
	}

	return &HeartbeatService{
		workspace: workspace,
		interval:  time.Duration(intervalMinutes) * time.Minute,
		enabled:   enabled,
		state:     state.NewManager(workspace),
		now:       time.Now,
	}
}

// SetBus sets the message bus for delivering heartbeat results.
func (hs *HeartbeatService) SetBus(msgBus *bus.MessageBus) {
	hs.mu.Lock()
	defer hs.mu.Unlock()
	hs.bus = msgBus
}

// SetHandler sets the heartbeat handler.
func (hs *HeartbeatService) SetHandler(handler HeartbeatHandler) {
	hs.mu.Lock()
	defer hs.mu.Unlock()
	hs.handler = handler
}

// SetShouldSkip installs the Trilho C runstate hook (AC-017-2): fn is
// called at the top of every executeHeartbeat, and a true result skips
// that dispatch entirely (logged, not enqueued for later -- a missed
// heartbeat is just a missed heartbeat, S17/S30). Pass nil to remove the
// hook and go back to always running (the default).
func (hs *HeartbeatService) SetShouldSkip(fn func() bool) {
	hs.mu.Lock()
	defer hs.mu.Unlock()
	hs.shouldSkip = fn
}

// Start begins the heartbeat service
func (hs *HeartbeatService) Start() error {
	hs.mu.Lock()
	defer hs.mu.Unlock()

	if hs.stopChan != nil {
		logger.InfoC("heartbeat", "Heartbeat service already running")
		return nil
	}

	if !hs.enabled {
		logger.InfoC("heartbeat", "Heartbeat service disabled")
		return nil
	}

	hs.stopChan = make(chan struct{})
	go hs.runLoop(hs.stopChan)

	logger.InfoCF("heartbeat", "Heartbeat service started", map[string]any{
		"interval_minutes": hs.interval.Minutes(),
	})

	return nil
}

// Stop gracefully stops the heartbeat service
func (hs *HeartbeatService) Stop() {
	hs.mu.Lock()
	defer hs.mu.Unlock()

	if hs.stopChan == nil {
		return
	}

	logger.InfoC("heartbeat", "Stopping heartbeat service")
	close(hs.stopChan)
	hs.stopChan = nil
}

// runLoop runs the heartbeat ticker
func (hs *HeartbeatService) runLoop(stopChan chan struct{}) {
	ticker := time.NewTicker(hs.interval)
	defer ticker.Stop()

	// Run first heartbeat after initial delay
	time.AfterFunc(time.Second, func() {
		hs.executeHeartbeat()
	})

	for {
		select {
		case <-stopChan:
			return
		case <-ticker.C:
			hs.executeHeartbeat()
		}
	}
}

// executeHeartbeat performs a single heartbeat check
func (hs *HeartbeatService) executeHeartbeat() {
	hs.mu.RLock()
	handler := hs.handler
	shouldSkip := hs.shouldSkip
	if !hs.enabled || hs.stopChan == nil {
		hs.mu.RUnlock()
		return
	}
	hs.mu.RUnlock()

	if shouldSkip != nil && shouldSkip() {
		logger.InfoC("heartbeat", "Skipping heartbeat: runstate busy (AC-017-2)")
		return
	}

	logger.DebugC("heartbeat", "Executing heartbeat")

	prompt := hs.buildPrompt()
	if prompt == "" {
		logger.InfoC("heartbeat", "No heartbeat prompt (HEARTBEAT.md empty or missing)")
		return
	}

	if handler == nil {
		hs.logErrorf("Heartbeat handler not configured")
		return
	}

	// Get last channel info for context
	lastChannel := hs.state.GetLastChannel()
	channel, chatID := hs.parseLastChannel(lastChannel)

	// Debug log for channel resolution
	hs.logInfof("Resolved channel: %s, chatID: %s (from lastChannel: %s)", channel, chatID, lastChannel)

	result := handler(prompt, channel, chatID)

	if result == nil {
		hs.logInfof("Heartbeat handler returned nil result")
		return
	}

	// Handle different result types
	if result.IsError {
		hs.logErrorf("Heartbeat error: %s", result.ForLLM)
		return
	}

	if result.Async {
		hs.logInfof("Async task started: %s", result.ForLLM)
		logger.InfoCF("heartbeat", "Async heartbeat task started",
			map[string]any{
				"message": result.ForLLM,
			})
		return
	}

	// Check if silent
	if result.Silent {
		hs.logInfof("Heartbeat OK - silent")
		return
	}

	// Send result to user
	// Reply to the chat the turn was built for (resolved above), not to
	// whichever chat is last when the turn finishes (T14).
	if result.ForUser != "" {
		hs.sendResponse(result.ForUser, channel, chatID)
	} else if result.ForLLM != "" {
		hs.sendResponse(result.ForLLM, channel, chatID)
	}

	hs.logInfof("Heartbeat completed: %s", result.ForLLM)
}

// buildPrompt builds the heartbeat prompt from HEARTBEAT.md
func (hs *HeartbeatService) buildPrompt() string {
	heartbeatPath := filepath.Join(hs.workspace, "HEARTBEAT.md")

	data, err := os.ReadFile(heartbeatPath)
	if err != nil {
		if os.IsNotExist(err) {
			hs.createDefaultHeartbeatTemplate()
			return ""
		}
		hs.logErrorf("Error reading HEARTBEAT.md: %v", err)
		return ""
	}

	content := string(data)
	if !heartbeatHasUserTasks(content) {
		return ""
	}

	// FR-021: the time closes the prompt instead of opening it, so every
	// byte of HEARTBEAT.md stays identical between ticks and remains
	// reusable from the prefix cache (ADR-015). Minute precision is all a
	// heartbeat task can act on.
	return fmt.Sprintf(`# Heartbeat Check

You are a proactive AI assistant. This is a scheduled heartbeat check.
Review the following tasks and execute any necessary actions using available skills.
If there is nothing that requires attention, respond ONLY with: HEARTBEAT_OK

%s

Current time: %s
`, content, hs.now().Format("2006-01-02 15:04 MST"))
}

// createDefaultHeartbeatTemplate recreates HEARTBEAT.md from the same
// embedded template onboard installs (workspace/HEARTBEAT.md).
func (hs *HeartbeatService) createDefaultHeartbeatTemplate() {
	heartbeatPath := filepath.Join(hs.workspace, "HEARTBEAT.md")

	defaultContent, err := fs.ReadFile(kuromatsu.OnboardWorkspace, "workspace/HEARTBEAT.md")
	if err != nil {
		hs.logErrorf("Failed to read the embedded HEARTBEAT.md template: %v", err)
		return
	}

	if err := fileutil.WriteFileAtomic(heartbeatPath, defaultContent, 0o644); err != nil {
		hs.logErrorf("Failed to create default HEARTBEAT.md: %v", err)
	} else {
		hs.logInfof("Created default HEARTBEAT.md template")
	}
}

func heartbeatHasUserTasks(content string) bool {
	trimmed := strings.TrimSpace(content)
	if trimmed == "" {
		return false
	}

	markerIdx := strings.Index(content, userTasksMarker)
	if markerIdx < 0 {
		return true
	}

	tasksSection := content[markerIdx+len(userTasksMarker):]
	for _, line := range strings.Split(tasksSection, "\n") {
		trimmedLine := strings.TrimSpace(line)
		if trimmedLine == "" {
			continue
		}
		if strings.HasPrefix(trimmedLine, "#") {
			continue
		}
		return true
	}

	return false
}

// sendResponse sends the heartbeat response to the last channel
func (hs *HeartbeatService) sendResponse(response, platform, userID string) {
	hs.mu.RLock()
	msgBus := hs.bus
	hs.mu.RUnlock()

	if msgBus == nil {
		hs.logInfof("No message bus configured, heartbeat result not sent")
		return
	}

	// No recorded (or an internal) channel: nowhere to deliver.
	if platform == "" || userID == "" {
		hs.logInfof("No deliverable last channel recorded, heartbeat result not sent")
		return
	}

	pubCtx, pubCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer pubCancel()
	msgBus.PublishOutbound(pubCtx, bus.OutboundMessage{
		Context: bus.NewOutboundContext(platform, userID, ""),
		Content: response,
	})

	hs.logInfof("Heartbeat result sent to %s", platform)
}

// parseLastChannel parses the last channel string into platform and userID.
// Returns empty strings for invalid or internal channels.
func (hs *HeartbeatService) parseLastChannel(lastChannel string) (platform, userID string) {
	if lastChannel == "" {
		return "", ""
	}

	// Parse channel format: "platform:user_id" (e.g., "telegram:123456")
	parts := strings.SplitN(lastChannel, ":", 2)
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		hs.logErrorf("Invalid last channel format: %s", lastChannel)
		return "", ""
	}

	platform, userID = parts[0], parts[1]

	// Skip internal channels
	if constants.IsInternalChannel(platform) {
		hs.logInfof("Skipping internal channel: %s", platform)
		return "", ""
	}

	return platform, userID
}

// logInfof logs an informational message to the heartbeat log
func (hs *HeartbeatService) logInfof(format string, args ...any) {
	hs.logf("INFO", format, args...)
}

// logErrorf logs an error message to the heartbeat log
func (hs *HeartbeatService) logErrorf(format string, args ...any) {
	hs.logf("ERROR", format, args...)
}

// maxHeartbeatLogBytes is the size at which heartbeat.log is rotated to
// heartbeat.log.1 (one generation kept).
const maxHeartbeatLogBytes = 1 << 20

// logf writes a message to the heartbeat log file
func (hs *HeartbeatService) logf(level, format string, args ...any) {
	logFile := filepath.Join(hs.workspace, "heartbeat.log")
	if info, statErr := os.Stat(logFile); statErr == nil && info.Size() > maxHeartbeatLogBytes {
		_ = os.Rename(logFile, logFile+".1")
	}
	f, err := os.OpenFile(logFile, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return
	}
	defer f.Close()

	timestamp := time.Now().Format("2006-01-02 15:04:05")
	fmt.Fprintf(f, "[%s] [%s] %s\n", timestamp, level, fmt.Sprintf(format, args...))
}
