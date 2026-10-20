// PicoClaw - Ultra-lightweight personal AI agent

package agent

import (
	"context"
	"errors"

	"github.com/andre25costa-code/kuromatsu/pkg/bus"
	"github.com/andre25costa-code/kuromatsu/pkg/logger"
)

func (al *AgentLoop) processMessageSync(ctx context.Context, msg bus.InboundMessage) {
	if al.channelManager != nil {
		defer al.channelManager.InvokeTypingStop(msg.Channel, msg.ChatID)
	}

	response, err := al.processMessage(ctx, msg)
	al.publishResponseOrError(ctx, msg.Channel, msg.ChatID, msg.SessionKey, response, err)
}

func (al *AgentLoop) runTurnWithSteering(ctx context.Context, initialMsg bus.InboundMessage) {
	// One user-turn deadline covers the initial message and the drain of
	// queued steering; replies and errors are published on ctx.
	turnCtx, cancel := al.userTurnContext(ctx)
	defer cancel()

	// Process the initial message
	response, err := al.processMessage(turnCtx, initialMsg)
	err = al.userTurnError(ctx, turnCtx, err)
	overrunReported := errors.Is(err, context.DeadlineExceeded)
	if err != nil {
		if !al.maybePublishError(ctx, initialMsg.Channel, initialMsg.ChatID, initialMsg.SessionKey, err) {
			return // context canceled
		}
		response = ""
	}
	finalResponse := response

	// Build continuation target
	target, targetErr := al.buildContinuationTarget(initialMsg)
	if targetErr != nil {
		logger.WarnCF("agent", "Failed to build steering continuation target",
			map[string]any{
				"channel": initialMsg.Channel,
				"error":   targetErr.Error(),
			})
		return
	}
	if target == nil {
		// System message or non-routable, response already published
		return
	}

	continued, continueErr := al.drainQueuedSteeringContinuations(turnCtx, target)
	continueErr = al.userTurnError(ctx, turnCtx, continueErr)
	drainOverrun := false
	if continueErr != nil {
		logger.WarnCF("agent", "Failed to continue queued steering",
			map[string]any{
				"channel": target.Channel,
				"chat_id": target.ChatID,
				"error":   continueErr.Error(),
			})
		drainOverrun = errors.Is(continueErr, context.DeadlineExceeded) && !overrunReported
	} else if continued != "" {
		finalResponse = continued
	}

	// Publish final response
	if finalResponse != "" {
		al.PublishResponseIfNeeded(ctx, target.Channel, target.ChatID, target.SessionKey, finalResponse)
	}
	// The deadline expired while a queued message was being answered: the
	// user still gets the overrun error for it (once per turn).
	if drainOverrun {
		al.maybePublishError(ctx, target.Channel, target.ChatID, target.SessionKey, continueErr)
	}
}

// drainAfterPendingStop answers the steering messages queued behind a turn
// that was stopped (/stop) before its worker started. Each drained message
// is a user turn, so the drain runs under one user-turn deadline; the reply
// and any error are published on ctx.
func (al *AgentLoop) drainAfterPendingStop(ctx context.Context, target *continuationTarget) {
	turnCtx, cancel := al.userTurnContext(ctx)
	defer cancel()

	continued, err := al.drainQueuedSteeringContinuations(turnCtx, target)
	if err = al.userTurnError(ctx, turnCtx, err); err != nil {
		al.maybePublishError(ctx, target.Channel, target.ChatID, target.SessionKey, err)
		return
	}
	if continued != "" {
		al.PublishResponseIfNeeded(ctx, target.Channel, target.ChatID, target.SessionKey, continued)
	}
}

func (al *AgentLoop) drainQueuedSteeringContinuations(
	ctx context.Context,
	target *continuationTarget,
) (string, error) {
	if target == nil {
		return "", nil
	}

	finalResponse := ""
	for al.pendingSteeringCountForScope(target.SessionKey) > 0 {
		if err := ctx.Err(); err != nil {
			return finalResponse, err
		}

		logger.InfoCF("agent", "Continuing queued steering after turn end",
			map[string]any{
				"channel":     target.Channel,
				"chat_id":     target.ChatID,
				"session_key": target.SessionKey,
				"queue_depth": al.pendingSteeringCountForScope(target.SessionKey),
			})

		continued, continueErr := al.Continue(ctx, target.SessionKey, target.Channel, target.ChatID)
		if continueErr != nil {
			return finalResponse, continueErr
		}
		if continued == "" {
			break
		}
		finalResponse = continued
	}

	return finalResponse, nil
}

func (al *AgentLoop) resolveSteeringTarget(msg bus.InboundMessage) (string, string, bool) {
	if msg.Channel == "system" {
		return "", "", false
	}

	route, agent, err := al.resolveMessageRoute(msg)
	if err != nil || agent == nil {
		return "", "", false
	}
	allocation := al.allocateRouteSession(route, msg)

	return resolveScopeKey(allocation.SessionKey, msg.SessionKey), agent.ID, true
}
