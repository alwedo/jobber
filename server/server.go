package server

import (
	"context"
	"database/sql"
	"errors"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"regexp"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/alwedo/jobber/assets"
	"github.com/alwedo/jobber/db"
	"github.com/alwedo/jobber/jobber"
	"github.com/alwedo/jobber/metrics"
	"github.com/alwedo/jobber/scrape"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

const (
	// Query Params.
	queryParamKeywords = "keywords"
	queryParamLocation = "location"
)

// Input validation regex.
var isValidKeywords = regexp.MustCompile(`^[A-Za-z0-9 ]+$`)
var isValidLocation = regexp.MustCompile(`^[A-Za-z ]+$`)

func Start(ctx context.Context, w io.Writer, getenv func(string) string, args []string) error {
	fs := flag.NewFlagSet("server", flag.ExitOnError)
	var hasMock bool
	fs.BoolVar(&hasMock, "mock", false, "") // Sets the scraper to a mock. Used for testing.
	var hasMockTimeout bool
	fs.BoolVar(&hasMockTimeout, "mockTimeout", false, "") // Sets the scraper mock timeout. Used for testing.
	var hasMetrics bool
	fs.BoolVar(&hasMetrics, "metrics", false, "") // Enables the /metrics endpoint.
	var port string
	fs.StringVar(&port, "port", ":80", "") // Sets the server port. Default port is 80.
	if err := fs.Parse(args[1:]); err != nil {
		return fmt.Errorf("parsing command lines: %w", err)
	}

	ctx, cancel := signal.NotifyContext(ctx, os.Interrupt, syscall.SIGTERM)
	defer cancel()
	l := slog.New(slog.NewJSONHandler(w, &slog.HandlerOptions{Level: slog.LevelInfo}))

	conn, err := pgxpool.New(ctx, getenv("DB_CONN"))
	if err != nil {
		return fmt.Errorf("unable to initialize db connection: %w", err)
	}
	defer conn.Close()
	if err := conn.Ping(ctx); err != nil {
		return fmt.Errorf("unable to ping database: %w", err)
	}

	r, err := newHTMLRenderer(assets.HTMLFiles, "base.tmpl", "xmlfeed.tmpl", "partials/*.tmpl")
	if err != nil {
		return fmt.Errorf("unable to parse templates: %w", err)
	}

	var jbrOpts []jobber.Options
	if hasMock {
		jbrOpts = append(jbrOpts, jobber.WithScrapeList(scrape.MockList))
	}
	if hasMockTimeout {
		jbrOpts = append(jbrOpts, jobber.WithTimeOut(time.Nanosecond))
	}
	j, jCloser := jobber.New(ctx, l, db.New(conn), jbrOpts...)
	defer jCloser()

	mux := http.NewServeMux()
	mux.HandleFunc("GET /feeds", feed(l, r, j))
	mux.HandleFunc("POST /feeds", create(l, r, j))
	mux.HandleFunc("GET /help", help(l, r))
	mux.HandleFunc("GET /", index(l, r))
	mux.Handle("GET /static/", staticHeadersMiddleware(http.StripPrefix("/static", http.FileServerFS(assets.StaticFiles))))
	mux.HandleFunc("GET /healthz", func(_ http.ResponseWriter, _ *http.Request) {})

	var handler http.Handler = mux
	if hasMetrics {
		mux.Handle("GET /metrics", promhttp.Handler())
		metrics.Init()
		handler = metrics.HTTPMiddleware(handler)
	}
	httpServer := &http.Server{
		Addr:              port,
		Handler:           handler,
		ReadHeaderTimeout: 10 * time.Second,
	}

	srvErr := make(chan error, 1)
	go func() {
		l.Info("listening on", slog.String("address", httpServer.Addr))
		if err := httpServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			srvErr <- err
		}
	}()

	select {
	case <-ctx.Done():
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := httpServer.Shutdown(ctx); err != nil {
			return fmt.Errorf("error shutting down http server: %w", err)
		}
	case err := <-srvErr:
		return fmt.Errorf("error in ListenAndServe: %w", err)
	}

	return nil
}

func index(log *slog.Logger, h *htmlRenderer) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/" {
			http.NotFound(w, r)
			return
		}
		if err := h.render(w, nil, "base", "pages/home.tmpl"); err != nil {
			internalError(w, log, "failed to execute template in server.index", err)
		}
	}
}

func help(log *slog.Logger, h *htmlRenderer) http.HandlerFunc {
	return func(w http.ResponseWriter, _ *http.Request) {
		if err := h.render(w, nil, "base", "pages/help.tmpl"); err != nil {
			internalError(w, log, "failed to execute template in server.help", err)
		}
	}
}

func create(log *slog.Logger, h *htmlRenderer, j *jobber.Jobber) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		params, err := validateParams([]string{queryParamKeywords, queryParamLocation}, w, r)
		if err != nil {
			log.Info("validateParams in server.create", slog.String("error", err.Error()))
			return
		}

		var timedOut bool
		if err := j.CreateQuery(r.Context(), params.Get(queryParamKeywords), params.Get(queryParamLocation)); err != nil {
			if errors.Is(err, jobber.ErrTimedOut) {
				timedOut = true
			} else {
				internalError(w, log, "failed to create query", err)
				return
			}
		}

		scheme := "https://"
		if r.Host == "localhost" {
			scheme = "http://"
		}
		u, err := url.Parse(scheme + r.Host + "/feeds")
		if err != nil {
			internalError(w, log, "failed to parse url in server.create", err)
			return
		}
		u.RawQuery = params.Encode()

		data := struct {
			URL      string
			TimedOut bool
		}{u.String(), timedOut}

		if err := h.render(w, data, "partial:response:create"); err != nil {
			internalError(w, log, "failed to execute template in server.create", err)
		}
	}
}

