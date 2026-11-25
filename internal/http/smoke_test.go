package http

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"mem-sys/internal/db"
	"mem-sys/internal/service"
	"mem-sys/internal/store"
)

type respEnvelope struct {
	Proposal *store.Proposal `json:"proposal,omitempty"`
	Modules  []store.Module  `json:"modules,omitempty"`
	Facts    []store.Fact    `json:"facts,omitempty"`
	OK       bool            `json:"ok,omitempty"`
	Error    string          `json:"error,omitempty"`
}

func smokeServer(t *testing.T) (*Server, http.Handler, string) {
	t.Helper()
	d, err := db.Open(":memory:")
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	t.Cleanup(func() { _ = d.Close() })

	st := store.New(d)
	svc := service.New(st, service.Config{AdminOverrideRequiresReason: true})

	ctx := context.Background()
	_ = st.CreatePrincipal(ctx, store.Principal{ID: "admin-alice", Name: "Alice", Type: "human", GlobalRole: "admin"})
	_ = st.CreatePrincipal(ctx, store.Principal{ID: "user-bob", Name: "Bob", Type: "human", GlobalRole: "user"})

	if _, err := st.GetModuleByName(ctx, "auth"); err != nil {
		mod := store.Module{Name: "auth", Description: "Auth module", Status: "active", CreatedBy: "admin-alice"}
		if err := st.CreateModule(ctx, mod); err != nil {
			t.Fatalf("create module: %v", err)
		}
	}

	m, err := st.GetModuleByName(ctx, "auth")
	if err != nil {
		t.Fatalf("get module: %v", err)
	}
	if err := st.GrantModuleOwner(ctx, store.ModuleOwnerGrant{ModuleID: m.ID, PrincipalID: "admin-alice", GrantedBy: "admin-alice"}); err != nil {
		t.Fatalf("grant owner: %v", err)
	}

	s := New(svc)
	return s, s.Handler(), m.ID
}

func do(t *testing.T, h http.Handler, method, path, token string, body interface{}) (*http.Response, []byte) {
	t.Helper()
	var rbody io.Reader
	if body != nil {
		j, _ := json.Marshal(body)
		rbody = bytes.NewReader(j)
	}
	req, _ := http.NewRequest(method, path, rbody)
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	return w.Result(), w.Body.Bytes()
}

func parseResp(t *testing.T, body []byte) respEnvelope {
	t.Helper()
	var e respEnvelope
	if err := json.Unmarshal(body, &e); err != nil {
		t.Fatalf("parse envelope: %v body=%s", err, string(body))
	}
	return e
}

func TestSmokeFlow(t *testing.T) {
	_, h, moduleID := smokeServer(t)

	// 1. GET /me as alice
	res, body := do(t, h, "GET", "/api/v1/me", "admin-alice", nil)
	if res.StatusCode != http.StatusOK {
		t.Fatalf("me: %d %s", res.StatusCode, string(body))
	}
	e := parseResp(t, body)

	// 2. List modules
	res, body = do(t, h, "GET", "/api/v1/modules", "admin-alice", nil)
	if res.StatusCode != http.StatusOK {
		t.Fatalf("list modules: %d %s", res.StatusCode, string(body))
	}
	e = parseResp(t, body)
	if len(e.Modules) == 0 {
		t.Fatal("no modules returned")
	}

	// 3. Bob submits a proposal
	propBody := map[string]interface{}{
		"text":               "The login endpoint rate limits users to 5 attempts per minute.",
		"module_hint":        "auth",
		"intent":             "new_fact",
		"idempotency_key":    "prop-smoke-001",
		"mapping_confidence": 0.9,
	}
	res, body = do(t, h, "POST", "/api/v1/proposals", "user-bob", propBody)
	if res.StatusCode != http.StatusCreated {
		t.Fatalf("create proposal: %d %s", res.StatusCode, string(body))
	}
	e = parseResp(t, body)
	if e.Proposal == nil || e.Proposal.ID == "" {
		t.Fatalf("proposal not created: %s", string(body))
	}
	propID := e.Proposal.ID

	// 4. Alice accepts the proposal
	reviewBody := map[string]interface{}{
		"action":       "accept",
		"acting_role":  "module_owner",
		"edited_title": "Login rate limit",
		"edited_body":  "The login endpoint allows 5 attempts per minute per user.",
	}
	res, body = do(t, h, "POST", "/api/v1/proposals/"+propID+"/review", "admin-alice", reviewBody)
	if res.StatusCode != http.StatusOK {
		t.Fatalf("review proposal: %d %s", res.StatusCode, string(body))
	}
	e = parseResp(t, body)
	if e.Proposal == nil || e.Proposal.Status != "accepted" {
		t.Fatalf("proposal not accepted: status=%s body=%s", e.Proposal.Status, string(body))
	}

	// 5. Facts in module
	res, body = do(t, h, "GET", "/api/v1/modules/"+moduleID+"/facts", "admin-alice", nil)
	if res.StatusCode != http.StatusOK {
		t.Fatalf("list facts: %d %s", res.StatusCode, string(body))
	}
	e = parseResp(t, body)
	if len(e.Facts) != 1 {
		t.Fatalf("expected 1 fact, got %d", len(e.Facts))
	}
	factID := e.Facts[0].ID

	// 6. Fact detail includes revisions and placements
	res, body = do(t, h, "GET", "/api/v1/facts/"+factID, "admin-alice", nil)
	if res.StatusCode != http.StatusOK {
		t.Fatalf("get fact: %d %s", res.StatusCode, string(body))
	}

	// 7. Export module as JSON
	exportBody := map[string]interface{}{"module_id": moduleID, "format": "json"}
	res, body = do(t, h, "POST", "/api/v1/exports", "admin-alice", exportBody)
	if res.StatusCode != http.StatusOK {
		t.Fatalf("export: %d %s", res.StatusCode, string(body))
	}
	if !bytes.Contains(body, []byte("login rate limit")) && !bytes.Contains(body, []byte("Login rate limit")) {
		t.Fatalf("export missing fact body: %s", string(body))
	}

	// 8. Unauthorized without token
	res, body = do(t, h, "GET", "/api/v1/me", "", nil)
	if res.StatusCode != http.StatusUnauthorized {
		t.Fatalf("expected 401 without token, got %d %s", res.StatusCode, string(body))
	}

	// 9. Non-owner cannot review
	res, body = do(t, h, "POST", "/api/v1/proposals/"+propID+"/review", "user-bob", map[string]interface{}{"action": "reject"})
	if res.StatusCode != http.StatusForbidden {
		t.Fatalf("expected 403 for non-owner review, got %d %s", res.StatusCode, string(body))
	}
}

