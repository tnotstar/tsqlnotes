package adapters

import (
	"net/http"
)

func RegisterRoutes() http.Handler {
	mux := http.NewServeMux()

	// API routes
	mux.HandleFunc("/api/connections", ConnectionsHandler)
	mux.HandleFunc("/api/connect", ConnectHandler)
	mux.HandleFunc("/api/disconnect", DisconnectHandler)
	mux.HandleFunc("/api/schema", SchemaHandler)
	mux.HandleFunc("/api/query", QueryHandler)
	mux.HandleFunc("/api/export", ExportHandler)
	mux.HandleFunc("/api/health", healthHandler)

	// Serve static files
	mux.Handle("/static/", http.StripPrefix("/static/", http.FileServer(http.Dir("web/static"))))

	// SSR Pages
	mux.HandleFunc("/", indexHandler)

	// CORS middleware wrapper
	handler := corsMiddleware(mux)

	// API routes
	return handler
}
