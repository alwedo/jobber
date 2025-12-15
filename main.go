package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"

	"github.com/alwedo/jobber/db"
	"github.com/alwedo/jobber/jobber"
	"github.com/alwedo/jobber/metrics"
	"github.com/alwedo/jobber/server"
	"github.com/jackc/pgx/v5/pgxpool"
	_ "golang.org/x/crypto/x509roots/fallback" // CA bundle for FROM Scratch
)

func main() {
	var (
		log    = slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))
		ctx    = context.Background()
		svrErr = make(chan error)
		c      = make(chan os.Signal, 1)
	)

	metrics.Init() // will panic if fails to init.

	d, dbCloser := initDB(ctx, log)
	defer dbCloser()

	j, jCloser := jobber.New(log, d)
	defer jCloser()

	svr, err := server.New(log, j)
	if err != nil {
		log.Error("unable to create server", slog.Any("error", err))
		return
	}
	defer func() {
		if err := svr.Shutdown(ctx); err != nil {
			log.Error("unable to shutdown server", slog.Any("error", err))
		}
	}()

	signal.Notify(c, os.Interrupt, syscall.SIGTERM)

	go func() {
		log.Info("starting server", slog.String("addr", svr.Addr))
		if err := svr.ListenAndServe(); err != nil {
			if errors.Is(err, http.ErrServerClosed) {
				log.Info("server closed", slog.Any("msg", err))
			} else {
				svrErr <- err
			}
		}
	}()

	select {
	case e := <-svrErr:
		log.Error("server error, shutting down...", slog.Any("error", e))
	case <-c:
		log.Info("shutting down...")
	}
}

func initDB(ctx context.Context, log *slog.Logger) (*db.Queries, func()) {
	host := os.Getenv("DB_HOST")
	if host == "" {
		host = "localhost"
	}
	connStr := fmt.Sprintf("host=%s user=jobber password=%s dbname=jobber sslmode=disable", host, os.Getenv("POSTGRES_PASSWORD"))
	conn, err := pgxpool.New(ctx, connStr)
	if err != nil {
		log.Error("unable to initialized db connection", slog.Any("error", err))
	}
	if err := conn.Ping(ctx); err != nil {
		log.Error("unable to ping database", slog.Any("error", err))
	}

	return db.New(conn), conn.Close
}
