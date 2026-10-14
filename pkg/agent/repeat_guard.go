package agent

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"

	"github.com/andre25costa-code/kuromatsu/pkg/tools"
)

// The repeated-tool-call guard stops a turn that keeps calling the same tool
// with the same arguments and keeps getting the same result back. A call is
// only counted as a repeat when its result matches too, so polling a tool
// whose output changes never accumulates a count.
const (
	// repeatedToolCallWarning is the tool result given in place of running
	// the call that reaches the limit. Arguments: tool name, previous count.
	repeatedToolCallWarning = "Repeated call blocked: `%s` was already called %d times in a row with identical arguments and returned the same result each time. Do not call it again with these arguments; use the previous result or answer the user."
	// repeatedToolCallResponse is the final answer of a turn ended by the
	// guard after the model ignored the warning. Argument: tool name.
	repeatedToolCallResponse = "I stopped because I kept calling `%s` with the same arguments and getting the same result. Please rephrase the request or give me more details."
	// repeatedToolCallSkipMessage answers each call skipped when the guard
	// ends the turn.
	repeatedToolCallSkipMessage = "Skipped due to repeated tool call."
	// repeatedToolCallReason is the Reason of the turn.tool.skipped event.
	repeatedToolCallReason = "repeated tool call"
)

type repeatGuardDecision int

const (
	repeatGuardAllow repeatGuardDecision = iota
	repeatGuardWarn
	repeatGuardStop
)

// toolCallSignature identifies a call by tool name and arguments.
// json.Marshal writes map keys in sorted order, so equal arguments always
// produce the same signature.
func toolCallSignature(name string, args map[string]any) string {
	argsJSON, err := json.Marshal(args)
	if err != nil {
		argsJSON = []byte(fmt.Sprintf("%v", args))
	}
	return name + "\x00" + string(argsJSON)
}

// toolResultHash identifies a result by its error flag and LLM content.
func toolResultHash(result *tools.ToolResult) string {
	if result == nil {
		return ""
	}
	sum := sha256.Sum256([]byte(fmt.Sprint(result.IsError) + "\x00" + result.ForLLM))
	return hex.EncodeToString(sum[:])
}

// checkRepeatedToolCall decides, before a call runs, whether it may run.
// limit is agents.defaults.max_repeated_tool_calls (0 = off). The call is
// held back when the same signature already ran limit-1 times in a row with
// the same result: the first time the model gets a warning, the next time
// the turn ends.
func checkRepeatedToolCall(exec *turnExecution, sig string, limit int) repeatGuardDecision {
	if exec == nil || limit <= 0 || sig != exec.lastToolSig || exec.toolRepeatCount < limit-1 {
		return repeatGuardAllow
	}
	if !exec.repeatGuardWarned {
		exec.repeatGuardWarned = true
		return repeatGuardWarn
	}
	return repeatGuardStop
}

// recordToolResult updates the repeat state after a call ran. A call with a
// new signature or a different result starts a new sequence.
func recordToolResult(exec *turnExecution, sig string, result *tools.ToolResult) {
	if exec == nil {
		return
	}
	hash := toolResultHash(result)
	if sig == exec.lastToolSig && hash == exec.lastResultHash {
		exec.toolRepeatCount++
		return
	}
	exec.lastToolSig = sig
	exec.lastResultHash = hash
	exec.toolRepeatCount = 1
	exec.repeatGuardWarned = false
}
