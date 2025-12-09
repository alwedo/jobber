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
INSERT INTO offers (id, title, company, location, posted_at)
VALUES ($1, $2, $3, $4, $5)
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
