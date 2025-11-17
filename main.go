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

	"github.com/Alvaroalonsobabbel/jobber/db"
	"github.com/Alvaroalonsobabbel/jobber/jobber"
	_ "modernc.org/sqlite"
)

func main() {
	logger, closer := initLogger()
	defer closer.Close()

	d, closer := initDB()
	defer closer.Close()

	j := jobber.New(logger, d)
	offers, err := j.PerformQuery(&db.Query{
		Keywords: "barista",
		Location: "potsdam",
	})
	if err != nil {
		log.Fatal(err)
	}
	o := offers[len(offers)-1]
	fmt.Println("title: " + o.Title)
	fmt.Println("location: " + o.Location)
	fmt.Println("company: " + o.Company)
	fmt.Println("link(edin): https://www.linkedin.com/jobs/view/" + o.ID)
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

func initDB() (*db.Queries, io.Closer) {
	d, err := sql.Open("sqlite", "jobber.sqlite")
	if err != nil {
		log.Fatalf("unable to open database: %v", err)
	}
	if _, err := d.ExecContext(context.Background(), ddl); err != nil {
		log.Fatalf("unable to create database: %v", err)
	}
	return db.New(d), d
}
