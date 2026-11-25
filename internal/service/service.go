package service

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"mem-sys/internal/store"
)

var (
	ErrNotFound     = errors.New("not found")
	ErrUnauthorized = errors.New("unauthorized")
	ErrForbidden    = errors.New("forbidden")
	ErrBadRequest   = errors.New("bad request")
	ErrConflict     = errors.New("conflict")
)

type RoleUsed string

const (
	RoleModuleOwner RoleUsed = "module_owner"
	RoleAdmin       RoleUsed = "admin"
)

type Actor struct {
	PrincipalID string
	GlobalRole  string
	ModuleRoles map[string]bool // module_id -> is owner
}

func (a *Actor) IsAdmin() bool {
	return a.GlobalRole == "admin"
}

func (a *Actor) IsOwnerOf(moduleID string) bool {
	return a.ModuleRoles != nil && a.ModuleRoles[moduleID]
}

// AuthzDecision captures how an action was authorized for audit.
type AuthzDecision struct {
	RoleUsed        RoleUsed
	AdminOverride   bool
	Reason          string
	ModuleID        string
	ActorGlobalRole string
}

type Config struct {
	AdminOverrideRequiresReason bool
	DefaultIdempotencyTTL       time.Duration
	DefaultIdempotencyMaxUses   int
}

type Service struct {
	st                *store.Store
	cfg               Config
	now               func() time.Time
	projectAllowlists map[string]*Allowlist // projectID -> allowlist
	globalAllowlist   *Allowlist            // fallback when project has no allowlist
}

func New(st *store.Store, cfg Config) *Service {
	svc, _ := NewWithPatterns(st, cfg, "")
	return svc
}

// NewWithPatterns is like New but loads a global allowlist from
// project_pattern.json. When patternFile is empty, no global allowlist is
// enforced (useful for tests and for opting out). Per-project allowlists are
// loaded from the database (project_patterns table) on demand.
func NewWithPatterns(st *store.Store, cfg Config, patternFile string) (*Service, error) {
	var globalAL *Allowlist
	if patternFile != "" {
		var err error
		globalAL, err = NewAllowlist(patternFile)
		if err != nil {
			return nil, err
		}
	}
	if cfg.DefaultIdempotencyTTL <= 0 {
		cfg.DefaultIdempotencyTTL = 24 * time.Hour
	}
	if cfg.DefaultIdempotencyMaxUses <= 0 {
		cfg.DefaultIdempotencyMaxUses = 1
	}
	return &Service{
		st:                st,
		cfg:               cfg,
		now:               time.Now,
		projectAllowlists: make(map[string]*Allowlist),
		globalAllowlist:   globalAL,
	}, nil
}

// AllowlistFor returns the allowlist for the given project. Falls back to the
// global allowlist if the project has none configured. Returns nil if neither
// is available (no restriction).
func (s *Service) AllowlistFor(ctx context.Context, projectID string) (*Allowlist, error) {
	if al, ok := s.projectAllowlists[projectID]; ok {
		return al, nil
	}
	// Try to load from DB
	patterns, err := s.st.GetProjectPatterns(ctx, projectID)
	if err != nil {
		return nil, err
	}
	if len(patterns) > 0 {
		al := &Allowlist{patterns: patterns}
		s.projectAllowlists[projectID] = al
		return al, nil
	}
	// Fall back to global
	return s.globalAllowlist, nil
}

// GlobalAllowlist returns the global allowlist, or nil if none was configured.
func (s *Service) GlobalAllowlist() *Allowlist {
	return s.globalAllowlist
}

// InvalidateProjectAllowlist clears the cached allowlist for a project so it
// will be reloaded from the database on next lookup.
func (s *Service) InvalidateProjectAllowlist(projectID string) {
	delete(s.projectAllowlists, projectID)
}

func (s *Service) nowStr() string {
	return s.now().UTC().Format(time.RFC3339)
}

