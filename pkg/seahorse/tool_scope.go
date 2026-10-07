package seahorse

import (
	"context"
	"fmt"

	"github.com/andre25costa-code/kuromatsu/pkg/tools"
)

// Tool calls must use the session attached by the agent runtime. A model
// argument must never grant access to another conversation in the shared DB.
func (r *RetrievalEngine) toolConversation(ctx context.Context) (int64, error) {
	key := tools.ToolSessionKey(ctx)
	if key == "" {
		return 0, fmt.Errorf("memory access requires a session context")
	}
	conv, err := r.store.GetConversationBySessionKey(ctx, key)
	if err != nil {
		return 0, err
	}
	if conv == nil {
		return 0, fmt.Errorf("no memory for the current session")
	}
	return conv.ConversationID, nil
}
