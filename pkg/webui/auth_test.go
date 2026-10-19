package webui

import (
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAuth_BearerAndCookie(t *testing.T) {
	e := newTestEnv(t)

	assert.Equal(t, http.StatusUnauthorized, e.do(req(http.MethodGet, "/api/ui/models", "")).Code)

	wrong := req(http.MethodGet, "/api/ui/models", "")
	wrong.Header.Set("Authorization", "Bearer nope")
	assert.Equal(t, http.StatusUnauthorized, e.do(wrong).Code)

	notBearer := req(http.MethodGet, "/api/ui/models", "")
	notBearer.Header.Set("Authorization", "Basic "+testToken)
	assert.Equal(t, http.StatusUnauthorized, e.do(notBearer).Code)

	assert.Equal(t, http.StatusOK, e.do(bearer(req(http.MethodGet, "/api/ui/models", ""))).Code)

	cookie, _ := e.login()
	r := req(http.MethodGet, "/api/ui/models", "")
	r.AddCookie(cookie)
	assert.Equal(t, http.StatusOK, e.do(r).Code)

	// An unknown or expired session is refused.
	stale := req(http.MethodGet, "/api/ui/models", "")
	stale.AddCookie(&http.Cookie{Name: sessionCookie, Value: "deadbeef"})
	assert.Equal(t, http.StatusUnauthorized, e.do(stale).Code)

	e.now = e.now.Add(sessionTTL + time.Minute)
	expired := req(http.MethodGet, "/api/ui/models", "")
	expired.AddCookie(cookie)
	assert.Equal(t, http.StatusUnauthorized, e.do(expired).Code)
}

func TestAuth_SessionSlidesAndLogout(t *testing.T) {
	e := newTestEnv(t)
	cookie, csrf := e.login()
	for range 3 { // 3 x 10 h > 12 h TTL: each use extends it
		e.now = e.now.Add(10 * time.Hour)
		r := req(http.MethodGet, "/api/ui/session", "")
		r.AddCookie(cookie)
		rec := e.do(r)
		require.Equal(t, http.StatusOK, rec.Code)
		assert.Contains(t, rec.Body.String(), csrf)
	}
	out := req(http.MethodPost, "/api/ui/logout", "")
	out.AddCookie(cookie)
	out.Header.Set("Origin", testOrigin)
	out.Header.Set(csrfHeader, csrf)
	require.Equal(t, http.StatusOK, e.do(out).Code)
	after := req(http.MethodGet, "/api/ui/models", "")
	after.AddCookie(cookie)
	assert.Equal(t, http.StatusUnauthorized, e.do(after).Code)
}

func TestAuth_LoginCookies(t *testing.T) {
	e := newTestEnv(t)
	r := req(http.MethodPost, "/api/ui/login", `{"token":"`+testToken+`"}`)
	r.Header.Set("Origin", testOrigin)
	rec := e.do(r)
	require.Equal(t, http.StatusOK, rec.Code)
	var sess, csrf *http.Cookie
	for _, c := range rec.Result().Cookies() {
		switch c.Name {
		case sessionCookie:
			sess = c
		case "km_csrf_18790":
			csrf = c
		}
	}
	require.NotNil(t, sess)
	require.NotNil(t, csrf)
	assert.True(t, sess.HttpOnly)
	assert.Equal(t, http.SameSiteStrictMode, sess.SameSite)
	assert.Equal(t, "/", sess.Path)
	assert.False(t, csrf.HttpOnly)
	assert.Equal(t, "/api/ui/", csrf.Path)
	assert.Equal(t, http.SameSiteStrictMode, csrf.SameSite)
	assert.Contains(t, rec.Body.String(), csrf.Value)
}

func TestAuth_CSRFRequired(t *testing.T) {
	e := newTestEnv(t)
	cookie, csrf := e.login()

	put := func(token string) int {
		r := req(http.MethodPut, "/api/ui/workspace/SOUL.md", "be kind")
		r.AddCookie(cookie)
		r.Header.Set("Origin", testOrigin)
		if token != "" {
			r.Header.Set(csrfHeader, token)
		}
		return e.do(r).Code
	}
	assert.Equal(t, http.StatusForbidden, put(""))
	assert.Equal(t, http.StatusForbidden, put("wrong"))
	assert.Equal(t, http.StatusOK, put(csrf))
	assert.Equal(t, "be kind", readFile(t, e.workspace+"/SOUL.md"))

	// A cookie next to a Bearer token still needs CSRF on a change.
	r := bearer(req(http.MethodPut, "/api/ui/workspace/SOUL.md", "x"))
	r.AddCookie(cookie)
	r.Header.Set("Origin", testOrigin)
	assert.Equal(t, http.StatusForbidden, e.do(r).Code)

	// Bearer alone needs neither Origin nor CSRF.
	assert.Equal(t, http.StatusOK, e.do(bearer(req(http.MethodPut, "/api/ui/workspace/SOUL.md", "y"))).Code)
}

func TestAuth_OriginCheck(t *testing.T) {
	e := newTestEnv(t)
	cookie, csrf := e.login()
	post := func(set func(r *http.Request)) int {
		r := req(http.MethodPut, "/api/ui/workspace/USER.md", "hi")
		r.AddCookie(cookie)
		r.Header.Set(csrfHeader, csrf)
		set(r)
		return e.do(r).Code
	}
	assert.Equal(t, http.StatusForbidden, post(func(r *http.Request) { r.Header.Set("Origin", "http://evil.example") }))
	assert.Equal(
		t,
		http.StatusForbidden,
		post(func(r *http.Request) { r.Header.Set("Origin", "http://localhost:3000") }),
	)
	assert.Equal(t, http.StatusForbidden, post(func(r *http.Request) {}), "no Origin and no Referer")
	assert.Equal(t, http.StatusForbidden, post(func(r *http.Request) { r.Header.Set("Origin", "null") }))
	assert.Equal(t, http.StatusForbidden, post(func(r *http.Request) {
		r.Header.Set("Referer", "http://evil.example/ui/")
	}))
	assert.Equal(t, http.StatusOK, post(func(r *http.Request) { r.Header.Set("Origin", testOrigin) }))
	assert.Equal(t, http.StatusOK, post(func(r *http.Request) {
		r.Header.Set("Referer", "http://localhost:18790/ui/#assistente")
	}))

	login := req(http.MethodPost, "/api/ui/login", `{"token":"`+testToken+`"}`)
	login.Header.Set("Origin", "http://evil.example")
	assert.Equal(t, http.StatusForbidden, e.do(login).Code)
	noOrigin := req(http.MethodPost, "/api/ui/login", `{"token":"`+testToken+`"}`)
	assert.Equal(t, http.StatusForbidden, e.do(noOrigin).Code)
}

func TestAuth_OriginExactWithPort(t *testing.T) {
	e := newTestEnv(t)
	cookie, csrf := e.login()
	try := func(host, origin string) int {
		r := req(http.MethodPut, "/api/ui/workspace/AGENT.md", "a")
		r.Host = host
		r.AddCookie(cookie)
		r.Header.Set(csrfHeader, csrf)
		r.Header.Set("Origin", origin)
		return e.do(r).Code
	}
	assert.Equal(t, http.StatusForbidden, try("localhost:18790", "http://localhost:3000"))
	assert.Equal(t, http.StatusForbidden, try("localhost:18790", "http://localhost"))
	assert.Equal(t, http.StatusForbidden, try("localhost:18790", "https://localhost:18790"))
	assert.Equal(t, http.StatusForbidden, try("localhost:18790", "http://127.0.0.1:18790"))
	assert.Equal(t, http.StatusOK, try("localhost:18790", "http://localhost:18790"))
	assert.Equal(t, http.StatusOK, try("localhost:8080", "http://localhost:8080"), "SSH tunnel on another port")
}

func TestAuth_LoopbackOnly(t *testing.T) {
	e := newTestEnv(t)
	for _, path := range []string{"/ui/", "/api/ui/models"} {
		r := bearer(req(http.MethodGet, path, ""))
		r.RemoteAddr = "192.168.0.10:5555"
		assert.Equal(t, http.StatusForbidden, e.do(r).Code, path)
	}
	login := req(http.MethodPost, "/api/ui/login", `{"token":"`+testToken+`"}`)
	login.RemoteAddr = "192.168.0.10:5555"
	login.Header.Set("Origin", testOrigin)
	assert.Equal(t, http.StatusForbidden, e.do(login).Code)

	bad := bearer(req(http.MethodGet, "/api/ui/models", ""))
	bad.RemoteAddr = "garbage"
	assert.Equal(t, http.StatusForbidden, e.do(bad).Code)

	assert.Equal(t, http.StatusOK, e.do(bearer(req(http.MethodGet, "/ui/", ""))).Code)

	// allow_remote: other peers, Hosts and proxy headers pass; auth still applies.
	d := e.deps()
	d.AllowRemote = true
	remote, err := newServer(d)
	require.NoError(t, err)
	r := bearer(req(http.MethodGet, "/api/ui/models", ""))
	r.RemoteAddr = "192.168.0.10:5555"
	r.Host = "kuromatsu.lan:18790"
	r.Header.Set("X-Forwarded-For", "1.2.3.4")
	rec := newRecorder()
	remote.ServeHTTP(rec, r)
	assert.Equal(t, http.StatusOK, rec.Code)
	unauth := req(http.MethodGet, "/api/ui/models", "")
	unauth.RemoteAddr = "192.168.0.10:5555"
	rec = newRecorder()
	remote.ServeHTTP(rec, unauth)
	assert.Equal(t, http.StatusUnauthorized, rec.Code)
}

func TestAuth_IPv4MappedLoopback(t *testing.T) {
	e := newTestEnv(t)
	for _, addr := range []string{"[::ffff:127.0.0.1]:5555", "[::1]:5555", "127.0.0.2:5555"} {
		r := bearer(req(http.MethodGet, "/api/ui/models", ""))
		r.RemoteAddr = addr
		assert.Equal(t, http.StatusOK, e.do(r).Code, addr)
	}
	r := bearer(req(http.MethodGet, "/api/ui/models", ""))
	r.RemoteAddr = "[::ffff:192.168.0.10]:5555"
	assert.Equal(t, http.StatusForbidden, e.do(r).Code)
}

func TestAuth_ProxyHeadersRejectedOnLoopback(t *testing.T) {
	e := newTestEnv(t)
	for _, h := range proxyHeaders {
		for _, path := range []string{"/ui/", "/api/ui/models"} {
			r := bearer(req(http.MethodGet, path, ""))
			r.Header.Set(h, "1.2.3.4")
			assert.Equal(t, http.StatusForbidden, e.do(r).Code, h+" "+path)
		}
	}
	r := bearer(req(http.MethodGet, "/api/ui/models", ""))
	r.Header.Set("X-Forwarded-For", "")
	assert.Equal(t, http.StatusForbidden, e.do(r).Code, "an empty proxy header still marks a proxy")
}

func TestAuth_HostAllowlist(t *testing.T) {
	e := newTestEnv(t)
	cases := map[string]int{
		"localhost":              http.StatusOK,
		"localhost:18790":        http.StatusOK,
		"localhost:8080":         http.StatusOK, // ssh -L 8080:localhost:18790
		"LOCALHOST:18790":        http.StatusOK,
		"127.0.0.1":              http.StatusOK,
		"127.0.0.1:18790":        http.StatusOK,
		"[::1]":                  http.StatusOK,
		"[::1]:18790":            http.StatusOK,
		"evil.example:18790":     http.StatusForbidden,
		"evil.example":           http.StatusForbidden,
		"localhost.evil.example": http.StatusForbidden,
		"127.0.0.2:18790":        http.StatusForbidden,
		"[::2]:18790":            http.StatusForbidden,
		"::1":                    http.StatusForbidden,
		"localhost:":             http.StatusForbidden,
		"localhost:99999":        http.StatusForbidden,
		"localhost:018790":       http.StatusForbidden,
		"":                       http.StatusForbidden,
	}
	for host, want := range cases {
		r := bearer(req(http.MethodGet, "/api/ui/models", ""))
		r.Host = host
		assert.Equal(t, want, e.do(r).Code, "Host %q", host)
	}
}

func TestAuth_LoginRateLimit(t *testing.T) {
	e := newTestEnv(t)
	try := func(token string) int {
		r := req(http.MethodPost, "/api/ui/login", `{"token":"`+token+`"}`)
		r.Header.Set("Origin", testOrigin)
		return e.do(r).Code
	}
	for i := range loginMaxFails {
		require.Equal(t, http.StatusUnauthorized, try("wrong"), "attempt %d", i+1)
	}
	assert.Equal(t, http.StatusTooManyRequests, try("wrong"), "6th attempt")
	assert.Equal(t, http.StatusTooManyRequests, try(testToken), "even the right token waits the minute")

	// Bearer is not limited.
	assert.Equal(t, http.StatusOK, e.do(bearer(req(http.MethodGet, "/api/ui/models", ""))).Code)

	e.now = e.now.Add(loginWindow)
	assert.Equal(t, http.StatusOK, try(testToken))
}

func TestAuth_NoCORSHeaders(t *testing.T) {
	e := newTestEnv(t)
	opt := req(http.MethodOptions, "/api/ui/models", "")
	opt.Header.Set("Origin", "http://evil.example")
	opt.Header.Set("Access-Control-Request-Method", "PUT")
	rec := e.do(opt)
	assert.True(t, rec.Code == http.StatusForbidden || rec.Code == http.StatusMethodNotAllowed, "code %d", rec.Code)
	cookie, csrf := e.login()

	requests := []func() *http.Request{
		func() *http.Request { return opt },
		func() *http.Request { return bearer(req(http.MethodGet, "/api/ui/models", "")) },
		func() *http.Request { return bearer(req(http.MethodGet, "/api/ui/status", "")) },
		func() *http.Request { return req(http.MethodGet, "/api/ui/models", "") },
		func() *http.Request {
			r := req(http.MethodPut, "/api/ui/workspace/SOUL.md", "x")
			r.AddCookie(cookie)
			r.Header.Set("Origin", testOrigin)
			r.Header.Set(csrfHeader, csrf)
			return r
		},
		func() *http.Request { return req(http.MethodGet, "/ui/", "") },
	}
	for i, mk := range requests {
		r := mk()
		r.Header.Set("Origin", "http://evil.example")
		rec := e.do(r)
		for name := range rec.Header() {
			assert.False(t, strings.HasPrefix(strings.ToLower(name), "access-control-"),
				"request %d answered with %s", i, name)
		}
	}
}

func TestStatic_SecurityHeaders(t *testing.T) {
	e := newTestEnv(t)
	rec := e.do(req(http.MethodGet, "/ui/", ""))
	require.Equal(t, http.StatusOK, rec.Code)
	assert.Contains(t, rec.Body.String(), "<nav")
	assert.Equal(t, contentSecurity, rec.Header().Get("Content-Security-Policy"))
	assert.Equal(t, "nosniff", rec.Header().Get("X-Content-Type-Options"))
	assert.Equal(t, "same-origin", rec.Header().Get("Referrer-Policy"))

	redirect := e.do(req(http.MethodGet, "/ui", ""))
	assert.Equal(t, http.StatusMovedPermanently, redirect.Code)
	assert.Equal(t, "/ui/", redirect.Header().Get("Location"))

	for _, p := range []string{"/ui/app.js", "/ui/app.css"} {
		assert.Equal(t, http.StatusOK, e.do(req(http.MethodGet, p, "")).Code, p)
	}
	assert.Equal(t, http.StatusNotFound, e.do(req(http.MethodGet, "/ui/../api/ui/models", "")).Code)
	assert.Equal(t, http.StatusMethodNotAllowed, e.do(req(http.MethodPost, "/ui/", "")).Code)
}

// Concurrent wrong logins cannot get more than the limit past the check.
func TestAuth_LoginRateLimitConcurrent(t *testing.T) {
	e := newTestEnv(t)
	const n = 20
	codes := make(chan int, n)
	var wg sync.WaitGroup
	for range n {
		wg.Add(1)
		go func() {
			defer wg.Done()
			r := req(http.MethodPost, "/api/ui/login", `{"token":"wrong"}`)
			r.Header.Set("Origin", testOrigin)
			codes <- e.do(r).Code
		}()
	}
	wg.Wait()
	close(codes)
	unauthorized := 0
	for c := range codes {
		if c == http.StatusUnauthorized {
			unauthorized++
		}
	}
	assert.Equal(t, loginMaxFails, unauthorized, "only the first attempts reach the token compare")

	// A success gives its slot back: 4 wrong + 1 right leaves room.
	e2 := newTestEnv(t)
	try := func(token string) int {
		r := req(http.MethodPost, "/api/ui/login", `{"token":"`+token+`"}`)
		r.Header.Set("Origin", testOrigin)
		return e2.do(r).Code
	}
	for range loginMaxFails - 1 {
		require.Equal(t, http.StatusUnauthorized, try("wrong"))
	}
	require.Equal(t, http.StatusOK, try(testToken))
	assert.Equal(t, http.StatusUnauthorized, try("wrong"), "the success did not use up the 5th slot")
}
