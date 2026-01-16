-- name: CreateQuery :one
INSERT INTO
    queries (keywords, location)
VALUES
    ($1, $2) RETURNING *;

-- name: ListQueries :many
SELECT
    *
FROM
    queries;

-- name: GetQuery :one
SELECT
    *
FROM
    queries
WHERE
    keywords = $1
    AND location = $2;

-- name: GetQueryByID :one
SELECT
    *
FROM
    queries
WHERE
    id = $1;

-- name: DeleteQuery :exec
DELETE FROM queries
WHERE
    id = $1;

-- name: UpdateQueryQAT :exec
UPDATE queries
SET
    queried_at = CURRENT_TIMESTAMP
WHERE
    id = $1;

-- name: UpdateQueryUAT :exec
UPDATE queries
SET
    updated_at = CURRENT_TIMESTAMP
WHERE
    id = $1;

-- name: CreateOffer :exec
INSERT INTO offers (id, title, company, location, posted_at, description, source, url)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
ON CONFLICT (id) DO NOTHING;

-- name: ListOffers :many
SELECT
    o.*
FROM
    queries q
    JOIN query_offers qo ON q.id = qo.query_id
    JOIN offers o ON qo.offer_id = o.id
WHERE
    q.id = $1
ORDER BY
    o.posted_at DESC;

-- name: CreateQueryOfferAssoc :exec
INSERT INTO query_offers (query_id, offer_id)
VALUES ($1, $2)
ON CONFLICT (query_id, offer_id) DO NOTHING;

-- name: DeleteOldOffers :exec
DELETE FROM offers
WHERE posted_at < NOW() - INTERVAL '7 days';

-- name: GetQueryScraper :one
WITH q AS (
    SELECT *
    FROM queries
    WHERE id = $1
),
ins AS (
    INSERT INTO query_scraper_status (query_id, scraper_name)
    SELECT q.id, $2
    FROM q
    WHERE NOT EXISTS (
        SELECT 1
        FROM query_scraper_status s
        WHERE s.query_id = q.id
          AND s.scraper_name = $2
    )
    RETURNING query_id, scraper_name, scraped_at
),
s AS (
    SELECT query_id, scraper_name, scraped_at
    FROM query_scraper_status
    WHERE query_id = $1
      AND scraper_name = $2

    UNION ALL

    SELECT query_id, scraper_name, scraped_at
    FROM ins
)
SELECT
    q.*,
    s.scraped_at
FROM q
JOIN s ON s.query_id = q.id;

-- name: UpdateQueryScrapedAt :exec
UPDATE query_scraper_status
SET scraped_at = CURRENT_TIMESTAMP
WHERE query_id = $1
  AND scraper_name = $2;
