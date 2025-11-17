-- name: CreateQuery :exec
INSERT INTO
    queries (keywords, location)
VALUES
    (?, ?);

-- name: GetQuery :one
SELECT
    *
FROM
    queries
WHERE
    keywords = ?
    AND location = ?;

-- name: CreateOffer :exec
INSERT
OR IGNORE INTO offers (id, title, company, location, posted_at)
VALUES
    (?, ?, ?, ?, ?);

-- name: ListOffersFromQuery :many
SELECT
    o.*
FROM
    queries q
    JOIN query_offers qo ON q.id = qo.query_id
    JOIN offers o ON qo.offer_id = o.id
WHERE
    q.keywords = ?
    AND q.location = ?;
