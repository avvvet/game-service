package handlers

import (
	"os"
	"time"

	"github.com/go-chi/chi"
	"github.com/go-chi/jwtauth"
	log "github.com/sirupsen/logrus"
)

func (h *Handler) SetRoutes(r *chi.Mux) {
	r.Route("/v1", func(r chi.Router) {

		// public routes here

		// Secure routes
		r.Group(func(r chi.Router) {
			r.Use(jwtauth.Verifier(h.tokenAuth))
			r.Use(jwtauth.Authenticator)

			r.Get("/health", h.HealthHandler)

		})
	})
}

func (h *Handler) InitAuth() {
	var jwtKey = os.Getenv("JWT_SECRET_KEY")
	h.tokenAuth = jwtauth.New("HS256", []byte(jwtKey), nil)

	expirationTime := time.Now().Add(7 * 24 * time.Hour).Unix()

	_, tokenString, _ := h.tokenAuth.Encode(map[string]interface{}{
		"service_id": 8003022,
		"exp":        expirationTime,
	})

	// For debugging only, comment it out in production
	log.Infof("DEBUG: JWT for testing expires soon : %s", tokenString)
}
