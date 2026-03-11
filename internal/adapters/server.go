package adapters

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"os"
	"time"
)

type Server struct {
	server  *http.Server
	handler http.Handler
	address string
}

func NewServer() *Server {
	hostEnv := os.Getenv("HOST")
	if hostEnv == "" {
		hostEnv = "localhost"
	}

	portEnv := os.Getenv("PORT")
	if portEnv == "" {
		portEnv = "8080"
	}

	addr := fmt.Sprintf("%s:%s", hostEnv, portEnv)
	hndr := RegisterRoutes()
	srv := &http.Server{
		Addr:         addr,
		Handler:      hndr,
		IdleTimeout:  time.Minute,
		ReadTimeout:  10 * time.Second,
		WriteTimeout: 30 * time.Second,
	}

	return &Server{
		server:  srv,
		handler: hndr,
		address: addr,
	}
}

func (s *Server) ListenAndServe() error {
	log.Printf("Backend server running on http://%s 🚀\n", s.address)
	return s.server.ListenAndServe()
}

func (s *Server) Shutdown(ctx context.Context) error {
	return s.server.Shutdown(ctx)
}
