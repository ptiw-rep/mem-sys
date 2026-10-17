package http

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"

	"github.com/go-chi/chi/v5"

	"mem-sys/internal/service"
	"mem-sys/internal/store"
)

type envelope map[string]interface{}

func writeJSON(w http.ResponseWriter, status int, v interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func decodeJSON(r *http.Request, v interface{}) error {
	body, err := io.ReadAll(r.Body)
	if err != nil {
		return err
	}
	if len(body) == 0 {
		return fmt.Errorf("empty body")
	}
	return json.Unmarshal(body, v)
}

func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, envelope{"error": msg})
}

func actor(r *http.Request) *service.Actor {
	return service.ActorFromCtx(r.Context())
}

// ---- /me ----

func (s *Server) handleMe(w http.ResponseWriter, r *http.Request) {
	a := actor(r)
	owned, err := s.svc.St().ListModulesOwnedByPrincipal(r.Context(), a.PrincipalID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	roles := []map[string]string{}
	for _, m := range owned {
		roles = append(roles, map[string]string{"module_id": m, "role": "owner"})
	}
	writeJSON(w, http.StatusOK, envelope{
		"principal_id":   a.PrincipalID,
		"global_role":    a.GlobalRole,
		"module_roles":   roles,
	})
}

// ---- modules ----

type createModuleReq struct {
	Name             string   `json:"name"`
	Description      string   `json:"description"`
	PathPatterns     []string `json:"path_patterns"`
	ExcludePatterns  []string `json:"exclude_patterns"`
	OwnerIDs         []string `json:"owner_ids"`
	ApprovalPolicy   interface{} `json:"approval_policy"`
}

func (s *Server) handleCreateModule(w http.ResponseWriter, r *http.Request) {
	a := actor(r)
	if !a.IsAdmin() {
		writeError(w, http.StatusForbidden, "admin required")
		return
	}
	var in createModuleReq
	if err := decodeJSON(r, &in); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if in.Name == "" {
		writeError(w, http.StatusBadRequest, "name required")
		return
	}
	if _, err := s.svc.St().GetModuleByName(r.Context(), in.Name); err == nil {
		writeError(w, http.StatusConflict, "module already exists")
		return
	}
	mod := store.Module{
		Name:             in.Name,
		Description:      in.Description,
		PathPatterns:     in.PathPatterns,
		ExcludePatterns:  in.ExcludePatterns,
		Status:           "active",
		CreatedBy:        a.PrincipalID,
	}
	if err := s.svc.St().CreateModule(r.Context(), mod); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	created, _ := s.svc.St().GetModuleByName(r.Context(), in.Name)
	for _, oid := range in.OwnerIDs {
		_ = s.svc.St().GrantModuleOwner(r.Context(), store.ModuleOwnerGrant{
			ModuleID:    created.ID,
			PrincipalID: oid,
			GrantedBy:   a.PrincipalID,
		})
	}
	created, _ = s.svc.St().GetModule(r.Context(), created.ID)
	writeJSON(w, http.StatusCreated, envelope{"module": created})
}

func (s *Server) handleListModules(w http.ResponseWriter, r *http.Request) {
	modules, err := s.svc.St().ListModules(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, envelope{"modules": modules})
}

func (s *Server) handleGetModule(w http.ResponseWriter, r *http.Request) {
	id := chiParam(r, "module_id")
	mod, err := s.svc.St().GetModule(r.Context(), id)
	if err != nil {
		writeError(w, http.StatusNotFound, "module not found")
		return
	}
	writeJSON(w, http.StatusOK, envelope{"module": mod})
}

func (s *Server) handlePatchModule(w http.ResponseWriter, r *http.Request) {
	id := chiParam(r, "module_id")
	var updates map[string]interface{}
	if err := decodeJSON(r, &updates); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if err := s.svc.St().PatchModule(r.Context(), id, updates); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, envelope{"ok": true})
}

