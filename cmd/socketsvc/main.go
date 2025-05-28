package main

import (
	"context"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"time"

	"github.com/avvvet/bingo-services/internal/nats"
	"github.com/go-chi/chi"
	"github.com/go-chi/chi/middleware"
	"github.com/go-chi/httprate"
	log "github.com/sirupsen/logrus"

	config "github.com/avvvet/bingo-services/configs"

	"github.com/avvvet/bingo-services/internal/socketsvc/broker"
	"github.com/avvvet/bingo-services/internal/socketsvc/routes"
	"github.com/avvvet/bingo-services/internal/socketsvc/ws"
)

const SERVICE_NAME = "socket"

func init() {
	instanceId := "001"
	config.Logging(SERVICE_NAME + "_service_" + instanceId)
	config.LoadEnv(SERVICE_NAME)
}

func main() {
	// Connect to NATS
	n, err := nats.Connect()
	if err != nil {
		log.Errorf("Error: unable to connect to NATS server %v", err)
		os.Exit(0)
	}

	defer n.Conn.Close()
	log.Printf("NATS connection established successfully %s", n.Url)

	// Setup router
	r := chi.NewRouter()
	c := config.CORS()

	// Middleware
	r.Use(middleware.RequestID)
	r.Use(middleware.RealIP)
	r.Use(config.CustomLoggerMiddleware())
	//r.Use(middleware.Recoverer)
	//r.Use(middleware.Timeout(60 * time.Second))
	r.Use(c.Handler)

	// to protect the service api from any over requests
	rateLimitStr := os.Getenv("RATE_LIMIT")
	rateLimit, err := strconv.Atoi(rateLimitStr)
	if err != nil {
		log.Fatalf("Invalid RATE_LIMIT value: %v", err)
	}
	r.Use(httprate.LimitByIP(rateLimit, 1*time.Minute))

	// Initialize websocket handler
	s := ws.NewWs()

	// Initialize routes
	routes.SetRoutes(r, s)

	// Initialize broker subscribe to relay service
	b := broker.NewBroker(n.Conn, s.GetConnection, s.GetRoomSockets) // s.GetConnecion dependency injection to broker
	s.Broker = b                                                     // set broker reference for websocket handler logic

	// subscribe to game server
	sub_player_init, err := b.Subscribe("game.service")
	if err != nil {
		log.Errorf("Error: unable to subscribe to queue %v", err)
		os.Exit(0)
	}

	// Create server with timeout settings
	server := &http.Server{
		Addr:         ":" + os.Getenv("SOCKET_SERVICE_PORT"),
		Handler:      r,
		ReadTimeout:  60 * time.Second,
		WriteTimeout: 60 * time.Second,
		IdleTimeout:  60 * time.Second,
	}

	// Graceful shutdown
	go func() {
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("ListenAndServe(): %v", err)
		}
	}()
	log.Infof("%s service running at port %s", SERVICE_NAME, server.Addr)

	// Wait for interrupt signal to gracefully shutdown the server
	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt)
	<-stop

	sub_player_init.Unsubscribe()

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	if err := server.Shutdown(ctx); err != nil {
		log.Fatalf("%s service shutdown Failed:%+v", SERVICE_NAME, err)
	}
	log.Infof("%s service gracefully stopped", SERVICE_NAME)
}
