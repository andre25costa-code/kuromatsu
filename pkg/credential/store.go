package credential

import "sync/atomic"

// SecureStore holds a passphrase in memory.
//
// Uses atomic.Pointer so reads and writes are lock-free.
// The passphrase is never written to disk; callers decide how to
// transport it outside this store (e.g., via cmd.Env or os.Environ).
type SecureStore struct {
	val atomic.Pointer[string]
}