func (s *Server) handleListModuleFacts(w http.ResponseWriter, r *http.Request) {
	id := chiParam(r, "module_id")
	facts, err := s.svc.St().ListFacts(r.Context(), &id, nil)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, envelope{"facts": facts})
}

func (s *Server) handleListModuleProposals(w http.ResponseWriter, r *http.Request) {
	id := chiParam(r, "module_id")
	props, err := s.svc.St().ListProposals(r.Context(), &id, nil)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, envelope{"proposals": props})
}

type grantOwnerReq struct {
	PrincipalID string `json:"principal_id"`
}

func (s *Server) handleGrantOwner(w http.ResponseWriter, r *http.Request) {
	a := actor(r)
	if !a.IsAdmin() {
		writeError(w, http.StatusForbidden, "admin required")
		return
	}
	id := chiParam(r, "module_id")
	var in grantOwnerReq
	if err := decodeJSON(r, &in); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if _, err := s.svc.St().GetPrincipal(r.Context(), in.PrincipalID); err != nil {
		writeError(w, http.StatusNotFound, "principal not found")
		return
	}
	if err := s.svc.St().GrantModuleOwner(r.Context(), store.ModuleOwnerGrant{
		ModuleID:    id,
		PrincipalID: in.PrincipalID,
		GrantedBy:   a.PrincipalID,
	}); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, envelope{"ok": true})
}

func (s *Server) handleRevokeOwner(w http.ResponseWriter, r *http.Request) {
	a := actor(r)
	if !a.IsAdmin() {
		writeError(w, http.StatusForbidden, "admin required")
		return
	}
	id := chiParam(r, "module_id")
	pid := chiParam(r, "principal_id")
	if err := s.svc.St().RevokeModuleOwner(r.Context(), id, pid); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, envelope{"ok": true})
}

func (s *Server) handleDecommission(w http.ResponseWriter, r *http.Request) {
	a := actor(r)
	if !a.IsAdmin() {
		writeError(w, http.StatusForbidden, "admin required")
		return
	}
	id := chiParam(r, "module_id")
	_ = id
	writeJSON(w, http.StatusOK, envelope{"ok": true, "note": "decommission workflow placeholder"})
}

// ---- proposals ----

type createProposalReq struct {
	Text               string  `json:"text"`
	RepoHint           string  `json:"repo_hint"`
	ModuleHint         string  `json:"module_hint"`
	FileHint           string  `json:"file_hint"`
	SymbolHint         string  `json:"symbol_hint"`
	Intent             string  `json:"intent"`
	IdempotencyKey     string  `json:"idempotency_key"`
	MappingConfidence  float64 `json:"mapping_confidence"`
	MappingExplanation string  `json:"mapping_explanation"`
}

func (s *Server) handleCreateProposal(w http.ResponseWriter, r *http.Request) {
	a := actor(r)
	var in createProposalReq
	if err := decodeJSON(r, &in); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	intent := store.ProposalIntent(in.Intent)
	if intent == "" {
		intent = store.IntentNewFact
	}
	text := in.Text
	if in.RepoHint != "" || in.ModuleHint != "" || in.FileHint != "" || in.SymbolHint != "" {
		text = fmt.Sprintf("%s | repo:%s module:%s file:%s symbol:%s", text, in.RepoHint, in.ModuleHint, in.FileHint, in.SymbolHint)
	}
	var targetModuleID *string
	if in.ModuleHint != "" {
		if m, err := s.svc.St().GetModuleByName(r.Context(), in.ModuleHint); err == nil {
			targetModuleID = &m.ID
		}
	}
	p, err := s.svc.CreateProposal(r.Context(), service.CreateProposalInput{
		SubmitterID:         a.PrincipalID,
		Source:              "http",
		RawPayload:          text,
		NormalizedText:      text,
		Intent:              intent,
		TargetModuleID:      targetModuleID,
		MappingConfidence:   in.MappingConfidence,
		MappingExplanation:  in.MappingExplanation,
		IdempotencyKey:      in.IdempotencyKey,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, envelope{"proposal": p})
}

func (s *Server) handleListProposals(w http.ResponseWriter, r *http.Request) {
	moduleID := r.URL.Query().Get("module_id")
	status := r.URL.Query().Get("status")
	var mID, st *string
	if moduleID != "" {
		mID = &moduleID
	}
	if status != "" {
		st = &status
	}
	props, err := s.svc.St().ListProposals(r.Context(), mID, st)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, envelope{"proposals": props})
}

