package main

import (
	"log"

	"fileconvy-server/internal/server"
)

func main() {
	app := server.NewRouter()

	if err := app.Run(":8080"); err != nil {
		log.Fatalf("failed to start server: %v", err)
	}
}
