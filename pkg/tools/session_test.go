package tools

import (
	"context"
	"fmt"
	"os"
	"runtime"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// TestSessionManager_CleanupKillsExpiredRunningSessions verifies the TTL: a
// running session older than backgroundSessionMaxAge is killed, stays visible
// for that cleanup run, and is removed once it has been done long enough.
func TestSessionManager_CleanupKillsExpiredRunningSessions(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("uses sleep from a POSIX shell")
	}
	tool, err := NewExecTool("", false)
	require.NoError(t, err)
	sm := NewSessionManager()
	t.Cleanup(sm.Stop)
	tool.sessionManager = sm

	result := tool.runBackground(context.Background(), "sleep 60", "", false)
	require.False(t, result.IsError, "result: %s", result.ForLLM)
	sessions := sm.List()
	require.Len(t, sessions, 1)
	session, err := sm.Get(sessions[0].ID)
	require.NoError(t, err)

	session.mu.Lock()
	session.StartTime = time.Now().Add(-2 * time.Hour).Unix()
	pid := session.PID
	session.mu.Unlock()
	require.Positive(t, pid)

	sm.cleanupOldSessions()

	require.Equal(t, "done", session.GetStatus())
	require.Equal(t, -1, session.GetExitCode())
	_, err = sm.Get(session.ID)
	require.NoError(t, err, "a session killed by the TTL stays visible until a later cleanup")
	require.Contains(t, session.Read(), "killed after 1h")

	proc, err := os.FindProcess(pid)
	require.NoError(t, err)
	require.Eventually(t, func() bool {
		return proc.Signal(syscall.Signal(0)) != nil
	}, 5*time.Second, 50*time.Millisecond, "process %d should be gone", pid)

	session.mu.Lock()
	session.EndTime = time.Now().Add(-31 * time.Minute).Unix()
	session.mu.Unlock()
	sm.cleanupOldSessions()
	_, err = sm.Get(session.ID)
	require.ErrorIs(t, err, ErrSessionNotFound)
}

// TestSessionManager_CleanupUsesEndTime verifies finished sessions are kept
// for 30 minutes after they end, however long ago they started.
func TestSessionManager_CleanupUsesEndTime(t *testing.T) {
	sm := NewSessionManager()
	t.Cleanup(sm.Stop)
	old := time.Now().Add(-3 * time.Hour).Unix()
	recentEnd := time.Now().Unix()
	oldEnd := time.Now().Add(-31 * time.Minute).Unix()
	sm.Add(&ProcessSession{ID: "recent-end", Status: "done", StartTime: old, EndTime: recentEnd})
	sm.Add(&ProcessSession{ID: "old-end", Status: "done", StartTime: old, EndTime: oldEnd})

	sm.cleanupOldSessions()

	_, err := sm.Get("recent-end")
	require.NoError(t, err)
	_, err = sm.Get("old-end")
	require.ErrorIs(t, err, ErrSessionNotFound)
}

// TestSessionManager_AddIfBelowConcurrent verifies the cap holds under
// concurrent inserts.
func TestSessionManager_AddIfBelowConcurrent(t *testing.T) {
	sm := NewSessionManager()
	t.Cleanup(sm.Stop)

	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			_ = sm.AddIfBelow(&ProcessSession{ID: fmt.Sprint(i), Status: "running"}, maxRunningBackgroundSessions)
		}(i)
	}
	wg.Wait()

	require.Len(t, sm.List(), maxRunningBackgroundSessions)
	require.Equal(t, maxRunningBackgroundSessions, sm.RunningCount())
	err := sm.AddIfBelow(&ProcessSession{ID: "extra", Status: "running"}, maxRunningBackgroundSessions)
	require.EqualError(t, err, "too many background sessions running (max 8); kill or wait for one")
}

func TestSessionManager_AddGet(t *testing.T) {
	sm := NewSessionManager()
	t.Cleanup(sm.Stop)
	session := &ProcessSession{
		ID:        "test-1",
		Command:   "echo hello",
		Status:    "running",
		StartTime: 1000,
	}

	sm.Add(session)

	got, err := sm.Get("test-1")
	require.NoError(t, err)
	require.Equal(t, "test-1", got.ID)
}

func TestSessionManager_Remove(t *testing.T) {
	sm := NewSessionManager()
	t.Cleanup(sm.Stop)
	session := &ProcessSession{
		ID:        "test-1",
		Command:   "echo hello",
		Status:    "running",
		StartTime: 1000,
	}
	sm.Add(session)
	sm.Remove("test-1")

	_, err := sm.Get("test-1")
	require.ErrorIs(t, err, ErrSessionNotFound)
}

func TestSessionManager_List(t *testing.T) {
	sm := NewSessionManager()
	t.Cleanup(sm.Stop)
	sm.Add(&ProcessSession{
		ID:        "test-1",
		Command:   "echo hello",
		Status:    "running",
		StartTime: 1000,
	})
	sm.Add(&ProcessSession{
		ID:        "test-2",
		Command:   "echo world",
		Status:    "running",
		StartTime: 1001,
	})
	sm.Add(&ProcessSession{
		ID:        "test-3",
		Command:   "echo done",
		Status:    "done",
		StartTime: 1002,
	})

	sessions := sm.List()
	require.Len(t, sessions, 3)

	ids := make(map[string]bool)
	for _, s := range sessions {
		ids[s.ID] = true
	}
	require.True(t, ids["test-1"])
	require.True(t, ids["test-2"])
	require.True(t, ids["test-3"])
}

func TestProcessSession_IsDone(t *testing.T) {
	session := &ProcessSession{Status: "running"}
	require.False(t, session.IsDone())

	session.Status = "done"
	require.True(t, session.IsDone())

	session.Status = "exited"
	require.True(t, session.IsDone())
}

func TestProcessSession_ToSessionInfo(t *testing.T) {
	session := &ProcessSession{
		ID:        "test-1",
		PID:       12345,
		Command:   "echo hello",
		Status:    "running",
		StartTime: 1000,
	}

	info := session.ToSessionInfo()
	require.Equal(t, "test-1", info.ID)
	require.Equal(t, "echo hello", info.Command)
	require.Equal(t, "running", info.Status)
	require.Equal(t, 12345, info.PID)
	require.Equal(t, int64(1000), info.StartedAt)
}
