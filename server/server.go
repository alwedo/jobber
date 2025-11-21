package server

import (
	"database/sql"
	_ "embed"
	"errors"
	"fmt"
	"html"
	"log/slog"
	"net/http"
	"text/template"
	"time"

	"github.com/Alvaroalonsobabbel/jobber/db"
	"github.com/Alvaroalonsobabbel/jobber/jobber"
)

const (
	queryParamKeywords = "keywords"
	queryParamLocation = "location"
)

//go:embed "rss.goxml"
var rssTmpl string

//go:embed "index.html"
var indexHTML string

type server struct {
	logger *slog.Logger
	jobber *jobber.Jobber
}

func New(l *slog.Logger, j *jobber.Jobber) *http.Server {
	s := &server{logger: l, jobber: j}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /feeds", s.feed())
	mux.HandleFunc("POST /feeds", s.create())
	mux.HandleFunc("/", s.index())

	return &http.Server{
		// TODO: add tls
		Addr:    ":80",
		Handler: mux,
	}
}

func (s *server) create() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		// TODO: make params mandatory to avoid blank quries
		k := r.FormValue(queryParamKeywords)
		l := r.FormValue(queryParamLocation)
		q, err := s.jobber.CreateQuery(k, l)
		if err != nil {
			s.logger.Error("failed to create query", "keywords", k, "location", l, "error", err)
			http.Error(w, "failed to create query", http.StatusInternalServerError)
			return
		}

		url := fmt.Sprintf("https://%s/feeds?keywords=%s&location=%s", r.Host, q.Keywords, q.Location)
		w.Write([]byte(html.EscapeString(url)))
	}
}

func (s *server) index() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, indexHTML)
	}
}

type data struct {
	Keywords, Location string
	Offers             []*db.Offer
}

func (s *server) feed() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		k := r.FormValue(queryParamKeywords)
		l := r.FormValue(queryParamLocation)

		offers, err := s.jobber.ListOffers(k, l)
		if err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				// TODO: return xml with invalid query?
				s.logger.Info("no query found", "keywords", k, "location", l)
				http.Error(w, "no query found", http.StatusNotFound)
				return
			}
			s.logger.Error("failed to get query: " + err.Error())
			http.Error(w, "failed to get query", http.StatusInternalServerError)
			return
		}

		w.Header().Add("Content-Type", "application/rss+xml")
		funcMap := template.FuncMap{
			"createdAt": func(o *db.Offer) string {
				return o.CreatedAt.Format(time.RFC1123Z)
			},
			"title": func(o *db.Offer) string {
				t := fmt.Sprintf("%s at %s (posted %s)", o.Title, o.Company, o.PostedAt.Format("Jan 2"))
				return html.EscapeString(t)
			},
		}
		tmp, err := template.New("rss").Funcs(funcMap).Parse(rssTmpl)
		if err != nil {
			s.logger.Error("failed to parse template: " + err.Error())
			http.Error(w, "failed to parse template", http.StatusInternalServerError)
			return
		}

		if err := tmp.Execute(w, &data{
			Keywords: k,
			Location: l,
			Offers:   offers,
		}); err != nil {
			s.logger.Error("failed to execute template: " + err.Error())
			http.Error(w, "failed to execute template", http.StatusInternalServerError)
			return
		}

	}
}
