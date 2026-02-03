package retryhttp_test

import (
	"errors"
	"io"
	"net/http"
	"strconv"
	"strings"
	"testing"
	"testing/synctest"

	"github.com/alwedo/jobber/scrape/retryhttp"
)

func TestDo(t *testing.T) {
	tests := []struct {
		name      string
		code      int
		opt       retryhttp.Option
		wantRetry bool
	}{
		{
			name:      "wants retries for status ",
			code:      http.StatusRequestTimeout,
			wantRetry: true,
		},
		{
			name:      "wants retries for status ",
			code:      http.StatusTooEarly,
			wantRetry: true,
		},
		{
			name:      "wants retries for status ",
			code:      http.StatusTooManyRequests,
			wantRetry: true,
		},
		{
			name:      "wants retries for status ",
			code:      http.StatusInternalServerError,
			wantRetry: true,
		},
		{
			name:      "wants retries for status ",
			code:      http.StatusBadGateway,
			wantRetry: true,
		},
		{
			name:      "wants retries for status ",
			code:      http.StatusServiceUnavailable,
			wantRetry: true,
		},
		{
			name:      "wants retries for status ",
			code:      http.StatusGatewayTimeout,
			wantRetry: true,
		},
		{
			name: "does not wants retries for status ",
			code: http.StatusBadRequest,
		},
		{
			name:      "added to extra retryable wants retries for status ",
			code:      http.StatusBadRequest,
			opt:       retryhttp.WithExtraRetryableStatus([]int{http.StatusBadRequest}),
			wantRetry: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name+http.StatusText(tt.code), func(t *testing.T) {
			synctest.Test(t, func(*testing.T) {
				{
					mock := &mock{t: t}
					opts := []retryhttp.Option{retryhttp.WithTransport(mock)}
					if tt.opt != nil {
						opts = append(opts, tt.opt)
					}
					rh := retryhttp.New(opts...)

					wantBody := "cuak"
					req, err := http.NewRequest(http.MethodGet, strconv.Itoa(tt.code), strings.NewReader(wantBody))
					if err != nil {
						t.Fatalf("unable to create http request: %v", err)
					}

					_, err = rh.Do(req)
					if tt.wantRetry && !errors.Is(err, retryhttp.ErrRetryable) {
						t.Errorf("expected ErrRetryable, got: %v", err)
					}

					synctest.Wait()

					// We check the body is always sent on each retry.
					if mock.calls != len(mock.bodies) {
						t.Errorf("wanted len mock calls to be the same as len of mock bodies. mockCalls: %d, mockBodies: %d", mock.calls, len(mock.bodies))
					}

					for _, gotBody := range mock.bodies {
						if wantBody != string(gotBody) {
							t.Errorf("wanted body %s, got %s", wantBody, string(gotBody))
						}
					}

					// Assert the retries.
					if tt.wantRetry && mock.calls < 2 {
						t.Errorf("wanted mock calls to be more than 1, got: %d", mock.calls)
					}

					if !tt.wantRetry && mock.calls != 1 {
						t.Errorf("wanted mock calls to be 1, got %d", mock.calls)
					}
				}
			})
		})
	}
}

// mock converts the url to the status code wanted to be returned.
type mock struct {
	t      testing.TB
	calls  int
	bodies [][]byte
}

func (m *mock) RoundTrip(r *http.Request) (*http.Response, error) {
	m.calls++

	b, err := io.ReadAll(r.Body)
	if err != nil {
		m.t.Fatalf("unable to read mock body: %v", err)
	}
	defer r.Body.Close()

	m.bodies = append(m.bodies, b)

	sc, err := strconv.Atoi(r.URL.String())
	if err != nil {
		m.t.Fatalf("unable to convert url.String() to i in mock: %v", err)
	}

	return &http.Response{StatusCode: sc}, nil
}
