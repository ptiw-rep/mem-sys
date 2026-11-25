package service

import (
	"context"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"mem-sys/internal/db"
	"mem-sys/internal/store"
)

func TestMatchGlob(t *testing.T) {
	tests := []struct {
		pattern string
		name    string
		want    bool
	}{
		// trailing /** matches dir and everything beneath
		{"services/auth/**", "services/auth/login.go", true},
		{"services/auth/**", "services/auth", true},
		{"services/auth/**", "services/auth/sub/deep/file.go", true},
		{"services/auth/**", "services/other/login.go", false},
		// /** alone matches everything
		{"**", "anything/at/all", true},
		{"**", "singlefile.go", true},
		// single * matches one segment
		{"services/*", "services/auth", true},
		{"services/*", "services/auth/login.go", false},
		// ? matches single char
		{"file?.go", "file1.go", true},
		{"file?.go", "file12.go", false},
		// exact match
		{"internal/store/facts.go", "internal/store/facts.go", true},
		{"internal/store/facts.go", "internal/store/modules.go", false},
		// ** in middle
		{"src/**/test.go", "src/a/b/test.go", true},
		{"src/**/test.go", "src/test.go", true},
		{"src/**/test.go", "other/src/test.go", false},
	}
	for _, tt := range tests {
		if got := matchGlob(tt.pattern, tt.name); got != tt.want {
			t.Errorf("matchGlob(%q, %q) = %v, want %v", tt.pattern, tt.name, got, tt.want)
		}
	}
}

func TestAllowlistIsAllowed(t *testing.T) {
	al := &Allowlist{patterns: []string{"services/auth/**", "packages/core/**"}}

	if !al.IsAllowed("services/auth/login.go") {
		t.Error("expected services/auth/login.go to be allowed")
	}
	if !al.IsAllowed("services/auth") {
		t.Error("expected services/auth to be allowed")
	}
	if al.IsAllowed("services/billing/login.go") {
		t.Error("expected services/billing/login.go to be disallowed")
	}
	if !al.IsAllowed("packages/core/util.go") {
		t.Error("expected packages/core/util.go to be allowed")
	}
}

func TestAllowlistPatternAllowed(t *testing.T) {
	al := &Allowlist{patterns: []string{"services/auth/**", "packages/core/**"}}

	if !al.PatternAllowed("services/auth/**") {
		t.Error("expected services/auth/** to be pattern-allowed")
	}
	if al.PatternAllowed("services/billing/**") {
		t.Error("expected services/billing/** to be pattern-disallowed")
	}
	// services/auth is covered by services/auth/** (trailing /** matches the dir itself)
	if !al.PatternAllowed("services/auth") {
		t.Error("expected services/auth to be pattern-allowed (covered by services/auth/**)")
	}
}

func TestAllowlistNil(t *testing.T) {
	var al *Allowlist
	if !al.IsAllowed("anything") {
		t.Error("nil allowlist should allow everything")
	}
	if !al.PatternAllowed("anything") {
		t.Error("nil allowlist should allow any pattern")
	}
	if al.AllowedModules() != nil {
		t.Error("nil allowlist AllowedModules should return nil")
	}
}

// --- Integration tests using in-memory SQLite ---

func newTestServiceWithPatterns(t *testing.T, patterns []string) (*serviceHarness, error) {
	t.Helper()

	// Write a temp project_pattern.json
	dir := t.TempDir()
	patternFile := filepath.Join(dir, "project_pattern.json")
	content := `{"root":"test","module_count":` + strconv.Itoa(len(patterns)) + `,"path_patterns":[` + strings.Join(quoted(patterns), ",") + `]}`
	if err := os.WriteFile(patternFile, []byte(content), 0644); err != nil {
		return nil, err
	}

	d, err := db.Open(":memory:")
	if err != nil {
		return nil, err
	}
	st := store.New(d)
	svc, err := NewWithPatterns(st, Config{AdminOverrideRequiresReason: true}, patternFile)
	if err != nil {
		d.Close()
		return nil, err
	}
	return &serviceHarness{svc: svc, db: d, st: st}, nil
}

func newTestServiceNoAllowlist(t *testing.T) *serviceHarness {
	t.Helper()
	d, err := db.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	st := store.New(d)
	svc := New(st, Config{AdminOverrideRequiresReason: true})
	return &serviceHarness{svc: svc, db: d, st: st}
}

