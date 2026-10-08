package seahorse

import "context"

// Test seams: helpers that only tests use. They live here, outside the
// production binary.

// AppendContextMessages bulk-appends messages to context_items.
func (s *Store) AppendContextMessages(ctx context.Context, convID int64, messageIDs []int64) error {
	items := make([]ContextItem, len(messageIDs))
	for i, id := range messageIDs {
		items[i] = ContextItem{ItemType: "message", MessageID: id}
	}
	return s.appendContextItems(ctx, convID, items)
}
