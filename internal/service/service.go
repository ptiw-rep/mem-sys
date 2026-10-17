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
	ErrNotFound      = errors.New("not found")
	ErrUnauthorized  = errors.New("unauthorized")
	ErrForbidden     = errors.New("forbidden")
	ErrBadRequest    = errors.New("bad request")
	ErrConflict      = errors.New("conflict")
)

type RoleUsed string

const (
	RoleModuleOwner RoleUsed = "module_owner"
	RoleAdmin       RoleUsed = "admin"
)

type Actor struct {
	PrincipalID   string
	GlobalRole    string
	ModuleRoles   map[string]bool // module_id -> is owner
}

func (a *Actor) IsAdmin() bool {
	return a.GlobalRole == "admin"
}

func (a *Actor) IsOwnerOf(moduleID string) bool {
	return a.ModuleRoles != nil && a.ModuleRoles[moduleID]
}

// AuthzDecision captures how an action was authorized for audit.
type AuthzDecision struct {
	RoleUsed         RoleUsed
	AdminOverride    bool
	Reason           string
	ModuleID         string
	ActorGlobalRole  string
}

type Config struct {
	AdminOverrideRequiresReason bool
}

type Service struct {
	st   *store.Store
	cfg  Config
	now  func() time.Time
}

func New(st *store.Store, cfg Config) *Service {
	return &Service{st: st, cfg: cfg, now: time.Now}
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
	actorRole := a.GlobalRole
	if a == nil {
		actorRole = "system"
	}
	return s.st.AppendEvent(ctx, store.Event{
		AggregateType:       aggregateType,
		AggregateID:         aggregateID,
		ActorID:             a.PrincipalID,
		ActorGlobalRole:     actorRole,
		RoleUsed:            string(roleUsed),
		AdminOverrideUsed:   adminOverride,
		Action:              action,
		Payload:             payload(pay),
		CorrelationID:       correlationID,
		OccurredAt:          s.nowStr(),
	})
}

// ---------------- Proposals ----------------

type CreateProposalInput struct {
	SubmitterID          string
	Source               string
	RawPayload           string
	NormalizedText       string
	Intent               store.ProposalIntent
	LinkedFactID         *string
	TargetModuleID       *string
	MappingConfidence    float64
	MappingExplanation   string
	IdempotencyKey       string
}

func (s *Service) CreateProposal(ctx context.Context, in CreateProposalInput) (*store.Proposal, error) {
	p := &store.Proposal{
		IdempotencyKey:      in.IdempotencyKey,
		SubmitterID:         in.SubmitterID,
		Source:              in.Source,
		RawPayload:          in.RawPayload,
		NormalizedText:      in.NormalizedText,
		Intent:              in.Intent,
		LinkedFactID:        in.LinkedFactID,
		TargetModuleID:      in.TargetModuleID,
		Status:              "submitted",
		MappingConfidence:   in.MappingConfidence,
		MappingExplanation:  in.MappingExplanation,
	}
	if err := s.st.CreateProposal(ctx, *p); err != nil {
		if strings.Contains(err.Error(), "UNIQUE constraint failed") || strings.Contains(err.Error(), "unique constraint") {
			// idempotency: return existing
			existing, gerr := s.st.GetProposalByIdempotencyKey(ctx, in.IdempotencyKey)
			if gerr == nil {
				return existing, nil
			}
		}
		return nil, fmt.Errorf("create proposal: %w", err)
	}
	if err := s.logEvent(ctx, "proposal", p.ID, "proposal.created", &Actor{PrincipalID: in.SubmitterID}, nil, p, p.ID); err != nil {
		return nil, err
	}
	return p, nil
}

type ReviewProposalInput struct {
	ProposalID           string
	Action               string // accept, accept_with_edits, reject, duplicate, reassign, request_clarification
	Comment              string
	EditedTitle          string
	EditedBody           string
	ActingRole           string // module_owner | admin
	UseAdminOverride     bool
	AdminOverrideReason  string
	NewModuleID          *string // for reassign
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
	if 	targetModuleID == nil {
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
		f := &store.Fact{
			Title:           title,
			Kind:            "other",
			Status:          "active",
			OriginProposalID: &p.ID,
			CreatedBy:       actor.PrincipalID,
		}
		if err := s.st.CreateFact(ctx, *f); err != nil {
			return nil, err
		}
		factID = f.ID

		rev := &store.FactRevision{
			FactID:           f.ID,
			Version:          1,
			Title:            title,
			Body:             body,
			AuthorID:         actor.PrincipalID,
			ApprovedBy:       &actor.PrincipalID,
			SourceProposalID: &p.ID,
			ChangeReason:     "accepted from proposal " + p.ID,
		}
		if err := s.st.CreateFactRevision(ctx, *rev); err != nil {
			return nil, err
		}
		if err := s.st.SetFactCurrentRevision(ctx, f.ID, rev.ID); err != nil {
			return nil, err
		}

		if p.TargetModuleID != nil {
			place := &store.FactPlacement{
				FactID:        f.ID,
				ModuleID:      *p.TargetModuleID,
				PlacementType: "primary",
				Status:        "active",
				ApprovedBy:    &actor.PrincipalID,
				Reason:        "accepted from proposal " + p.ID,
			}
			if err := s.st.CreateFactPlacement(ctx, *place); err != nil {
				return nil, err
			}
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
	FactID               string
	ToModuleID           string
	Reason               string
	InitiatedBy          string
	ActingRole           string
	UseAdminOverride     bool
	AdminOverrideReason  string
	SourceRole           string
	TargetRole           string
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
		FactID:             in.FactID,
		FromModuleID:       fromModuleID,
		ToModuleID:         in.ToModuleID,
		Status:             "proposed",
		Reason:             in.Reason,
		InitiatedBy:        actor.PrincipalID,
		RoleUsedSource:     string(srcDec.RoleUsed),
		RoleUsedTarget:     string(tgtDec.RoleUsed),
		AdminOverrideUsed:  srcDec.AdminOverride || tgtDec.AdminOverride,
	}
	if err := s.st.CreateMoveRequest(ctx, *mr); err != nil {
		return nil, err
	}
	_ = s.logEvent(ctx, "fact", in.FactID, "fact.move_proposed", actor, srcDec, map[string]interface{}{
		"from_module": fromModuleID, "to_module": in.ToModuleID, "reason": in.Reason,
	}, in.FactID)
	return mr, nil
}

type ReviewMoveInput struct {
	MoveRequestID        string
	Action               string // accept or reject (target side)
	Comment              string
	ActingRole           string
	UseAdminOverride     bool
	AdminOverrideReason  string
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
			if err := s.st.CreateFactPlacement(ctx, *tgtPlace); err != nil {
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
	ModuleID  string
	Format    string // json | markdown
}

func (s *Service) ExportModule(ctx context.Context, scope ExportScope) ([]byte, error) {
	mod, err := s.st.GetModule(ctx, scope.ModuleID)
	if err != nil {
		return nil, ErrNotFound
	}
	facts, err := s.st.ListFacts(ctx, &scope.ModuleID, strPtr("active"))
	if err != nil {
		return nil, err
	}
	proposals, err := s.st.ListProposals(ctx, &scope.ModuleID, nil)
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
		ID        string `json:"id"`
		Title     string `json:"title"`
		Kind      string `json:"kind"`
		Status    string `json:"status"`
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
