package api

import (
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/ruaandeysel/vault/internal/api/handlers"
)

func (s *Server) setupRoutes() *chi.Mux {
	r := chi.NewRouter()
	r.Use(middleware.Logger)
	r.Use(middleware.Recoverer)
	r.Use(middleware.Heartbeat("/ping"))

	r.Route("/api/v1", func(r chi.Router) {
		r.Get("/health", s.handleHealth)
		r.Get("/ws", s.hub.HandleWS)

		storageH := handlers.NewStorageHandler(s.db)
		r.Route("/storage", func(r chi.Router) {
			r.Get("/", storageH.List)
			r.Post("/", storageH.Create)
			r.Get("/{id}", storageH.Get)
			r.Put("/{id}", storageH.Update)
			r.Delete("/{id}", storageH.Delete)
			r.Post("/{id}/test", storageH.TestConnection)
		})

		jobH := handlers.NewJobHandler(s.db)
		r.Route("/jobs", func(r chi.Router) {
			r.Get("/", jobH.List)
			r.Post("/", jobH.Create)
			r.Get("/{id}", jobH.Get)
			r.Put("/{id}", jobH.Update)
			r.Delete("/{id}", jobH.Delete)
			r.Get("/{id}/history", jobH.GetHistory)
			r.Get("/{id}/restore-points", jobH.GetRestorePoints)
		})
	})

	return r
}

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	respondJSON(w, http.StatusOK, map[string]string{
		"status":  "ok",
		"version": "0.1.0",
	})
}
