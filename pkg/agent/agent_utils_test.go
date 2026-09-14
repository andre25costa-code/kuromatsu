package agent

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/andre25costa-code/kuromatsu/pkg/runstate"
)

// S09/ADR-016 point 6: activeRequestsInc is the Inference hook every real
// LLM call (CallLLM/retryLLMCall) goes through -- it must refuse while
// Suspended, and must not touch activeReqCount when it does (nothing was
// actually started, so there is nothing for activeRequestsDec to undo).
func TestActiveRequestsInc_RefusedWhenSuspended(t *testing.T) {
	rs := runstate.New()
	rs.Suspend()
	defer rs.Resume()

	al := &AgentLoop{runstate: rs}
	al.activeReqCond = sync.NewCond(&al.activeReqMu)

	if ok := al.activeRequestsInc(); ok {
		t.Fatal("activeRequestsInc() = true while Suspended, want false")
	}
	al.activeReqMu.Lock()
	count := al.activeReqCount
	al.activeReqMu.Unlock()
	if count != 0 {
		t.Fatalf("activeReqCount = %d after a refused Inc, want 0", count)
	}
}

func TestActiveRequestsInc_SucceedsWhenNotSuspended(t *testing.T) {
	rs := runstate.New()
	al := &AgentLoop{runstate: rs}
	al.activeReqCond = sync.NewCond(&al.activeReqMu)

	if ok := al.activeRequestsInc(); !ok {
		t.Fatal("activeRequestsInc() = false on a non-Suspended engine, want true")
	}
	if !rs.Snapshot().Has(runstate.Inference) {
		t.Fatal("Inference bit not set after a successful activeRequestsInc")
	}
	al.activeRequestsDec()
	if rs.Snapshot().Has(runstate.Inference) {
		t.Fatal("Inference bit still set after activeRequestsDec")
	}
}

func TestInferMediaType(t *testing.T) {
	tests := []struct {
		name        string
		filename    string
		contentType string
		want        string
	}{
		{
			name:        "png content type",
			filename:    "diagram",
			contentType: "image/png",
			want:        "image",
		},
		{
			name:        "jpeg extension fallback",
			filename:    "photo.JPG",
			contentType: "",
			want:        "image",
		},
		{
			name:        "svg content type is file",
			filename:    "diagram",
			contentType: "image/svg+xml",
			want:        "file",
		},
		{
			name:        "svg content type with parameters is file",
			filename:    "diagram",
			contentType: "image/svg+xml; charset=utf-8",
			want:        "file",
		},
		{
			name:        "svg extension fallback is file",
			filename:    "diagram.SVG",
			contentType: "",
			want:        "file",
		},
		{
			name:        "audio content type",
			filename:    "voice",
			contentType: "audio/ogg",
			want:        "audio",
		},
		{
			name:        "ogg application content type",
			filename:    "voice.ogg",
			contentType: "application/ogg",
			want:        "audio",
		},
		{
			name:        "video extension fallback",
			filename:    "clip.MP4",
			contentType: "",
			want:        "video",
		},
		{
			name:        "unknown type",
			filename:    "archive.bin",
			contentType: "application/octet-stream",
			want:        "file",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := inferMediaType(tt.filename, tt.contentType)
			if got != tt.want {
				t.Fatalf("inferMediaType(%q, %q) = %q, want %q", tt.filename, tt.contentType, got, tt.want)
			}
		})
	}
}

// A.3 (Trilho G): waitForRunstateResume gives a runstate.ErrBusy suspension
// its own wait budget, distinct from the generic max_llm_retries backoff.

func TestWaitForRunstateResume_ReturnsTrueOnResumeWithinBudget(t *testing.T) {
	rs := runstate.New()
	rs.Suspend()

	go func() {
		time.Sleep(30 * time.Millisecond)
		rs.Resume()
	}()

	start := time.Now()
	got := waitForRunstateResume(context.Background(), rs, time.Second)
	elapsed := time.Since(start)

	if !got {
		t.Fatal("waitForRunstateResume() = false, want true after Resume() within budget")
	}
	if elapsed > 500*time.Millisecond {
		t.Fatalf("waitForRunstateResume() took %v, want close to the ~30ms Resume() delay", elapsed)
	}
}

func TestWaitForRunstateResume_TimesOutBeyondGenericBudget(t *testing.T) {
	rs := runstate.New()
	rs.Suspend()
	defer rs.Resume()

	start := time.Now()
	got := waitForRunstateResume(context.Background(), rs, 50*time.Millisecond)
	elapsed := time.Since(start)

	if got {
		t.Fatal("waitForRunstateResume() = true, want false -- engine never resumed")
	}
	if elapsed < 50*time.Millisecond {
		t.Fatalf("waitForRunstateResume() returned after %v, want it to have waited out the 50ms budget", elapsed)
	}
}

func TestWaitForRunstateResume_NilEngineOrZeroBudgetIsNoOp(t *testing.T) {
	if got := waitForRunstateResume(context.Background(), nil, time.Second); got {
		t.Fatal("waitForRunstateResume(nil engine) = true, want false (no-op)")
	}
	rs := runstate.New()
	if got := waitForRunstateResume(context.Background(), rs, 0); got {
		t.Fatal("waitForRunstateResume(max=0) = true, want false (no-op, byte-identical to before this existed)")
	}
}

func TestWaitForRunstateResume_ReturnsFalseWhenCtxCanceled(t *testing.T) {
	rs := runstate.New()
	rs.Suspend()
	defer rs.Resume()

	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(20 * time.Millisecond)
		cancel()
	}()

	start := time.Now()
	got := waitForRunstateResume(ctx, rs, 5*time.Second)
	elapsed := time.Since(start)

	if got {
		t.Fatal("waitForRunstateResume() = true, want false -- ctx was canceled, engine never resumed")
	}
	if elapsed > time.Second {
		t.Fatalf("waitForRunstateResume() took %v, want it to return promptly on ctx cancellation, not wait out the 5s budget", elapsed)
	}
}
