package jobber

import (
	"bytes"
	"context"
	"database/sql"
	_ "embed"
	"io"
	"log"
	"log/slog"
	"net/http"
	"os"
	"testing"

	"github.com/Alvaroalonsobabbel/jobber/db"
	_ "modernc.org/sqlite"
)

func TestListQueries(t *testing.T) {
	d, closeDB := testDB(t)
	defer closeDB() //nolint:errcheck
	j := &Jobber{
		client: http.DefaultClient,
		logger: slog.New(slog.NewTextHandler(io.Discard, &slog.HandlerOptions{})),
		db:     d,
	}
	queries := j.ListQueries()
	if len(queries) != 3 {
		t.Errorf("expected 3 queries, got %d", len(queries))
	}
	if queries[0].Keywords != "software engineer python" {
		t.Errorf("expected keywords 'software engineer python', got %s", queries[0].Keywords)
	}
	if queries[1].Location != "New York" {
		t.Errorf("expected location 'New York', got %s", queries[1].Location)
	}
}

func TestNewQuery(t *testing.T) {
	d, closeDB := testDB(t)
	defer closeDB() //nolint:errcheck
	j := &Jobber{
		client: http.DefaultClient,
		logger: slog.New(slog.NewTextHandler(io.Discard, &slog.HandlerOptions{})),
		db:     d,
	}
	wantKeywords := "golang"
	wantLocation := "Berlin"
	wantFTPR := "r172800"

	query := j.NewQuery(&db.CreateQueryParams{
		Keywords: wantKeywords,
		Location: wantLocation,
		FTpr:     wantFTPR,
	})
	if query.Keywords != wantKeywords {
		t.Errorf("expected keywords '%s', got %s", wantKeywords, query.Keywords)
	}
	if query.Location != wantLocation {
		t.Errorf("expected location '%s', got %s", wantLocation, query.Location)
	}
	if query.FTpr != wantFTPR {
		t.Errorf("expected FTpr '%s', got %s", wantFTPR, query.FTpr)
	}
	if query.ID != 4 {
		t.Errorf("expected ID '4', got %d", query.ID)
	}
	if query.FJt != "" {
		t.Errorf("expected FJt '', got %s", query.FJt)
	}
}

func TestRunQuery(t *testing.T) {
	d, closeDB := testDB(t)
	defer closeDB() //nolint:errcheck
	j := &Jobber{
		client: &http.Client{Transport: newMockResp(t)},
		logger: slog.New(slog.NewTextHandler(io.Discard, &slog.HandlerOptions{})),
		db:     d,
	}
	query := &db.Query{
		ID: 3, // This is an existing query in the DB seed.
		// the rest of the params are not needed here since we're
		// faking the http call and we'll receive always the same doc
	}
	offers := j.RunQuery(query)
	// There are 10 offers in the example and one offer in the DB seed for this query.
	if len(offers) != 11 {
		t.Errorf("expected 11 offer, got %d", len(offers))
	}
	if offers[len(offers)-1].ID != "existing_offer" {
		t.Errorf("expected ID 'existing_offer', got %s", offers[0].ID)
	}
}

func TestFetchOffers(t *testing.T) {
	mockResp := newMockResp(t)
	j := &Jobber{
		client: &http.Client{Transport: mockResp},
		logger: slog.New(slog.NewTextHandler(io.Discard, &slog.HandlerOptions{})),
	}
	query := &db.Query{
		Keywords: "golang",
		Location: "the moon",
		FTpr:     "cuak",
		FJt:      "yes",
	}
	resp, err := j.fetchOffers(query)
	if err != nil {
		t.Errorf("error fetching offers: %s", err.Error())
	}
	defer resp.Close()
	values := mockResp.req.URL.Query()
	if values.Get(ParamKeywords) != "golang" {
		t.Errorf("expected 'keywords' in query params to be 'golang', got %s", values.Get(ParamKeywords))
	}
	if values.Get(ParamLocation) != "the moon" {
		t.Errorf("expected 'location' in query params to be 'the moon', got %s", values.Get(ParamLocation))
	}
	if values.Get(ParamFTPR) != "cuak" {
		t.Errorf("expected 'f_TPR' in query params to be 'cuak', got %s", values.Get(ParamFTPR))
	}
	if values.Get(ParamFJT) != "yes" {
		t.Errorf("expected 'f_JT' in query params to be 'yes', got %s", values.Get(ParamFJT))
	}
	if mockResp.req.URL.Host != "www.linkedin.com" {
		t.Errorf("expected host to be 'www.linkedin.com', got %s", mockResp.req.URL.Host)
	}
	if mockResp.req.URL.Path != "/jobs/search" {
		t.Errorf("expected path to be '/jobs/search', got %s", mockResp.req.URL.Path)
	}
	file, err := os.Open("example.html")
	if err != nil {
		t.Fatalf("failed to open file: %s", err.Error())
	}
	defer file.Close()
	want, err := io.ReadAll(file)
	if err != nil {
		t.Fatalf("failed to read example.html file: %s", err.Error())
	}
	if len(want) != len(mockResp.respBody) {
		t.Errorf("expected response body length to be %d, got %d", len(want), len(mockResp.respBody))
	}
}

