package agent

import (
	"testing"
	"time"

	"github.com/andre25costa-code/kuromatsu/pkg/bus"
	"github.com/andre25costa-code/kuromatsu/pkg/config"
	"github.com/andre25costa-code/kuromatsu/pkg/providers"
	"github.com/andre25costa-code/kuromatsu/pkg/tools"
)

// Test seams: helpers that only tests use. They live here, outside the
// production binary, after the dead-code cleanup moved them out of the
// package sources.

func (r *mcpRuntime) hasManager() bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.manager != nil
}

func NamedHook(name string, hook any) HookRegistration {
	return HookRegistration{
		Name:   name,
		Source: HookSourceInProcess,
		Hook:   hook,
	}
}

func newRuntimeEventLogger(cfg *config.Config) *runtimeEventLogger {
	logCfg := config.EffectiveEventLoggingConfig(cfg)
	if !logCfg.Enabled {
		return nil
	}
	return newRuntimeEventLoggerFromConfig(logCfg)
}

// push enqueues a steering message in the legacy fallback scope.
func (sq *steeringQueue) push(msg providers.Message) error {
	return sq.pushScope(manualSteeringScope, msg)
}

// dequeue removes and returns pending steering messages from the legacy
// fallback scope according to the configured mode.
func (sq *steeringQueue) dequeue() []providers.Message {
	return sq.dequeueScope(manualSteeringScope)
}

// len returns the number of queued messages across all scopes.
func (sq *steeringQueue) len() int {
	sq.mu.Lock()
	defer sq.mu.Unlock()

	total := 0
	for _, queue := range sq.queues {
		total += len(queue)
	}
	return total
}

// dequeueSteeringMessages is the internal method called by the agent loop
// to poll for steering messages in the legacy fallback scope.
func (al *AgentLoop) dequeueSteeringMessages() []providers.Message {
	if al.steering == nil {
		return nil
	}
	return al.steering.dequeue()
}

// dequeuePendingSubTurnResults polls the SubTurn result channel for the given
// session and returns all available results without blocking.
// Returns nil if no active turn state exists for this session.
func (al *AgentLoop) dequeuePendingSubTurnResults(sessionKey string) []*tools.ToolResult {
	tsInterface, ok := al.activeTurnStates.Load(sessionKey)
	if !ok {
		return nil
	}
	ts, ok := tsInterface.(*turnState)
	if !ok {
		return nil
	}

	var results []*tools.ToolResult
	for {
		select {
		case result, ok := <-ts.pendingResults:
			if !ok {
				return results
			}
			if result != nil {
				results = append(results, result)
			}
		default:
			return results
		}
	}
}

// Steer enqueues a user message to be injected into the currently running
// agent loop. The message will be picked up after the current tool finishes
// executing, causing any remaining tool calls in the batch to be skipped.
func (al *AgentLoop) Steer(msg providers.Message) error {
	scope := ""
	agentID := ""
	if ts := al.getAnyActiveTurnState(); ts != nil {
		scope = ts.sessionKey
		agentID = ts.agentID
	}
	return al.enqueueSteeringMessage(scope, agentID, msg)
}

// InjectFollowUp enqueues a message to be automatically processed after the current
// turn completes. Unlike Steer(), which interrupts the current execution, InjectFollowUp
// waits for the current turn to finish naturally before processing the message.
//
// This is useful for:
// - Automated workflows that need to chain multiple turns
// - Background tasks that should run after the main task completes
// - Scheduled follow-up actions
//
// The message will be processed via Continue() when the agent becomes idle.
func (al *AgentLoop) InjectFollowUp(msg providers.Message) error {
	// InjectFollowUp uses the same steering queue mechanism as Steer(),
	// but the semantic difference is in when it's called:
	// - Steer() is called during active execution to interrupt
	// - InjectFollowUp() is called when planning future work
	//
	// Both end up in the same queue and are processed by Continue()
	// when the agent is idle.
	return al.Steer(msg)
}

// InjectSteering is an alias for Steer() to match the design document naming.
// It injects a steering message into the currently running agent loop.
func (al *AgentLoop) InjectSteering(msg providers.Message) error {
	return al.Steer(msg)
}

// waitForQueuedSteering waits until the loop has put at least want messages
// for msg's session in the steering queue. A test that publishes a "late"
// message and then releases the running turn must wait for this first:
// otherwise the turn can end before the loop reads the message from the bus,
// and the message correctly becomes a turn of its own.
func waitForQueuedSteering(t *testing.T, al *AgentLoop, msg bus.InboundMessage, want int) {
	t.Helper()
	scope, _, ok := al.resolveSteeringTarget(msg)
	if !ok {
		t.Fatalf("no steering target for %+v", msg.Context)
	}
	deadline := time.Now().Add(2 * time.Second)
	for al.pendingSteeringCountForScope(scope) < want {
		if time.Now().After(deadline) {
			t.Fatalf("timeout waiting for %d message(s) in the steering queue, have %d",
				want, al.pendingSteeringCountForScope(scope))
		}
		time.Sleep(5 * time.Millisecond)
	}
}
