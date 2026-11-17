package main

import (
	"errors"
	"net"
	"net/http"
	"net/url"
	"strings"
	"testing"
)

func TestARedirectErrorHidesTheUserinfoAndQueryOfTheTarget(t *testing.T) {
	b := redirecting(t, "GET /v2/catalog", 302, "http://alice:hunter2@other.example/v2/catalog?token=s3cret&x=1")

	_, err := b.client().Catalog()
	if err == nil || !strings.Contains(err.Error(), "other.example/v2/catalog") {
		t.Fatalf("want the refusal to name the target host and path, got %v", err)
	}
	for _, leak := range []string{"alice", "hunter2", "s3cret", "token", "x=1"} {
		if strings.Contains(err.Error(), leak) {
			t.Errorf("the error leaks %q: %v", leak, err)
		}
	}
}

func TestAFailedRequestHidesTheUserinfoAndQueryOfItsURL(t *testing.T) {
	b := newBroker(t, nil)
	b.srv.Config.Handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, _, _ := w.(http.Hijacker).Hijack()
		_ = conn.Close()
	})
	c := b.client()
	c.URL = strings.Replace(c.URL, "http://", "http://alice:hunter2@", 1)

	_, err := c.LastOperation(fixtureID, OperationRef{Operation: "op-secret-token", PlanID: "p", ServiceID: "s"})
	if err == nil {
		t.Fatal("want an error")
	}
	for _, leak := range []string{"alice", "hunter2", "op-secret-token", "operation="} {
		if strings.Contains(err.Error(), leak) {
			t.Errorf("the error leaks %q: %v", leak, err)
		}
	}
}

func TestABrokerURLThatWillNotParseNeverShowsItsPassword(t *testing.T) {
	for _, bad := range []string{
		"http://bob:SECRETPW@host:badport",
		"http://bob:SECRETPW@host:80/a%zz",
		"http://bob:SECRETPW@[::1",
	} {
		c := &Client{URL: bad, MaxRetries: -1}
		for name, call := range map[string]func() error{
			"read":  func() error { _, err := c.Catalog(); return err },
			"write": func() error { _, err := c.Redeploy(fixtureID, false); return err },
		} {
			err := call()
			if err == nil {
				t.Fatalf("%s %s: want an error", bad, name)
			}
			for _, leak := range []string{"SECRETPW", "bob"} {
				if strings.Contains(err.Error(), leak) {
					t.Errorf("%s %s: the error leaks %q: %v", bad, name, leak, err)
				}
			}
		}
	}
}

func TestScrubURLDropsEveryPartThatCanCarryASecret(t *testing.T) {
	for in, want := range map[string]string{
		"http://u:p@host:8080/a/b?q=1#frag": "http://host:8080/a/b",
		"http://host/a#only-a-fragment":     "http://host/a",
		"http://host/a?":                    "http://host/a",
		"http://host/a":                     "http://host/a",
	} {
		if got := scrubURL(in); got != want {
			t.Errorf("%s: got %q, want %q", in, got, want)
		}
	}
}

func TestARefusedRedirectIsNotRetriedEvenWhenAnotherCauseIsRetryable(t *testing.T) {
	refusal := RedirectError{Method: "GET", Target: "http://other.example/", Reason: "it changes the scheme or the host"}
	err := errors.Join(refusal, &net.OpError{Op: "read", Err: timeoutError{}})

	for _, method := range []string{"GET", "HEAD", "POST"} {
		if (Client{}).shouldRetry(method, err) {
			t.Errorf("%s: a refused redirect was set to be retried", method)
		}
	}
	if !(Client{}).shouldRetry("GET", &net.OpError{Op: "read", Err: timeoutError{}}) {
		t.Error("the retryable cause alone should be retried, or this test proves nothing")
	}
}

func TestAParseFailureNeverQuotesAnyPartOfTheURL(t *testing.T) {
	for _, bad := range []string{
		"http://bob:SEC%zzRET@host/",
		"http://bob:SECRETPW@host:badport",
		"http://bob:SECRETPW@[::1",
		"http://bob:SECRETPW@ho st/",
		"http://bob:SEC\x7fRET@host/",
		"http://bo b:SECRETPW@host/",
	} {
		c := &Client{URL: bad, MaxRetries: -1}
		for name, call := range map[string]func() error{
			"read":  func() error { _, err := c.Catalog(); return err },
			"write": func() error { _, err := c.Redeploy(fixtureID, false); return err },
		} {
			err := call()
			if err == nil {
				t.Fatalf("%q %s: want an error", bad, name)
			}
			for _, leak := range []string{"SECRETPW", "SEC", "RET", "bob", "%zz", "badport", "ho st", "bo b", "\x7f"} {
				if strings.Contains(err.Error(), leak) {
					t.Errorf("%q %s: the error leaks %q: %v", bad, name, leak, err)
				}
			}
		}
	}
}

func TestParseReasonNamesTheKindOfFailureWithoutQuotingInput(t *testing.T) {
	for in, want := range map[string]string{
		"http://h:badport/": "its port is not valid",
		"http://h/a%zz":     "it has a bad percent escape",
		"http://[::1/":      "its host is not valid",
		"http://h o/":       "its host is not valid",
		"http://h/\x7f":     "it has an invalid character",
		"http://a b:c@h/":   "its user information is not valid",
	} {
		_, err := url.Parse(in)
		if err == nil {
			t.Fatalf("%q parsed", in)
		}
		if got := parseReason(err); got != want {
			t.Errorf("%q: got %q, want %q", in, got, want)
		}
	}
}

func TestParseReasonFallsBackToAFixedPhrase(t *testing.T) {
	for _, err := range []error{
		errors.New("something nobody planned for: quoting SECRETPW"),
		&url.Error{Op: "parse", URL: "http://bob:SECRETPW@h", Err: errors.New("a new kind of failure with SECRETPW")},
	} {
		if got := parseReason(err); got != "it is not valid" {
			t.Errorf("got %q, want the fallback", got)
		}
	}
}

func TestABrokerURLWithoutASchemeOrHostIsRefusedBeforeAnyRequest(t *testing.T) {
	for _, bad := range []string{
		"bob:SECRETPW@h",
		"bob:SECRETPW@h:8080",
		"//bob:SECRETPW@h",
		"ftp://bob:SECRETPW@h/",
		"http://",
		"https:///path",
		"",
		"host.example",
	} {
		c := &Client{URL: bad, MaxRetries: -1}
		for name, call := range map[string]func() error{
			"read":  func() error { _, err := c.Catalog(); return err },
			"write": func() error { _, err := c.Redeploy(fixtureID, false); return err },
		} {
			err := call()
			if err == nil {
				t.Fatalf("%q %s: want an error", bad, name)
			}
			if !strings.Contains(err.Error(), "broker URL") {
				t.Errorf("%q %s: %v does not say what is wrong", bad, name, err)
			}
			for _, leak := range []string{"SECRETPW", "bob", "host.example"} {
				if strings.Contains(err.Error(), leak) {
					t.Errorf("%q %s: the error leaks %q: %v", bad, name, leak, err)
				}
			}
		}
	}
}

func TestCheckBrokerURLAcceptsHTTPAndHTTPS(t *testing.T) {
	for _, ok := range []string{"http://h", "https://h:8080/", "HTTPS://user:pw@h/p"} {
		if err := checkBrokerURL(ok); err != nil {
			t.Errorf("%q: %v", ok, err)
		}
	}
}