type serviceHarness struct {
	svc *Service
	db  *db.DB
	st  *store.Store
}

func (h *serviceHarness) close() {
	h.db.Close()
}

func (h *serviceHarness) seedPrincipal(ctx context.Context, id, name, globalRole string) error {
	return h.st.CreatePrincipal(ctx, store.Principal{ID: id, Name: name, Type: "human", GlobalRole: globalRole})
}

func (h *serviceHarness) seedModule(ctx context.Context, name string, patterns []string, status string) (*store.Module, error) {
	mod := store.Module{Name: name, Description: "test", PathPatterns: patterns, Status: status, CreatedBy: "admin-alice"}
	if err := h.st.CreateModule(ctx, mod); err != nil {
		return nil, err
	}
	return h.st.GetModuleByName(ctx, name)
}

func (h *serviceHarness) grantOwner(ctx context.Context, moduleID, principalID string) error {
	return h.st.GrantModuleOwner(ctx, store.ModuleOwnerGrant{ModuleID: moduleID, PrincipalID: principalID, GrantedBy: "admin-alice"})
}

func TestValidateModulePatterns(t *testing.T) {
	ctx := context.Background()
	h, err := newTestServiceWithPatterns(t, []string{"services/auth/**", "packages/core/**"})
	if err != nil {
		t.Fatal(err)
	}
	defer h.close()

	if err := h.seedPrincipal(ctx, "admin-alice", "Alice", "admin"); err != nil {
		t.Fatal(err)
	}

	// Valid module: all patterns in allowlist
	mod, err := h.seedModule(ctx, "auth", []string{"services/auth/**"}, "active")
	if err != nil {
		t.Fatal(err)
	}
	if err := h.svc.ValidateModulePatterns(ctx, mod.ID); err != nil {
		t.Errorf("expected valid module to pass, got: %v", err)
	}

	// Invalid module: pattern not in allowlist
	mod2, err := h.seedModule(ctx, "billing", []string{"services/billing/**"}, "active")
	if err != nil {
		t.Fatal(err)
	}
	if err := h.svc.ValidateModulePatterns(ctx, mod2.ID); err == nil {
		t.Error("expected invalid module pattern to fail validation")
	} else {
		t.Logf("got expected error: %v", err)
	}

	// Non-existent module
	if err := h.svc.ValidateModulePatterns(ctx, "does-not-exist"); err != ErrNotFound {
		t.Errorf("expected ErrNotFound for missing module, got: %v", err)
	}

	// Nil allowlist service should pass everything
	h2 := newTestServiceNoAllowlist(t)
	defer h2.close()
	if err := h2.svc.ValidateModulePatterns(ctx, "any-module"); err != nil {
		t.Errorf("nil allowlist should not error, got: %v", err)
	}
}

