package webui

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"path"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/andre25costa-code/kuromatsu/pkg/config"
	"github.com/andre25costa-code/kuromatsu/pkg/logger"
)

const (
	sessionCookie   = "km_session"
	csrfCookiePref  = "km_csrf_"
	csrfHeader      = "X-Csrf-Token"
	sessionTTL      = 12 * time.Hour
	loginWindow     = time.Minute
	loginMaxFails   = 5
	maxJSONBody     = 64 << 10
	maxLoginBody    = 4 << 10
	apiPrefix       = "/api/ui/"
	uiPrefix        = "/ui/"
	contentSecurity = "default-src 'self'; script-src 'self'; style-src 'self'; img-src 'self' data:; " +
		"connect-src 'self'; frame-ancestors 'none'"
)

// proxyHeaders mark a request forwarded by a proxy or tunnel on this host:
// it arrives from 127.0.0.1 but comes from elsewhere, so it is refused as
// remote.
var proxyHeaders = []string{
	"X-Forwarded-For", "X-Forwarded-Host", "X-Forwarded-Proto", "Forwarded",
	"X-Real-IP", "CF-Connecting-IP", "True-Client-IP",
}

type session struct {
	csrf string
	exp  time.Time
}

type loginFailures struct {
	count int
	start time.Time
}

// server is the web UI handler. It is safe for concurrent use.
type server struct {
	d      Deps
	api    *http.ServeMux
	static http.Handler

	mu       sync.Mutex // sessions and fails
	sessions map[string]session
	fails    map[netip.Addr]loginFailures

	// cfgMu serializes every load-change-save of the config file.
	cfgMu sync.Mutex

	// newInstaller builds the skill installer; tests replace it.
	newInstaller func(workspace string, rt *config.Config) (skillInstaller, error)
}

// New returns the handler for /ui, /ui/ and /api/ui/. It fails without a
// token: the page is never served unauthenticated.
func New(d Deps) (http.Handler, error) {
	s, err := newServer(d)
	if err != nil {
		return nil, err
	}
	return s, nil
}

func newServer(d Deps) (*server, error) {
	if d.Token == "" {
		return nil, errors.New("webui: no access token")
	}
	if d.Now == nil {
		d.Now = time.Now
	}
	s := &server{
		d:            d,
		api:          http.NewServeMux(),
		static:       staticHandler(),
		sessions:     map[string]session{},
		fails:        map[netip.Addr]loginFailures{},
		newInstaller: defaultInstaller,
	}
	s.routes()
	return s, nil
}

// Mounter is where the handler is registered (the gateway's channel
// manager, whose mux survives reloads).
type Mounter interface {
	HandleHTTP(pattern string, h http.Handler) error
}

// Mount registers the web UI on m when cfg (the running config) enables it
// and a token is present. Without a token it logs a warning and mounts
// nothing; it never writes the config. It reports whether the UI is served.
func Mount(m Mounter, cfg *config.Config, d Deps) (bool, error) {
	if cfg == nil || !cfg.WebUI.Enabled {
		return false, nil
	}
	d.Token = cfg.WebUI.Token.String()
	if d.Token == "" {
		logger.WarnCF("webui", "web UI disabled: no token; run 'kuromatsu ui token'", nil)
		return false, nil
	}
	d.AllowRemote = cfg.WebUI.AllowRemote
	if d.AllowRemote {
		logger.WarnCF("webui", "web UI reachable from the network over plain HTTP; prefer an SSH tunnel", nil)
	}
	h, err := New(d)
	if err != nil {
		return false, err
	}
	for _, p := range []string{"/ui", uiPrefix, apiPrefix} {
		if err = m.HandleHTTP(p, h); err != nil {
			return false, err
		}
	}
	logger.InfoCF("webui", "web UI mounted", map[string]any{"path": uiPrefix, "allow_remote": d.AllowRemote})
	return true, nil
}

