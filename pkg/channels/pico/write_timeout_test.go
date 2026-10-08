package pico

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"
)

// N15 (audit round 2): pico write_timeout was accepted and never read, and
// writes had no deadline. A client that stops reading filled the socket
// buffers and blocked the writer -- while it held writeMu, so every other send
// on that connection blocked too. A write now fails after writeTimeout.
func TestPicoConn_WriteGivesUpOnAStalledClient(t *testing.T) {
	release := make(chan struct{})
	upgrader := websocket.Upgrader{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer conn.Close()
		<-release // accept the connection, never read from it
	}))
	defer server.Close()
	defer close(release)

	conn, resp, err := websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(server.URL, "http"), nil)
	if err != nil {
		t.Fatalf("Dial: %v", err)
	}
	defer resp.Body.Close()
	defer conn.Close()

	pc := &picoConn{id: "stalled", conn: conn, writeTimeout: 200 * time.Millisecond}
	payload := map[string]string{"content": strings.Repeat("x", 256*1024)}

	done := make(chan error, 1)
	go func() {
		for i := 0; i < 400; i++ { // ~100 MB, far beyond the socket buffers
			if err := pc.writeJSON(payload); err != nil {
				done <- err
				return
			}
		}
		done <- nil
	}()
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("all writes to a client that never reads succeeded")
		}
	case <-time.After(10 * time.Second):
		t.Fatal("write still blocked on a stalled client")
	}
}
