package seahorse

import (
	"sync"
	"testing"
)

func TestCompactionLazyInitConcurrent(t *testing.T) {
	engine := newTestEngine(t)
	start := make(chan struct{})
	var wg sync.WaitGroup
	for i := 0; i < 32; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			engine.initCompactionOnce()
			if engine.compaction == nil || engine.compaction.shutdownCtx == nil {
				t.Error("partially initialized compact engine")
			}
		}()
	}
	close(start)
	wg.Wait()
}