// ServeHTTP applies, in order: the loopback and proxy-header check, the Host
// allowlist, the security headers, then the route.
func (s *server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if !s.d.AllowRemote {
		if !fromLoopback(r) {
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}
		if !hostAllowed(r.Host) {
			http.Error(w, "forbidden host", http.StatusForbidden)
			return
		}
	}

	h := w.Header()
	h.Set("X-Content-Type-Options", "nosniff")
	h.Set("Referrer-Policy", "same-origin")
	h.Set("X-Frame-Options", "DENY")
	h.Set("Content-Security-Policy", contentSecurity)

	p := r.URL.Path
	// Only canonical paths: "..", "//" and "." segments never reach a route.
	if c := path.Clean(p); c != p && c+"/" != p {
		http.NotFound(w, r)
		return
	}
	switch {
	case p == "/ui":
		http.Redirect(w, r, uiPrefix, http.StatusMovedPermanently)
	case strings.HasPrefix(p, apiPrefix):
		h.Set("Cache-Control", "no-store")
		s.serveAPI(w, r)
	case strings.HasPrefix(p, uiPrefix):
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			w.Header().Set("Allow", "GET, HEAD")
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		s.static.ServeHTTP(w, r)
	default:
		http.NotFound(w, r)
	}
}

// fromLoopback reports whether r comes straight from this host: a loopback
// peer address (IPv4-mapped included) and no proxy header.
func fromLoopback(r *http.Request) bool {
	ap, err := netip.ParseAddrPort(r.RemoteAddr)
	if err != nil || !ap.Addr().Unmap().IsLoopback() {
		return false
	}
	for _, name := range proxyHeaders {
		if _, ok := r.Header[http.CanonicalHeaderKey(name)]; ok {
			return false
		}
	}
	return true
}

// hostAllowed accepts localhost, 127.0.0.1 and [::1], with or without a
// port (any port, so an SSH tunnel on another local port works). Any other
// name is refused, which defeats DNS rebinding.
func hostAllowed(hostport string) bool {
	host, port := hostport, ""
	switch {
	case strings.HasPrefix(hostport, "["):
		end := strings.Index(hostport, "]")
		if end < 0 {
			return false
		}
		host, port = hostport[1:end], hostport[end+1:]
		if port != "" {
			if !strings.HasPrefix(port, ":") {
				return false
			}
			port = port[1:]
		}
		if host != "::1" {
			return false
		}
	case strings.Count(hostport, ":") > 1:
		return false // bare IPv6 must be bracketed
	default:
		if i := strings.IndexByte(hostport, ':'); i >= 0 {
			host, port = hostport[:i], hostport[i+1:]
		}
		switch strings.ToLower(host) {
		case "localhost", "127.0.0.1":
		default:
			return false
		}
	}
	if port == "" {
		return !strings.HasSuffix(hostport, ":")
	}
	n, err := strconv.Atoi(port)
	return err == nil && n > 0 && n <= 65535 && strconv.Itoa(n) == port
}

// sameOrigin reports whether Origin (or, without it, Referer) is exactly
// http://<Host>: scheme, host and port.
func sameOrigin(r *http.Request) bool {
	want := "http://" + r.Host
	if o := r.Header.Get("Origin"); o != "" {
		return o == want
	}
	if ref := r.Header.Get("Referer"); ref != "" {
		u, err := url.Parse(ref)
		return err == nil && u.Scheme == "http" && u.Host == r.Host
	}
	return false
}

func isSafeMethod(m string) bool {
	return m == http.MethodGet || m == http.MethodHead
}