// Authenticate resolves an actor from a principal id (bearer token sub).
func (s *Service) Authenticate(ctx context.Context, principalID string) (*Actor, error) {
	p, err := s.st.GetPrincipal(ctx, principalID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) || err.Error() == "sql: no rows in result set" {
			return nil, ErrNotFound
		}
		return nil, err
	}
	owned, err := s.st.ListModulesOwnedByPrincipal(ctx, p.ID)
	if err != nil {
		return nil, err
	}
	roles := map[string]bool{}
	for _, m := range owned {
		roles[m] = true
	}
	return &Actor{PrincipalID: p.ID, GlobalRole: p.GlobalRole, ModuleRoles: roles}, nil
}

// CanReviewModule returns the authz decision for acting on a module.
// Admin + owner of target -> module_owner. Admin not owner -> admin override (reason required).
// Owner -> module_owner. Neither -> denied.
func (s *Service) CanReviewModule(a *Actor, moduleID string) (*AuthzDecision, error) {
	owner := a.IsOwnerOf(moduleID)
	if owner {
		return &AuthzDecision{RoleUsed: RoleModuleOwner, AdminOverride: false, ModuleID: moduleID, ActorGlobalRole: a.GlobalRole}, nil
	}
	if a.IsAdmin() {
		return &AuthzDecision{RoleUsed: RoleAdmin, AdminOverride: true, ModuleID: moduleID, ActorGlobalRole: a.GlobalRole}, nil
	}
	return nil, ErrForbidden
}

// ValidateModulePatterns checks that every path pattern on the module is
// present in its project's allowlist. Returns a descriptive error listing the
// first disallowed pattern. No allowlist means no restriction.
func (s *Service) ValidateModulePatterns(ctx context.Context, moduleID string) error {
	mod, err := s.st.GetModule(ctx, moduleID)
	if err != nil {
		return ErrNotFound
	}
	al, err := s.AllowlistFor(ctx, mod.ProjectID)
	if err != nil {
		return err
	}
	if al == nil {
		return nil
	}
	for _, pat := range mod.PathPatterns {
		if !al.PatternAllowed(pat) {
			return fmt.Errorf("module %q (project %q) has disallowed path pattern %q (allowed: %v)",
				moduleID, mod.ProjectID, pat, al.AllowedModules())
		}
	}
	return nil
}

// ReconcileModules compares each active module's path_patterns against its
// project's allowlist. Modules whose patterns no longer match any allowed
// pattern are moved to read_only so no new facts can be accepted into them.
// If projectID is non-empty, only modules in that project are reconciled.
// It returns a list of affected module IDs. This should be run after
// regenerating project_pattern.json (e.g. on a schedule or after a git sync).
func (s *Service) ReconcileModules(ctx context.Context, projectID ...string) ([]string, error) {
	var targetProject string
	if len(projectID) > 0 {
		targetProject = projectID[0]
	}
	var affected []string
	modules, err := s.st.ListModules(ctx)
	if err != nil {
		return nil, err
	}
	for _, mod := range modules {
		if mod.Status != "active" {
			continue
		}
		if targetProject != "" && mod.ProjectID != targetProject {
			continue
		}
		al, err := s.AllowlistFor(ctx, mod.ProjectID)
		if err != nil {
			return nil, err
		}
		if al == nil {
			continue // no allowlist -> no restriction
		}
		allowed := false
		for _, pat := range mod.PathPatterns {
			if al.PatternAllowed(pat) {
				allowed = true
				break
			}
		}
		if !allowed {
			if err := s.st.PatchModule(ctx, mod.ID, map[string]interface{}{
				"status": "read_only",
			}); err != nil {
				return nil, err
			}
			affected = append(affected, mod.ID)
			_ = s.logEvent(ctx, "module", mod.ID, "module.drift_detected", nil, nil, map[string]interface{}{
				"reason":   "path_patterns no longer in project allowlist",
				"project":  mod.ProjectID,
				"patterns": mod.PathPatterns,
			}, mod.ID)
		}
	}
	return affected, nil
}

