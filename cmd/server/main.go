package main

import (
	"context"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"uni-backend/internal/auth"
	"uni-backend/internal/ws"
)

func main() {
	log.SetFlags(log.LstdFlags | log.Lshortfile)
	log.Println("Starting UnI Tap-and-Hold backend server...")

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Initialize Firebase Auth + Firestore.
	fa, err := auth.NewFirebaseAuth(ctx)
	if err != nil {
		log.Fatalf("Failed to initialize Firebase: %v", err)
	}
	defer fa.Close()

	// Create HTTP mux with WebSocket and health endpoints.
	mux := ws.NewServeMux(fa)

	// Determine port.
	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}

	server := &http.Server{
		Addr:         ":" + port,
		Handler:      mux,
		ReadTimeout:  15 * time.Second,
		WriteTimeout: 15 * time.Second,
		IdleTimeout:  60 * time.Second,
	}

	// Start server in a goroutine.
	go func() {
		log.Printf("Server listening on :%s", port)
		log.Printf("  WebSocket endpoint: ws://localhost:%s/ws?token=FIREBASE_ID_TOKEN", port)
		log.Printf("  Health endpoint:    http://localhost:%s/health", port)
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("Server failed: %v", err)
		}
	}()

	// Graceful shutdown on SIGINT/SIGTERM.
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	sig := <-quit
	log.Printf("Received signal %s, shutting down gracefully...", sig)

	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer shutdownCancel()

	if err := server.Shutdown(shutdownCtx); err != nil {
		log.Fatalf("Server forced to shutdown: %v", err)
	}

	log.Println("Server stopped")
}
