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
		"text":             "The login endpoint rate limits users to 5 attempts per minute.",
		"module_hint":      "auth",
		"intent":           "new_fact",
		"idempotency_key":  "prop-smoke-001",
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
		"action":            "accept",
		"acting_role":       "module_owner",
		"edited_title":      "Login rate limit",
		"edited_body":       "The login endpoint allows 5 attempts per minute per user.",
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