func (s *Service) GetActorModuleRoles(ctx context.Context, a *Actor) ([]store.Principal, error) {
	return nil, nil
}

func payload(v interface{}) string {
	b, _ := json.Marshal(v)
	return string(b)
}

func (s *Service) logEvent(ctx context.Context, aggregateType, aggregateID, action string, a *Actor, dec *AuthzDecision, pay interface{}, correlationID string) error {
	roleUsed := RoleModuleOwner
	adminOverride := false
	if dec != nil {
		roleUsed = dec.RoleUsed
		adminOverride = dec.AdminOverride
	}
	actorID := "system"
	actorRole := "system"
	if a != nil {
		actorID = a.PrincipalID
		actorRole = a.GlobalRole
	}
	return s.st.AppendEvent(ctx, store.Event{
		AggregateType:     aggregateType,
		AggregateID:       aggregateID,
		ActorID:           actorID,
		ActorGlobalRole:   actorRole,
		RoleUsed:          string(roleUsed),
		AdminOverrideUsed: adminOverride,
		Action:            action,
		Payload:           payload(pay),
		CorrelationID:     correlationID,
		OccurredAt:        s.nowStr(),
	})
}

// ---------------- Proposals ----------------

type CreateProposalInput struct {
	SubmitterID        string
	Source             string
	RawPayload         string
	NormalizedText     string
	Intent             store.ProposalIntent
	LinkedFactID       *string
	TargetModuleID     *string
	PRNumber           *int
	MappingConfidence  float64
	MappingExplanation string
	IdempotencyKey     string
}

func (s *Service) CreateProposal(ctx context.Context, in CreateProposalInput) (*store.Proposal, error) {
	p := &store.Proposal{
		IdempotencyKey:     in.IdempotencyKey,
		SubmitterID:        in.SubmitterID,
		Source:             in.Source,
		RawPayload:         in.RawPayload,
		NormalizedText:     in.NormalizedText,
		Intent:             in.Intent,
		LinkedFactID:       in.LinkedFactID,
		TargetModuleID:     in.TargetModuleID,
		PRNumber:           in.PRNumber,
		Status:             "submitted",
		MappingConfidence:  in.MappingConfidence,
		MappingExplanation: in.MappingExplanation,
	}
	created, err := s.st.CreateProposal(ctx, *p)
	if err != nil {
		if strings.Contains(err.Error(), "UNIQUE constraint failed") || strings.Contains(err.Error(), "unique constraint") {
			// idempotency: return existing
			existing, gerr := s.st.GetProposalByIdempotencyKey(ctx, in.IdempotencyKey)
			if gerr == nil {
				return existing, nil
			}
		}
		return nil, fmt.Errorf("create proposal: %w", err)
	}
	if err := s.logEvent(ctx, "proposal", created.ID, "proposal.created", &Actor{PrincipalID: in.SubmitterID}, nil, created, created.ID); err != nil {
		return nil, err
	}
	return created, nil
}

type GenerateIdempotencyKeyInput struct {
	PrincipalID string        `json:"principal_id"`
	MaxUses     int           `json:"max_uses,omitempty"`
	TTL         time.Duration `json:"ttl,omitempty"`
}

func (s *Service) GenerateIdempotencyKey(ctx context.Context, in GenerateIdempotencyKeyInput) (*store.IdempotencyKey, error) {
	maxUses := in.MaxUses
	if maxUses <= 0 {
		maxUses = s.cfg.DefaultIdempotencyMaxUses
	}
	ttl := in.TTL
	if ttl <= 0 {
		ttl = s.cfg.DefaultIdempotencyTTL
	}

	return s.st.CreateIdempotencyKey(ctx, store.CreateIdempotencyKeyInput{
		PrincipalID: in.PrincipalID,
		MaxUses:     maxUses,
		TTL:         ttl,
	})
}

