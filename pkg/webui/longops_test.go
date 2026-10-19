package webui

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/andre25costa-code/kuromatsu/pkg/config"
)

// deadlineRecorder records the write deadline a handler asks for, as
// http.ResponseController does through SetWriteDeadline.
type deadlineRecorder struct {
	*httptest.ResponseRecorder
	deadline time.Time
}

func (d *deadlineRecorder) SetWriteDeadline(t time.Time) error {
	d.deadline = t
	return nil
}

// slowInstaller stands in for a 35 s install without sleeping: it reports
// the time it would take through elapsed.
type slowInstaller struct {
	repo    string
	elapsed time.Duration
}

func (s *slowInstaller) InstallFromGitHub(ctx context.Context, repo string) error {
	s.repo = repo
	s.elapsed = 35 * time.Second
	if dl, ok := ctx.Deadline(); !ok || time.Until(dl) < s.elapsed {
		return context.DeadlineExceeded
	}
	return nil
}

func (s *slowInstaller) Uninstall(string) error { return nil }

func TestSkillsInstall_ExtendsWriteDeadline(t *testing.T) {
	e := newTestEnv(t)
	inst := &slowInstaller{}
	e.srv.newInstaller = func(string, *config.Config) (skillInstaller, error) { return inst, nil }

	rec := &deadlineRecorder{ResponseRecorder: httptest.NewRecorder()}
	start := time.Now()
	e.srv.ServeHTTP(rec, bearer(req(http.MethodPost, "/api/ui/skills", `{"repo":"owner/name"}`)))
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	assert.Equal(t, "owner/name", inst.repo)
	assert.GreaterOrEqual(t, rec.deadline.Sub(start), 3*time.Minute-time.Second)

	// Without deadline support the install is not started.
	inst.repo = ""
	plain := httptest.NewRecorder()
	e.srv.ServeHTTP(plain, bearer(req(http.MethodPost, "/api/ui/skills", `{"repo":"owner/name"}`)))
	assert.Equal(t, http.StatusInternalServerError, plain.Code)
	assert.Empty(t, inst.repo)

	inst.repo = ""
	for _, repo := range []string{"../../etc", "../..", "owner/..", "./x", "owner/...", "owner", "a/b/c"} {
		bad := &deadlineRecorder{ResponseRecorder: httptest.NewRecorder()}
		e.srv.ServeHTTP(bad, bearer(req(http.MethodPost, "/api/ui/skills", `{"repo":"`+repo+`"}`)))
		assert.Equal(t, http.StatusUnprocessableEntity, bad.Code, repo)
	}
	assert.Empty(t, inst.repo, "no install started for a bad repo")
	ok := &deadlineRecorder{ResponseRecorder: httptest.NewRecorder()}
	e.srv.ServeHTTP(ok, bearer(req(http.MethodPost, "/api/ui/skills", `{"repo":"owner/.github"}`)))
	assert.Equal(t, http.StatusOK, ok.Code, "a leading dot is a valid name")
}