func TestIdempotencyKeyHTTP(t *testing.T) {
	_, h, _ := smokeServer(t)

	// 1. Create key with explicit principal
	res, body := do(t, h, "POST", "/api/v1/idempotency-keys", "admin-alice", map[string]interface{}{
		"principal_id": "user-bob",
		"max_uses":     2,
		"ttl_seconds":  3600,
	})
	if res.StatusCode != http.StatusCreated {
		t.Fatalf("create key: %d %s", res.StatusCode, string(body))
	}
	var keyResp struct {
		IdempotencyKey struct {
			Key string `json:"key"`
		} `json:"idempotency_key"`
	}
	if err := json.Unmarshal(body, &keyResp); err != nil {
		t.Fatalf("parse key response: %v body=%s", err, string(body))
	}
	if keyResp.IdempotencyKey.Key == "" {
		t.Fatal("key is empty")
	}

	// 2. Create key without principal_id -> defaults to actor (admin-alice)
	res, body = do(t, h, "POST", "/api/v1/idempotency-keys", "admin-alice", map[string]interface{}{})
	if res.StatusCode != http.StatusCreated {
		t.Fatalf("create key default principal: %d %s", res.StatusCode, string(body))
	}

	// 3. Nonexistent principal_id -> 400
	res, body = do(t, h, "POST", "/api/v1/idempotency-keys", "admin-alice", map[string]interface{}{
		"principal_id": "nonexistent-user",
	})
	if res.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400 for nonexistent principal, got %d %s", res.StatusCode, string(body))
	}

	// 4. Invalid max_uses (0 is allowed, defaults to 1)
	res, body = do(t, h, "POST", "/api/v1/idempotency-keys", "user-bob", map[string]interface{}{
		"principal_id": "user-bob",
		"max_uses":     0,
	})
	if res.StatusCode != http.StatusCreated {
		t.Fatalf("create key max_uses 0: %d %s", res.StatusCode, string(body))
	}
}

