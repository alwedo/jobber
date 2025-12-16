// Package jobber orchestrates scheduled scraping of job offers from external sources based on
// user-defined search queries. It manages query lifecycle (creation, scheduling, expiration),
// persists results to a database, and automatically prunes stale queries after 7 days of inactivity.
// Each query runs on an hourly cron schedule, deduplicates offers, and maintains query-offer
// associations for efficient retrieval.
package jobber

import (
	"context"
	"errors"
	"fmt"

	"log/slog"
	"time"

	"github.com/alwedo/jobber/db"
	"github.com/alwedo/jobber/metrics"
	"github.com/alwedo/jobber/scrape"
	"github.com/go-co-op/gocron/v2"
	"github.com/google/uuid"
	"github.com/jackc/pgerrcode"
	"github.com/jackc/pgx/v5/pgconn"
)

type Jobber struct {
	ctx    context.Context
	scpr   scrape.Scraper
	logger *slog.Logger
	db     *db.Queries
	sched  gocron.Scheduler
}

func New(log *slog.Logger, db *db.Queries) (*Jobber, func()) {
	return NewConfigurableJobber(log, db, scrape.LinkedIn())
}

func NewConfigurableJobber(log *slog.Logger, db *db.Queries, s scrape.Scraper) (*Jobber, func()) {
	sched, err := gocron.NewScheduler()
	if err != nil {
		log.Error("failed to create scheduler", slog.String("error", err.Error()))
	}
	ctx, cancelCtx := context.WithCancel(context.Background())
	j := &Jobber{
		ctx:    ctx,
		scpr:   s,
		logger: log,
		db:     db,
		sched:  sched,
	}

	// Initial job scheduling.
	queries, err := j.db.ListQueries(j.ctx)
	if err != nil {
		j.logger.Error("unable to list queries in jobber.scheduleQueries", slog.String("error", err.Error()))
	}
	for _, q := range queries {
		j.scheduleQuery(q)
	}
	j.schedDeleteOldOffers()
	j.sched.Start()

	return j, func() {
		cancelCtx()
		if err := j.sched.Shutdown(); err != nil {
			j.logger.Error("failed to shutdown scheduler", slog.String("error", err.Error()))
		}
	}
}

// CreateQuery creates a new query and schedules it.
// If the query already exists the creation will be ignored.
func (j *Jobber) CreateQuery(keywords, location string) error {
	query, err := j.db.CreateQuery(j.ctx, &db.CreateQueryParams{
		Keywords: keywords,
		Location: location,
	})
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == pgerrcode.UniqueViolation {
		// If the query exist we just return. The server will respond with the RSS feed url.
		return nil
	}
	if err != nil {
		return fmt.Errorf("failed to create query: %w", err)
	}
	j.logger.Info("created new query",
		slog.Int64("queryID", query.ID),
		slog.String("keywords", keywords),
		slog.String("location", location),
	)
	metrics.JobberNewQueries.WithLabelValues(keywords, location).Inc()

	// After creating a new query we schedule it and run it immediately
	// so the feed has initial data. In the frontend we use a spinner
	// with htmx while this is being processed.
	done := make(chan struct{})
	o := []gocron.JobOption{
		gocron.WithStartAt(gocron.WithStartImmediately()),
		gocron.WithEventListeners(gocron.AfterJobRuns(func(uuid.UUID, string) {
			done <- struct{}{}
			defer close(done)
		})),
	}

	j.scheduleQuery(query, o...)

	// Blocks and waits for the job to finish or for a timeout.
	select {
	case <-done:
	case <-time.After(10 * time.Second):
		j.logger.Info("scheduleQuery in jobber.CreateQuery took more than 10 sec", slog.String("keywords", keywords), slog.String("location", location))
	}

	return nil
}

