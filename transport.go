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
	"net/http/httptrace"
	"net/url"
	"os"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"
	"unicode"
)

// commandKeyHeader carries the second credential a broker asks for before
// it shows or resumes a batch command job.
const commandKeyHeader = "X-Blacksmith-Command-Key"

// redacted stands in for anything a log line must not carry.
const redacted = "[redacted]"

// Default timeouts. A read gives up quickly. A call that waits for the
// director to finish work gets much longer.
const (
	readTimeout = 30 * time.Second
	longTimeout = 15 * time.Minute
)

// long returns a client for calls that wait on BOSH, such as an errand,
// a restart, or a recreate.
func (c Client) long() Client {
	c.longCall = true
	return c
}

// requestTimeout is how long one request may take. An explicit Timeout
// wins for every call.
func (c Client) requestTimeout() time.Duration {
	switch {
	case c.Timeout > 0:
		return c.Timeout
	case c.longCall:
		return longTimeout
	}
	return readTimeout
}

// parseTimeout reads a --timeout value as a duration such as 90s or 5m,
// or as a bare number of seconds. An empty value means no override.
func parseTimeout(s string) (time.Duration, error) {
	if s == "" {
		return 0, nil
	}

	d, err := time.ParseDuration(s)
	if err != nil {
		n, nerr := strconv.Atoi(s)
		if nerr != nil {
			return 0, fmt.Errorf("invalid timeout %q; use a duration such as 90s or 5m", s)
		}
		d = time.Duration(n) * time.Second
	}
	if d <= 0 {
		return 0, fmt.Errorf("invalid timeout %q; it must be positive", s)
	}
	return d, nil
}

// UnknownOutcomeError says a request that changes things timed out, so
// the action may or may not have started.
type UnknownOutcomeError struct {
	Method string
	Cause  error
}

func (e UnknownOutcomeError) Error() string {
	return fmt.Sprintf("The %s request was sent but no complete answer came back, so its outcome is unknown and the action may still be running, and we suggest running boss tasks list before running it again.", e.Method)
}

// NotSentError marks a failure that happened before any connection to the
// broker existed, so no byte of the request left this machine.
type NotSentError struct{ Cause error }

func (e NotSentError) Error() string {
	return "nothing was sent to the broker: " + e.Cause.Error()
}

func (e NotSentError) Unwrap() error { return e.Cause }

func (e UnknownOutcomeError) Unwrap() error { return e.Cause }

// transports holds the one Transport each TLS setting needs. Every
// request of the process shares them, so a long follow reuses its
// connections instead of leaving a new one open each time.
var transports sync.Map

// transport returns the shared Transport for the client's TLS setting.
// Boss never sets an Idempotency-Key header, so Go never sends a POST,
// PATCH, or DELETE again on its own after a reused connection breaks.
func (c Client) transport() *http.Transport {
	if t, ok := transports.Load(c.InsecureSkipVerify); ok {
		return t.(*http.Transport)
	}

	t := &http.Transport{
		TLSClientConfig: &tls.Config{
			// #nosec G402 -- InsecureSkipVerify is a configurable option for development environments
			// and connecting to services with self-signed certificates. User must explicitly enable it.
			InsecureSkipVerify: c.InsecureSkipVerify,
		},
		Proxy:           http.ProxyFromEnvironment,
		IdleConnTimeout: 90 * time.Second,
	}
	actual, _ := transports.LoadOrStore(c.InsecureSkipVerify, t)
	return actual.(*http.Transport)
}

// RedirectError says why Boss did not follow a redirect.
type RedirectError struct {
	Method string
	Target string
	Reason string
}

func (e RedirectError) Error() string {
	return fmt.Sprintf("the broker answered a %s request with a redirect to %s, which Boss does not follow because %s", e.Method, e.Target, e.Reason)
}