type ReviewProposalInput struct {
	ProposalID          string
	Action              string // accept, accept_with_edits, reject, duplicate, reassign, request_clarification
	Comment             string
	EditedTitle         string
	EditedBody          string
	ActingRole          string // module_owner | admin
	UseAdminOverride    bool
	AdminOverrideReason string
	NewModuleID         *string // for reassign
}

func (s *Service) ReviewProposal(ctx context.Context, in ReviewProposalInput) (*store.Proposal, error) {
	p, err := s.st.GetProposal(ctx, in.ProposalID)
	if err != nil {
		return nil, ErrNotFound
	}

	targetModuleID := p.TargetModuleID
	if in.NewModuleID != nil {
		targetModuleID = in.NewModuleID
	}
	if targetModuleID == nil {
		return nil, ErrBadRequest
	}

	// The actor must be supplied by the handler through context.
	actor := ActorFromCtx(ctx)
	if actor == nil {
		return nil, ErrUnauthorized
	}

	dec, err := s.CanReviewModule(actor, *targetModuleID)
	if err != nil {
		return nil, err
	}
	if dec.AdminOverride && s.cfg.AdminOverrideRequiresReason && in.AdminOverrideReason == "" {
		return nil, ErrBadRequest
	}
	dec.Reason = in.AdminOverrideReason

	switch in.Action {
	case "accept", "accept_with_edits":
		return s.acceptProposal(ctx, p, actor, dec, in)
	case "reject":
		return s.rejectProposal(ctx, p, actor, dec, in)
	case "duplicate":
		return s.markDuplicate(ctx, p, actor, dec, in)
	case "reassign":
		return s.reassignProposal(ctx, p, actor, dec, in)
	case "request_clarification":
		return s.requestClarification(ctx, p, actor, dec, in)
	default:
		return nil, ErrBadRequest
	}
}

func (s *Service) acceptProposal(ctx context.Context, p *store.Proposal, actor *Actor, dec *AuthzDecision, in ReviewProposalInput) (*store.Proposal, error) {
	// Guard: fact may only be placed into a module whose path patterns are
	// in the project's allowlist. This prevents a proposal from being accepted
	// into a module that is no longer authorized.
	if p.TargetModuleID != nil {
		if err := s.ValidateModulePatterns(ctx, *p.TargetModuleID); err != nil {
			return nil, ErrBadRequest
		}
	}

	title := in.EditedTitle
	body := in.EditedBody
	if title == "" {
		title = p.NormalizedText
		if len(title) > 80 {
			title = title[:80]
		}
	}
	if body == "" {
		body = p.NormalizedText
	}

	factID := ""
	{
		fact, err := s.st.CreateFact(ctx, store.Fact{
			Title:            title,
			Kind:             "other",
			Status:           "active",
			OriginProposalID: &p.ID,
			CreatedBy:        actor.PrincipalID,
		})
		if err != nil {
			return nil, err
		}
		factID = fact.ID

		rev, err := s.st.CreateFactRevision(ctx, store.FactRevision{
			FactID:           fact.ID,
			Version:          1,
			Title:            title,
			Body:             body,
			AuthorID:         actor.PrincipalID,
			ApprovedBy:       &actor.PrincipalID,
			SourceProposalID: &p.ID,
			ChangeReason:     "accepted from proposal " + p.ID,
		})
		if err != nil {
			return nil, err
		}
		if err := s.st.SetFactCurrentRevision(ctx, fact.ID, rev.ID); err != nil {
			return nil, err
		}

		if p.TargetModuleID != nil {
			place, err := s.st.CreateFactPlacement(ctx, store.FactPlacement{
				FactID:        fact.ID,
				ModuleID:      *p.TargetModuleID,
				PlacementType: "primary",
				Status:        "active",
				ApprovedBy:    &actor.PrincipalID,
				Reason:        "accepted from proposal " + p.ID,
			})
			if err != nil {
				return nil, err
			}
			_ = place
		}
	}

	if err := s.st.UpdateProposalStatus(ctx, p.ID, "accepted", p.MappingConfidence, p.MappingExplanation); err != nil {
		return nil, err
	}

	_ = s.logEvent(ctx, "proposal", p.ID, "proposal.reviewed", actor, dec, map[string]interface{}{
		"action": in.Action, "fact_id": factID, "comment": in.Comment,
	}, p.ID)
	_ = s.logEvent(ctx, "fact", factID, "fact.created", actor, dec, map[string]interface{}{
		"origin_proposal_id": p.ID, "title": title,
	}, factID)

	p.Status = "accepted"
	return p, nil
}