// ListOffers return the list of offers posted in the last 7 days for a
// given query's keywords and location.
// If the query doesn't exist, a sql.ErrNoRows will be returned.
func (j *Jobber) ListOffers(keywords, location string) ([]*db.Offer, error) {
	q, err := j.db.GetQuery(j.ctx, &db.GetQueryParams{
		Keywords: keywords,
		Location: location,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to get query: %w", err)
	}
	if err := j.db.UpdateQueryQAT(j.ctx, q.ID); err != nil {
		j.logger.Error("unable to update query timestamp", slog.Int64("queryID", q.ID), slog.String("error", err.Error()))
	}
	return j.db.ListOffers(j.ctx, q.ID)
}

func (j *Jobber) runQuery(qID int64) {
	q, err := j.db.GetQueryByID(j.ctx, qID)
	if err != nil {
		j.logger.Error("unable to get query in jobber.runQuery", slog.Int64("queryID", qID), slog.String("error", err.Error()))
		return
	}

	// We remove queries that haven't been used for longer than 7 days.
	if time.Since(q.QueriedAt.Time) > time.Hour*24*7 {
		if err := j.db.DeleteQuery(j.ctx, q.ID); err != nil {
			j.logger.Error("unable to delete query in jobber.runQuery", slog.Int64("queryID", q.ID), slog.String("error", err.Error()))
		}
		j.sched.RemoveByTags(q.Keywords + q.Location)
		metrics.JobberScheduledQueries.WithLabelValues(fmt.Sprintf("%d", q.ID), q.Keywords+q.Location, "").Dec()

		j.logger.Info("deleting unused query", slog.Int64("queryID", q.ID), slog.String("keywords", q.Keywords), slog.String("location", q.Location))
		return
	}

	offers, err := j.scpr.Scrape(j.ctx, q)
	if err != nil {
		if errors.Is(err, scrape.ErrRetryable) {
			// Retryable errors still bring data. We log a warning for further analysis and continue.
			j.logger.Warn("exhausted retries in jobber.runQuery", slog.Int64("queryID", q.ID), slog.Any("error", err))
		} else {
			j.logger.Error("scrape in jobber.runQuery", slog.Int64("queryID", q.ID), slog.String("error", err.Error()))
			return
		}
	}
	if len(offers) > 0 {
		for _, o := range offers {
			if err := j.db.CreateOffer(j.ctx, &o); err != nil {
				j.logger.Error("unable to create offer in jobber.runQuery", slog.Int64("queryID", q.ID), slog.String("error", err.Error()))
				continue
			}
			if err := j.db.CreateQueryOfferAssoc(j.ctx, &db.CreateQueryOfferAssocParams{
				QueryID: q.ID,
				OfferID: o.ID,
			}); err != nil {
				j.logger.Error("unable to create query offer association in jobber.runQuery", slog.Int64("queryID", q.ID), slog.String("error", err.Error()))
			}
		}
	}

	if err := j.db.UpdateQueryUAT(j.ctx, q.ID); err != nil {
		j.logger.Error("unable to update query timestamp in jobber.runQuery", slog.Int64("queryID", q.ID), slog.String("error", err.Error()))
	}

	j.logger.Debug("successfuly completed jobber.runQuery", slog.Int64("queryID", q.ID), slog.String("keywords", q.Keywords), slog.String("location", q.Location))
}

func (j *Jobber) scheduleQuery(q *db.Query, o ...gocron.JobOption) {
	opts := []gocron.JobOption{gocron.WithTags(q.Keywords + q.Location)}
	opts = append(opts, o...)

	cron := fmt.Sprintf("%d * * * *", q.CreatedAt.Time.Minute())
	job, err := j.sched.NewJob(
		gocron.CronJob(cron, false),
		gocron.NewTask(func(q int64) { j.runQuery(q) }, q.ID),
		opts...,
	)
	if err != nil {
		j.logger.Error("unable to schedule query in jobber.scheduleQuery", slog.Int64("queryID", q.ID), slog.String("error", err.Error()))
		return
	}

	metrics.JobberScheduledQueries.WithLabelValues(fmt.Sprintf("%d", q.ID), q.Keywords+q.Location, cron).Inc()
	j.logger.Info("scheduled query", slog.Int64("queryID", q.ID), slog.String("cron", cron), slog.Any("tags", job.Tags()))
}

func (j *Jobber) schedDeleteOldOffers() {
	at := "0 2 * * *" // Every day at 2:00 am.
	_, err := j.sched.NewJob(
		gocron.CronJob(at, false),
		gocron.NewTask(func() {
			if err := j.db.DeleteOldOffers(j.ctx); err != nil {
				j.logger.Error("unable to delete old offers", slog.String("error", err.Error()))
			}
		}),
		gocron.WithStartAt(gocron.WithStartImmediately()),
	)
	if err != nil {
		j.logger.Error("unable to schedule DeleteOldOffers job", slog.String("error", err.Error()))
	}
}
