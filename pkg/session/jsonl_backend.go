package session

import (
	"context"
	"encoding/json"
	"strings"

	"github.com/andre25costa-code/kuromatsu/pkg/logger"
	"github.com/andre25costa-code/kuromatsu/pkg/memory"
	"github.com/andre25costa-code/kuromatsu/pkg/providers"
)

// JSONLBackend adapts a memory.Store into the SessionStore interface.
// Write errors are logged rather than returned, matching the fire-and-forget
// contract of SessionManager that the agent loop relies on.
type JSONLBackend struct {
	store memory.Store
}

type metaAwareStore interface {
	GetSessionMeta(ctx context.Context, sessionKey string) (memory.SessionMeta, error)
	UpsertSessionMeta(ctx context.Context, sessionKey string, scope json.RawMessage, aliases []string) error
	ResolveSessionKey(ctx context.Context, sessionKey string) (string, bool, error)
}

type aliasPromotingStore interface {
	PromoteAliasHistory(ctx context.Context, sessionKey string, scope json.RawMessage, aliases []string) (bool, error)
}

// MetadataAwareSessionStore exposes structured session metadata operations.
type MetadataAwareSessionStore interface {
	EnsureSessionMetadata(sessionKey string, scope *SessionScope, aliases []string)
	ResolveSessionKey(sessionKey string) string
	GetSessionScope(sessionKey string) *SessionScope
}

// NewJSONLBackend wraps a memory.Store for use as a SessionStore.
func NewJSONLBackend(store memory.Store) *JSONLBackend {
	return &JSONLBackend{store: store}
}

func (b *JSONLBackend) resolveSessionKey(sessionKey string) string {
	metaStore, ok := b.store.(metaAwareStore)
	if !ok {
		return sessionKey
	}
	resolved, found, err := metaStore.ResolveSessionKey(context.Background(), sessionKey)
	if err != nil {
		logger.ErrorCF("session", "resolve session key failed", map[string]any{
			"session_key": sessionKey, "error": err.Error(),
		})
		return sessionKey
	}
	if found && resolved != "" {
		return resolved
	}
	return sessionKey
}

// ResolveSessionKey maps aliases onto their canonical session key when the
// underlying store supports structured metadata. Unknown aliases fall back to
// the original input so existing callers remain compatible.
func (b *JSONLBackend) ResolveSessionKey(sessionKey string) string {
	return b.resolveSessionKey(sessionKey)
}

// EnsureSessionMetadata persists scope and alias metadata for a session.
func (b *JSONLBackend) EnsureSessionMetadata(sessionKey string, scope *SessionScope, aliases []string) {
	metaStore, ok := b.store.(metaAwareStore)
	if !ok {
		return
	}
	sessionKey = strings.TrimSpace(sessionKey)
	if sessionKey == "" {
		return
	}

	var rawScope json.RawMessage
	if scope != nil {
		data, err := json.Marshal(scope)
		if err != nil {
			logger.ErrorCF("session", "encode session scope failed", map[string]any{
				"session_key": sessionKey, "error": err.Error(),
			})
			return
		}
		rawScope = data
	}
	ctx := context.Background()
	if err := metaStore.UpsertSessionMeta(ctx, sessionKey, rawScope, aliases); err != nil {
		logger.ErrorCF("session", "upsert session metadata failed", map[string]any{
			"session_key": sessionKey, "error": err.Error(),
		})
		return
	}

	if promotingStore, ok := b.store.(aliasPromotingStore); ok {
		if _, err := promotingStore.PromoteAliasHistory(ctx, sessionKey, rawScope, aliases); err != nil {
			logger.ErrorCF("session", "promote alias history failed", map[string]any{
				"session_key": sessionKey, "error": err.Error(),
			})
		}
	}
}

// GetSessionScope reads structured scope metadata for a session key or alias.
func (b *JSONLBackend) GetSessionScope(sessionKey string) *SessionScope {
	metaStore, ok := b.store.(metaAwareStore)
	if !ok {
		return nil
	}
	sessionKey = b.resolveSessionKey(sessionKey)
	meta, err := metaStore.GetSessionMeta(context.Background(), sessionKey)
	if err != nil {
		logger.ErrorCF("session", "get session metadata failed", map[string]any{
			"session_key": sessionKey, "error": err.Error(),
		})
		return nil
	}
	if len(meta.Scope) == 0 {
		return nil
	}
	var scope SessionScope
	if err := json.Unmarshal(meta.Scope, &scope); err != nil {
		logger.ErrorCF("session", "decode session scope failed", map[string]any{
			"session_key": sessionKey, "error": err.Error(),
		})
		return nil
	}
	return CloneScope(&scope)
}

func (b *JSONLBackend) AddMessage(sessionKey, role, content string) {
	sessionKey = b.resolveSessionKey(sessionKey)
	if err := b.store.AddMessage(context.Background(), sessionKey, role, content); err != nil {
		logger.ErrorCF("session", "add message failed", map[string]any{
			"session_key": sessionKey, "error": err.Error(),
		})
	}
}

func (b *JSONLBackend) AddFullMessage(sessionKey string, msg providers.Message) {
	sessionKey = b.resolveSessionKey(sessionKey)
	if err := b.store.AddFullMessage(context.Background(), sessionKey, msg); err != nil {
		logger.ErrorCF("session", "add full message failed", map[string]any{
			"session_key": sessionKey, "error": err.Error(),
		})
	}
}

func (b *JSONLBackend) GetHistory(key string) []providers.Message {
	key = b.resolveSessionKey(key)
	msgs, err := b.store.GetHistory(context.Background(), key)
	if err != nil {
		logger.ErrorCF("session", "get history failed", map[string]any{
			"session_key": key, "error": err.Error(),
		})
		return []providers.Message{}
	}
	return msgs
}

func (b *JSONLBackend) GetSummary(key string) string {
	key = b.resolveSessionKey(key)
	summary, err := b.store.GetSummary(context.Background(), key)
	if err != nil {
		logger.ErrorCF("session", "get summary failed", map[string]any{
			"session_key": key, "error": err.Error(),
		})
		return ""
	}
	return summary
}

func (b *JSONLBackend) SetSummary(key, summary string) {
	key = b.resolveSessionKey(key)
	if err := b.store.SetSummary(context.Background(), key, summary); err != nil {
		logger.ErrorCF("session", "set summary failed", map[string]any{
			"session_key": key, "error": err.Error(),
		})
	}
}

func (b *JSONLBackend) SetHistory(key string, history []providers.Message) {
	key = b.resolveSessionKey(key)
	if err := b.store.SetHistory(context.Background(), key, history); err != nil {
		logger.ErrorCF("session", "set history failed", map[string]any{
			"session_key": key, "error": err.Error(),
		})
	}
}

func (b *JSONLBackend) TruncateHistory(key string, keepLast int) {
	key = b.resolveSessionKey(key)
	if err := b.store.TruncateHistory(context.Background(), key, keepLast); err != nil {
		logger.ErrorCF("session", "truncate history failed", map[string]any{
			"session_key": key, "error": err.Error(),
		})
	}
}

// Save persists session state. Since the JSONL store fsyncs every write
// immediately, the data is already durable. Save runs compaction to reclaim
// space from logically truncated messages (no-op when there are none).
func (b *JSONLBackend) Save(key string) error {
	key = b.resolveSessionKey(key)
	return b.store.Compact(context.Background(), key)
}

// Close releases resources held by the underlying store.
func (b *JSONLBackend) Close() error {
	return b.store.Close()
}

// ListSessions returns all known session keys.
func (b *JSONLBackend) ListSessions() []string {
	return b.store.ListSessions()
}