func (s *Server) handleGetProposal(w http.ResponseWriter, r *http.Request) {
	id := chiParam(r, "proposal_id")
	p, err := s.svc.St().GetProposal(r.Context(), id)
	if err != nil {
		writeError(w, http.StatusNotFound, "proposal not found")
		return
	}
	writeJSON(w, http.StatusOK, envelope{"proposal": p})
}

type reviewProposalReq struct {
	Action               string  `json:"action"`
	Comment              string  `json:"comment"`
	EditedTitle          string  `json:"edited_title"`
	EditedBody           string  `json:"edited_body"`
	ActingRole           string  `json:"acting_role"`
	UseAdminOverride     bool    `json:"use_admin_override"`
	AdminOverrideReason  string  `json:"admin_override_reason"`
	NewModuleID          string  `json:"new_module_id"`
}

func (s *Server) handleReviewProposal(w http.ResponseWriter, r *http.Request) {
	id := chiParam(r, "proposal_id")
	var in reviewProposalReq
	if err := decodeJSON(r, &in); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if in.ActingRole == "" {
		in.ActingRole = "module_owner"
	}
	var newModID *string
	if in.NewModuleID != "" {
		newModID = &in.NewModuleID
	}
	p, err := s.svc.ReviewProposal(r.Context(), service.ReviewProposalInput{
		ProposalID:          id,
		Action:              in.Action,
		Comment:             in.Comment,
		EditedTitle:         in.EditedTitle,
		EditedBody:          in.EditedBody,
		ActingRole:          in.ActingRole,
		UseAdminOverride:    in.UseAdminOverride,
		AdminOverrideReason: in.AdminOverrideReason,
		NewModuleID:         newModID,
	})
	if err != nil {
		switch {
		case errors.Is(err, service.ErrForbidden):
			writeError(w, http.StatusForbidden, err.Error())
		case errors.Is(err, service.ErrNotFound):
			writeError(w, http.StatusNotFound, err.Error())
		default:
			writeError(w, http.StatusBadRequest, err.Error())
		}
		return
	}
	writeJSON(w, http.StatusOK, envelope{"proposal": p})
}

// ---- facts ----

func (s *Server) handleListFacts(w http.ResponseWriter, r *http.Request) {
	facts, err := s.svc.St().ListFacts(r.Context(), nil, nil)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, envelope{"facts": facts})
}

func (s *Server) handleGetFact(w http.ResponseWriter, r *http.Request) {
	id := chiParam(r, "fact_id")
	f, err := s.svc.St().GetFact(r.Context(), id)
	if err != nil {
		writeError(w, http.StatusNotFound, "fact not found")
		return
	}
	revisions, _ := s.svc.St().ListFactRevisions(r.Context(), id)
	placements, _ := s.svc.St().GetFactPlacements(r.Context(), id)
	anchors, _ := s.svc.St().ListFactAnchors(r.Context(), id)
	writeJSON(w, http.StatusOK, envelope{
		"fact":        f,
		"revisions":   revisions,
		"placements":  placements,
		"anchors":     anchors,
	})
}

func (s *Server) handleListFactRevisions(w http.ResponseWriter, r *http.Request) {
	id := chiParam(r, "fact_id")
	revs, err := s.svc.St().ListFactRevisions(r.Context(), id)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, envelope{"revisions": revs})
}

func (s *Server) handleDeprecateFact(w http.ResponseWriter, r *http.Request) {
	id := chiParam(r, "fact_id")
	if err := s.svc.St().UpdateFactStatus(r.Context(), id, "deprecated"); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, envelope{"ok": true})
}

