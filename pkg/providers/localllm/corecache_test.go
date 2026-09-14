package localllm

import "testing"

func TestPickCoreCacheSlot_PrefersFreeSlot(t *testing.T) {
	// Slot 0 occupied, slot 1 free -- must pick the free one regardless of
	// generation counters.
	got := pickCoreCacheSlot([]bool{true, false}, []uint64{100, 0})
	if got != 1 {
		t.Fatalf("pickCoreCacheSlot() = %d, want 1 (the free slot)", got)
	}
}

func TestPickCoreCacheSlot_FirstFreeSlotWhenSeveral(t *testing.T) {
	got := pickCoreCacheSlot([]bool{false, false}, []uint64{0, 0})
	if got != 0 {
		t.Fatalf("pickCoreCacheSlot() = %d, want 0 (first free slot)", got)
	}
}

func TestPickCoreCacheSlot_EvictsLeastRecentlyUsedWhenAllOccupied(t *testing.T) {
	// Both occupied: slot 0 has the lower (older) generation counter, so it
	// must be the one evicted.
	got := pickCoreCacheSlot([]bool{true, true}, []uint64{5, 9})
	if got != 0 {
		t.Fatalf("pickCoreCacheSlot() = %d, want 0 (lowest gen = least recently used)", got)
	}

	got = pickCoreCacheSlot([]bool{true, true}, []uint64{9, 5})
	if got != 1 {
		t.Fatalf("pickCoreCacheSlot() = %d, want 1 (lowest gen = least recently used)", got)
	}
}
