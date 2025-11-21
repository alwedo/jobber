-- name: CreateQuery :one
INSERT INTO
    queries (keywords, location)
VALUES
    (?, ?) RETURNING *;

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
    keywords = ?
    AND location = ?;

-- name: GetQueryByID :one
SELECT
    *
FROM
    queries
WHERE
    id = ?;

-- name: DeleteQuery :exec
DELETE FROM queries
WHERE
    id = ?;

-- name: UpdateQueryQAT :exec
UPDATE queries
SET
    queried_at = CURRENT_TIMESTAMP
WHERE
    id = ?;

-- name: UpdateQueryUAT :exec
UPDATE queries
SET
    updated_at = CURRENT_TIMESTAMP
WHERE
    id = ?;

-- name: CreateOffer :exec
INSERT
OR IGNORE INTO offers (id, title, company, location, posted_at)
VALUES
    (?, ?, ?, ?, ?);

-- name: ListOffers :many
SELECT
    o.*
FROM
    queries q
    JOIN query_offers qo ON q.id = qo.query_id
    JOIN offers o ON qo.offer_id = o.id
WHERE
    q.id = ?
    AND o.posted_at >= ?
ORDER BY
    o.posted_at DESC;

-- name: CreateQueryOfferAssoc :exec
INSERT
OR IGNORE INTO query_offers (query_id, offer_id)
VALUES
    (?, ?);
