package main

import (
	"log"

	"github.com/joho/godotenv"
	"github.com/tnotstar/tsqlnotes/internal/adapters"
)

func main() {
	// Load environment variables from .env file if it exists
	if err := godotenv.Load(); err != nil {
		log.Println("No .env file found, using system environment variables")
	}

	server := adapters.NewServer()
	if err := server.ListenAndServe(); err != nil {
		log.Fatalf("Server failed to start: %v", err)
	}
}