func TestMultiProjectHTTP(t *testing.T) {
	_, h, _ := smokeServer(t)

	// 1. Create project (admin only)
	res, body := do(t, h, "POST", "/api/v1/projects", "admin-alice", map[string]interface{}{
		"name":        "backend",
		"description": "Backend services",
	})
	if res.StatusCode != http.StatusCreated {
		t.Fatalf("create project: %d %s", res.StatusCode, string(body))
	}
	var projResp struct {
		Project struct {
			ID   string `json:"id"`
			Name string `json:"name"`
		} `json:"project"`
	}
	if err := json.Unmarshal(body, &projResp); err != nil {
		t.Fatalf("parse project: %v", err)
	}
	if projResp.Project.ID == "" || projResp.Project.Name != "backend" {
		t.Fatalf("unexpected project: %+v", projResp)
	}
	projectID := projResp.Project.ID

	// 2. Non-admin cannot create project
	res, body = do(t, h, "POST", "/api/v1/projects", "user-bob", map[string]interface{}{
		"name": "nope",
	})
	if res.StatusCode != http.StatusForbidden {
		t.Fatalf("expected 403 for non-admin project create, got %d %s", res.StatusCode, string(body))
	}

	// 3. Duplicate project name -> 409
	res, body = do(t, h, "POST", "/api/v1/projects", "admin-alice", map[string]interface{}{
		"name": "backend",
	})
	if res.StatusCode != http.StatusConflict {
		t.Fatalf("expected 409 for duplicate project, got %d %s", res.StatusCode, string(body))
	}

	// 4. List projects - should include default + backend
	res, body = do(t, h, "GET", "/api/v1/projects", "admin-alice", nil)
	if res.StatusCode != http.StatusOK {
		t.Fatalf("list projects: %d %s", res.StatusCode, string(body))
	}
	var listResp struct {
		Projects []struct {
			ID   string `json:"id"`
			Name string `json:"name"`
		} `json:"projects"`
	}
	if err := json.Unmarshal(body, &listResp); err != nil {
		t.Fatalf("parse projects list: %v", err)
	}
	if len(listResp.Projects) < 2 {
		t.Fatalf("expected at least 2 projects (default + backend), got %d", len(listResp.Projects))
	}

	// 5. Get project
	res, body = do(t, h, "GET", "/api/v1/projects/"+projectID, "admin-alice", nil)
	if res.StatusCode != http.StatusOK {
		t.Fatalf("get project: %d %s", res.StatusCode, string(body))
	}

	// 6. Set project patterns
	res, body = do(t, h, "PUT", "/api/v1/projects/"+projectID+"/patterns", "admin-alice", map[string]interface{}{
		"path_patterns": []string{"services/auth/**", "services/billing/**"},
	})
	if res.StatusCode != http.StatusOK {
		t.Fatalf("set patterns: %d %s", res.StatusCode, string(body))
	}

	// 7. Get project patterns
	res, body = do(t, h, "GET", "/api/v1/projects/"+projectID+"/patterns", "admin-alice", nil)
	if res.StatusCode != http.StatusOK {
		t.Fatalf("get patterns: %d %s", res.StatusCode, string(body))
	}
	var patResp struct {
		PathPatterns []string `json:"path_patterns"`
	}
	if err := json.Unmarshal(body, &patResp); err != nil {
		t.Fatalf("parse patterns: %v", err)
	}
	if len(patResp.PathPatterns) != 2 {
		t.Fatalf("expected 2 patterns, got %d", len(patResp.PathPatterns))
	}

	// 8. Create module in project with allowed pattern
	res, body = do(t, h, "POST", "/api/v1/modules", "admin-alice", map[string]interface{}{
		"name":          "auth-svc",
		"project_id":    projectID,
		"path_patterns": []string{"services/auth/**"},
	})
	if res.StatusCode != http.StatusCreated {
		t.Fatalf("create module in project: %d %s", res.StatusCode, string(body))
	}
	var modResp struct {
		Module struct {
			ID        string `json:"id"`
			ProjectID string `json:"project_id"`
		} `json:"module"`
	}
	if err := json.Unmarshal(body, &modResp); err != nil {
		t.Fatalf("parse module: %v", err)
	}
	if modResp.Module.ProjectID != projectID {
		t.Fatalf("expected module project_id %s, got %s", projectID, modResp.Module.ProjectID)
	}
	authModID := modResp.Module.ID

	// 9. Create module with disallowed pattern in project -> 400
	res, body = do(t, h, "POST", "/api/v1/modules", "admin-alice", map[string]interface{}{
		"name":          "payments",
		"project_id":    projectID,
		"path_patterns": []string{"services/payments/**"},
	})
	if res.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400 for disallowed pattern, got %d %s", res.StatusCode, string(body))
	}

	// 10. List project modules
	res, body = do(t, h, "GET", "/api/v1/projects/"+projectID+"/modules", "admin-alice", nil)
	if res.StatusCode != http.StatusOK {
		t.Fatalf("list project modules: %d %s", res.StatusCode, string(body))
	}
	var modListResp struct {
		Modules []struct {
			ID string `json:"id"`
		} `json:"modules"`
	}
	if err := json.Unmarshal(body, &modListResp); err != nil {
		t.Fatalf("parse module list: %v", err)
	}
	if len(modListResp.Modules) != 1 {
		t.Fatalf("expected 1 module in project, got %d", len(modListResp.Modules))
	}

	// 11. Reconcile - change patterns to invalidate auth-svc, then reconcile
	res, body = do(t, h, "PUT", "/api/v1/projects/"+projectID+"/patterns", "admin-alice", map[string]interface{}{
		"path_patterns": []string{"services/billing/**"},
	})
	if res.StatusCode != http.StatusOK {
		t.Fatalf("update patterns: %d %s", res.StatusCode, string(body))
	}
	res, body = do(t, h, "POST", "/api/v1/modules/reconcile?project_id="+projectID, "admin-alice", nil)
	if res.StatusCode != http.StatusOK {
		t.Fatalf("reconcile: %d %s", res.StatusCode, string(body))
	}
	var recResp struct {
		Affected []string `json:"affected"`
		Count    int      `json:"count"`
	}
	if err := json.Unmarshal(body, &recResp); err != nil {
		t.Fatalf("parse reconcile: %v", err)
	}
	if recResp.Count != 1 {
		t.Fatalf("expected 1 affected module, got %d body=%s", recResp.Count, string(body))
	}
	found := false
	for _, id := range recResp.Affected {
		if id == authModID {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected auth-svc (%s) in affected, got %v", authModID, recResp.Affected)
	}

	// 12. Verify module is now read_only
	res, body = do(t, h, "GET", "/api/v1/modules/"+authModID, "admin-alice", nil)
	if res.StatusCode != http.StatusOK {
		t.Fatalf("get module after reconcile: %d %s", res.StatusCode, string(body))
	}
	var modAfter struct {
		Module struct {
			Status string `json:"status"`
		} `json:"module"`
	}
	if err := json.Unmarshal(body, &modAfter); err != nil {
		t.Fatalf("parse module: %v", err)
	}
	if modAfter.Module.Status != "read_only" {
		t.Fatalf("expected read_only, got %s", modAfter.Module.Status)
	}
}