func TestParseLinkedinBody(t *testing.T) {
	j := &Jobber{logger: slog.New(slog.NewTextHandler(io.Discard, &slog.HandlerOptions{}))}

	file, err := os.Open("example.html")
	if err != nil {
		log.Fatalf("failed to open file: %s", err.Error())
	}
	defer file.Close()

	jobs, err := j.parseLinkedinBody(file, 1)
	if err != nil {
		t.Fatalf("error parsing example.html: %s", err.Error())
	}
	if len(jobs) != 10 {
		t.Errorf("expected 10 jobs, got %d", len(jobs))
	}
	if jobs[0].ID != "4331449214" {
		t.Errorf("expected job ID 4331449214, got %s", jobs[0].ID)
	}
	if jobs[0].Title != "(Senior) Software Engineer - Backend (m/w/d)" {
		t.Errorf("expected job title '(Senior) Software Engineer - Backend (m/w/d)', got '%s'", jobs[0].Title)
	}
	if jobs[0].Location != "Berlin, Berlin, Germany" {
		t.Errorf("expected job location 'Berlin, Berlin, Germany', got '%s'", jobs[0].Location)
	}
	if jobs[0].Company != "FinCompare - Smarter Business Finance" {
		t.Errorf("expected job company 'FinCompare - Smarter Business Finance', got '%s'", jobs[0].Company)
	}
	if jobs[0].PostedAt.Format("2006-01-02") != "2025-10-30" {
		t.Errorf("expected job posted at time %v, got %v", "2025-10-30", jobs[0].PostedAt.Format("2006-01-02"))
	}
	if jobs[0].QueryID != 1 {
		t.Errorf("expected job query ID 1, got %d", jobs[0].QueryID)
	}
}

func testDB(t testing.TB) (*db.Queries, func() error) {
	schema, err := os.Open("../schema.sql")
	if err != nil {
		t.Fatalf("unable to open DB schema: %s", err)
	}
	defer schema.Close()
	ddl, err := io.ReadAll(schema)
	if err != nil {
		t.Fatalf("unable to read DB schema: %s", err)
	}
	d, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("failed to open database: %s", err)
	}
	if _, err := d.ExecContext(context.Background(), string(ddl)); err != nil {
		t.Fatalf("failed to execute DB schema: %s", err)
	}
	seed := `
INSERT INTO queries (keywords, location, f_tpr, f_jt) VALUES
('software engineer python', 'San Francisco', 'r604800', 'F'),
('data scientist remote', 'New York', 'r86400', 'C'),
('golang', 'Berlin', 'r172800', '');
INSERT INTO offers (id, query_id, title, company, location, posted_at) VALUES
('offer_001', 1, 'Senior Python Developer', 'TechCorp Inc', 'San Francisco, CA', '2024-01-15 10:30:00'),
('existing_offer', 3, 'Junior Golang Dweeb', 'Späti GmbH', 'Berlin', '2024-01-15 10:30:00');
`
	if _, err := d.ExecContext(context.Background(), seed); err != nil {
		t.Fatalf("failed to seed database: %s", err)
	}
	return db.New(d), d.Close
}

type mockResp struct {
	req      *http.Request
	respBody []byte
}

func (h *mockResp) RoundTrip(req *http.Request) (*http.Response, error) {
	h.req = req

	return &http.Response{
		StatusCode: http.StatusOK,
		Body:       io.NopCloser(bytes.NewReader(h.respBody)),
	}, nil
}

func newMockResp(t testing.TB) *mockResp {
	example, err := os.Open("example.html")
	if err != nil {
		t.Fatalf("failed to open example.html: %s", err)
	}
	defer example.Close()
	respBody, err := io.ReadAll(example)
	if err != nil {
		t.Fatalf("failed to read example.html: %s", err)
	}
	return &mockResp{
		respBody: respBody,
	}
}