func (s *Service) rejectProposal(ctx context.Context, p *store.Proposal, actor *Actor, dec *AuthzDecision, in ReviewProposalInput) (*store.Proposal, error) {
	if err := s.st.UpdateProposalStatus(ctx, p.ID, "rejected", p.MappingConfidence, p.MappingExplanation); err != nil {
		return nil, err
	}
	_ = s.logEvent(ctx, "proposal", p.ID, "proposal.reviewed", actor, dec, map[string]interface{}{
		"action": "reject", "comment": in.Comment, "reason": in.AdminOverrideReason,
	}, p.ID)
	p.Status = "rejected"
	return p, nil
}

func (s *Service) markDuplicate(ctx context.Context, p *store.Proposal, actor *Actor, dec *AuthzDecision, in ReviewProposalInput) (*store.Proposal, error) {
	if err := s.st.UpdateProposalStatus(ctx, p.ID, "duplicate", p.MappingConfidence, p.MappingExplanation); err != nil {
		return nil, err
	}
	_ = s.logEvent(ctx, "proposal", p.ID, "proposal.reviewed", actor, dec, map[string]interface{}{
		"action": "duplicate", "comment": in.Comment,
	}, p.ID)
	p.Status = "duplicate"
	return p, nil
}

func (s *Service) reassignProposal(ctx context.Context, p *store.Proposal, actor *Actor, dec *AuthzDecision, in ReviewProposalInput) (*store.Proposal, error) {
	newID := in.NewModuleID
	if newID == nil {
		return nil, ErrBadRequest
	}
	if _, err := s.st.GetModule(ctx, *newID); err != nil {
		return nil, ErrNotFound
	}
	if err := s.st.UpdateProposalStatus(ctx, p.ID, "submitted", p.MappingConfidence, "reassigned to "+*newID); err != nil {
		return nil, err
	}
	if _, err := s.st.ExecContext(ctx, `UPDATE proposals SET target_module_id = ?, updated_at = ? WHERE id = ?`, *newID, s.nowStr(), p.ID); err != nil {
		return nil, err
	}
	_ = s.logEvent(ctx, "proposal", p.ID, "proposal.reviewed", actor, dec, map[string]interface{}{
		"action": "reassign", "new_module_id": *newID, "comment": in.Comment,
	}, p.ID)
	p.TargetModuleID = newID
	p.Status = "submitted"
	return p, nil
}

func (s *Service) requestClarification(ctx context.Context, p *store.Proposal, actor *Actor, dec *AuthzDecision, in ReviewProposalInput) (*store.Proposal, error) {
	if err := s.st.UpdateProposalStatus(ctx, p.ID, "needs_clarification", p.MappingConfidence, p.MappingExplanation); err != nil {
		return nil, err
	}
	_ = s.logEvent(ctx, "proposal", p.ID, "proposal.reviewed", actor, dec, map[string]interface{}{
		"action": "request_clarification", "comment": in.Comment,
	}, p.ID)
	p.Status = "needs_clarification"
	return p, nil
}

// ---------------- Facts: move ----------------

type MoveFactInput struct {
	FactID              string
	ToModuleID          string
	Reason              string
	InitiatedBy         string
	ActingRole          string
	UseAdminOverride    bool
	AdminOverrideReason string
	SourceRole          string
	TargetRole          string
}

