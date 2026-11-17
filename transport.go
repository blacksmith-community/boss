package main

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// commandKeyHeader carries the second credential a broker asks for before
// it shows or resumes a batch command job.
const commandKeyHeader = "X-Blacksmith-Command-Key"

// redacted stands in for anything a log line must not carry.
const redacted = "[redacted]"

// httpClient builds the HTTP client for one request.
func (c Client) httpClient() *http.Client {
	timeout := c.Timeout
	if timeout == 0 {
		timeout = 30 * time.Second
	}

	return &http.Client{
		Timeout: timeout,
		Transport: &http.Transport{
			TLSClientConfig: &tls.Config{
				// #nosec G402 -- InsecureSkipVerify is a configurable option for development environments
				// and connecting to services with self-signed certificates. User must explicitly enable it.
				InsecureSkipVerify: c.InsecureSkipVerify,
			},
			Proxy: http.ProxyFromEnvironment,
		},
	}
}

// buildURL joins the broker's base URL with a path that may carry a query.
func (c Client) buildURL(path string) (string, error) {
	u, err := url.Parse(strings.TrimSuffix(c.URL, "/"))
	if err != nil {
		return "", fmt.Errorf("invalid URL %s: %w", c.URL, err)
	}

	rel, err := url.Parse(path)
	if err != nil {
		return "", fmt.Errorf("invalid path %s: %w", path, err)
	}

	u.Path = rel.Path
	u.RawPath = rel.RawPath
	u.RawQuery = rel.RawQuery

	return u.String(), nil
}

// isSensitivePath says whether a response from the path can hold
// credentials, so that no log shows its body.
func isSensitivePath(path string) bool {
	p := path
	if i := strings.IndexByte(p, '?'); i >= 0 {
		p = p[:i]
	}

	return strings.HasSuffix(p, "/credentials") ||
		strings.HasSuffix(p, "/manifest") ||
		strings.HasSuffix(p, "/manifest-details") ||
		strings.HasPrefix(p, "/b/upgrade/tasks/") ||
		(strings.HasPrefix(p, "/b/tasks/") && strings.HasSuffix(p, "/output")) ||
		strings.Contains(p, "/errands/")
}

// requestLogBody gives the text a debug line may show for a request body.
// Provisioning and update bodies carry the operator's parameters, which can
// hold secrets, so they show their size only.
func requestLogBody(path string, b []byte) string {
	if strings.HasPrefix(path, "/v2/service_instances/") || isSensitivePath(path) {
		return fmt.Sprintf("%s (%d bytes)", redacted, len(b))
	}

	return string(b)
}

// responseLogBody gives the text a debug line may show for a response body.
func responseLogBody(path string, b []byte) string {
	if isSensitivePath(path) {
		return fmt.Sprintf("%s (%d bytes)", redacted, len(b))
	}

	return string(b)
}

// traceHeaders writes headers to w, hiding the ones that carry credentials.
func traceHeaders(w io.Writer, h http.Header) {
	for name, values := range h {
		lower := strings.ToLower(name)
		for _, v := range values {
			if lower == "authorization" || lower == strings.ToLower(commandKeyHeader) {
				v = redacted
			}
			_, _ = fmt.Fprintf(w, "%s: %s\n", name, v)
		}
	}
}

func (c Client) do(method, path string, in interface{}) (*http.Response, error) {
	return c.doWithRetry(method, path, in)
}

// idempotent says whether a method may be sent again after the broker
// answered or the connection broke midway.
func idempotent(method string) bool {
	return method == http.MethodGet || method == http.MethodHead
}

// backoff is the pause before the next attempt.
func (c Client) backoff(attempt int) time.Duration {
	unit := c.Backoff
	if unit == 0 {
		unit = time.Second
	}
	return time.Duration(attempt*attempt) * unit
}

// doWithRetry performs the request with retry logic. A GET or HEAD is sent
// again after a 5xx answer or a transient error. Any other method is sent
// again only when the connection was refused, because then no byte reached
// the broker. A POST, PATCH, PUT, or DELETE that may have arrived is never
// sent twice.
func (c Client) doWithRetry(method, path string, in interface{}) (*http.Response, error) {
	maxRetries := c.MaxRetries
	if maxRetries == 0 {
		maxRetries = 3
	}
	if maxRetries < 0 {
		maxRetries = 0
	}

	var lastErr error
	for attempts := 1; ; attempts++ {
		res, err := c.doSingle(method, path, in)
		if err == nil {
			if res.StatusCode < 500 || !idempotent(method) || attempts > maxRetries {
				return res, nil
			}

			_, _ = io.Copy(io.Discard, res.Body)
			_ = res.Body.Close()
			lastErr = fmt.Errorf("the broker answered %d", res.StatusCode)
		} else {
			lastErr = err
			if !c.shouldRetry(method, err) || attempts > maxRetries {
				if attempts == 1 {
					return nil, err
				}
				return nil, fmt.Errorf("request failed after %d attempts: %w", attempts, err)
			}
		}

		pause := c.backoff(attempts)
		if c.Debug {
			_, _ = fmt.Fprintf(c.stderr(), "Retrying request after %s (attempt %d/%d): %v\n", pause, attempts+1, maxRetries+1, lastErr)
		}

		select {
		case <-c.context().Done():
			return nil, c.context().Err()
		case <-time.After(pause):
		}
	}
}

