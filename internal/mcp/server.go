package mcp

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"os"
	"strings"

	"mem-sys/internal/service"
	"mem-sys/internal/store"
)

type Server struct {
	svc         *service.Service
	log         *slog.Logger
	in          *bufio.Reader
	out         io.Writer
	principalID string
	ctx         context.Context
}

type jsonrpcReq struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      interface{}     `json:"id"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params"`
}

type jsonrpcResp struct {
	JSONRPC string      `json:"jsonrpc"`
	ID      interface{} `json:"id"`
	Result  interface{} `json:"result,omitempty"`
	Error   *jsonrpcErr `json:"error,omitempty"`
}

type jsonrpcErr struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

func New(svc *service.Service) *Server {
	ctx := service.WithActor(context.Background(), &service.Actor{
		PrincipalID: envOr("MEM_SYS_PRINCIPAL", "user-local"),
		GlobalRole:  "user",
	})
	return &Server{
		svc:         svc,
		log:         slog.New(slog.NewTextHandler(io.Discard, nil)),
		in:          bufio.NewReader(os.Stdin),
		out:         os.Stdout,
		principalID: envOr("MEM_SYS_PRINCIPAL", "user-local"),
		ctx:         ctx,
	}
}

func envOr(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}

func (s *Server) Run() error {
	for {
		line, err := s.in.ReadString('\n')
		if err != nil {
			if err == io.EOF {
				return nil
			}
			return err
		}
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		s.handle(line)
	}
}

func (s *Server) handle(line string) {
	var req jsonrpcReq
	if err := json.Unmarshal([]byte(line), &req); err != nil {
		s.write(nil, -32700, "Parse error", nil)
		return
	}
	resp := s.dispatch(req)
	b, _ := json.Marshal(resp)
	fmt.Fprintln(s.out, string(b))
}

func (s *Server) write(id interface{}, code int, msg string, result interface{}) {
	resp := jsonrpcResp{JSONRPC: "2.0", ID: id}
	if code != 0 {
		resp.Error = &jsonrpcErr{Code: code, Message: msg}
	} else {
		resp.Result = result
	}
	b, _ := json.Marshal(resp)
	fmt.Fprintln(s.out, string(b))
}

func (s *Server) dispatch(req jsonrpcReq) jsonrpcResp {
	switch req.Method {
	case "initialize":
		return jsonrpcResp{JSONRPC: "2.0", ID: req.ID, Result: map[string]interface{}{
			"protocolVersion": "2024-11-05",
			"capabilities":    map[string]interface{}{},
			"serverInfo":      map[string]interface{}{"name": "mem-sys", "version": "1.0.0"},
		}}
	case "tools/list":
		return jsonrpcResp{JSONRPC: "2.0", ID: req.ID, Result: map[string]interface{}{"tools": toolsList()}}
	case "tools/call":
		return s.toolsCall(req)
	default:
		return jsonrpcResp{JSONRPC: "2.0", ID: req.ID, Error: &jsonrpcErr{Code: -32601, Message: "Method not found: " + req.Method}}
	}
}

