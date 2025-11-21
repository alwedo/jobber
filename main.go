package main

import (
	"context"
	"database/sql"
	_ "embed"
	"errors"
	"io"
	"log"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"

	"github.com/Alvaroalonsobabbel/jobber/db"
	"github.com/Alvaroalonsobabbel/jobber/jobber"
	"github.com/Alvaroalonsobabbel/jobber/server"
	_ "modernc.org/sqlite"
)

func main() {
	var (
		ctx    = context.Background()
		svrErr = make(chan error, 1)
		c      = make(chan os.Signal, 1)
	)

	logger, logCloser := initLogger()
	defer logCloser.Close()

	d, dbCloser := initDB(ctx)
	defer dbCloser.Close()

	j, jCloser := jobber.New(logger, d)
	defer jCloser()

	svr := server.New(logger, j)
	defer svr.Shutdown(ctx)

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

//go:embed schema.sql
var ddl string

func initLogger() (*slog.Logger, io.Closer) {
	out, err := os.OpenFile("jobber.log", os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0666)
	if err != nil {
		log.Fatalf("unable to open log file: %v", err)
	}

	handler := slog.NewJSONHandler(out, &slog.HandlerOptions{Level: slog.LevelDebug})
	return slog.New(handler), out
}

func initDB(ctx context.Context) (*db.Queries, io.Closer) {
	d, err := sql.Open("sqlite", "jobber.sqlite")
	if err != nil {
		log.Fatalf("unable to open database: %v", err)
	}
	if _, err := d.ExecContext(ctx, "PRAGMA journal_mode = WAL"); err != nil {
		log.Fatalf("unable to set WAL mode: %v", err)
	}
	if _, err := d.ExecContext(ctx, "PRAGMA busy_timeout = 30000"); err != nil {
		log.Fatalf("unable to set busy timeout: %v", err)
	}
	if _, err := d.ExecContext(ctx, ddl); err != nil {
		log.Fatalf("unable to create database: %v", err)
	}
	return db.New(d), d
}