func (s *server) serveAPI(w http.ResponseWriter, r *http.Request) {
	// No CORS: a preflight never succeeds and no Access-Control-* header is
	// ever sent.
	if r.Method == http.MethodOptions {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if r.URL.Path == apiPrefix+"login" {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		s.login(w, r)
		return
	}

	sid, sess, hasSession := s.sessionFrom(r)
	if authz := r.Header.Get("Authorization"); authz != "" {
		token, ok := strings.CutPrefix(authz, "Bearer ")
		if !ok || subtle.ConstantTimeCompare([]byte(token), []byte(s.d.Token)) != 1 {
			writeError(w, http.StatusUnauthorized, "unauthorized")
			return
		}
	} else if !hasSession {
		writeError(w, http.StatusUnauthorized, "unauthorized")
		return
	}

	// A request carrying the session cookie is treated as coming from the
	// browser, even with a Bearer token: state changes need the exact
	// Origin and the session's CSRF token.
	if _, err := r.Cookie(sessionCookie); err == nil && !isSafeMethod(r.Method) {
		if !sameOrigin(r) {
			writeError(w, http.StatusForbidden, "origin not allowed")
			return
		}
		got := r.Header.Get(csrfHeader)
		if !hasSession || got == "" || subtle.ConstantTimeCompare([]byte(got), []byte(sess.csrf)) != 1 {
			writeError(w, http.StatusForbidden, "missing or invalid CSRF token")
			return
		}
	}
	if hasSession {
		s.touch(sid)
	}
	s.api.ServeHTTP(w, r)
}

// sessionFrom returns the live session named by the request's cookie.
func (s *server) sessionFrom(r *http.Request) (string, session, bool) {
	c, err := r.Cookie(sessionCookie)
	if err != nil || c.Value == "" {
		return "", session{}, false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	sess, ok := s.sessions[c.Value]
	if !ok || !s.d.Now().Before(sess.exp) {
		delete(s.sessions, c.Value)
		return "", session{}, false
	}
	return c.Value, sess, true
}

// touch slides the session's expiry.
func (s *server) touch(sid string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if sess, ok := s.sessions[sid]; ok {
		sess.exp = s.d.Now().Add(sessionTTL)
		s.sessions[sid] = sess
	}
}

func randomHex(n int) (string, error) {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

// csrfCookieName carries the port of the address the browser used, so two
// local apps on different ports do not share it.
func csrfCookieName(r *http.Request) string {
	port := "80"
	if _, p, err := net.SplitHostPort(r.Host); err == nil && p != "" {
		port = p
	}
	return csrfCookiePref + port
}

func (s *server) login(w http.ResponseWriter, r *http.Request) {
	if !sameOrigin(r) {
		writeError(w, http.StatusForbidden, "origin not allowed")
		return
	}
	ip := netip.IPv6Unspecified()
	if ap, err := netip.ParseAddrPort(r.RemoteAddr); err == nil {
		ip = ap.Addr().Unmap()
	}
	now := s.d.Now()

	s.mu.Lock()
	for k, f := range s.fails {
		if now.Sub(f.start) >= loginWindow {
			delete(s.fails, k)
		}
	}
	for k, sess := range s.sessions {
		if !now.Before(sess.exp) {
			delete(s.sessions, k)
		}
	}
	limited := s.fails[ip].count >= loginMaxFails
	s.mu.Unlock()
	if limited {
		writeError(w, http.StatusTooManyRequests, "too many failed logins; wait a minute")
		return
	}

	var body struct {
		Token string `json:"token"`
	}
	if err := decodeJSON(w, r, &body, maxLoginBody); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if subtle.ConstantTimeCompare([]byte(body.Token), []byte(s.d.Token)) != 1 {
		s.mu.Lock()
		f := s.fails[ip]
		if f.count == 0 {
			f.start = now
		}
		f.count++
		s.fails[ip] = f
		s.mu.Unlock()
		logger.WarnCF("webui", "web UI login failed", map[string]any{"remote": ip.String()})
		writeError(w, http.StatusUnauthorized, "invalid token")
		return
	}

	sid, err := randomHex(32)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "cannot create session")
		return
	}
	csrf, err := randomHex(32)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "cannot create session")
		return
	}
	s.mu.Lock()
	s.sessions[sid] = session{csrf: csrf, exp: now.Add(sessionTTL)}
	s.mu.Unlock()

	http.SetCookie(w, &http.Cookie{
		Name: sessionCookie, Value: sid, Path: "/",
		HttpOnly: true, SameSite: http.SameSiteStrictMode, Secure: false,
	})
	http.SetCookie(w, &http.Cookie{
		Name: csrfCookieName(r), Value: csrf, Path: apiPrefix,
		SameSite: http.SameSiteStrictMode, Secure: false,
	})
	writeJSON(w, http.StatusOK, map[string]string{"csrf": csrf})
}

func (s *server) logout(w http.ResponseWriter, r *http.Request) {
	if c, err := r.Cookie(sessionCookie); err == nil {
		s.mu.Lock()
		delete(s.sessions, c.Value)
		s.mu.Unlock()
	}
	http.SetCookie(w, &http.Cookie{
		Name: sessionCookie, Value: "", Path: "/", MaxAge: -1,
		HttpOnly: true, SameSite: http.SameSiteStrictMode,
	})
	http.SetCookie(w, &http.Cookie{
		Name: csrfCookieName(r), Value: "", Path: apiPrefix, MaxAge: -1,
		SameSite: http.SameSiteStrictMode,
	})
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

// sessionInfo returns the CSRF token of the cookie session, so the page can
// recover it after a reload (the CSRF cookie's path keeps it out of reach of
// the page's scripts).
func (s *server) sessionInfo(w http.ResponseWriter, r *http.Request) {
	_, sess, ok := s.sessionFrom(r)
	if !ok {
		writeJSON(w, http.StatusOK, map[string]string{"csrf": ""})
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"csrf": sess.csrf})
}