func (s *Server) toolsCall(req jsonrpcReq) jsonrpcResp {
	var params struct {
		Name      string          `json:"name"`
		Arguments json.RawMessage `json:"arguments"`
	}
	if err := json.Unmarshal(req.Params, &params); err != nil {
		return jsonrpcResp{JSONRPC: "2.0", ID: req.ID, Error: &jsonrpcErr{Code: -32602, Message: "Invalid params"}}
	}

	ctx := s.ctx
	var result interface{}
	var err error

	switch params.Name {
	case "get_my_roles":
		result, err = s.getMyRoles(ctx)
	case "list_modules":
		result, err = s.listModules(ctx)
	case "get_module":
		result, err = s.getModule(ctx, params.Arguments)
	case "list_module_facts":
		result, err = s.listModuleFacts(ctx, params.Arguments)
	case "get_fact":
		result, err = s.getFact(ctx, params.Arguments)
	case "get_fact_history":
		result, err = s.getFactHistory(ctx, params.Arguments)
	case "propose_fact":
		result, err = s.proposeFact(ctx, params.Arguments)
	case "list_proposals":
		result, err = s.listProposals(ctx, params.Arguments)
	case "get_proposal":
		result, err = s.getProposal(ctx, params.Arguments)
	case "review_proposal":
		result, err = s.reviewProposal(ctx, params.Arguments)
	case "move_fact":
		result, err = s.moveFact(ctx, params.Arguments)
	case "list_move_requests":
		result, err = s.listMoveRequests(ctx, params.Arguments)
	case "review_move_request":
		result, err = s.reviewMoveRequest(ctx, params.Arguments)
	case "add_module":
		result, err = s.addModule(ctx, params.Arguments)
	case "decommission_module":
		result, err = s.decommissionModule(ctx, params.Arguments)
	case "export_module":
		result, err = s.exportModule(ctx, params.Arguments)
	case "get_anchor_status":
		result, err = s.getAnchorStatus(ctx, params.Arguments)
	default:
		return jsonrpcResp{JSONRPC: "2.0", ID: req.ID, Error: &jsonrpcErr{Code: -32601, Message: "Unknown tool: " + params.Name}}
	}

	if err != nil {
		return jsonrpcResp{JSONRPC: "2.0", ID: req.ID, Error: &jsonrpcErr{Code: -32603, Message: err.Error()}}
	}
	return jsonrpcResp{JSONRPC: "2.0", ID: req.ID, Result: result}
}

// ---------- tool implementations ----------

type idParams struct {
	ID string `json:"id"`
}

type moduleParams struct {
	ModuleID string `json:"module_id"`
}

type factParams struct {
	FactID string `json:"fact_id"`
}

type moveParams struct {
	MoveID string `json:"move_id"`
}

type proposeFactArgs struct {
	Text               string  `json:"text"`
	ModuleHint         string  `json:"module_hint"`
	FileHint           string  `json:"file_hint"`
	SymbolHint         string  `json:"symbol_hint"`
	Intent             string  `json:"intent"`
	IdempotencyKey     string  `json:"idempotency_key"`
	MappingConfidence  float64 `json:"mapping_confidence"`
}

func (s *Server) getMyRoles(ctx context.Context) (interface{}, error) {
	a := service.ActorFromCtx(ctx)
	owned, err := s.svc.St().ListModulesOwnedByPrincipal(ctx, a.PrincipalID)
	if err != nil {
		return nil, err
	}
	roles := []map[string]string{}
	for _, m := range owned {
		roles = append(roles, map[string]string{"module_id": m, "role": "owner"})
	}
	return map[string]interface{}{
		"principal_id": a.PrincipalID,
		"global_role":  a.GlobalRole,
		"module_roles": roles,
	}, nil
}

func (s *Server) listModules(ctx context.Context) (interface{}, error) {
	return s.svc.St().ListModules(ctx)
}

func (s *Server) getModule(ctx context.Context, args json.RawMessage) (interface{}, error) {
	var p idParams
	if err := json.Unmarshal(args, &p); err != nil {
		return nil, err
	}
	return s.svc.St().GetModule(ctx, p.ID)
}

func (s *Server) listModuleFacts(ctx context.Context, args json.RawMessage) (interface{}, error) {
	var p moduleParams
	if err := json.Unmarshal(args, &p); err != nil {
		return nil, err
	}
	return s.svc.St().ListFacts(ctx, &p.ModuleID, nil)
}

