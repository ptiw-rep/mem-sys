package service

import (
	"context"
	"testing"
	"time"

	"mem-sys/internal/db"
	"mem-sys/internal/store"
)

func TestMVPFlow(t *testing.T) {
	d, err := db.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	st := store.New(d)
	svc := New(st, Config{AdminOverrideRequiresReason: true})
	ctx := context.Background()

	// principals
	if err := st.CreatePrincipal(ctx, store.Principal{ID: "admin-alice", Name: "Alice", Type: "human", GlobalRole: "admin"}); err != nil {
		t.Fatal(err)
	}
	if err := st.CreatePrincipal(ctx, store.Principal{ID: "user-bob", Name: "Bob", Type: "human", GlobalRole: "user"}); err != nil {
		t.Fatal(err)
	}

	// module
	mod := store.Module{Name: "auth", Description: "Auth module", Status: "active", CreatedBy: "admin-alice"}
	if err := st.CreateModule(ctx, mod); err != nil {
		t.Fatal(err)
	}
	m, err := st.GetModuleByName(ctx, "auth")
	if err != nil {
		t.Fatal(err)
	}
	if err := st.GrantModuleOwner(ctx, store.ModuleOwnerGrant{ModuleID: m.ID, PrincipalID: "admin-alice", GrantedBy: "admin-alice"}); err != nil {
		t.Fatal(err)
	}

	// bob submits proposal
	p, err := svc.CreateProposal(ctx, CreateProposalInput{
		SubmitterID:    "user-bob",
		Source:         "http",
		RawPayload:     "login rate limit 5/min",
		NormalizedText: "login rate limit 5/min",
		Intent:         store.IntentNewFact,
		TargetModuleID: &m.ID,
	})
	if err != nil {
		t.Fatal(err)
	}
	if p.ID == "" {
		t.Fatal("proposal id empty")
	}

	// alice (owner) accepts
	actor := &Actor{PrincipalID: "admin-alice", GlobalRole: "admin", ModuleRoles: map[string]bool{m.ID: true}}
	ctx = WithActor(ctx, actor)
	p, err = svc.ReviewProposal(ctx, ReviewProposalInput{
		ProposalID: p.ID,
		Action:     "accept",
	})
	if err != nil {
		t.Fatal(err)
	}
	if p.Status != "accepted" {
		t.Fatalf("expected accepted, got %s", p.Status)
	}

	// fact created
	facts, err := st.ListFacts(ctx, &m.ID, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(facts) != 1 {
		t.Fatalf("expected 1 fact, got %d", len(facts))
	}
	f := facts[0]
	if f.Title == "" {
		t.Fatal("fact title empty")
	}

	// revisions
	revs, err := st.ListFactRevisions(ctx, f.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(revs) != 1 {
		t.Fatalf("expected 1 revision, got %d", len(revs))
	}

	// placement
	placements, err := st.GetFactPlacements(ctx, f.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(placements) != 1 || placements[0].Status != "active" {
		t.Fatalf("expected 1 active placement, got %+v", placements)
	}

	// export
	data, err := svc.ExportModule(ctx, ExportScope{ModuleID: m.ID, Format: "json"})
	if err != nil {
		t.Fatal(err)
	}
	if len(data) == 0 {
		t.Fatal("export empty")
	}

	// events
	events, err := st.ListEvents(ctx, "proposal", p.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(events) == 0 {
		t.Fatal("no events logged")
	}

	t.Logf("MVP flow OK: proposal=%s fact=%s revs=%d placements=%d events=%d export=%d bytes",
		p.ID, f.ID, len(revs), len(placements), len(events), len(data))
}

func TestIdempotencyKeyFlow(t *testing.T) {
	d, err := db.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	st := store.New(d)
	svc := New(st, Config{AdminOverrideRequiresReason: true})
	ctx := context.Background()

	// create a principal
	if err := st.CreatePrincipal(ctx, store.Principal{ID: "user-bob", Name: "Bob", Type: "human", GlobalRole: "user"}); err != nil {
		t.Fatal(err)
	}

	// generate a key with default TTL and max uses
	key, err := svc.GenerateIdempotencyKey(ctx, GenerateIdempotencyKeyInput{
		PrincipalID: "user-bob",
	})
	if err != nil {
		t.Fatal(err)
	}
	if key.Key == "" {
		t.Fatal("key is empty")
	}
	if key.PrincipalID != "user-bob" {
		t.Fatalf("expected principal user-bob, got %s", key.PrincipalID)
	}
	if key.MaxUses != 1 {
		t.Fatalf("expected max_uses 1, got %d", key.MaxUses)
	}
	if key.UsedCount != 0 {
		t.Fatalf("expected used_count 0, got %d", key.UsedCount)
	}
	if key.ExpiresAt.IsZero() {
		t.Fatal("expires_at is zero")
	}

	// validate and consume
	consumed, err := st.ValidateAndConsumeIdempotencyKey(ctx, key.Key)
	if err != nil {
		t.Fatal(err)
	}
	if consumed.UsedCount != 1 {
		t.Fatalf("expected used_count 1, got %d", consumed.UsedCount)
	}

	// second consume should fail (max uses reached)
	_, err = st.ValidateAndConsumeIdempotencyKey(ctx, key.Key)
	if err == nil {
		t.Fatal("expected error on second consume (max uses reached)")
	}

	// nonexistent key should fail
	_, err = st.ValidateAndConsumeIdempotencyKey(ctx, "nonexistent-key")
	if err == nil {
		t.Fatal("expected error on nonexistent key")
	}

	// custom max uses
	key2, err := svc.GenerateIdempotencyKey(ctx, GenerateIdempotencyKeyInput{
		PrincipalID: "user-bob",
		MaxUses:     3,
		TTL:         time.Hour,
	})
	if err != nil {
		t.Fatal(err)
	}
	if key2.MaxUses != 3 {
		t.Fatalf("expected max_uses 3, got %d", key2.MaxUses)
	}

	// invalid principal
	_, err = svc.GenerateIdempotencyKey(ctx, GenerateIdempotencyKeyInput{
		PrincipalID: "nonexistent",
	})
	if err == nil {
		t.Fatal("expected error for nonexistent principal")
	}
}
