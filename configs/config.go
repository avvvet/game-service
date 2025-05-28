package config

import (
	"net/http"
	"os"
	"path/filepath"
	"time"

	"github.com/go-chi/chi/middleware"
	"github.com/go-chi/cors"
	"github.com/gofrs/uuid"
	log "github.com/sirupsen/logrus"

	"github.com/joho/godotenv"
)

var InstanceId string

func LoadEnv(service string) {
	log.Info("service configuration and env variables loading started ...")
	err := godotenv.Load("./.env")
	if err != nil {
		log.Fatal("Error loading .env file")
	}

	log.Info(".env file loaded.")
}

func CreateUniqueInstance(service string) string {
	id, err := uuid.NewV4() // instance identifier
	if err != nil {
		log.Errorf("error generating instanceId: %s", err)
		os.Exit(0)
	}
	InstanceId = id.String()
	log.Infof(service+" service with Instance ID: %s is ready", id)
	return id.String()
}

func GetInstanceId() string {
	return InstanceId
}

func CORS() *cors.Cors {
	/*
		corsOptions := cors.New(cors.Options{
			AllowedOrigins: []string{
				"https://",
			},
			AllowedMethods: []string{"GET", "POST", "PUT", "DELETE", "OPTIONS", "PATCH"},
			AllowedHeaders: []string{"*"},
		})

	*/

	corsOptions := cors.New(cors.Options{
		AllowedOrigins:   []string{"https://webrtcwire.online", "http://localhost:5173"},
		AllowedMethods:   []string{"GET", "POST", "PUT", "DELETE", "OPTIONS"},
		AllowedHeaders:   []string{"Accept", "Authorization", "Content-Type", "X-CSRF-Token"},
		ExposedHeaders:   []string{"Link"},
		AllowCredentials: true,
		MaxAge:           300, // Maximum value not ignored by any of major browsers
	})

	return corsOptions
}

func Logging(service string) {
	logFolder := ".l_g"

	_, err := os.Stat(logFolder)
	if os.IsNotExist(err) {
		err = os.Mkdir(logFolder, 0755)
		if err != nil {
			log.Warnf("unable to create folder for log %s", err)
			return
		}
	}

	logFilePath := filepath.Join(logFolder, service+".log")

	file, err := os.OpenFile(logFilePath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	if err != nil {
		log.Fatal("Failed to open log file:", err)
	}

	log.SetOutput(file)

	log.SetFormatter(&log.TextFormatter{})
	log.SetLevel(log.InfoLevel)

	log.Infof("log to file started for service: %s", service)
}

func CustomLoggerMiddleware() func(next http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			start := time.Now()
			ww := middleware.NewWrapResponseWriter(w, r.ProtoMajor)

			defer func() {
				log.Printf("%s %s %s %d %s %s",
					r.Method,
					r.RequestURI,
					r.RemoteAddr,
					ww.Status(),
					http.StatusText(ww.Status()),
					time.Since(start),
				)
			}()

			next.ServeHTTP(ww, r)
		})
	}
}