func (s *Server) handleArchiveFact(w http.ResponseWriter, r *http.Request) {
	id := chiParam(r, "fact_id")
	if err := s.svc.St().UpdateFactStatus(r.Context(), id, "archived"); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, envelope{"ok": true})
}

type moveFactReq struct {
	ToModuleID         string `json:"to_module_id"`
	Reason             string `json:"reason"`
	UseAdminOverride   bool   `json:"use_admin_override"`
	AdminOverrideReason string `json:"admin_override_reason"`
}

func (s *Server) handleMoveFact(w http.ResponseWriter, r *http.Request) {
	a := actor(r)
	id := chiParam(r, "fact_id")
	var in moveFactReq
	if err := decodeJSON(r, &in); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	mr, err := s.svc.MoveFact(r.Context(), service.MoveFactInput{
		FactID:               id,
		ToModuleID:           in.ToModuleID,
		Reason:               in.Reason,
		InitiatedBy:          a.PrincipalID,
		UseAdminOverride:     in.UseAdminOverride,
		AdminOverrideReason:  in.AdminOverrideReason,
	})
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, envelope{"move_request": mr})
}

// ---- moves ----

type createMoveReq struct {
	FactID     string `json:"fact_id"`
	ToModuleID string `json:"to_module_id"`
	Reason     string `json:"reason"`
}

func (s *Server) handleCreateMoveRequest(w http.ResponseWriter, r *http.Request) {
	a := actor(r)
	var in createMoveReq
	if err := decodeJSON(r, &in); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	mr, err := s.svc.MoveFact(r.Context(), service.MoveFactInput{
		FactID:     in.FactID,
		ToModuleID: in.ToModuleID,
		Reason:     in.Reason,
		InitiatedBy: a.PrincipalID,
	})
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, envelope{"move_request": mr})
}

func (s *Server) handleGetMoveRequest(w http.ResponseWriter, r *http.Request) {
	id := chiParam(r, "move_id")
	mr, err := s.svc.St().GetMoveRequest(r.Context(), id)
	if err != nil {
		writeError(w, http.StatusNotFound, "move request not found")
		return
	}
	writeJSON(w, http.StatusOK, envelope{"move_request": mr})
}

type reviewMoveReq struct {
	Action                string `json:"action"`
	Comment               string `json:"comment"`
	UseAdminOverride      bool   `json:"use_admin_override"`
	AdminOverrideReason   string `json:"admin_override_reason"`
}

func (s *Server) handleReviewMoveRequest(w http.ResponseWriter, r *http.Request) {
	id := chiParam(r, "move_id")
	var in reviewMoveReq
	if err := decodeJSON(r, &in); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	mr, err := s.svc.ReviewMoveRequest(r.Context(), service.ReviewMoveInput{
		MoveRequestID:       id,
		Action:              in.Action,
		Comment:             in.Comment,
		UseAdminOverride:    in.UseAdminOverride,
		AdminOverrideReason: in.AdminOverrideReason,
	})
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, envelope{"move_request": mr})
}

// ---- exports ----

type createExportReq struct {
	ModuleID string `json:"module_id"`
	Format   string `json:"format"`
}

func (s *Server) handleCreateExport(w http.ResponseWriter, r *http.Request) {
	var in createExportReq
	if err := decodeJSON(r, &in); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if in.Format == "" {
		in.Format = "json"
	}
	data, err := s.svc.ExportModule(r.Context(), service.ExportScope{
		ModuleID: in.ModuleID,
		Format:   in.Format,
	})
	if err != nil {
		writeError(w, http.StatusNotFound, err.Error())
		return
	}
	if in.Format == "markdown" {
		w.Header().Set("Content-Type", "text/markdown")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(data)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(data)
}

func (s *Server) handleGetExport(w http.ResponseWriter, r *http.Request) {
	id := chiParam(r, "export_id")
	_ = id
	writeJSON(w, http.StatusOK, envelope{"export_id": id, "status": "completed", "note": "export inline"})
}

// ---- helpers ----

func chiParam(r *http.Request, key string) string {
	return chi.URLParam(r, key)
}
