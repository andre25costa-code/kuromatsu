package agent

import (
	"sync"
	"testing"

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