// --- JSON helpers -----------------------------------------------------------

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(v); err != nil {
		logger.DebugCF("webui", "write response failed", map[string]any{"error": err.Error()})
	}
}

func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}

// decodeJSON reads at most limit bytes of JSON into v, refusing unknown
// fields and trailing data.
func decodeJSON(w http.ResponseWriter, r *http.Request, v any, limit int64) error {
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, limit))
	dec.DisallowUnknownFields()
	if err := dec.Decode(v); err != nil {
		var tooBig *http.MaxBytesError
		if errors.As(err, &tooBig) {
			return fmt.Errorf("request body over %d bytes", limit)
		}
		return fmt.Errorf("invalid JSON: %w", err)
	}
	if _, err := dec.Token(); !errors.Is(err, io.EOF) {
		return errors.New("invalid JSON: trailing data")
	}
	return nil
}

// --- config writes ----------------------------------------------------------

// statusError pairs an error with the HTTP status it answers.
type statusError struct {
	status int
	err    error
}

func (e *statusError) Error() string { return e.err.Error() }

func unprocessable(format string, args ...any) error {
	return &statusError{http.StatusUnprocessableEntity, fmt.Errorf(format, args...)}
}

func notFound(format string, args ...any) error {
	return &statusError{http.StatusNotFound, fmt.Errorf(format, args...)}
}

// editConfig loads the config as stored (never the environment), applies
// change, validates the result without process-wide effects, saves it and
// asks the gateway to reload. Nothing is written when change or the
// validation fails. Every config write goes through cfgMu.
func (s *server) editConfig(w http.ResponseWriter, change func(*config.Config) error) {
	s.cfgMu.Lock()
	defer s.cfgMu.Unlock()

	cfg, err := config.LoadConfigForEdit(s.d.ConfigPath)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "load config: "+err.Error())
		return
	}
	if err = change(cfg); err != nil {
		status := http.StatusUnprocessableEntity
		var se *statusError
		if errors.As(err, &se) {
			status = se.status
		}
		writeError(w, status, err.Error())
		return
	}
	if err = config.ValidateForSave(cfg); err != nil {
		writeError(w, http.StatusUnprocessableEntity, "invalid config: "+err.Error())
		return
	}
	if err = config.SaveConfig(s.d.ConfigPath, cfg); err != nil {
		writeError(w, http.StatusInternalServerError, "save config: "+err.Error())
		return
	}
	logger.InfoCF("webui", "config changed from the web UI", nil)
	s.reload(w)
}

// reload asks the gateway to apply the saved config. A reload already
// running is not an error: the change is saved and applies on the next one.
func (s *server) reload(w http.ResponseWriter) {
	if s.d.Reload == nil {
		writeJSON(w, http.StatusAccepted, map[string]string{"warning": "saved; restart the gateway to apply"})
		return
	}
	if err := s.d.Reload(); err != nil {
		writeJSON(w, http.StatusAccepted, map[string]string{"warning": "saved; reload not started: " + err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "reloading": true})
}

// runtimeConfig is the running config, or the stored one when the gateway
// gave no getter.
func (s *server) runtimeConfig() (*config.Config, error) {
	if s.d.Runtime != nil {
		if cfg := s.d.Runtime(); cfg != nil {
			return cfg, nil
		}
	}
	return config.LoadConfigForEdit(s.d.ConfigPath)
}

// withDeadline extends the shared server's write timeout for a long
// operation. It fails when the response writer cannot do it.
func withDeadline(w http.ResponseWriter, d time.Duration) error {
	return http.NewResponseController(w).SetWriteDeadline(time.Now().Add(d))
}

// opContext bounds a long operation.
func opContext(r *http.Request, d time.Duration) (context.Context, context.CancelFunc) {
	return context.WithTimeout(r.Context(), d)
}
