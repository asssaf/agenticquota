package main

import (
	"log"
	"net/http"
	"os"

	"agenticquota/internal/handler"
	"agenticquota/internal/service"
)

func setupRouter() *http.ServeMux {
	quotaSvc := service.NewQuotaService()
	quotaHandler := handler.NewQuotaHandler(quotaSvc)

	mux := http.NewServeMux()

	// Expose /api/v1/quota wrapped in the APIKeyMiddleware
	quotaRoute := http.HandlerFunc(quotaHandler.HandleQuota)
	mux.Handle("/api/v1/quota", handler.APIKeyMiddleware(quotaRoute))

	// Expose /api/v1/quota/history wrapped in the APIKeyMiddleware
	historyRoute := http.HandlerFunc(quotaHandler.HandleQuotaHistory)
	mux.Handle("/api/v1/quota/history", handler.APIKeyMiddleware(historyRoute))

	// Expose /api/v1/quota/history/reset wrapped in the APIKeyMiddleware
	resetHistoryRoute := http.HandlerFunc(quotaHandler.HandleQuotaResetHistory)
	mux.Handle("/api/v1/quota/history/reset", handler.APIKeyMiddleware(resetHistoryRoute))

	// Health check endpoint (public, standard for GAE health checks / lifecycles)
	mux.HandleFunc("/_ah/health", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("ok"))
	})

	// Serve static files for the dashboard from the local web directory (for local dev)
	mux.Handle("/", http.FileServer(http.Dir("web")))

	return mux
}

func main() {
	// 1. Initialize router
	mux := setupRouter()

	// 2. Retrieve port configuration from environment
	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
		log.Printf("Defaulting to port %s", port)
	}

	// 3. Start HTTP Server
	log.Printf("Starting agenticquota server on port %s...", port)
	if err := http.ListenAndServe(":"+port, mux); err != nil {
		log.Fatalf("Server failed to start: %v", err)
	}
}