func (s *Service) MoveFact(ctx context.Context, in MoveFactInput) (*store.MoveRequest, error) {
	if _, err := s.st.GetFact(ctx, in.FactID); err != nil {
		return nil, ErrNotFound
	}
	// find current active placement
	placements, err := s.st.GetFactPlacements(ctx, in.FactID)
	if err != nil {
		return nil, err
	}
	var fromModuleID string
	for _, pl := range placements {
		if pl.Status == "active" {
			fromModuleID = pl.ModuleID
			break
		}
	}
	if fromModuleID == "" {
		return nil, ErrBadRequest
	}
	if fromModuleID == in.ToModuleID {
		return nil, ErrBadRequest
	}

	actor := ActorFromCtx(ctx)
	if actor == nil {
		return nil, ErrUnauthorized
	}

	// source decision
	srcDec, err := s.CanReviewModule(actor, fromModuleID)
	if err != nil {
		return nil, err
	}
	// target decision
	tgtDec, err := s.CanReviewModule(actor, in.ToModuleID)
	if err != nil {
		return nil, err
	}

	mr := &store.MoveRequest{
		FactID:            in.FactID,
		FromModuleID:      fromModuleID,
		ToModuleID:        in.ToModuleID,
		Status:            "proposed",
		Reason:            in.Reason,
		InitiatedBy:       actor.PrincipalID,
		RoleUsedSource:    string(srcDec.RoleUsed),
		RoleUsedTarget:    string(tgtDec.RoleUsed),
		AdminOverrideUsed: srcDec.AdminOverride || tgtDec.AdminOverride,
	}
	createdMR, err := s.st.CreateMoveRequest(ctx, *mr)
	if err != nil {
		return nil, err
	}
	_ = s.logEvent(ctx, "fact", in.FactID, "fact.move_proposed", actor, srcDec, map[string]interface{}{
		"from_module": fromModuleID, "to_module": in.ToModuleID, "reason": in.Reason,
	}, in.FactID)
	return createdMR, nil
}

type ReviewMoveInput struct {
	MoveRequestID       string
	Action              string // accept or reject (target side)
	Comment             string
	ActingRole          string
	UseAdminOverride    bool
	AdminOverrideReason string
}

func (s *Service) ReviewMoveRequest(ctx context.Context, in ReviewMoveInput) (*store.MoveRequest, error) {
	mr, err := s.st.GetMoveRequest(ctx, in.MoveRequestID)
	if err != nil {
		return nil, ErrNotFound
	}
	actor := ActorFromCtx(ctx)
	if actor == nil {
		return nil, ErrUnauthorized
	}
	// target module owner/admin approval
	dec, err := s.CanReviewModule(actor, mr.ToModuleID)
	if err != nil {
		return nil, err
	}
	if dec.AdminOverride && s.cfg.AdminOverrideRequiresReason && in.AdminOverrideReason == "" {
		return nil, ErrBadRequest
	}
	dec.Reason = in.AdminOverrideReason

	if in.Action == "accept" {
		// mark target placement active, source archived
		{
			tgtPlace := &store.FactPlacement{
				FactID:        mr.FactID,
				ModuleID:      mr.ToModuleID,
				PlacementType: "primary",
				Status:        "active",
				ApprovedBy:    &actor.PrincipalID,
				Reason:        "move accepted from " + mr.FromModuleID,
			}
			if _, err := s.st.CreateFactPlacement(ctx, *tgtPlace); err != nil {
				return nil, err
			}
			placements, err := s.st.GetFactPlacements(ctx, mr.FactID)
			if err != nil {
				return nil, err
			}
			for _, pl := range placements {
				if pl.ModuleID == mr.FromModuleID && pl.Status == "active" {
					if err := s.st.UpdateFactPlacementStatus(ctx, pl.ID, "archived", &actor.PrincipalID); err != nil {
						return nil, err
					}
				}
			}
		}
		if err := s.st.UpdateMoveRequest(ctx, mr.ID, "accepted", "", actor.PrincipalID, "", string(dec.RoleUsed), dec.AdminOverride, strPtr(s.nowStr())); err != nil {
			return nil, err
		}
		_ = s.logEvent(ctx, "fact", mr.FactID, "fact.move_accepted", actor, dec, map[string]interface{}{
			"from_module": mr.FromModuleID, "to_module": mr.ToModuleID,
		}, mr.ID)
		mr.Status = "accepted"
		return mr, nil
	} else if in.Action == "reject" {
		if err := s.st.UpdateMoveRequest(ctx, mr.ID, "rejected", "", actor.PrincipalID, "", string(dec.RoleUsed), dec.AdminOverride, strPtr(s.nowStr())); err != nil {
			return nil, err
		}
		_ = s.logEvent(ctx, "fact", mr.FactID, "fact.move_rejected", actor, dec, map[string]interface{}{
			"from_module": mr.FromModuleID, "to_module": mr.ToModuleID,
		}, mr.ID)
		mr.Status = "rejected"
		return mr, nil
	}
	return nil, ErrBadRequest
}

