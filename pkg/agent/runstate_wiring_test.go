package agent

import (
	"testing"

	"github.com/andre25costa-code/kuromatsu/pkg/config"
	"github.com/andre25costa-code/kuromatsu/pkg/runstate"
)

func TestRunstateEngineForConfig_DisabledOrNilReturnsNil(t *testing.T) {
	if got := runstateEngineForConfig(nil); got != nil {
		t.Fatalf("runstateEngineForConfig(nil) = %v, want nil", got)
	}
	if got := runstateEngineForConfig(&config.Config{}); got != nil {
		t.Fatalf("runstateEngineForConfig(disabled) = %v, want nil", got)
	}
}

func TestRunstateEngineForConfig_EnabledReturnsTheProcessWideSingleton(t *testing.T) {
	cfg := &config.Config{Runstate: config.RunstateConfig{Enabled: true}}
	got := runstateEngineForConfig(cfg)
	if got == nil {
		t.Fatal("runstateEngineForConfig(enabled) = nil, want runstate.Default()")
	}
	if got != runstate.Default() {
		t.Fatal("runstateEngineForConfig(enabled) returned an instance other than runstate.Default() -- a reload would silently stop sharing state with gateway.go's publishers")
	}
}

func TestAgentLoop_RsEnter_NilRunstateIsANoOp(t *testing.T) {
	al := &AgentLoop{} // runstate left nil, as it is with runstate.enabled=false
	release := al.rsEnter(runstate.ToolExec)
	release() // must not panic
	release() // must be safe to call twice, like every other Enter() release
}

func TestAgentLoop_RsEnter_RealEngineIncsAndDecs(t *testing.T) {
	rs := runstate.New()
	al := &AgentLoop{runstate: rs}

	release := al.rsEnter(runstate.ToolExec)
	if !rs.Snapshot().Has(runstate.ToolExec) {
		t.Fatal("ToolExec not active after rsEnter")
	}
	release()
	if rs.Snapshot().Has(runstate.ToolExec) {
		t.Fatal("ToolExec still active after release")
	}
}

func TestAgentLoop_RsTryEnter_NilRunstateIsANoOp(t *testing.T) {
	al := &AgentLoop{} // runstate left nil, as it is with runstate.enabled=false
	release, ok := al.rsTryEnter(runstate.Inference)
	if !ok {
		t.Fatal("rsTryEnter with nil runstate refused, want always-succeeds passthrough")
	}
	release() // must not panic
	release() // must be safe to call twice
}

func TestAgentLoop_RsTryEnter_RealEngineIncsAndDecs(t *testing.T) {
	rs := runstate.New()
	al := &AgentLoop{runstate: rs}

	release, ok := al.rsTryEnter(runstate.ToolExec)
	if !ok {
		t.Fatal("rsTryEnter refused on a fresh, non-Suspended engine")
	}
	if !rs.Snapshot().Has(runstate.ToolExec) {
		t.Fatal("ToolExec not active after rsTryEnter")
	}
	release()
	if rs.Snapshot().Has(runstate.ToolExec) {
		t.Fatal("ToolExec still active after release")
	}
}

// S09/ADR-016 point 6: the invariant this whole primitive exists for --
// rsTryEnter must refuse Inference/ToolExec while Suspended, unlike
// rsEnter (which Reflex alone uses and which never refuses).
func TestAgentLoop_RsTryEnter_RefusedWhenSuspended(t *testing.T) {
	rs := runstate.New()
	rs.Suspend()
	defer rs.Resume()
	al := &AgentLoop{runstate: rs}

	if _, ok := al.rsTryEnter(runstate.Inference); ok {
		t.Fatal("rsTryEnter(Inference) succeeded while Suspended, want refused")
	}
	if _, ok := al.rsTryEnter(runstate.ToolExec); ok {
		t.Fatal("rsTryEnter(ToolExec) succeeded while Suspended, want refused")
	}
}

func TestAgentLoop_RsSnapshot_ReadsUnderLock(t *testing.T) {
	rs := runstate.New()
	al := &AgentLoop{runstate: rs}
	if al.rsSnapshot() != rs {
		t.Fatal("rsSnapshot() did not return the wired engine")
	}
}
