// Package jobber retrieves job offers from linedin based on query
// parameters and store the queries and the job offers on the database.
package jobber

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/Alvaroalonsobabbel/jobber/db"
	"github.com/go-co-op/gocron/v2"
	"github.com/google/uuid"
	"modernc.org/sqlite"
	sqlite3 "modernc.org/sqlite/lib"
)

type Jobber struct {
	ctx      context.Context
	linkedIn *linkedIn
	logger   *slog.Logger
	db       *db.Queries
	sched    gocron.Scheduler
	schedMap map[int64]uuid.UUID
}

func New(log *slog.Logger, db *db.Queries) (*Jobber, func() error) {
	j := &Jobber{
		ctx:      context.Background(),
		linkedIn: NewLinkedIn(log),
		logger:   log,
		db:       db,
		schedMap: make(map[int64]uuid.UUID),
	}
	s, err := gocron.NewScheduler()
	if err != nil {
		log.Error("failed to create scheduler", slog.String("error", err.Error()))
	}
	j.sched = s

	queries, err := j.db.ListQueries(j.ctx)
	if err != nil {
		j.logger.Error("unable to list queries in jobber.scheduleQueries", slog.String("error", err.Error()))
	}
	for _, q := range queries {
		j.scheduleQuery(q)
	}
	s.Start()

	return j, j.sched.Shutdown
}

func (j *Jobber) CreateQuery(keywords, location string) (*db.Query, error) {
	query, err := j.db.CreateQuery(j.ctx, &db.CreateQueryParams{
		Keywords: keywords,
		Location: location,
	})
	var sqliteErr *sqlite.Error
	if errors.As(err, &sqliteErr) && sqliteErr.Code() == sqlite3.SQLITE_CONSTRAINT_UNIQUE {
		// If the query exist we return the existing query.
		eq, err := j.db.GetQuery(j.ctx, &db.GetQueryParams{
			Keywords: keywords,
			Location: location,
		})
		if err != nil {
			return nil, fmt.Errorf("failed to get query: %w", err)
		}
		return eq, nil
	}
	if err != nil {
		return nil, fmt.Errorf("failed to create query: %w", err)
	}
	j.logger.Info("created new query",
		slog.Int64("queryID", query.ID),
		slog.String("keywords", keywords),
		slog.String("location", location),
	)

	// After creating a new query we run it so the feed has initial data.
	// In the frontend we use a spinner with htmx while this is being processed.
	j.runQuery(query.ID)

	j.scheduleQuery(query)

	return query, nil
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
	return j.db.ListOffers(j.ctx, &db.ListOffersParams{
		ID:       q.ID,
		PostedAt: time.Now().AddDate(0, 0, -7), // List offers posted in the last 7 days.
	})
}

func (j *Jobber) runQuery(qID int64) {
	q, err := j.db.GetQueryByID(j.ctx, qID)
	if err != nil {
		j.logger.Error("unable to get query in jobber.runQuery", slog.Int64("queryID", qID), slog.String("error", err.Error()))
		return
	}

	// We remove queries that haven't been used for longer than 7 days.
	if time.Since(q.QueriedAt) > time.Hour*24*7 {
		if err := j.db.DeleteQuery(j.ctx, q.ID); err != nil {
			j.logger.Error("unable to delete query in jobber.runQuery", slog.Int64("queryID", q.ID), slog.String("error", err.Error()))
		}
		if err := j.sched.RemoveJob(j.schedMap[qID]); err != nil {
			j.logger.Error("unable to remove job from scheduler in jobber.runQuery", slog.Int64("queryID", q.ID), slog.String("error", err.Error()))
		}
		delete(j.schedMap, qID)

		j.logger.Info("deleting unused query", slog.Int64("queryID", q.ID), slog.String("keywords", q.Keywords), slog.String("location", q.Location))
		return
	}

	// TODO: extend ctx to linkedIn.search
	offers, err := j.linkedIn.search(q)
	if err != nil {
		j.logger.Error("unable to perform linkedIn search in jobber.runQuery", slog.Int64("queryID", q.ID), slog.String("error", err.Error()))
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

	j.logger.Info("jobber.runQuery successfully completed query", slog.Int64("queryID", q.ID))
}

func (j *Jobber) scheduleQuery(q *db.Query) {
	cron := fmt.Sprintf("%d * * * *", q.CreatedAt.Minute())
	job, err := j.sched.NewJob(
		gocron.CronJob(cron, false),
		gocron.NewTask(func(q int64) { j.runQuery(q) }, q.ID),
	)
	if err != nil {
		j.logger.Error("unable to schedule query in jobber.scheduleQuery", slog.Int64("queryID", q.ID), slog.String("error", err.Error()))
		return
	}
	j.schedMap[q.ID] = job.ID()
	j.logger.Info("scheduled query", slog.Int64("queryID", q.ID), slog.String("cron", cron), slog.Any("jobID", job.ID()))
}