func TestMultiProjectCrossIsolationHTTP(t *testing.T) {
	_, h, _ := smokeServer(t)

	// Create two projects with different patterns
	mkProj := func(name string, patterns []string) string {
		t.Helper()
		res, body := do(t, h, "POST", "/api/v1/projects", "admin-alice", map[string]interface{}{"name": name})
		if res.StatusCode != http.StatusCreated {
			t.Fatalf("create project %s: %d %s", name, res.StatusCode, string(body))
		}
		var r struct {
			Project struct {
				ID string `json:"id"`
			} `json:"project"`
		}
		_ = json.Unmarshal(body, &r)
		res, body = do(t, h, "PUT", "/api/v1/projects/"+r.Project.ID+"/patterns", "admin-alice", map[string]interface{}{"path_patterns": patterns})
		if res.StatusCode != http.StatusOK {
			t.Fatalf("set patterns %s: %d %s", name, res.StatusCode, string(body))
		}
		return r.Project.ID
	}
	pA := mkProj("proj-a", []string{"services/a/**"})
	pB := mkProj("proj-b", []string{"services/b/**"})

	mkMod := func(name, pid, pat string) string {
		t.Helper()
		res, body := do(t, h, "POST", "/api/v1/modules", "admin-alice", map[string]interface{}{
			"name": name, "project_id": pid, "path_patterns": []string{pat},
		})
		if res.StatusCode != http.StatusCreated {
			t.Fatalf("create module %s: %d %s", name, res.StatusCode, string(body))
		}
		var r struct {
			Module struct {
				ID string `json:"id"`
			} `json:"module"`
		}
		_ = json.Unmarshal(body, &r)
		return r.Module.ID
	}
	modA := mkMod("mod-a", pA, "services/a/**")
	modB := mkMod("mod-b", pB, "services/b/**")

	// Invalidate project A patterns -> reconcile A only
	do(t, h, "PUT", "/api/v1/projects/"+pA+"/patterns", "admin-alice", map[string]interface{}{"path_patterns": []string{"services/new/**"}})
	res, body := do(t, h, "POST", "/api/v1/modules/reconcile?project_id="+pA, "admin-alice", nil)
	if res.StatusCode != http.StatusOK {
		t.Fatalf("reconcile A: %d %s", res.StatusCode, string(body))
	}
	var rec struct {
		Affected []string `json:"affected"`
	}
	_ = json.Unmarshal(body, &rec)
	if len(rec.Affected) != 1 || rec.Affected[0] != modA {
		t.Fatalf("expected only modA affected, got %v", rec.Affected)
	}

	// modB should still be active
	res, body = do(t, h, "GET", "/api/v1/modules/"+modB, "admin-alice", nil)
	var m struct {
		Module struct {
			Status string `json:"status"`
		} `json:"module"`
	}
	_ = json.Unmarshal(body, &m)
	if m.Module.Status != "active" {
		t.Fatalf("modB should remain active, got %s", m.Module.Status)
	}
}