func TestReconcileModules(t *testing.T) {
	ctx := context.Background()
	h, err := newTestServiceWithPatterns(t, []string{"services/auth/**", "packages/core/**"})
	if err != nil {
		t.Fatal(err)
	}
	defer h.close()

	if err := h.seedPrincipal(ctx, "admin-alice", "Alice", "admin"); err != nil {
		t.Fatal(err)
	}

	// Active module with valid pattern -> stays active
	modValid, err := h.seedModule(ctx, "auth", []string{"services/auth/**"}, "active")
	if err != nil {
		t.Fatal(err)
	}

	// Active module with invalid pattern -> becomes read_only
	modInvalid, err := h.seedModule(ctx, "legacy", []string{"services/legacy/**"}, "active")
	if err != nil {
		t.Fatal(err)
	}

	// Already read_only module -> skipped
	modRO, err := h.seedModule(ctx, "archive", []string{"services/archive/**"}, "read_only")
	if err != nil {
		t.Fatal(err)
	}

	affected, err := h.svc.ReconcileModules(ctx)
	if err != nil {
		t.Fatal(err)
	}

	// Only the invalid active module should be affected
	if len(affected) != 1 || affected[0] != modInvalid.ID {
		t.Errorf("expected only legacy module affected, got %v", affected)
	}

	// Verify statuses
	mValid, _ := h.st.GetModule(ctx, modValid.ID)
	if mValid.Status != "active" {
		t.Errorf("expected valid module to stay active, got %s", mValid.Status)
	}
	mInvalid, _ := h.st.GetModule(ctx, modInvalid.ID)
	if mInvalid.Status != "read_only" {
		t.Errorf("expected invalid module to be read_only, got %s", mInvalid.Status)
	}
	mRO, _ := h.st.GetModule(ctx, modRO.ID)
	if mRO.Status != "read_only" {
		t.Errorf("expected read_only module to stay read_only, got %s", mRO.Status)
	}

	// Nil allowlist reconcile should be a no-op
	h2 := newTestServiceNoAllowlist(t)
	defer h2.close()
	affected2, err := h2.svc.ReconcileModules(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(affected2) != 0 {
		t.Errorf("nil allowlist reconcile should affect nothing, got %v", affected2)
	}
}

func TestAcceptProposalRespectsAllowlist(t *testing.T) {
	ctx := context.Background()
	h, err := newTestServiceWithPatterns(t, []string{"services/auth/**"})
	if err != nil {
		t.Fatal(err)
	}
	defer h.close()

	if err := h.seedPrincipal(ctx, "admin-alice", "Alice", "admin"); err != nil {
		t.Fatal(err)
	}
	if err := h.seedPrincipal(ctx, "user-bob", "Bob", "user"); err != nil {
		t.Fatal(err)
	}

	// Allowed module
	modAllowed, err := h.seedModule(ctx, "auth", []string{"services/auth/**"}, "active")
	if err != nil {
		t.Fatal(err)
	}
	if err := h.grantOwner(ctx, modAllowed.ID, "admin-alice"); err != nil {
		t.Fatal(err)
	}

	// Disallowed module
	modDisallowed, err := h.seedModule(ctx, "billing", []string{"services/billing/**"}, "active")
	if err != nil {
		t.Fatal(err)
	}
	if err := h.grantOwner(ctx, modDisallowed.ID, "admin-alice"); err != nil {
		t.Fatal(err)
	}

	// Proposal into allowed module should be accepted
	pAllowed, err := h.svc.CreateProposal(ctx, CreateProposalInput{
		SubmitterID:    "user-bob",
		Source:         "http",
		RawPayload:     "login rate limit",
		NormalizedText: "login rate limit",
		Intent:         store.IntentNewFact,
		TargetModuleID: &modAllowed.ID,
	})
	if err != nil {
		t.Fatal(err)
	}
	actor := &Actor{PrincipalID: "admin-alice", GlobalRole: "admin", ModuleRoles: map[string]bool{modAllowed.ID: true}}
	ctxA := WithActor(ctx, actor)
	pAllowed, err = h.svc.ReviewProposal(ctxA, ReviewProposalInput{
		ProposalID: pAllowed.ID,
		Action:     "accept",
	})
	if err != nil {
		t.Errorf("expected proposal into allowed module to succeed, got: %v", err)
	}
	if pAllowed.Status != "accepted" {
		t.Errorf("expected accepted, got %s", pAllowed.Status)
	}

	// Proposal into disallowed module should be rejected at accept time
	pDisallowed, err := h.svc.CreateProposal(ctx, CreateProposalInput{
		SubmitterID:    "user-bob",
		Source:         "http",
		RawPayload:     "billing logic",
		NormalizedText: "billing logic",
		Intent:         store.IntentNewFact,
		TargetModuleID: &modDisallowed.ID,
	})
	if err != nil {
		t.Fatal(err)
	}
	actor2 := &Actor{PrincipalID: "admin-alice", GlobalRole: "admin", ModuleRoles: map[string]bool{modDisallowed.ID: true}}
	ctxA2 := WithActor(ctx, actor2)
	_, err = h.svc.ReviewProposal(ctxA2, ReviewProposalInput{
		ProposalID: pDisallowed.ID,
		Action:     "accept",
	})
	if err == nil {
		t.Error("expected proposal into disallowed module to be rejected")
	} else {
		t.Logf("got expected error: %v", err)
	}
}

// --- helpers ---

func quoted(strs []string) []string {
	out := make([]string, len(strs))
	for i, s := range strs {
		out[i] = `"` + s + `"`
	}
	return out
}
