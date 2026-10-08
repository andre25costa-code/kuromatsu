package agent

import (
	runtimeevents "github.com/andre25costa-code/kuromatsu/pkg/events"
)

func hookMetaFromRuntimeEvent(evt runtimeevents.Event) HookMeta {
	meta := HookMeta{
		AgentID:      evt.Scope.AgentID,
		TurnID:       evt.Scope.TurnID,
		ParentTurnID: evt.Correlation.ParentTurnID,
		SessionKey:   evt.Scope.SessionKey,
		TracePath:    evt.Correlation.TraceID,
	}
	if evt.Attrs != nil {
		if source, ok := evt.Attrs["agent_source"].(string); ok {
			meta.Source = source
		}
		if iteration, ok := evt.Attrs["iteration"].(int); ok {
			meta.Iteration = iteration
		}
	}
	return meta
}
