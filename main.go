package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"

	"github.com/Alvaroalonsobabbel/jobber/db"
	"github.com/Alvaroalonsobabbel/jobber/jobber"
	"github.com/Alvaroalonsobabbel/jobber/server"
	"github.com/jackc/pgx/v5/pgxpool"
)

func main() {
	var (
		ctx    = context.Background()
		svrErr = make(chan error, 1)
		c      = make(chan os.Signal, 1)
	)

	logger, logCloser := initLogger()
	defer logCloser()

	d, dbCloser := initDB(ctx)
	defer dbCloser()

	j, jCloser := jobber.New(logger, d)
	defer jCloser()

	svr := server.New(logger, j)
	defer func() {
		if err := svr.Shutdown(ctx); err != nil {
			logger.Error("unable to shutdown server", slog.String("error", err.Error()))
		}
	}()

	signal.Notify(c, os.Interrupt, syscall.SIGTERM)

	go func() {
		log.Println("starting server in port " + svr.Addr)
		if err := svr.ListenAndServe(); err != nil {
			if errors.Is(err, http.ErrServerClosed) {
				log.Println(err)
			} else {
				log.Println(err)
				svrErr <- err
			}
		}
	}()

	select {
	case <-svrErr:
		log.Println("\nserver error, shutting down...")
	case <-c:
		log.Println("\nshutting down...")
	}
}

func initLogger() (*slog.Logger, func()) {
	out, err := os.OpenFile("jobber.log", os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0666)
	if err != nil {
		log.Fatalf("unable to open log file: %v", err)
	}

	handler := slog.NewJSONHandler(out, &slog.HandlerOptions{Level: slog.LevelDebug})
	return slog.New(handler), func() {
		if err := out.Close(); err != nil {
			log.Printf("unable to close log file: %v", err)
		}
	}
}

func initDB(ctx context.Context) (*db.Queries, func()) {
	connStr := fmt.Sprintf("host=localhost user=jobber password=%s dbname=jobber sslmode=disable", os.Getenv("POSTGRES_PASSWORD"))
	conn, err := pgxpool.New(ctx, connStr)
	if err != nil {
		log.Fatalf("unable to initialized db connection: %v", err)
	}
	if err := conn.Ping(ctx); err != nil {
		log.Fatalf("unable to ping database: %v", err)
	}

	return db.New(conn), conn.Close
}
