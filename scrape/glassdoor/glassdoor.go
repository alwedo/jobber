package glassdoor

import (
	"context"
	"log/slog"

	"github.com/alwedo/jobber/db"
	"github.com/alwedo/jobber/scrape/retryhttp"
)

const Name = "Glassdoor"

type glassdoor struct {
	client *retryhttp.Client
	logger *slog.Logger
}

func New(l *slog.Logger) *glassdoor { //nolint: revive
	return &glassdoor{client: retryhttp.New(), logger: l}
}

func (g *glassdoor) Scrape(ctx context.Context, query *db.GetQueryScraperRow) ([]db.CreateOfferParams, error) {
	return []db.CreateOfferParams{}, nil
}
