package tools

import (
	"context"
	"fmt"
	"strconv"
	"strings"
)

// SysmonTool gives the agent deterministic visibility into (and limited
// control over) the machine it runs on -- memory, top processes, load, and
// disk usage -- in place of the Sipeed hardware tools it replaces (i2c/spi/
// serial, ADR-007, FR-011). Destructive actions (kill/renice) are gated by
// AllowDestructive and denied by default (BR-007/AC-011-2).
type SysmonTool struct {
	allowDestructive bool

	// stateReader is the Trilho C runstate bridge (ADR-016 point 5): when
	// set, action=state reports the injected snapshot. nil (the default,
	// when WithStateReader is never called) makes action=state report a
	// clear "not available" message instead of erroring -- sysmon's other
	// actions are completely unaffected either way (FR-011 unchanged).
	stateReader func() (bits uint32, names []string)
}

// NewSysmonTool creates a SysmonTool. allowDestructive gates the "kill" and
// "renice" proc actions; read-only actions (mem/top/load/disk/state) always
// work.
func NewSysmonTool(allowDestructive bool) *SysmonTool {
	return &SysmonTool{allowDestructive: allowDestructive}
}

// WithStateReader injects the runstate snapshot reader action=state
// reports (instance.go, C2). Returns the receiver so callers can chain it
// onto NewSysmonTool the same way NewWriteFileTool.SetAlternativeTools is
// chained elsewhere in this package.
func (t *SysmonTool) WithStateReader(reader func() (bits uint32, names []string)) *SysmonTool {
	t.stateReader = reader
	return t
}

func (t *SysmonTool) Name() string {
	return "sysmon"
}

func (t *SysmonTool) Description() string {
	return "Inspect the host machine's resource usage: memory (respects the " +
		"container/cgroup limit when running in Docker), the top N processes by " +
		"RSS, load average, and disk usage. Can also send a signal to a process " +
		"(action=proc, op=kill) or change its scheduling priority (op=renice), " +
		"both of which are disabled unless explicitly allowed in config. " +
		"action=state reports what the agent is doing right now (runstate)."
}

func (t *SysmonTool) Parameters() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"action": map[string]any{
				"type":        "string",
				"description": "What to inspect or do.",
				"enum":        []string{"mem", "top", "load", "disk", "proc", "state"},
			},
			"limit": map[string]any{
				"type":        "integer",
				"description": "For action=top: how many processes to return, by RSS descending. Default 10.",
			},
			"pid": map[string]any{
				"type":        "integer",
				"description": "For action=proc: the target process ID.",
			},
			"op": map[string]any{
				"type":        "string",
				"description": "For action=proc: \"kill\" sends SIGKILL, \"renice\" applies niceness.",
				"enum":        []string{"kill", "renice"},
			},
			"niceness": map[string]any{
				"type":        "integer",
				"description": "For action=proc, op=renice: target niceness (-20 to 19).",
			},
		},
		"required": []string{"action"},
	}
}

func (t *SysmonTool) Execute(_ context.Context, args map[string]any) *ToolResult {
	action, _ := args["action"].(string)
	switch strings.TrimSpace(action) {
	case "mem":
		return sysmonMemResult()
	case "top":
		limit := 10
		if v, ok := args["limit"]; ok {
			n, err := toInt(v)
			if err != nil || n <= 0 {
				return ErrorResult("limit must be a positive integer")
			}
			limit = n
		}
		return sysmonTopResult(limit)
	case "load":
		return sysmonLoadResult()
	case "disk":
		return sysmonDiskResult()
	case "proc":
		return t.executeProc(args)
	case "state":
		return t.executeState()
	default:
		return ErrorResult(fmt.Sprintf("unknown action %q; must be one of: mem, top, load, disk, proc, state", action))
	}
}

// executeState reports the runstate snapshot injected via WithStateReader,
// or a clear "not available" result when no reader was ever injected
// (runstate.enabled=false, the default -- FR-011 stays otherwise
// unaffected).
func (t *SysmonTool) executeState() *ToolResult {
	if t.stateReader == nil {
		return NewToolResult("runstate is not enabled (runstate.enabled=false)")
	}
	bits, names := t.stateReader()
	label := "idle"
	if len(names) > 0 {
		label = strings.Join(names, ",")
	}
	return NewToolResult(fmt.Sprintf("bits=%d names=%s", bits, label))
}

func (t *SysmonTool) executeProc(args map[string]any) *ToolResult {
	pidVal, ok := args["pid"]
	if !ok {
		return ErrorResult("proc requires \"pid\"")
	}
	pid, err := toInt(pidVal)
	if err != nil || pid <= 0 {
		return ErrorResult("pid must be a positive integer")
	}

	op, _ := args["op"].(string)
	switch strings.TrimSpace(op) {
	case "kill":
		if !t.allowDestructive {
			return ErrorResult("ação desabilitada: defina tools.sysmon.allow_destructive=true no config para permitir proc kill")
		}
		return sysmonKillResult(pid)
	case "renice":
		if !t.allowDestructive {
			return ErrorResult("ação desabilitada: defina tools.sysmon.allow_destructive=true no config para permitir proc renice")
		}
		niceness := 0
		if v, ok := args["niceness"]; ok {
			n, err := toInt(v)
			if err != nil || n < -20 || n > 19 {
				return ErrorResult("niceness must be an integer between -20 and 19")
			}
			niceness = n
		}
		return sysmonReniceResult(pid, niceness)
	default:
		return ErrorResult("proc requires \"op\": kill or renice")
	}
}

func toInt(v any) (int, error) {
	switch n := v.(type) {
	case int:
		return n, nil
	case int64:
		return int(n), nil
	case float64:
		return int(n), nil
	case string:
		return strconv.Atoi(strings.TrimSpace(n))
	default:
		return 0, fmt.Errorf("unsupported numeric type %T", v)
	}
}
