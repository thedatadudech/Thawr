package api

import (
	"context"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/thedatadudech/thawr/internal/control"
)

func TestPolicyEndpoints(t *testing.T) {
	path := filepath.Join(t.TempDir(), "policy.yaml")
	env := newRESTEnv(t, func(d *RESTDeps, e *restEnv) {
		svc := control.NewPolicyService(e.st, d.Logger, path, nil)
		if err := svc.LoadInitial(context.Background()); err != nil {
			t.Fatal(err)
		}
		d.Policy = svc
	})
	write := func(doc string) {
		if err := os.WriteFile(path, []byte(doc), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	// Show works for members; check and reload are admin only.
	_, member := env.login("alice", "alicepassword")
	var shown control.PolicyReport
	decode(t, env.do(env.handler, member, http.MethodGet, "/api/v1/policy", nil, false), &shown)
	if shown.Summary.Rules != 0 || shown.Hash != "" {
		t.Errorf("empty policy shown as %+v", shown)
	}
	if rec := env.do(env.handler, member, http.MethodPost, "/api/v1/policy/reload", nil, true); rec.Code != http.StatusForbidden {
		t.Errorf("member reload: %d", rec.Code)
	}
	if rec := env.do(env.handler, member, http.MethodPost, "/api/v1/policy/check", map[string]string{"yaml": "version: 1\n"}, true); rec.Code != http.StatusForbidden {
		t.Errorf("member check: %d", rec.Code)
	}

	// Check reports errors without installing anything.
	var checked struct {
		OK bool `json:"ok"`
		control.PolicyReport
	}
	decode(t, env.do(env.local, session{}, http.MethodPost, "/api/v1/policy/check", map[string]string{"yaml": "version: 1\nacls:\n  - action: accept\n    src: [ghost]\n    dst: ['*:*']\n"}, false), &checked)
	if checked.OK || len(checked.Errors) != 1 || !strings.Contains(checked.Errors[0], "acls[0].src[0]") {
		t.Errorf("check: %+v", checked)
	}
	decode(t, env.do(env.local, session{}, http.MethodPost, "/api/v1/policy/check", map[string]string{"yaml": "version: 1\nacls:\n  - action: accept\n    src: [alice]\n    dst: ['tag:prod:22']\n"}, false), &checked)
	if !checked.OK || checked.Summary.Rules != 1 || len(checked.Warnings) != 1 {
		t.Errorf("check valid: %+v", checked)
	}

	// Reload with an invalid file keeps the old policy and says why.
	write("version: 1\nacls:\n  - action: accept\n    src: [ghost]\n    dst: ['*:*']\n")
	rec := env.do(env.local, session{}, http.MethodPost, "/api/v1/policy/reload", nil, false)
	if rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), "unknown user") {
		t.Errorf("invalid reload: %d %s", rec.Code, rec.Body.String())
	}
	write("version: 1\nacls:\n  - action: accept\n    src: [alice]\n    dst: ['markus:22']\n")
	var reloaded control.PolicyReport
	rec = env.do(env.local, session{}, http.MethodPost, "/api/v1/policy/reload", nil, false)
	if rec.Code != http.StatusOK {
		t.Fatalf("reload: %d %s", rec.Code, rec.Body.String())
	}
	decode(t, rec, &reloaded)
	if reloaded.Summary.Rules != 1 || reloaded.Hash == "" {
		t.Errorf("reload report: %+v", reloaded)
	}
	decode(t, env.do(env.handler, member, http.MethodGet, "/api/v1/policy", nil, false), &shown)
	if shown.Hash != reloaded.Hash || !strings.Contains(shown.Source, "markus:22") {
		t.Errorf("show after reload: %+v", shown)
	}
}
