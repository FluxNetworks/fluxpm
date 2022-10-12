package route

import (
	"net/http"
	"time"

	"github.com/RichardKnop/machinery/v1"
	"github.com/go-chi/chi"
	"github.com/go-chi/chi/middleware"
	"github.com/go-chi/cors"
	"github.com/go-redis/redis/v8"
	"github.com/jmoiron/sqlx"
	log "github.com/sirupsen/logrus"

	"os"
	"path/filepath"

	"github.com/FluxNetworks/fluxpm/internal/config"
	"github.com/FluxNetworks/fluxpm/internal/db"
	"github.com/FluxNetworks/fluxpm/internal/frontend"
	"github.com/FluxNetworks/fluxpm/internal/graph"
	"github.com/FluxNetworks/fluxpm/internal/jobs"
	"github.com/FluxNetworks/fluxpm/internal/logger"
)

// FrontendHandler serves an embed React client through chi
type FrontendHandler struct {
	staticPath string
	indexPath  string
}

// IsDir checks if the given file is a directory
func IsDir(f http.File) bool {
	fi, err := f.Stat()
	if err != nil {
		return false
	}
	return fi.IsDir()
}

// ServeHTTP attempts to serve a requested file for the embedded React client
func (h FrontendHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	path, err := filepath.Abs(r.URL.Path)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	f, err := frontend.Frontend.Open(path)
	if os.IsNotExist(err) || IsDir(f) {
		index, err := frontend.Frontend.Open("index.html")
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		http.ServeContent(w, r, "index.html", time.Now(), index)
		return
	} else if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	http.ServeContent(w, r, path, time.Now(), f)
}

// FluxpmHandler contains all the route handlers
type FluxpmHandler struct {
	repo      db.Repository
	AppConfig config.AppConfig
}

// NewRouter creates a new router for chi
func NewRouter(dbConnection *sqlx.DB, redisClient *redis.Client, jobServer *machinery.Server, appConfig config.AppConfig) (chi.Router, error) {
	formatter := new(log.TextFormatter)
	formatter.TimestampFormat = "02-01-2006 15:04:05"
	formatter.FullTimestamp = true

	routerLogger := log.New()
	routerLogger.SetLevel(log.InfoLevel)
	routerLogger.Formatter = formatter
	r := chi.NewRouter()
	r.Use(middleware.RequestID)
	r.Use(middleware.RealIP)
	r.Use(logger.NewStructuredLogger(routerLogger))
	r.Use(middleware.Recoverer)
	r.Use(middleware.Timeout(60 * time.Second))

	r.Use(cors.Handler(cors.Options{
		// AllowedOrigins:   []string{"https://foo.com"}, // Use this to allow specific origin hosts
		AllowedOrigins: []string{"https://*", "http://*"},
		// AllowOriginFunc:  func(r *http.Request, origin string) bool { return true },
		AllowedMethods:   []string{"GET", "POST", "PUT", "DELETE", "OPTIONS"},
		AllowedHeaders:   []string{"Accept", "Authorization", "Cookie", "Content-Type", "X-CSRF-Token"},
		ExposedHeaders:   []string{"Link"},
		AllowCredentials: true,
		MaxAge:           300, // Maximum value not ignored by any of major browsers
	}))

	repository := db.NewRepository(dbConnection)
	fluxpmHandler := TFluxpmHandler{*repository, appConfig}

	var imgServer = http.FileServer(http.Dir("./uploads/"))
	r.Group(func(mux chi.Router) {
		mux.Mount("/auth", authResource{}.Routes(fluxpmHandler))
		mux.Handle("/__graphql", graph.NewPlaygroundHandler("/graphql"))
		mux.Mount("/uploads/", http.StripPrefix("/uploads/", imgServer))
		mux.Post("/auth/confirm", fluxpmHandler.ConfirmUser)
		mux.Post("/auth/register", fluxpmHandler.RegisterUser)
		mux.Get("/settings", fluxpmHandler.PublicSettings)
		mux.Post("/logger", fluxpmHandler.HandleClientLog)
	})
	auth := AuthenticationMiddleware{*repository}
	jobQueue := jobs.JobQueue{
		Repository: *repository,
		AppConfig:  appConfig,
		Server:     jobServer,
	}
	r.Group(func(mux chi.Router) {
		mux.Use(auth.Middleware)
		mux.Post("/users/me/avatar", fluxpmHandler.ProfileImageUpload)
		mux.Mount("/graphql", graph.NewHandler(*repository, appConfig, jobQueue, redisClient))
	})

	frontend := FrontendHandler{staticPath: "build", indexPath: "index.html"}
	r.Handle("/*", frontend)

	return r, nil
}
