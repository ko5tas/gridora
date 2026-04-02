package server

import (
	"html/template"
	"log/slog"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/ko5tas/gridora/internal/config"
	"github.com/ko5tas/gridora/internal/store"
	"github.com/ko5tas/gridora/web"
)

// Server serves the Gridora dashboard and API.
type Server struct {
	store     store.Store
	config    *config.Config
	templates *template.Template
	logger    *slog.Logger
	router    chi.Router
}

// New creates a new HTTP server.
func New(store store.Store, cfg *config.Config, logger *slog.Logger) *Server {
	s := &Server{
		store:  store,
		config: cfg,
		logger: logger,
	}

	s.templates = template.Must(template.New("").Funcs(s.templateFuncs()).ParseFS(web.Templates, "templates/*.html"))
	s.router = s.routes()
	return s
}

// Handler returns the http.Handler for use with http.Server.
func (s *Server) Handler() http.Handler {
	return s.router
}

func (s *Server) routes() chi.Router {
	r := chi.NewRouter()

	r.Use(middleware.Recoverer)
	r.Use(middleware.RealIP)
	r.Use(middleware.Compress(5))
	r.Use(middleware.Timeout(30 * time.Second))

	// Static assets (vendored JS/CSS)
	r.Handle("/static/*", http.StripPrefix("/static/", http.FileServer(http.FS(web.Static))))

	// Dashboard
	r.Get("/", s.handleDashboard)
	r.Get("/history", http.RedirectHandler("/", http.StatusMovedPermanently).ServeHTTP)

	// API v1
	r.Route("/api/v1", func(r chi.Router) {
		r.Get("/status", s.handleAPIStatus)
		r.Get("/status/stream", s.handleSSE)
		r.Get("/energy/minute", s.handleEnergyMinute)
		r.Get("/energy/hourly", s.handleEnergyHourly)
		r.Get("/energy/daily", s.handleEnergyDaily)
		r.Get("/energy/weekly", s.handleEnergyWeekly)
		r.Get("/energy/monthly", s.handleEnergyMonthly)
		r.Get("/energy/quarterly", s.handleEnergyQuarterly)
		r.Get("/energy/yearly", s.handleEnergyYearly)
		r.Get("/export", s.handleExportDownload)
	})

	return r
}

func (s *Server) templateFuncs() template.FuncMap {
	return template.FuncMap{
		"formatDate": func(t time.Time) string {
			return t.Format("02/01/2006")
		},
		"formatTime": func(t time.Time) string {
			return t.Format("15:04")
		},
		"formatDateTime": func(t time.Time) string {
			return t.Format("02/01/2006 15:04")
		},
	}
}