func (s *Server) getFact(ctx context.Context, args json.RawMessage) (interface{}, error) {
	var p factParams
	if err := json.Unmarshal(args, &p); err != nil {
		return nil, err
	}
	f, err := s.svc.St().GetFact(ctx, p.FactID)
	if err != nil {
		return nil, err
	}
	revs, _ := s.svc.St().ListFactRevisions(ctx, p.FactID)
	placements, _ := s.svc.St().GetFactPlacements(ctx, p.FactID)
	anchors, _ := s.svc.St().ListFactAnchors(ctx, p.FactID)
	return map[string]interface{}{"fact": f, "revisions": revs, "placements": placements, "anchors": anchors}, nil
}

func (s *Server) getFactHistory(ctx context.Context, args json.RawMessage) (interface{}, error) {
	var p factParams
	if err := json.Unmarshal(args, &p); err != nil {
		return nil, err
	}
	return s.svc.St().ListFactRevisions(ctx, p.FactID)
}

func (s *Server) proposeFact(ctx context.Context, args json.RawMessage) (interface{}, error) {
	var in proposeFactArgs
	if err := json.Unmarshal(args, &in); err != nil {
		return nil, err
	}
	a := service.ActorFromCtx(ctx)
	intent := store.ProposalIntent(in.Intent)
	if intent == "" {
		intent = store.IntentNewFact
	}
	var targetModuleID *string
	if in.ModuleHint != "" {
		if m, err := s.svc.St().GetModuleByName(ctx, in.ModuleHint); err == nil {
			targetModuleID = &m.ID
		}
	}
	return s.svc.CreateProposal(ctx, service.CreateProposalInput{
		SubmitterID:         a.PrincipalID,
		Source:              "mcp",
		RawPayload:          in.Text,
		NormalizedText:      in.Text,
		Intent:              intent,
		TargetModuleID:      targetModuleID,
		MappingConfidence:   in.MappingConfidence,
		IdempotencyKey:      in.IdempotencyKey,
	})
}

func (s *Server) listProposals(ctx context.Context, args json.RawMessage) (interface{}, error) {
	var p struct {
		ModuleID string `json:"module_id"`
		Status   string `json:"status"`
	}
	if err := json.Unmarshal(args, &p); err != nil {
		return nil, err
	}
	var mID, st *string
	if p.ModuleID != "" {
		mID = &p.ModuleID
	}
	if p.Status != "" {
		st = &p.Status
	}
	return s.svc.St().ListProposals(ctx, mID, st)
}

func (s *Server) getProposal(ctx context.Context, args json.RawMessage) (interface{}, error) {
	var p idParams
	if err := json.Unmarshal(args, &p); err != nil {
		return nil, err
	}
	return s.svc.St().GetProposal(ctx, p.ID)
}

type reviewProposalArgs struct {
	ProposalID          string `json:"proposal_id"`
	Action              string `json:"action"`
	Comment             string `json:"comment"`
	EditedTitle         string `json:"edited_title"`
	EditedBody          string `json:"edited_body"`
	UseAdminOverride    bool   `json:"use_admin_override"`
	AdminOverrideReason string `json:"admin_override_reason"`
}

func (s *Server) reviewProposal(ctx context.Context, args json.RawMessage) (interface{}, error) {
	var in reviewProposalArgs
	if err := json.Unmarshal(args, &in); err != nil {
		return nil, err
	}
	var newModID *string
	return s.svc.ReviewProposal(ctx, service.ReviewProposalInput{
		ProposalID:          in.ProposalID,
		Action:              in.Action,
		Comment:             in.Comment,
		EditedTitle:         in.EditedTitle,
		EditedBody:          in.EditedBody,
		UseAdminOverride:    in.UseAdminOverride,
		AdminOverrideReason: in.AdminOverrideReason,
		NewModuleID:         newModID,
	})
}

func (s *Server) moveFact(ctx context.Context, args json.RawMessage) (interface{}, error) {
	var in struct {
		FactID     string `json:"fact_id"`
		ToModuleID string `json:"to_module_id"`
		Reason     string `json:"reason"`
	}
	if err := json.Unmarshal(args, &in); err != nil {
		return nil, err
	}
	return s.svc.MoveFact(ctx, service.MoveFactInput{
		FactID:     in.FactID,
		ToModuleID: in.ToModuleID,
		Reason:     in.Reason,
		InitiatedBy: service.ActorFromCtx(ctx).PrincipalID,
	})
}

