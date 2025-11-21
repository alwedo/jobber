CREATE TABLE IF NOT EXISTS queries (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    keywords TEXT NOT NULL,
    location TEXT NOT NULL,
    created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    queried_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP, -- Last time RSS client used the query.
    updated_at TIMESTAMP, -- Last time the query was executed.
    UNIQUE (keywords, location)
);

CREATE INDEX IF NOT EXISTS idx_queries_keywords_location ON queries (keywords, location);

CREATE TABLE IF NOT EXISTS offers (
    id TEXT PRIMARY KEY,
    title TEXT NOT NULL,
    company TEXT NOT NULL,
    location TEXT NOT NULL,
    posted_at TIMESTAMP NOT NULL,
    created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX IF NOT EXISTS idx_offers_posted_at ON offers (posted_at DESC);

CREATE TABLE IF NOT EXISTS query_offers (
    query_id INTEGER NOT NULL,
    offer_id TEXT NOT NULL,
    PRIMARY KEY (query_id, offer_id),
    FOREIGN KEY (query_id) REFERENCES queries (id) ON DELETE CASCADE,
    FOREIGN KEY (offer_id) REFERENCES offers (id) ON DELETE CASCADE
);
