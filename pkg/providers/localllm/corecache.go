// Package localllm -- corecache.go has no build tag (unlike engine_cgo.go /
// engine_stub.go), same rationale as prefix.go: the slot-selection decision
// for B2 (window-core KV parking, ADR-015 point 8) is plain, cgo-free
// arithmetic and is testable on any machine, including this one (Windows,
// no C toolchain).
package localllm

// pickCoreCacheSlot decides which of the fixed coreCacheSlots (see
// engine_cgo.go) parked-core slots a fresh core should occupy: the first
// free slot (valid[i] == false), or -- once every slot is occupied -- the
// least-recently-used one (lowest gens[i]). valid and gens must have equal,
// non-zero length. The caller (engine_cgo.go) is responsible for actually
// moving KV cells for whichever slot this returns; this function only
// picks the index.
func pickCoreCacheSlot(valid []bool, gens []uint64) int {
	for i, v := range valid {
		if !v {
			return i
		}
	}
	slot := 0
	for i := 1; i < len(gens); i++ {
		if gens[i] < gens[slot] {
			slot = i
		}
	}
	return slot
}