func (s *Server) listMoveRequests(ctx context.Context, args json.RawMessage) (interface{}, error) {
	return s.svc.St().ListMoveRequests(ctx, nil, nil)
}

type reviewMoveArgs struct {
	MoveRequestID       string `json:"move_request_id"`
	Action              string `json:"action"`
	UseAdminOverride    bool   `json:"use_admin_override"`
	AdminOverrideReason string `json:"admin_override_reason"`
}

func (s *Server) reviewMoveRequest(ctx context.Context, args json.RawMessage) (interface{}, error) {
	var in reviewMoveArgs
	if err := json.Unmarshal(args, &in); err != nil {
		return nil, err
	}
	return s.svc.ReviewMoveRequest(ctx, service.ReviewMoveInput{
		MoveRequestID:       in.MoveRequestID,
		Action:              in.Action,
		UseAdminOverride:    in.UseAdminOverride,
		AdminOverrideReason: in.AdminOverrideReason,
	})
}

type addModuleArgs struct {
	Name        string   `json:"name"`
	Description string   `json:"description"`
	OwnerIDs    []string `json:"owner_ids"`
}

func (s *Server) addModule(ctx context.Context, args json.RawMessage) (interface{}, error) {
	var in addModuleArgs
	if err := json.Unmarshal(args, &in); err != nil {
		return nil, err
	}
	a := service.ActorFromCtx(ctx)
	if !a.IsAdmin() {
		return nil, fmt.Errorf("admin required")
	}
	mod := store.Module{Name: in.Name, Description: in.Description, Status: "active", CreatedBy: a.PrincipalID}
	if err := s.svc.St().CreateModule(ctx, mod); err != nil {
		return nil, err
	}
	created, _ := s.svc.St().GetModule(ctx, mod.ID)
	for _, oid := range in.OwnerIDs {
		_ = s.svc.St().GrantModuleOwner(ctx, store.ModuleOwnerGrant{
			ModuleID:    created.ID,
			PrincipalID: oid,
			GrantedBy:   a.PrincipalID,
		})
	}
	created, _ = s.svc.St().GetModule(ctx, created.ID)
	return created, nil
}

func (s *Server) decommissionModule(ctx context.Context, args json.RawMessage) (interface{}, error) {
	return map[string]interface{}{"ok": true, "note": "placeholder"}, nil
}

func (s *Server) exportModule(ctx context.Context, args json.RawMessage) (interface{}, error) {
	var in struct {
		ModuleID string `json:"module_id"`
		Format   string `json:"format"`
	}
	if err := json.Unmarshal(args, &in); err != nil {
		return nil, err
	}
	if in.Format == "" {
		in.Format = "json"
	}
	data, err := s.svc.ExportModule(ctx, service.ExportScope{ModuleID: in.ModuleID, Format: in.Format})
	if err != nil {
		return nil, err
	}
	return string(data), nil
}

func (s *Server) getAnchorStatus(ctx context.Context, args json.RawMessage) (interface{}, error) {
	var p factParams
	if err := json.Unmarshal(args, &p); err != nil {
		return nil, err
	}
	return s.svc.St().ListFactAnchors(ctx, p.FactID)
}

func toolsList() []map[string]interface{} {
	tools := []string{
		"get_my_roles", "list_modules", "get_module", "list_module_facts", "get_fact",
		"get_fact_history", "propose_fact", "list_proposals", "get_proposal", "review_proposal",
		"move_fact", "list_move_requests", "review_move_request", "add_module",
		"decommission_module", "export_module", "get_anchor_status",
	}
	out := []map[string]interface{}{}
	for _, t := range tools {
		out = append(out, map[string]interface{}{"name": t, "description": "mem-sys tool: " + t})
	}
	return out
}
