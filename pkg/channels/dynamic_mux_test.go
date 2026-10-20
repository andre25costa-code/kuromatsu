package channels

import (
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"
)

func TestDynamicServeMuxExactMatch(t *testing.T) {
	dm := newDynamicServeMux()
	dm.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	rec := httptest.NewRecorder()
	dm.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/health", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
}

func TestDynamicServeMuxSubtreePrefixMatch(t *testing.T) {
	dm := newDynamicServeMux()
	dm.HandleFunc("/api/", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusCreated)
	})

	for _, path := range []string{"/api/", "/api/v1", "/api/v1/resource"} {
		rec := httptest.NewRecorder()
		dm.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
		if rec.Code != http.StatusCreated {
			t.Fatalf("path %q: expected 201, got %d", path, rec.Code)
		}
	}
}

func TestDynamicServeMuxExactOverPrefix(t *testing.T) {
	dm := newDynamicServeMux()
	dm.HandleFunc("/api", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	dm.HandleFunc("/api/", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusCreated)
	})

	// Exact match wins
	rec := httptest.NewRecorder()
	dm.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("exact match: expected 200, got %d", rec.Code)
	}

	// Prefix match for sub-paths
	rec = httptest.NewRecorder()
	dm.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v1", nil))
	if rec.Code != http.StatusCreated {
		t.Fatalf("prefix match: expected 201, got %d", rec.Code)
	}
}

func TestDynamicServeMuxLongestPrefixWins(t *testing.T) {
	dm := newDynamicServeMux()
	dm.HandleFunc("/a/", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	dm.HandleFunc("/a/b/", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusAccepted)
	})

	rec := httptest.NewRecorder()
	dm.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/a/b/c", nil))
	if rec.Code != http.StatusAccepted {
		t.Fatalf("longest prefix: expected 202, got %d", rec.Code)
	}
}

func TestDynamicServeMuxNotFound(t *testing.T) {
	dm := newDynamicServeMux()
	rec := httptest.NewRecorder()
	dm.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/nonexistent", nil))
	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", rec.Code)
	}
}

func TestDynamicServeMuxUnhandle(t *testing.T) {
	dm := newDynamicServeMux()
	dm.HandleFunc("/test", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	// Verify it works before removal
	rec := httptest.NewRecorder()
	dm.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/test", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("before unhandle: expected 200, got %d", rec.Code)
	}

	// Remove and verify 404
	dm.Unhandle("/test")
	rec = httptest.NewRecorder()
	dm.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/test", nil))
	if rec.Code != http.StatusNotFound {
		t.Fatalf("after unhandle: expected 404, got %d", rec.Code)
	}
}

func TestDynamicServeMuxConcurrent(t *testing.T) {
	dm := newDynamicServeMux()
	dm.HandleFunc("/static", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	var wg sync.WaitGroup
	const goroutines = 50

	// Concurrent Handle/Unhandle
	for i := 0; i < goroutines; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			pattern := "/concurrent"
			if i%2 == 0 {
				dm.HandleFunc(pattern, func(w http.ResponseWriter, r *http.Request) {
					w.WriteHeader(http.StatusAccepted)
				})
			} else {
				dm.Unhandle(pattern)
			}
		}(i)
	}

	// Concurrent ServeHTTP
	for i := 0; i < goroutines; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			rec := httptest.NewRecorder()
			dm.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/static", nil))
			// Should not panic; result is either 200 or 404
			_ = rec.Code
		}()
	}

	wg.Wait()
}

func TestDynamicServeMuxHandleUsesHandler(t *testing.T) {
	dm := newDynamicServeMux()

	var called bool
	dm.Handle("/handler", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
	}))

	rec := httptest.NewRecorder()
	dm.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/handler", nil))
	if !called {
		t.Fatal("handler was not called")
	}
}

// A long-running handler must not block registrations (or the requests
// queued behind a waiting registration).
func TestDynamicServeMuxLongHandlerDoesNotBlockHandle(t *testing.T) {
	dm := newDynamicServeMux()
	entered := make(chan struct{})
	release := make(chan struct{})
	dm.HandleFunc("/slow", func(w http.ResponseWriter, r *http.Request) {
		close(entered)
		<-release
	})

	done := make(chan struct{})
	go func() {
		dm.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/slow", nil))
		close(done)
	}()
	<-entered

	registered := make(chan struct{})
	go func() {
		dm.HandleFunc("/new", func(w http.ResponseWriter, r *http.Request) {})
		close(registered)
	}()
	select {
	case <-registered:
	case <-time.After(2 * time.Second):
		t.Fatal("Handle blocked while another handler was running")
	}
	close(release)
	<-done
}

func TestManagerHandleHTTP_RequiresSetup(t *testing.T) {
	m := &Manager{}
	if err := m.HandleHTTP("/ui/", http.NotFoundHandler()); err == nil {
		t.Fatal("HandleHTTP before SetupHTTPServerListeners must fail")
	}
	m.SetupHTTPServerListeners(nil, "127.0.0.1:0", nil)
	if err := m.HandleHTTP("/ui/", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusTeapot)
	})); err != nil {
		t.Fatal(err)
	}
	rec := httptest.NewRecorder()
	m.mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/ui/x", nil))
	if rec.Code != http.StatusTeapot {
		t.Fatalf("got %d, want 418", rec.Code)
	}
}
