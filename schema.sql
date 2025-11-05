CREATE TABLE IF NOT EXISTS queries (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    keywords TEXT NOT NULL,
    location TEXT NOT NULL,
    -- **Time Posted Range (f_TPR):**
    -- - `r86400` = Past 24 hours
    -- - `r604800` = Past week
    -- - `r2592000` = Past month
    -- - `rALL` = Any time
    f_tpr TEXT NOT NULL,
    -- **Job Type (f_JT):**
    -- - `F` = Full-time
    -- - `P` = Part-time
    -- - `C` = Contract
    -- - `T` = Temporary
    -- - `I` = Internship
    -- - `V` = Volunteer
    -- - `O` = Other
    f_jt TEXT NOT NULL,
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE IF NOT EXISTS offers (
    id TEXT NOT NULL PRIMARY KEY UNIQUE,
    query_id INTEGER NOT NULL,
    title TEXT NOT NULL,
    company TEXT NOT NULL,
    location TEXT NOT NULL,
    ignored BOOL NOT NULL DEFAULT 0,
    posted_at TIMESTAMP NOT NULL,
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    FOREIGN KEY (query_id) REFERENCES queries (id)
);
