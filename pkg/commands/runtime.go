package commands

import (
	"context"
	"time"

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

	// ConfigSummary backs /config: the safe overview of the running config
	// (no secrets, no allow_from entries).
	ConfigSummary func() (string, error)
	// SetConfigKey backs /config set: it changes one chat-settable key in
	// the config file and returns the old and new values. Applying the
	// change is left to ReloadConfig.
	SetConfigKey func(key, value string) (oldValue, newValue string, err error)
	// CanMutate reports whether senderID on channel may change settings and
	// scheduled jobs from the chat.
	CanMutate func(channel, senderID string) bool
	// Cron returns the scheduler behind /cron, or nil when no scheduler runs
	// in this process (for example `kuromatsu agent`).
	Cron func() CronAPI
}

// CronAPI is what /cron needs from the scheduler. Job IDs passed to Enable,
// RunNow and History come from Resolve, which only finds jobs the given
// chat may see.
type CronAPI interface {
	// List returns the jobs visible from channel/chatID.
	List(channel, chatID string) []CronJobView
	// Enable switches a job on or off; switching on starts a new failure
	// streak.
	Enable(id string, on bool) error
	// RunNow marks an enabled job as due now.
	RunNow(id string) error
	// History returns up to n of the job's runs, most recent first.
	History(id string, n int) []CronRunView
	// Templates lists the automation templates /cron new accepts.
	Templates() []CronTemplateView
	// Create makes a job from template tpl, delivered to channel/to.
	Create(tpl string, params map[string]string, channel, to string) (CronJobView, error)
	// Resolve finds the one job visible from channel/chatID whose ID is id
	// or starts with id (at least 4 characters).
	Resolve(id, channel, chatID string) (string, error)
}

// CronJobView is a scheduled job as /cron shows it.
type CronJobView struct {
	ID       string
	Name     string
	Schedule string // human-readable schedule
	// State is "on", "paused" or "auto-disabled".
	State string
	// NextRun is the next run time; zero when none is scheduled.
	NextRun time.Time
	// Created is false when Create found an identical job instead of
	// making a new one.
	Created bool
}

// CronRunView is one recorded run of a job.
type CronRunView struct {
	Started  time.Time
	Duration time.Duration
	Status   string
	Error    string
}

// CronTemplateView is one automation template as /cron templates lists it.
type CronTemplateView struct {
	ID       string
	Title    string
	Required []string // names of the required parameters
	Optional []string // names of the optional parameters
}
