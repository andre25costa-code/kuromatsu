package state

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/andre25costa-code/kuromatsu/pkg/fileutil"
	"github.com/andre25costa-code/kuromatsu/pkg/logger"
)

// State represents the persistent state for a workspace.
// It includes information about the last active channel/chat.
type State struct {
	// LastChannel is the last channel used for communication
	LastChannel string `json:"last_channel,omitempty"`

	// LastChatID is the last chat ID used for communication
	LastChatID string `json:"last_chat_id,omitempty"`

	// Timestamp is the last time this state was updated
	Timestamp time.Time `json:"timestamp"`
}

// Manager manages persistent state with atomic saves.
type Manager struct {
	workspace string
	state     *State
	mu        sync.RWMutex
	stateFile string

	// seen identifies the version of stateFile held in state. Several
	// Managers (agent, heartbeat, gateway) share one state.json, so each read
	// and write first reloads the file when another one changed it.
	seenMod  time.Time
	seenSize int64
}

// NewManager creates a new state manager for the given workspace.
func NewManager(workspace string) *Manager {
	stateDir := filepath.Join(workspace, "state")
	stateFile := filepath.Join(stateDir, "state.json")
	oldStateFile := filepath.Join(workspace, "state.json")

	// Create state directory if it doesn't exist
	if err := os.MkdirAll(stateDir, 0o700); err != nil {
		logger.WarnCF("state", "failed to create state directory", map[string]any{
			"dir":   stateDir,
			"error": err.Error(),
		})
	}

	sm := &Manager{
		workspace: workspace,
		stateFile: stateFile,
		state:     &State{},
	}

	// Try to load from new location first
	if _, err := os.Stat(stateFile); os.IsNotExist(err) {
		// New file doesn't exist, try migrating from old location
		if data, err := os.ReadFile(oldStateFile); err == nil {
			if err := json.Unmarshal(data, sm.state); err == nil {
				// Migrate to new location
				if err := sm.saveAtomic(); err != nil {
					logger.WarnCF("state", "failed to save state", map[string]any{
						"error": err.Error(),
					})
				}
				logger.InfoCF("state", "migrated state", map[string]any{
					"from": oldStateFile,
					"to":   stateFile,
				})
			}
		}
	} else {
		// Load from new location
		if err := sm.load(); err != nil {
			logger.WarnCF("state", "failed to load state", map[string]any{
				"error": err.Error(),
			})
		}
	}

	return sm
}

// SetLastChannel atomically updates the last channel and saves the state.
// This method uses a temp file + rename pattern for atomic writes,
// ensuring that the state file is never corrupted even if the process crashes.
func (sm *Manager) SetLastChannel(channel string) error {
	sm.mu.Lock()
	defer sm.mu.Unlock()
	sm.refreshLocked()

	// Update state
	sm.state.LastChannel = channel
	sm.state.Timestamp = time.Now()

	// Atomic save using temp file + rename
	if err := sm.saveAtomic(); err != nil {
		return fmt.Errorf("failed to save state atomically: %w", err)
	}

	return nil
}

// SetLastChatID atomically updates the last chat ID and saves the state.
func (sm *Manager) SetLastChatID(chatID string) error {
	sm.mu.Lock()
	defer sm.mu.Unlock()
	sm.refreshLocked()

	// Update state
	sm.state.LastChatID = chatID
	sm.state.Timestamp = time.Now()

	// Atomic save using temp file + rename
	if err := sm.saveAtomic(); err != nil {
		return fmt.Errorf("failed to save state atomically: %w", err)
	}

	return nil
}

// GetLastChannel returns the last channel from the state.
func (sm *Manager) GetLastChannel() string {
	sm.mu.Lock()
	defer sm.mu.Unlock()
	sm.refreshLocked()
	return sm.state.LastChannel
}

// GetLastChatID returns the last chat ID from the state.
func (sm *Manager) GetLastChatID() string {
	sm.mu.Lock()
	defer sm.mu.Unlock()
	sm.refreshLocked()
	return sm.state.LastChatID
}

// GetTimestamp returns the timestamp of the last state update.
func (sm *Manager) GetTimestamp() time.Time {
	sm.mu.Lock()
	defer sm.mu.Unlock()
	sm.refreshLocked()
	return sm.state.Timestamp
}

// refreshLocked reloads stateFile when it changed since this Manager last
// read or wrote it (another Manager in this process, or another process,
// wrote it). A missing or unreadable file keeps the state in memory.
// Must be called with the lock held.
func (sm *Manager) refreshLocked() {
	info, err := os.Stat(sm.stateFile)
	if err != nil || (info.ModTime().Equal(sm.seenMod) && info.Size() == sm.seenSize) {
		return
	}
	fresh := &State{}
	data, err := os.ReadFile(sm.stateFile)
	if err != nil || json.Unmarshal(data, fresh) != nil {
		return
	}
	sm.state = fresh
	sm.seenMod, sm.seenSize = info.ModTime(), info.Size()
}

// markSeenLocked records the current version of stateFile as the one held
// in memory. Must be called with the lock held.
func (sm *Manager) markSeenLocked() {
	if info, err := os.Stat(sm.stateFile); err == nil {
		sm.seenMod, sm.seenSize = info.ModTime(), info.Size()
	}
}

// saveAtomic performs an atomic save using temp file + rename.
// This ensures that the state file is never corrupted:
// 1. Write to a temp file
// 2. Sync to disk (critical for SD cards/flash storage)
// 3. Rename temp file to target (atomic on POSIX systems)
// 4. If rename fails, cleanup the temp file
//
// Must be called with the lock held.
func (sm *Manager) saveAtomic() error {
	// Use unified atomic write utility with explicit sync for flash storage reliability.
	// Using 0o600 (owner read/write only) for secure default permissions.
	data, err := json.MarshalIndent(sm.state, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal state: %w", err)
	}

	if err := fileutil.WriteFileAtomic(sm.stateFile, data, 0o600); err != nil {
		return err
	}
	sm.markSeenLocked()
	return nil
}

// load loads the state from disk.
func (sm *Manager) load() error {
	data, err := os.ReadFile(sm.stateFile)
	if err != nil {
		// File doesn't exist yet, that's OK
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("failed to read state file: %w", err)
	}

	if err := json.Unmarshal(data, sm.state); err != nil {
		return fmt.Errorf("failed to unmarshal state: %w", err)
	}
	sm.markSeenLocked()

	return nil
}
