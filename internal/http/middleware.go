package http

import (
	"net/http"
	"strings"

	"mem-sys/internal/service"
)

func authMiddleware(svc *service.Service) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			auth := r.Header.Get("Authorization")
			if auth == "" {
				writeJSON(w, http.StatusUnauthorized, envelope{"error": "unauthorized"})
				return
			}
			parts := strings.SplitN(auth, " ", 2)
			if len(parts) != 2 || !strings.EqualFold(parts[0], "bearer") {
				writeJSON(w, http.StatusUnauthorized, envelope{"error": "invalid authorization header"})
				return
			}
			a, err := svc.Authenticate(r.Context(), parts[1])
			if err != nil {
				writeJSON(w, http.StatusUnauthorized, envelope{"error": "unauthorized"})
				return
			}
			r = r.WithContext(service.WithActor(r.Context(), a))
			next.ServeHTTP(w, r)
		})
	}
}
