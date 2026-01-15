CREATE TABLE IF NOT EXISTS query_scraper_status (
    query_id BIGINT NOT NULL REFERENCES queries(id) ON DELETE CASCADE,
    scraper_name TEXT NOT NULL,
    scraped_at TIMESTAMPTZ,
    PRIMARY KEY (query_id, scraper_name)
);
