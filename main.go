package main

import (
	"context"
	"log"
	"os"

	"github.com/alwedo/jobber/server"
	_ "golang.org/x/crypto/x509roots/fallback" // CA bundle for FROM Scratch
)

func main() {
	if err := server.Start(context.Background(), os.Stdout, os.Getenv, os.Args); err != nil {
		log.Fatalf("starting server: %v", err)
	}
}
