package commands

import (
	"context"

	"github.com/andre25costa-code/kuromatsu/pkg/config"
)

type MCPServerInfo struct {
	Name      string
	Enabled   bool
	Deferred  bool
	Connected bool
	ToolCount int
}

type MCPToolParameterInfo struct {
	Name        string
	Type        string
	Description string
	Required    bool
}

type MCPToolInfo struct {
	Name        string
	Description string
	Parameters  []MCPToolParameterInfo
}

// ContextStats describes current session context window usage.
type ContextStats struct {
	UsedTokens        int
	TotalTokens       int // model context window
	HistoryTokens     int // history-only tokens (what maybeSummarize checks)
	CompressAtTokens  int // hard budget compression threshold
	SummarizeAtTokens int // soft summarization trigger
	UsedPercent       int // 0-100
	MessageCount      int
}

// StopResult describes the outcome of a stop request for the current session.
type StopResult struct {
	Stopped  bool
	TaskName string
}

// Runtime provides runtime dependencies to command handlers. It is constructed
// per-request by the agent loop so that per-request state (like session scope)
// can coexist with long-lived callbacks (like GetModelInfo).
type Runtime struct {
	Config             *config.Config
	GetModelInfo       func() (name, provider string)
	AskSideQuestion    func(ctx context.Context, question string) (string, error)
	ListAgentIDs       func() []string
	ListDefinitions    func() []Definition
	ListSkillNames     func() []string
	ListMCPServers     func(ctx context.Context) []MCPServerInfo
	ListMCPTools       func(ctx context.Context, serverName string) ([]MCPToolInfo, error)
	GetEnabledChannels func() []string
	GetActiveTurn      func() any // Returning any to avoid circular dependency with agent package
	GetContextStats    func() *ContextStats
	SwitchModel        func(value string) (oldModel string, err error)
	SwitchChannel      func(value string) error
	ClearHistory       func() error
	ReloadConfig       func() error
	StopActiveTurn     func() (StopResult, error)

	// ListFocusWindows and GetFocusState back /foco's informational,
	// no-args form (ADR-014/FR-014). The actual window-changing forms
	// ("/foco <window>", "/foco <window> <message>", "/foco auto|off")
	// are special-cased in pkg/agent/agent_command.go's
	// applyExplicitFocusCommand (mirroring /use) since they need to
	// mutate the in-flight processOptions, which a Runtime handler can't
	// do — these two fields exist for introspection/testability and any
	// future generic listing.
	ListFocusWindows func() []string
	GetFocusState    func() (current string, sticky bool)

	// QueryStats backs /stats [window] [hours] (ADR-017/FR-019, Trilho C
	// C4): a pre-formatted report string, or an error whose message is
	// itself safe to show the user (e.g. "telemetry is disabled") rather
	// than a raw internal error. Always non-nil in production wiring
	// (pkg/agent/agent_command.go) -- the nil-vs-disabled distinction is
	// handled inside the closure, not by this field being absent, so
	// cmd_stats.go's Handler can call it unconditionally.
	QueryStats func(ctx context.Context, window string, hours int) (string, error)
}
