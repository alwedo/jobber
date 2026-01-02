package retryhttp

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"net/http"
	"time"
)

const maxRetries = 5 // Exponential backoff limit.

var ErrRetryable = errors.New("scrape: retryable error")

var isRetryable = map[int]bool{
	http.StatusRequestTimeout:      true,
	http.StatusTooEarly:            true,
	http.StatusTooManyRequests:     true,
	http.StatusInternalServerError: true,
	http.StatusBadGateway:          true,
	http.StatusServiceUnavailable:  true,
	http.StatusGatewayTimeout:      true,
}

type Client struct {
	client *http.Client
}

func New() *Client {
	return &Client{http.DefaultClient}
}

func NewWithTransport(rt http.RoundTripper) *Client {
	return &Client{&http.Client{Transport: rt}}
}

// Do executes the HTTP request with retry logic for retryable status codes.
// This implementation buffers and resets the body for each retry if req.Body is non-nil.
func (c *Client) Do(req *http.Request) (*http.Response, error) {
	var (
		bodyBytes []byte
		retries   int
		resp      *http.Response
		err       error
	)

	// Buffer the body for retries.
	if req.Body != nil {
		bodyBytes, err := io.ReadAll(req.Body)
		req.Body.Close()
		if err != nil {
			return nil, fmt.Errorf("failed to read request body for retries in retryhttp.Do: %w", err)
		}
		// Reset for the first attempt
		req.Body = io.NopCloser(bytes.NewReader(bodyBytes))
	}

	for {
		if err := req.Context().Err(); err != nil {
			return nil, err
		}
		resp, err = c.client.Do(req)
		if err != nil {
			return nil, fmt.Errorf("failed to perform http request in retryhttp.Do: %w", err)
		}
		if resp.StatusCode != http.StatusOK {
			if isRetryable[resp.StatusCode] {
				resp.Body.Close()
				if retries >= maxRetries {
					return nil, fmt.Errorf("%w after %d retries in retryhttp.Do", ErrRetryable, retries)
				}
				retries++
				time.Sleep(time.Second << retries)
				// Reset the body for the next try:
				if len(bodyBytes) > 0 {
					req.Body = io.NopCloser(bytes.NewReader(bodyBytes))
				}
				continue
			}
			body, readErr := io.ReadAll(resp.Body)
			resp.Body.Close()
			if readErr != nil {
				return nil, fmt.Errorf("unable to read response body after error in retryhttp.Do: %w", readErr)
			}
			return nil, fmt.Errorf("retryhttp.Do received status code: %d, url: %s, message: %s", resp.StatusCode, req.URL.String(), string(body))
		}
		break
	}

	return resp, nil
}