// checkRedirect follows a redirect only for a read, and only within the
// same scheme and host, so an action is never sent anywhere the operator
// did not name and the command key never leaves the broker.
func checkRedirect(req *http.Request, via []*http.Request) error {
	first := via[0]
	target := scrubURL(req.URL.String())

	switch {
	case first.Method != http.MethodGet && first.Method != http.MethodHead:
		return RedirectError{Method: first.Method, Target: target, Reason: "an action must not be sent twice or to another place"}
	case req.URL.Scheme != first.URL.Scheme || req.URL.Host != first.URL.Host:
		return RedirectError{Method: first.Method, Target: target, Reason: "it changes the scheme or the host"}
	case len(via) >= 10:
		return RedirectError{Method: first.Method, Target: target, Reason: "there were too many redirects"}
	}
	return nil
}

// httpClient builds the HTTP client for one request.
func (c Client) httpClient() *http.Client {
	var rt http.RoundTripper = c.transport()
	if c.RoundTripper != nil {
		rt = c.RoundTripper
	}
	return &http.Client{
		Timeout:       c.requestTimeout(),
		Transport:     rt,
		CheckRedirect: checkRedirect,
	}
}

// logURL is the broker's base URL with the password of any userinfo
// hidden, for debug output.
func (c Client) logURL() string {
	u, err := url.Parse(c.URL)
	if err != nil {
		return "[unparsable URL]"
	}
	return u.Redacted()
}

// parseReason says what is wrong with a URL that did not parse, in a
// fixed phrase. The error of the standard library quotes the input,
// userinfo and bad escapes included, so its text is only used to pick a
// phrase and never shown.
func parseReason(err error) string {
	var ue *url.Error
	if !errors.As(err, &ue) {
		return "it is not valid"
	}

	cause := ue.Err.Error()
	switch {
	case strings.Contains(cause, "invalid port"):
		return "its port is not valid"
	case strings.Contains(cause, "invalid URL escape"):
		return "it has a bad percent escape"
	case strings.Contains(cause, "invalid userinfo"):
		return "its user information is not valid"
	case strings.Contains(cause, "in host") || strings.Contains(cause, "host name"):
		return "its host is not valid"
	case strings.Contains(cause, "invalid character") || strings.Contains(cause, "control character"):
		return "it has an invalid character"
	}
	return "it is not valid"
}

// checkBrokerURL says whether the broker URL can be used. It needs the
// scheme http or https and a host. The message never repeats the value,
// because the value can hold a password.
func checkBrokerURL(raw string) error {
	u, err := url.Parse(strings.TrimSuffix(raw, "/"))
	if err != nil {
		return fmt.Errorf("the broker URL is not valid, because %s", parseReason(err))
	}

	if scheme := strings.ToLower(u.Scheme); scheme != "http" && scheme != "https" {
		return errors.New("the broker URL is not valid, because it must start with http:// or https://")
	}
	if u.Host == "" {
		return errors.New("the broker URL is not valid, because it names no host")
	}
	return nil
}

// buildURL joins the broker's base URL with a path that may carry a query.
func (c Client) buildURL(path string) (string, error) {
	if err := checkBrokerURL(c.URL); err != nil {
		return "", err
	}
	u, err := url.Parse(strings.TrimSuffix(c.URL, "/"))
	if err != nil {
		return "", err
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
				return nil, fmt.Errorf("gave up after %d attempts: %w", attempts, err)
			}
		}

		pause := c.backoff(attempts)
		if c.Debug {
			_, _ = fmt.Fprintf(c.stderr(), "Retrying request after %s (attempt %d/%d): %v\n", pause, attempts+1, maxRetries+1, lastErr)
		}

		select {
		case <-c.context().Done():
			// A refused request never left, whatever happens to the pause.
			var ns NotSentError
			if errors.As(lastErr, &ns) {
				return nil, NotSentError{Cause: c.context().Err()}
			}
			return nil, c.context().Err()
		case <-time.After(pause):
		}
	}
}