// refused says whether the error is a failure to connect, which happens
// before any byte of the request leaves.
func refused(err error) bool {
	var op *net.OpError
	if errors.As(err, &op) && op.Op == "dial" {
		return true
	}
	return strings.Contains(err.Error(), "connection refused")
}

// shouldRetry determines if an error is retryable
func (c Client) shouldRetry(method string, err error) bool {
	if errors.Is(err, context.Canceled) {
		return false
	}

	if refused(err) {
		return true
	}

	if !idempotent(method) {
		return false
	}

	var ne net.Error
	if errors.As(err, &ne) && ne.Timeout() {
		return true
	}

	return strings.Contains(err.Error(), "timeout") ||
		strings.Contains(err.Error(), "temporary failure")
}

// doSingle performs a single HTTP request
func (c Client) doSingle(method, path string, in interface{}) (*http.Response, error) {
	var body io.Reader
	if in != nil {
		b, err := json.Marshal(in)
		if err != nil {
			return nil, fmt.Errorf("failed to marshal request: %w", err)
		}

		body = bytes.NewBuffer(b)
		if c.Debug {
			_, _ = fmt.Fprintf(c.stderr(), "REQUEST: %s %s\n", method, c.URL+path)
			_, _ = fmt.Fprintf(c.stderr(), "BODY: %s\n", requestLogBody(path, b))
		}
	} else if c.Debug {
		_, _ = fmt.Fprintf(c.stderr(), "REQUEST: %s %s\n", method, c.URL+path)
	}

	target, err := c.buildURL(path)
	if err != nil {
		return nil, err
	}

	req, err := http.NewRequestWithContext(c.context(), method, target, body)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	// Set API version header for v2 endpoints
	if strings.HasPrefix(path, "/v2/") {
		version := c.BrokerAPIVersion
		if version == "" {
			version = "2.16"
		}
		req.Header.Set("X-Broker-API-Version", version)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	req.SetBasicAuth(c.Username, c.Password)
	if c.CommandKey != "" {
		req.Header.Set(commandKeyHeader, c.CommandKey)
	}

	if c.Trace {
		var buf bytes.Buffer
		fmt.Fprintf(&buf, "%s %s\n", req.Method, req.URL.RequestURI())
		traceHeaders(&buf, req.Header)
		_, _ = fmt.Fprintf(c.stderr(), "=================================\n%s\n", buf.String())
	}

	res, err := c.httpClient().Do(req)
	if err != nil {
		return nil, fmt.Errorf("HTTP request failed: %w", err)
	}

	if c.Trace {
		var buf bytes.Buffer
		fmt.Fprintf(&buf, "%s\n", res.Status)
		traceHeaders(&buf, res.Header)
		_, _ = fmt.Fprintf(c.stderr(), "=================================\n%s\n", buf.String())
	}

	return res, nil
}

// rawRequest sends the request and returns the status and the body, with
// an APIError for any answer of 400 or higher.
func (c Client) rawRequest(method, path string, in interface{}) (int, []byte, error) {
	res, err := c.do(method, path, in)
	if err != nil {
		return 0, nil, fmt.Errorf("request failed: %w", err)
	}

	defer func() { _ = res.Body.Close() }()

	b, err := io.ReadAll(res.Body)
	if err != nil {
		return res.StatusCode, nil, fmt.Errorf("failed to read response: %w", err)
	}

	if c.Debug {
		_, _ = fmt.Fprintf(c.stderr(), "RESPONSE: %d\n", res.StatusCode)
		_, _ = fmt.Fprintf(c.stderr(), "BODY: %s\n", responseLogBody(path, b))
	}

	if method == http.MethodDelete && res.StatusCode == 410 {
		/* this is okay - already deleted */
		return res.StatusCode, b, nil
	}

	// Check for error response
	if res.StatusCode >= 400 {
		var apiErr APIError
		if json.Unmarshal(b, &apiErr) == nil && apiErr.Code != "" {
			apiErr.Status = res.StatusCode
			return res.StatusCode, b, apiErr
		}

		// Fallback for non-JSON error responses
		apiErr = APIError{
			Status:      res.StatusCode,
			Code:        "HTTPError",
			Description: fmt.Sprintf("HTTP %d: %s", res.StatusCode, res.Status),
		}
		if len(b) > 0 && !isSensitivePath(path) {
			apiErr.Description += " - " + strings.TrimSpace(string(b))
		}
		return res.StatusCode, b, apiErr
	}

	if res.StatusCode < 200 || res.StatusCode > 299 {
		return res.StatusCode, b, fmt.Errorf("unexpected status %d: %s", res.StatusCode, res.Status)
	}

	return res.StatusCode, b, nil
}

func (c Client) request(method, path string, in, out interface{}) (int, error) {
	status, b, err := c.rawRequest(method, path, in)
	if err != nil {
		return status, err
	}

	// Parse response if output provided
	if out != nil && len(b) > 0 {
		if err := json.Unmarshal(b, out); err != nil {
			return status, fmt.Errorf("failed to parse response: %w", err)
		}
	}

	return status, nil
}
