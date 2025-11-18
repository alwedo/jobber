// Package jobber retrieves job offers from linedin based on query
// parameters and store the queries and the job offers on the database.
package jobber

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	"github.com/Alvaroalonsobabbel/jobber/db"
	"modernc.org/sqlite"
	sqlite3 "modernc.org/sqlite/lib"
)

type Jobber struct {
	linkedIn *linkedIn
	logger   *slog.Logger
	db       *db.Queries
}

func New(log *slog.Logger, db *db.Queries) *Jobber {
	return &Jobber{
		linkedIn: NewLinkedIn(log),
		logger:   log,
		db:       db,
	}
}

func (j *Jobber) CreateQuery(keywords, location string) (*db.Query, error) {
	query, err := j.db.CreateQuery(context.Background(), &db.CreateQueryParams{
		Keywords: keywords,
		Location: location,
	})
	var sqliteErr *sqlite.Error
	if errors.As(err, &sqliteErr) && sqliteErr.Code() == sqlite3.SQLITE_CONSTRAINT_UNIQUE {
		// If the query exist we return the existing query.
		eq, err := j.db.GetQuery(context.Background(), &db.GetQueryParams{
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
	return query, nil
}

func (j *Jobber) ListOffers(keywords, location string) ([]*db.Offer, error) {
	q, err := j.db.GetQuery(context.Background(), &db.GetQueryParams{
		Keywords: keywords,
		Location: location,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to get query: %w", err)
	}
	if err := j.db.UpdateQueryTS(context.Background(), q.ID); err != nil {
		return nil, fmt.Errorf("failed to update query timestamp: %w", err)
	}
	return j.db.ListOffers(context.Background(), q.ID)
}
