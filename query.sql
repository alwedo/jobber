-- name: CreateOffer :exec
INSERT INTO
    offers (id, title, company, location, posted_at, query_id)
VALUES
    (?, ?, ?, ?, ?, ?);

-- name: ListOffers :many
SELECT
    *
FROM
    offers
WHERE
    query_id = ?;

-- name: CreateQuery :one
INSERT INTO
    queries (keywords, location, f_tpr, f_jt)
VALUES
    (?, ?, ?, ?) RETURNING *;

-- name: ListQueries :many
SELECT
    *
FROM
    queries;
