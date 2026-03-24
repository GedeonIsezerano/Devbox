package server

import (
	"database/sql"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/user/devbox/internal/crypto"
)

// Server wraps an http.Server with application dependencies.
type Server struct {
	*http.Server
	DB        *sql.DB
	Encryptor crypto.Encryptor
	Nonces    *crypto.NonceStore
}

// Config holds the configuration needed to create a new Server.
type Config struct {
	ListenAddr string
	DB         *sql.DB
	Encryptor  crypto.Encryptor
	Nonces     *crypto.NonceStore
}

// NewServer creates a new Server with all routes and middleware wired up.
func NewServer(cfg Config) *Server {
	r := chi.NewRouter()

	// Global middleware.
	r.Use(RateLimiter(60))
	r.Use(MaxBodySize(1 << 20)) // 1MB

	// Health endpoint — no auth required.
	r.Get("/health", handleHealth)

	// Auth routes — registered in later tasks.
	r.Route("/auth", func(r chi.Router) {
		// Handlers will be added by Task 9.
	})

	// Project routes — registered in later tasks.
	r.Route("/projects", func(r chi.Router) {
		r.Use(RequireAuth(cfg.DB))
		// Handlers will be added by Task 10.
	})

	// Token routes — registered in later tasks.
	r.Route("/tokens", func(r chi.Router) {
		r.Use(RequireAuth(cfg.DB))
		// Handlers will be added by Task 11.
	})

	srv := &Server{
		Server: &http.Server{
			Addr:    cfg.ListenAddr,
			Handler: r,
		},
		DB:        cfg.DB,
		Encryptor: cfg.Encryptor,
		Nonces:    cfg.Nonces,
	}

	return srv
}
