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
	ctx := context.Background()
	query, err := j.db.CreateQuery(ctx, &db.CreateQueryParams{
		Keywords: keywords,
		Location: location,
	})
	var sqliteErr *sqlite.Error
	if errors.As(err, &sqliteErr) && sqliteErr.Code() == sqlite3.SQLITE_CONSTRAINT_UNIQUE {
		// If the query exist we return the existing query.
		eq, err := j.db.GetQuery(ctx, &db.GetQueryParams{
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

	// TODO: perform this assync
	offers, err := j.linkedIn.search(query)
	if err != nil {
		j.logger.Error("unable to perform linkedIn search", slog.String("error", err.Error()))
	}
	if offers != nil || len(offers) > 0 {
		for _, o := range offers {
			if err := j.db.CreateOffer(ctx, &o); err != nil {
				j.logger.Error("unable to create offer", slog.String("error", err.Error()))
				continue
			}
			if err := j.db.CreateQueryOfferAssoc(ctx, &db.CreateQueryOfferAssocParams{
				QueryID: query.ID,
				OfferID: o.ID,
			}); err != nil {
				j.logger.Error("unable to create query offer association", slog.String("error", err.Error()))
			}
		}
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