func feed(log *slog.Logger, h *htmlRenderer, j *jobber.Jobber) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		params, err := validateParams([]string{queryParamKeywords, queryParamLocation}, w, r)
		if err != nil {
			log.Info("validateParams in server.feed", slog.String("error", err.Error()))
			return
		}
		var (
			keywords = params.Get(queryParamKeywords)
			location = params.Get(queryParamLocation)
		)

		offers, updatedAt, err := j.ListOffers(r.Context(), &db.GetAndUpdateQueryParams{
			Keywords: keywords,
			Location: location,
		})
		if err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				http.NotFound(w, r)
			} else {
				internalError(w, log, "failed to get query in server.feed", err)
			}
			return
		}
		if updatedAt.Valid && time.Since(updatedAt.Time) < time.Hour {
			// We set a Cache-Control header with max-age so clients don't
			// waste time re-fetching information that hasn't been updated.
			// Since the queries get updated hourly, we want the max-age value
			// to be time in seconds until the next update.
			// If the calculated value is more than one hour we don't retun the
			// header since we can't guarantee when the next update will be.
			t := time.Hour - time.Since(updatedAt.Time)
			w.Header().Add("Cache-Control", "max-age="+strconv.Itoa(int(t.Seconds())))
		}

		// Set template and Content-Type header based on Accept header.
		// If Accept header is 'text/html' we assue the request is coming
		// from a browser, otherwise it's an RSS reader.
		ct := "application/rss+xml"
		tmpl := "xmlfeed"
		var addtmpl []string
		if strings.Contains(r.Header.Get("Accept"), "text/html") {
			ct = "text/html"
			tmpl = "base"
			addtmpl = []string{"pages/feed.tmpl"}
		}
		w.Header().Add("Content-Type", ct)

		data := struct {
			Keywords string
			Location string
			Host     string
			Offers   []*db.Offer
		}{
			Keywords: keywords,
			Location: location,
			Host:     r.Host,
			Offers:   offers,
		}

		if err := h.render(w, data, tmpl, addtmpl...); err != nil {
			internalError(w, log, "failed to execute template in server.feed", err)
		}
	}
}

func staticHeadersMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path := strings.TrimPrefix(r.URL.Path, "/static/")

		var ct string
		switch {
		case strings.HasPrefix(path, "css/"):
			ct = "text/css"
		case strings.HasPrefix(path, "js/"):
			ct = "application/javascript"
		}

		if ct != "" {
			w.Header().Add("Content-Type", ct)
			w.Header().Add("Cache-Control", "public, max-age=31536000, immutable")
			w.Header().Add("X-Content-Type-Options", "nosniff")
		}
		next.ServeHTTP(w, r)
	})
}

func internalError(w http.ResponseWriter, l *slog.Logger, msg string, err error) {
	// If the client disconnected (broken pipe / connection reset) avoid
	// attempting further writes which will only produce noise (and the
	// superfluous WriteHeader log). Log at info and return.
	var errStr string
	if err != nil {
		errStr = err.Error()
	}
	if strings.Contains(errStr, "broken pipe") || strings.Contains(errStr, "connection reset by peer") {
		l.Info(msg, slog.String("error", errStr))
		return
	}

	l.Error(msg, slog.String("error", errStr))
	http.Error(w, "it's not you it's me", http.StatusInternalServerError)
}

// validateParams receives a list of params, validate they've been supplied in the request and normalizes them.
// If a param is missing or contains invalid characters, it will respond with 400.
func validateParams(params []string, w http.ResponseWriter, r *http.Request) (url.Values, error) {
	r.Body = http.MaxBytesReader(w, r.Body, 1024)
	if err := r.ParseForm(); err != nil {
		var maxErr *http.MaxBytesError
		if ok := errors.As(err, &maxErr); ok {
			http.Error(w, "request body too large", http.StatusRequestEntityTooLarge)
			return nil, fmt.Errorf("request body too large: %w", err)
		}
		http.Error(w, "invalid form", http.StatusBadRequest)
		return nil, fmt.Errorf("parsing form: %w", err)
	}

	missing := []string{}
	invalid := []string{}
	valid := url.Values{}
	for _, p := range params {
		v := r.FormValue(p)
		switch {
		case v == "":
			missing = append(missing, p)
		case p == queryParamKeywords && !isValidKeywords.MatchString(v) ||
			p == queryParamLocation && !isValidLocation.MatchString(v):
			invalid = append(invalid, p)
		default:
			valid.Add(p, strings.ToLower(strings.TrimSpace(v)))
		}
	}
	if len(missing) != 0 || len(invalid) != 0 {
		w.WriteHeader(http.StatusBadRequest)
		var errStr []string
		if len(missing) != 0 {
			errStr = append(errStr, fmt.Sprintf("missing params: %v", missing))
		}
		if len(invalid) != 0 {
			errStr = append(errStr, fmt.Sprintf("invalid params: %v, only [A-Za-z0-9] allowed for keywords and [A-Za-z] for location", invalid))
		}
		_, err := fmt.Fprint(w, strings.Join(errStr, ", "))
		if err != nil {
			w.WriteHeader(http.StatusInternalServerError)
			return nil, fmt.Errorf("unable to write response in validateParams: %w", err)
		}
		return nil, fmt.Errorf("missing params in validateParams: %v", missing)
	}
	return valid, nil
}
