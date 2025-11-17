package jobber

import (
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

func TestFetchOffers(t *testing.T) {
	mockResp := newMockResp(t)
	j := &Jobber{
		client: &http.Client{Transport: mockResp},
		logger: slog.New(slog.NewTextHandler(io.Discard, &slog.HandlerOptions{})),
	}
	query := &db.Query{
		Keywords: "golang",
		Location: "the moon",
	}
	resp, err := j.fetchOffers(query, 0)
	if err != nil {
		t.Errorf("error fetching offers: %s", err.Error())
	}
	defer resp.Close()
	values := mockResp.req.URL.Query()
	if values.Get(paramKeywords) != "golang" {
		t.Errorf("expected 'keywords' in query params to be 'golang', got %s", values.Get(paramKeywords))
	}
	if values.Get(paramLocation) != "the moon" {
		t.Errorf("expected 'location' in query params to be 'the moon', got %s", values.Get(paramLocation))
	}
	if values.Get(paramFTPR) != lastWeek {
		t.Errorf("expected 'f_TPR' in query params to be lastlastWeek, got %s", values.Get(paramFTPR))
	}
	if mockResp.req.URL.Host != "www.linkedin.com" {
		t.Errorf("expected host to be 'www.linkedin.com', got %s", mockResp.req.URL.Host)
	}
	if mockResp.req.URL.Path != "/jobs-guest/jobs/api/seeMoreJobPostings/search" {
		t.Errorf("expected path to be '/jobs-guest/jobs/api/seeMoreJobPostings/search', got %s", mockResp.req.URL.Path)
	}
	file, err := os.Open("example1.html")
	if err != nil {
		t.Fatalf("failed to open file: %s", err.Error())
	}
	defer file.Close()
	want, err := io.ReadAll(file)
	if err != nil {
		t.Fatalf("failed to read example1.html file: %s", err.Error())
	}
	got, err := io.ReadAll(resp)
	if err != nil {
		t.Errorf("unable to read response body: %v", err)
	}
	if len(want) != len(got) {
		t.Errorf("expected response body length to be %d, got %d", len(want), len(got))
	}
}

func TestParseLinkedinBody(t *testing.T) {
	j := &Jobber{logger: slog.New(slog.NewTextHandler(io.Discard, &slog.HandlerOptions{}))}

	file, err := os.Open("example1.html")
	if err != nil {
		log.Fatalf("failed to open file: %s", err.Error())
	}
	defer file.Close()

	jobs, err := j.parseLinkedinBody(file)
	if err != nil {
		t.Fatalf("error parsing example.html: %s", err.Error())
	}
	if len(jobs) != 10 {
		t.Errorf("expected 10 jobs, got %d", len(jobs))
	}
	if jobs[0].ID != "4322119156" {
		t.Errorf("expected job ID 4322119156, got %s", jobs[0].ID)
	}
	if jobs[0].Title != "Software Engineer (Golang)" {
		t.Errorf("expected job title 'Software Engineer (Golang)', got '%s'", jobs[0].Title)
	}
	if jobs[0].Location != "Berlin, Berlin, Germany" {
		t.Errorf("expected job location 'Berlin, Berlin, Germany', got '%s'", jobs[0].Location)
	}
	if jobs[0].Company != "Delivery Hero" {
		t.Errorf("expected job company 'Delivery Hero', got '%s'", jobs[0].Company)
	}
	if jobs[0].PostedAt.Format("2006-01-02") != "2025-11-13" {
		t.Errorf("expected job posted at time %v, got %v", "2025-11-13", jobs[0].PostedAt.Format("2006-01-02"))
	}
}

func TestPerformQuery(t *testing.T) {
	mockResp := newMockResp(t)
	j := &Jobber{
		client: &http.Client{Transport: mockResp},
		logger: slog.New(slog.NewTextHandler(io.Discard, &slog.HandlerOptions{})),
	}
	query := &db.Query{
		Keywords: "golang",
		Location: "the moon",
	}
	offers, err := j.PerformQuery(query)
	if err != nil {
		t.Errorf("expected no error, got %v", err)
	}
	if len(offers) != 27 {
		t.Errorf("expected 27 offers, got %d", len(offers))
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
INSERT INTO queries (keywords, location) VALUES
('software engineer python', 'San Francisco'),
('data scientist remote', 'New York'),
('golang', 'Berlin');
INSERT INTO offers (id, title, company, location, posted_at) VALUES
('offer_001', 'Senior Python Developer', 'TechCorp Inc', 'San Francisco, CA', '2024-01-15 10:30:00'),
('existing_offer', 'Junior Golang Dweeb', 'Späti GmbH', 'Berlin', '2024-01-15 10:30:00');
INSERT INTO query_offers (query_id, offer_id) VALUES
(1, 'offer_001'),
(3, 'existing_offer'),
(1, 'existing_offer');
`
	if _, err := d.ExecContext(context.Background(), seed); err != nil {
		t.Fatalf("failed to seed database: %s", err)
	}
	return db.New(d), d.Close
}

type mockResp struct {
	t   testing.TB
	req *http.Request
}

func (h *mockResp) RoundTrip(req *http.Request) (*http.Response, error) {
	h.req = req
	fn := "example1.html"
	switch h.req.URL.Query().Get("start") {
	case "10":
		fn = "example2.html"
	case "20":
		fn = "example3.html"
	}

	body, err := os.Open(fn)
	if err != nil {
		h.t.Fatalf("failed to open %s in mockResp.RoundTrip: %s", fn, err)
	}

	return &http.Response{
		StatusCode: http.StatusOK,
		Body:       body,
	}, nil
}

func newMockResp(t testing.TB) *mockResp {
	return &mockResp{t: t}
}
