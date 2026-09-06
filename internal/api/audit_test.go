package api

import (
	"context"
	"net/http"
	"net/url"
	"strconv"
	"testing"
	"time"

	"github.com/thedatadudech/thawr/internal/control"
	"github.com/thedatadudech/thawr/internal/store"
)

func TestAuditEndpoint(t *testing.T) {
	now := time.Date(2026, 9, 6, 12, 0, 0, 0, time.UTC)
	env := newRESTEnv(t, func(d *RESTDeps, e *restEnv) {
		d.Audit = e.st.Audit()
		d.Now = func() time.Time { return now }
	})
	ctx := context.Background()
	// The env's users were created without an auditor; seed rows directly.
	seed := []store.AuditEntry{
		{At: now.Add(-48 * time.Hour), Actor: "markus", ActorRole: store.RoleAdmin, Action: control.AuditTokenCreate, Target: "tk_1", Details: map[string]string{"owner": "alice"}},
		{At: now.Add(-2 * time.Hour), Actor: "peer:nas", ActorRole: control.RolePeer, Action: control.AuditPeerRotateKey, Target: "p1"},
		{At: now.Add(-time.Hour), Actor: "alice", ActorRole: store.RoleMember, Action: control.AuditTokenRevoke, Target: "tk_2"},
	}
	for _, e := range seed {
		if err := env.st.Audit().Append(ctx, e); err != nil {
			t.Fatal(err)
		}
	}

	if rec := env.do(env.handler, session{}, http.MethodGet, "/api/v1/audit", nil, false); rec.Code != http.StatusUnauthorized {
		t.Errorf("anonymous: %d", rec.Code)
	}
	_, alice := env.login("alice", "alicepassword")
	if rec := env.do(env.handler, alice, http.MethodGet, "/api/v1/audit", nil, false); rec.Code != http.StatusForbidden {
		t.Errorf("member: %d %s", rec.Code, rec.Body.String())
	}
	_, admin := env.login("markus", "adminpassword")
	list := func(h http.Handler, s session, query string) ([]auditView, int) {
		rec := env.do(h, s, http.MethodGet, "/api/v1/audit"+query, nil, false)
		if rec.Code != http.StatusOK {
			return nil, rec.Code
		}
		var out []auditView
		decode(t, rec, &out)
		return out, rec.Code
	}
	all, code := list(env.handler, admin, "")
	// The two logins above were recorded by the env's own users service
	// only when it carries an auditor; count what was seeded plus those.
	if code != http.StatusOK || len(all) < 3 || all[len(all)-1].Action != control.AuditTokenCreate || all[len(all)-1].Details["owner"] != "alice" {
		t.Fatalf("admin list: %d %+v", code, all)
	}
	if got, _ := list(env.handler, admin, "?action=peer.rotate_key"); len(got) != 1 || got[0].Actor != "peer:nas" || got[0].ActorRole != control.RolePeer {
		t.Errorf("action filter: %+v", got)
	}
	if got, _ := list(env.handler, admin, "?actor=alice&action=token.revoke"); len(got) != 1 || got[0].Target != "tk_2" {
		t.Errorf("actor filter: %+v", got)
	}
	if got, _ := list(env.handler, admin, "?since=24h"); len(got) != len(all)-1 {
		t.Errorf("since duration: %d of %d", len(got), len(all))
	}
	if got, _ := list(env.handler, admin, "?since="+url.QueryEscape(now.Add(-90*time.Minute).Format(time.RFC3339))); len(got) != len(all)-2 {
		t.Errorf("since time: %d of %d", len(got), len(all))
	}
	if got, _ := list(env.handler, admin, "?limit=1"); len(got) != 1 || got[0].ID != all[0].ID {
		t.Errorf("limit: %+v", got)
	}
	if got, _ := list(env.handler, admin, "?before_id="+itoa(all[1].ID)); len(got) != len(all)-2 || got[0].ID != all[2].ID {
		t.Errorf("before_id: %+v", got)
	}
	for _, bad := range []string{"?since=yesterday", "?since=-5m", "?limit=0", "?limit=5000", "?before_id=x", "?before_id=-1"} {
		if _, code := list(env.handler, admin, bad); code != http.StatusBadRequest {
			t.Errorf("%s: %d, want 400", bad, code)
		}
	}
	// The admin socket needs no session.
	if got, code := list(env.local, session{}, "?action=token.create"); code != http.StatusOK || len(got) != 1 {
		t.Errorf("local: %d %+v", code, got)
	}
	// Without the dependency the route does not exist.
	bare := newRESTEnv(t)
	_, bareAdmin := bare.login("markus", "adminpassword")
	if rec := bare.do(bare.handler, bareAdmin, http.MethodGet, "/api/v1/audit", nil, false); rec.Code != http.StatusNotFound {
		t.Errorf("without Audit dep: %d", rec.Code)
	}
}

func itoa(n int64) string { return strconv.FormatInt(n, 10) }
