package main

import (
	"context"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"time"

	"github.com/go-chi/chi"
	"github.com/go-chi/chi/middleware"
	"github.com/go-chi/httprate"

	config "github.com/avvvet/bingo-services/configs"
	"github.com/avvvet/bingo-services/internal/gamesvc/broker"
	"github.com/avvvet/bingo-services/internal/gamesvc/db"
	handlers "github.com/avvvet/bingo-services/internal/gamesvc/handlers"
	"github.com/avvvet/bingo-services/internal/gamesvc/service"
	"github.com/avvvet/bingo-services/internal/gamesvc/store"
	nats "github.com/avvvet/bingo-services/internal/nats"
	log "github.com/sirupsen/logrus"
)

const SERVICE_NAME = "game"

var instanceId string

func init() {
	instanceId = "001"
	config.Logging(SERVICE_NAME + "_service_" + instanceId)
	config.LoadEnv(SERVICE_NAME)
}

func main() {

	// pg connection
	dbpool, err := db.Connect()
	if err != nil {
		log.Fatalf("Failed to connect to DB: %v", err)
	}
	defer db.ClosePool()
	log.Printf("pg connection established successfully")

	userStore := store.NewUserStore(dbpool)
	userService := service.NewUserService(userStore)

	balanceStore := store.NewBalanceStore(dbpool)
	balanceService := service.NewBalanceService(balanceStore)

	gameStore := store.NewGameStore(dbpool)
	gameService := service.NewGameService(gameStore)

	gamePlayerStore := store.NewGamePlayerStore(dbpool)
	gamePlayerService := service.NewGamePlayerService(gamePlayerStore)

	cardStore := store.NewCardStore(dbpool)
	cardService := service.NewCardService(cardStore)

	// Connect to NATS
	n, err := nats.Connect()
	if err != nil {
		log.Errorf("Error: unable to connect to NATS server %v", err)
		os.Exit(0)
	}

	defer n.Conn.Close()
	log.Printf("NATS connection established successfully %s", n.Url)

	// init peer message broker
	broker := broker.NewBroker(n.Conn,
		userService, balanceService, gameService, gamePlayerService, cardService)

	// subscribe to socket service
	topic := "socket.service"
	sub, err := broker.SubscribSocketService(topic)
	if err != nil {
		log.Errorf("Error: unable to subscribe to queue %v", err)
		os.Exit(0)
	}

	// Setup router
	r := chi.NewRouter()
	c := config.CORS()

	// Middleware
	r.Use(middleware.RequestID)
	r.Use(middleware.RealIP)
	r.Use(config.CustomLoggerMiddleware())
	r.Use(middleware.Recoverer)
	r.Use(middleware.Timeout(60 * time.Second))
	r.Use(c.Handler)

	// to protect the service api from any over requests
	rateLimitStr := os.Getenv("RATE_LIMIT")
	rateLimit, err := strconv.Atoi(rateLimitStr)
	if err != nil {
		log.Fatalf("Invalid RATE_LIMIT value: %v", err)
	}
	r.Use(httprate.LimitByIP(rateLimit, 1*time.Minute))

	// Init handlers and routes
	h := handlers.NewHandler()
	h.InitAuth()
	h.SetRoutes(r)

	// Create server with timeout settings
	server := &http.Server{
		Addr:         ":" + os.Getenv("GAME_SERVICE_PORT"),
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

	sub.Unsubscribe()

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	if err := server.Shutdown(ctx); err != nil {
		log.Fatalf("%s service shutdown Failed:%+v", SERVICE_NAME, err)
	}
	log.Infof("%s service gracefully stopped", SERVICE_NAME)
}