// refused says whether the error is a failure to connect, which happens
// before any byte of the request leaves.
func refused(err error) bool {
	var ns NotSentError
	if errors.As(err, &ns) {
		return true
	}
	var op *net.OpError
	if errors.As(err, &op) && op.Op == "dial" {
		return true
	}
	return errors.Is(err, syscall.ECONNREFUSED)
}

// shouldRetry says whether a failed request may be sent again. It looks
// at the type of the error and never at its text, because the text can
// hold anything a server sent, such as the target of a redirect.
func (c Client) shouldRetry(method string, err error) bool {
	var redirect RedirectError
	if errors.Is(err, context.Canceled) || errors.As(err, &redirect) {
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

	var dns *net.DNSError
	return errors.As(err, &dns) && (dns.IsTemporary || dns.IsTimeout)
}

// doSingle performs a single HTTP request
func (c Client) doSingle(method, path string, in interface{}) (*http.Response, error) {
	var body io.Reader
	if in != nil {
		b, err := json.Marshal(in)
		if err != nil {
			return nil, NotSentError{Cause: fmt.Errorf("failed to marshal request: %w", err)}
		}

		body = bytes.NewBuffer(b)
		if c.Debug {
			_, _ = fmt.Fprintf(c.stderr(), "REQUEST: %s %s\n", method, c.logURL()+path)
			_, _ = fmt.Fprintf(c.stderr(), "BODY: %s\n", requestLogBody(path, b))
		}
	} else if c.Debug {
		_, _ = fmt.Fprintf(c.stderr(), "REQUEST: %s %s\n", method, c.logURL()+path)
	}

	target, err := c.buildURL(path)
	if err != nil {
		return nil, NotSentError{Cause: err}
	}

	req, err := http.NewRequestWithContext(c.context(), method, target, body)
	if err != nil {
		return nil, NotSentError{Cause: fmt.Errorf("failed to create request: %w", err)}
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
	// The key opens only the batch command routes, so no other route, and
	// no log of one, ever sees it.
	if c.CommandKey != "" && strings.HasPrefix(req.URL.Path, "/b/upgrade/") {
		req.Header.Set(commandKeyHeader, c.CommandKey)
	}

	if c.Trace {
		var buf bytes.Buffer
		fmt.Fprintf(&buf, "%s %s\n", req.Method, req.URL.RequestURI())
		traceHeaders(&buf, req.Header)
		_, _ = fmt.Fprintf(c.stderr(), "=================================\n%s\n", buf.String())
	}

	// Until a connection exists, nothing of the request has left.
	var connected atomic.Bool
	req = req.WithContext(httptrace.WithClientTrace(req.Context(), &httptrace.ClientTrace{
		GotConn: func(httptrace.GotConnInfo) { connected.Store(true) },
	}))

	res, err := c.httpClient().Do(req)
	if err != nil {
		var ue *url.Error
		if errors.As(err, &ue) {
			ue.URL = scrubURL(ue.URL)
		}
		if !connected.Load() {
			return nil, NotSentError{Cause: err}
		}
		return nil, err
	}

	if c.Trace {
		var buf bytes.Buffer
		fmt.Fprintf(&buf, "%s\n", res.Status)
		traceHeaders(&buf, res.Header)
		_, _ = fmt.Fprintf(c.stderr(), "=================================\n%s\n", buf.String())
	}

	return res, nil
}

// sentWithoutAnswer says whether a request that changes things may have
// reached the broker and then got no usable answer. Anything that fails
// after a connection exists counts, whatever the cause, because the broker
// may have acted. A failure before a connection, and a refused redirect
// that came from an answer, do not.
func sentWithoutAnswer(method string, err error) bool {
	if idempotent(method) {
		return false
	}
	var ns NotSentError
	var redirect RedirectError
	return !errors.As(err, &ns) && !errors.As(err, &redirect)
}

// rawRequest sends the request and returns the status and the body, with
// an APIError for any answer of 400 or higher.
func (c Client) rawRequest(method, path string, in interface{}) (int, []byte, error) {
	res, err := c.do(method, path, in)
	if err != nil {
		if sentWithoutAnswer(method, err) {
			return 0, nil, UnknownOutcomeError{Method: method, Cause: err}
		}
		return 0, nil, fmt.Errorf("request failed: %w", err)
	}

	defer func() { _ = res.Body.Close() }()

	b, err := io.ReadAll(res.Body)
	if err != nil {
		if !idempotent(method) {
			return res.StatusCode, nil, UnknownOutcomeError{Method: method, Cause: err}
		}
		return res.StatusCode, nil, fmt.Errorf("failed to read response: %w", err)
	}

	if c.Debug {
		_, _ = fmt.Fprintf(c.stderr(), "RESPONSE: %d\n", res.StatusCode)
		_, _ = fmt.Fprintf(c.stderr(), "BODY: %s\n", responseLogBody(path, b))
	}

	// Brokers before the lifecycle routes answer an unknown route with a
	// plain-text 404, which no instance lookup ever produces.
	if res.StatusCode == 404 && isRouteMissing(b) {
		return res.StatusCode, b, BrokerTooOldError{Method: method, Path: path}
	}

	// Check for error response
	if res.StatusCode >= 400 {
		var apiErr APIError
		if json.Unmarshal(b, &apiErr) == nil && apiErr.Code != "" {
			apiErr.Status = res.StatusCode
			if apiErr.BrokerCode == "deployment_not_managed" {
				return res.StatusCode, b, NotManagedError{Deployment: deploymentInPath(path), Message: apiErr.Code}
			}
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

// isRouteMissing reports whether a 404 body is the router's own answer for
// a route the broker does not have.
func isRouteMissing(body []byte) bool {
	switch strings.TrimSpace(string(body)) {
	case "endpoint not found", "upgrade endpoint not found":
		return true
	}
	return false
}

// BrokerTooOldError means the broker has no route for a command.
type BrokerTooOldError struct {
	Method string
	Path   string
}

// routeCommands maps the start of a route to the command that needs it.
var routeCommands = []struct{ prefix, command string }{
	{"/b/tasks", "boss tasks"},
	{"/b/upgrade", "boss batch"},
	{"/b/certificates", "boss certificates"},
	{"/b/deployments", "boss deployment"},
	{"/b/orphans", "boss orphans"},
	{"/b/bosh/vm-monitor", "boss monitor"},
}

func (e BrokerTooOldError) Error() string {
	path := e.Path
	if i := strings.IndexByte(path, '?'); i >= 0 {
		path = path[:i]
	}

	command := "this command"
	for _, rc := range routeCommands {
		if strings.HasPrefix(path, rc.prefix) {
			command = "`" + rc.command + "`"
			break
		}
	}
	if command == "this command" && (strings.HasSuffix(path, "/recreate") || strings.HasSuffix(path, "/restart")) {
		command = "`boss redeploy`"
	}

	return fmt.Sprintf("this broker is too old for %s, because it has no %s %s route; upgrade the broker to use it", command, e.Method, path)
}

// commandKey returns the key a batch command job needs. A key given
// directly wins, and otherwise the key comes from the first line of the
// file, which keeps it out of the shell history and the process list.
func commandKey(key, file string) (string, error) {
	if key != "" || file == "" {
		return key, nil
	}

	b, err := os.ReadFile(file) // #nosec G304 -- the operator names the key file
	if err != nil {
		return "", fmt.Errorf("cannot read the command key file: %w", err)
	}
	line, _, _ := strings.Cut(string(b), "\n")
	return strings.TrimSpace(line), nil
}

// NotManagedError says the broker refused an action on a deployment it
// does not manage. Older brokers never send this code.
type NotManagedError struct {
	Deployment string
	// Message is the text of the broker's error field. It comes from the
	// server, so reasonText cleans it, or withholds it, before anyone sees it.
	Message string
}

func (e NotManagedError) Error() string {
	msg := "the broker does not manage that deployment, so it refused the request"
	if e.Deployment != "" {
		msg = fmt.Sprintf("the broker does not manage deployment %s, so it refused the request", e.Deployment)
	}
	switch why, withheld := reasonText(e.Message); {
	case withheld:
		msg += ". " + why
	case why != "":
		msg += ". The broker said: " + why
	}
	return msg
}

// maxReason is the most runes of a server's reason that an error shows.
const maxReason = 512

// reasonWithheld is what an error says in place of a reason that may hold
// a secret.
const reasonWithheld = "The broker gave a reason that may hold a secret, so Boss does not show it."

// riskyWords are the words that can mark a secret in a reason. A reason
// that holds one anywhere, in any case, is not shown.
var riskyWords = []string{"pass", "pw", "pwd", "secret", "token", "key", "auth", "credential", "cookie", "bearer"}

// reasonText cleans the broker's reason for display. It fails closed.
// Control and format characters become spaces first, and then a reason
// that might hold a secret is withheld entirely, because no pattern can
// list every shape a secret takes. That means any of = @ % " { or //,
// a letter outside ASCII, or any risky word, which over-withholds and is meant to. The second
// result says whether the reason was withheld. A reason that is shown
// is on one line and cut at 512 runes.
func reasonText(s string) (string, bool) {
	spaced := strings.Map(func(r rune) rune {
		if unicode.IsControl(r) || unicode.Is(unicode.Cf, r) {
			return ' '
		}
		return r
	}, s)
	spaced = strings.Join(strings.Fields(spaced), " ")

	// The same text with those characters deleted shows a word that was
	// split in two to get past the check.
	joined := strings.Map(func(r rune) rune {
		if unicode.IsControl(r) || unicode.Is(unicode.Cf, r) || r == '\u2800' || r == '\u3164' {
			return -1
		}
		return r
	}, s)

	for _, text := range []string{spaced, joined} {
		if mayHoldSecret(text) {
			return reasonWithheld, true
		}
	}

	if runes := []rune(spaced); len(runes) > maxReason {
		spaced = string(runes[:maxReason]) + "…"
	}
	return spaced, false
}

func mayHoldSecret(s string) bool {
	if strings.ContainsAny(s, "=@%\"{") || strings.Contains(s, "//") {
		return true
	}
	// A letter outside ASCII can stand in for an ASCII one, as a fullwidth
	// or Cyrillic letter does, so the word check would miss it.
	for _, r := range s {
		if r > unicode.MaxASCII && unicode.IsLetter(r) {
			return true
		}
	}
	lower := strings.ToLower(s)
	for _, word := range riskyWords {
		if strings.Contains(lower, word) {
			return true
		}
	}
	return false
}

// deploymentInPath returns the deployment name from a /b/deployments/NAME/...
// path, or an empty string for any other path.
func deploymentInPath(path string) string {
	rest, ok := strings.CutPrefix(path, "/b/deployments/")
	if !ok {
		return ""
	}
	name, _, _ := strings.Cut(rest, "/")
	name, _, _ = strings.Cut(name, "?")
	if unescaped, err := url.PathUnescape(name); err == nil {
		return unescaped
	}
	return name
}

// scrubURL drops the userinfo, the query, and the fragment from a URL so
// an error never shows a password, a token, or an operation value that
// the URL carried, including one a server put in a Location header. A
// string that does not parse as a URL is not shown at all.
func scrubURL(raw string) string {
	u, err := url.Parse(raw)
	if err != nil {
		return "[unparsable URL]"
	}
	u.User = nil
	u.Path = withoutPathParameters(u.Path)
	u.RawPath = ""
	u.RawQuery = ""
	u.ForceQuery = false
	u.Fragment = ""
	return u.String()
}

// withoutPathParameters drops the ;name=value parameters that a path
// segment can carry, because a server may put a session token there.
func withoutPathParameters(path string) string {
	segments := strings.Split(path, "/")
	for i, segment := range segments {
		segments[i], _, _ = strings.Cut(segment, ";")
	}
	return strings.Join(segments, "/")
}
