// Package retryhttp provides an HTTP client wrapper with automatic retries,
// exponential backoff, and optional request mutation.
//
// The client retries requests when the response status code is classified as
// retryable. A default set of retryable HTTP status codes is provided and can
// be extended via options.
//
// If retries are exhausted the client will respond with ErrRetrayble.
package retryhttp

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"net/http"
	"time"

	ua "github.com/lib4u/fake-useragent"
)

const maxRetries = 5 // Exponential backoff limit.

var ErrRetryable = errors.New("too many retries")

type Option func(*Client)

// WithExtraRetryableStatus adds custom retriable status to the pool.
func WithExtraRetryableStatus(status []int) Option {
	return func(c *Client) {
		for _, v := range status {
			c.isRetryable[v] = true
		}
	}
}

// WithRandomUserAgent will add a random User-Agent header for each http call.
func WithRandomUserAgent() Option {
	return func(c *Client) {
		u, err := ua.New()
		if err != nil {
			panic(err) // TODO: refactor to not panic
		}
		c.ua = u
	}
}

// WithTransport overwrites the http client
// with a custom RoundTripper for testing.
func WithTransport(rt http.RoundTripper) Option {
	return func(c *Client) {
		c.client.Transport = rt
	}
}

type Client struct {
	client      *http.Client
	isRetryable map[int]bool
	ua          *ua.UserAgent
}

func New(opts ...Option) *Client {
	c := &Client{
		client: &http.Client{},
		isRetryable: map[int]bool{
			http.StatusRequestTimeout:      true,
			http.StatusTooEarly:            true,
			http.StatusTooManyRequests:     true,
			http.StatusInternalServerError: true,
			http.StatusBadGateway:          true,
			http.StatusServiceUnavailable:  true,
			http.StatusGatewayTimeout:      true,
		},
	}
	for _, o := range opts {
		o(c)
	}
	return c
}

// Do executes the HTTP request with retry logic for retryable status codes.
// This implementation buffers and resets the body for each retry if req.Body is non-nil.
func (c *Client) Do(req *http.Request) (*http.Response, error) {
	// Buffer the body for retries.
	if req.Body != nil {
		bodyBytes, err := io.ReadAll(req.Body)
		req.Body.Close()
		if err != nil {
			return nil, fmt.Errorf("failed to read request body for retries in retryhttp.Do: %w", err)
		}

		req.GetBody = func() (io.ReadCloser, error) {
			return io.NopCloser(bytes.NewReader(bodyBytes)), nil
		}

		// Reset for the first attempt
		req.Body, err = req.GetBody()
		if err != nil {
			return nil, fmt.Errorf("failed to re-read request body in req.GetBody: %w", err)
		}
	}

	var retries int
	for {
		if c.ua != nil {
			req.Header.Set("User-Agent", c.ua.GetRandom())
		}

		resp, err := c.client.Do(req)
		if err != nil {
			return nil, fmt.Errorf("failed to perform http request in retryhttp.Do: %w", err)
		}

		if c.isRetryable[resp.StatusCode] {
			if retries >= maxRetries {
				return resp, fmt.Errorf("%w with status code %d", ErrRetryable, resp.StatusCode)
			}
			resp.Body.Close()
			retries++

			// While waiting for the next try we also listen for ctx cancellation.
			t := time.NewTimer(time.Second << retries)
			select {
			case <-t.C:
				// Reset the body and retry after the delay.
				if req.Body != nil {
					req.Body, err = req.GetBody()
					if err != nil {
						return nil, fmt.Errorf("failed to re-read request body in req.GetBody after a try: %w", err)
					}
				}
				continue
			case <-req.Context().Done():
				return nil, fmt.Errorf("retryhttp.Do ctx cancelled: %w", req.Context().Err())
			}
		}

		return resp, nil
	}
}
