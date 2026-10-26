package http

import (
	"io"
	"log/slog"
	"net/http"

	"github.com/go-chi/chi/v5"
	"mem-sys/internal/service"
)

type Server struct {
	svc *service.Service
	log *slog.Logger
	r   *chi.Mux
}

func New(svc *service.Service) *Server {
	return &Server{
		svc: svc,
		log: slog.New(slog.NewTextHandler(io.Discard, nil)),
		r:   chi.NewRouter(),
	}
}

func (s *Server) Routes() http.Handler {
	r := s.r
	r.Route("/api/v1", func(r chi.Router) {
		r.Use(authMiddleware(s.svc))

		r.Get("/me", s.handleMe)

		r.Route("/modules", func(r chi.Router) {
			r.Get("/", s.handleListModules)
			r.Post("/", s.handleCreateModule)
			r.Route("/{module_id}", func(r chi.Router) {
				r.Get("/", s.handleGetModule)
				r.Patch("/", s.handlePatchModule)
				r.Get("/facts", s.handleListModuleFacts)
				r.Get("/proposals", s.handleListModuleProposals)
				r.Post("/owners", s.handleGrantOwner)
				r.Delete("/owners/{principal_id}", s.handleRevokeOwner)
				r.Post("/decommission", s.handleDecommission)
			})
		})

		r.Route("/proposals", func(r chi.Router) {
			r.Post("/", s.handleCreateProposal)
			r.Get("/", s.handleListProposals)
			r.Get("/{proposal_id}", s.handleGetProposal)
			r.Post("/{proposal_id}/review", s.handleReviewProposal)
		})

		r.Route("/idempotency-keys", func(r chi.Router) {
			r.Post("/", s.handleCreateIdempotencyKey)
		})

		r.Route("/facts", func(r chi.Router) {
			r.Get("/", s.handleListFacts)
			r.Get("/{fact_id}", s.handleGetFact)
			r.Get("/{fact_id}/revisions", s.handleListFactRevisions)
			r.Post("/{fact_id}/deprecate", s.handleDeprecateFact)
			r.Post("/{fact_id}/archive", s.handleArchiveFact)
			r.Post("/{fact_id}/move", s.handleMoveFact)
		})

		r.Route("/move-requests", func(r chi.Router) {
			r.Post("/", s.handleCreateMoveRequest)
			r.Get("/{move_id}", s.handleGetMoveRequest)
			r.Post("/{move_id}/review", s.handleReviewMoveRequest)
		})

		r.Route("/exports", func(r chi.Router) {
			r.Post("/", s.handleCreateExport)
			r.Get("/{export_id}", s.handleGetExport)
		})
	})
	return r
}

func (s *Server) Handler() http.Handler {
	return s.Routes()
}
