package main

import (
	"context"
	"database/sql"
	_ "embed"
	"fmt"
	"io"
	"log"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/Alvaroalonsobabbel/jobber/db"
	"github.com/Alvaroalonsobabbel/jobber/jobber"
	"github.com/Alvaroalonsobabbel/jobber/server"
	_ "modernc.org/sqlite"
)

func main() {
	ctx, ctxCancel := context.WithCancel(context.Background())
	defer ctxCancel()

	logger, logCloser := initLogger()
	defer logCloser.Close()

	d, dbCloser := initDB(ctx)
	defer dbCloser.Close()

	j, jobberWait := jobber.New(ctx, logger, d)
	svr := server.New(logger, j)

	c := make(chan os.Signal, 1)
	signal.Notify(c, os.Interrupt, syscall.SIGTERM)

	go func() {
		log.Println("starting server in port 80")
		if err := svr.ListenAndServe(); err != nil {
			log.Fatal(err)
		}
	}()

	go func() {
		<-c
		fmt.Println("\nSignal received, cleaning up...")
		if err := svr.Shutdown(ctx); err != nil {
			fmt.Printf("error shutting down server: %v\n", err)
		}
		ctxCancel()
		jobberWait()
		if err := dbCloser.Close(); err != nil {
			fmt.Printf("error closing database: %v\n", err)
		}
		if err := logCloser.Close(); err != nil {
			fmt.Printf("error closing logger: %v\n", err)
		}
		os.Exit(1)
	}()

	select {}
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
	if _, err := d.ExecContext(ctx, ddl); err != nil {
		log.Fatalf("unable to create database: %v", err)
	}
	return db.New(d), d
}
