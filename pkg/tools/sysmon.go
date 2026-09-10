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
}

// NewSysmonTool creates a SysmonTool. allowDestructive gates the "kill" and
// "renice" proc actions; read-only actions (mem/top/load/disk) always work.
func NewSysmonTool(allowDestructive bool) *SysmonTool {
	return &SysmonTool{allowDestructive: allowDestructive}
}

func (t *SysmonTool) Name() string {
	return "sysmon"
}

func (t *SysmonTool) Description() string {
	return "Inspect the host machine's resource usage: memory (respects the " +
		"container/cgroup limit when running in Docker), the top N processes by " +
		"RSS, load average, and disk usage. Can also send a signal to a process " +
		"(action=proc, op=kill) or change its scheduling priority (op=renice), " +
		"both of which are disabled unless explicitly allowed in config."
}

func (t *SysmonTool) Parameters() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"action": map[string]any{
				"type":        "string",
				"description": "What to inspect or do.",
				"enum":        []string{"mem", "top", "load", "disk", "proc"},
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
	default:
		return ErrorResult(fmt.Sprintf("unknown action %q; must be one of: mem, top, load, disk, proc", action))
	}
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