// ---------------- Export ----------------

type ExportScope struct {
	ModuleID string
	Format   string // json | markdown
}

func (s *Service) ExportModule(ctx context.Context, scope ExportScope) ([]byte, error) {
	mod, err := s.st.GetModule(ctx, scope.ModuleID)
	if err != nil {
		return nil, ErrNotFound
	}
	facts, err := s.st.ListFacts(ctx, &scope.ModuleID, strPtr("active"), nil)
	if err != nil {
		return nil, err
	}
	proposals, err := s.st.ListProposals(ctx, &scope.ModuleID, nil, nil, nil)
	if err != nil {
		return nil, err
	}

	switch scope.Format {
	case "markdown":
		return exportMarkdown(mod, facts, proposals), nil
	default:
		return exportJSON(mod, facts, proposals)
	}
}

func exportJSON(mod *store.Module, facts []store.Fact, proposals []store.Proposal) ([]byte, error) {
	type factOut struct {
		ID        string               `json:"id"`
		Title     string               `json:"title"`
		Kind      string               `json:"kind"`
		Status    string               `json:"status"`
		Revisions []store.FactRevision `json:"revisions"`
	}
	out := map[string]interface{}{
		"export_version": "1.0",
		"exported_at":    time.Now().UTC().Format(time.RFC3339),
		"scope":          map[string]string{"module_id": mod.ID},
		"modules":        []store.Module{*mod},
		"facts":          facts,
		"proposals":      proposals,
		"events":         []interface{}{},
	}
	return json.MarshalIndent(out, "", "  ")
}

func exportMarkdown(mod *store.Module, facts []store.Fact, proposals []store.Proposal) []byte {
	var b strings.Builder
	b.WriteString("# Module: " + mod.Name + "\n\n")
	b.WriteString("## Accepted Facts\n\n")
	for _, f := range facts {
		b.WriteString("### " + f.Title + "\n\n")
		b.WriteString("- Status: " + f.Status + "\n")
		b.WriteString("- Kind: " + f.Kind + "\n\n")
	}
	b.WriteString("## Proposals\n\n")
	for _, p := range proposals {
		b.WriteString("### Proposal " + p.ID + "\n\n")
		b.WriteString("- Status: " + p.Status + "\n")
		b.WriteString("- Intent: " + string(p.Intent) + "\n\n")
	}
	return []byte(b.String())
}

// ---------------- helpers ----------------

type ctxKey string

const actorCtxKey ctxKey = "actor"

func WithActor(ctx context.Context, a *Actor) context.Context {
	return context.WithValue(ctx, actorCtxKey, a)
}

func ActorFromCtx(ctx context.Context) *Actor {
	a, _ := ctx.Value(actorCtxKey).(*Actor)
	return a
}

func (s *Service) St() *store.Store { return s.st }

func strPtr(s string) *string { return &s }
