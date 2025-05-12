package routes

import (
	"os"
	"time"

	"github.com/avvvet/bingo-services/internal/socketsvc/handlers"
	"github.com/avvvet/bingo-services/internal/socketsvc/ws"
	"github.com/go-chi/chi"
	"github.com/go-chi/jwtauth"
	log "github.com/sirupsen/logrus"
)

var tokenAuth *jwtauth.JWTAuth

func SetRoutes(r *chi.Mux, ws *ws.Ws) {
	h := handlers.NewHandler(ws)
	r.Route("/v1", func(r chi.Router) {
		r.Get("/ws", h.HandleWebSocket)
		// Secure routes
		r.Group(func(r chi.Router) {
			r.Use(jwtauth.Verifier(tokenAuth))
			r.Use(jwtauth.Authenticator)

			r.Get("/health", h.HealthHandler)

		})
	})
}

func InitAuth() {
	var jwtKey = os.Getenv("JWT_SECRET_KEY")
	tokenAuth = jwtauth.New("HS256", []byte(jwtKey), nil)

	expirationTime := time.Now().Add(7 * 24 * time.Hour).Unix()

	_, tokenString, _ := tokenAuth.Encode(map[string]interface{}{
		"service_id": 8003022,
		"exp":        expirationTime,
	})

	// For debugging only, comment it out in production
	log.Infof("DEBUG: JWT for testing expires soon : %s", tokenString)
}
