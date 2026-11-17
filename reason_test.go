package main

import (
	"strings"
	"testing"
)

func TestTheReasonHidesSecretsInEveryShapeItCanTake(t *testing.T) {
	for name, tc := range map[string]struct {
		in    string
		leaks []string
		keeps string
	}{
		"userinfo without a scheme":  {"sent to alice:hunter2@host.example now", []string{"alice", "hunter2"}, "host.example"},
		"userinfo in a network path": {"see //alice:hunter2@host.example/p", []string{"alice", "hunter2"}, "host.example/p"},
		"a path parameter":           {"see https://host.example/a;token=s3cret/b", []string{"s3cret"}, "host.example/a"},
		"a password pair":            {"login password=hunter2 failed", []string{"hunter2"}, "password="},
		"a mixed case token pair":    {"bad Access_Token=s3cret here", []string{"s3cret"}, "Access_Token="},
		"an api key pair":            {"api_key=s3cret&x=1", []string{"s3cret"}, "api_key="},
		"a passwd pair":              {"passwd=s3cret", []string{"s3cret"}, "passwd="},
		"a secret pair":              {"client_secret=s3cret", []string{"s3cret"}, "client_secret="},
		"a pair after a semicolon":   {"a;key=s3cret", []string{"s3cret"}, "key="},
	} {
		got := plainText(tc.in)
		for _, leak := range tc.leaks {
			if strings.Contains(got, leak) {
				t.Errorf("%s: %q leaks %q", name, got, leak)
			}
		}
		if !strings.Contains(got, tc.keeps) {
			t.Errorf("%s: %q lost %q", name, got, tc.keeps)
		}
	}
}

func TestTheReasonKeepsPunctuationAfterAURL(t *testing.T) {
	got := plainText("(see https://host.example/a?x=1), then https://host.example/b.")
	want := "(see https://host.example/a), then https://host.example/b."
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestTheReasonLeavesOrdinaryTextAlone(t *testing.T) {
	in := "deployment fixture-dep is not one of this broker's service instances, so the broker will not change it"
	if got := plainText(in); got != in {
		t.Errorf("got %q", got)
	}
}

func TestTheReasonReplacesFormatCharactersAndIsCapped(t *testing.T) {
	got := plainText("a\u202eb\u200bc")
	if strings.ContainsAny(got, "\u202e\u200b") || got != "a b c" {
		t.Errorf("got %q", got)
	}

	long := plainText(strings.Repeat("x", 600))
	if n := len([]rune(long)); n != 513 || !strings.HasSuffix(long, "…") {
		t.Errorf("got %d runes ending %q, want 512 and an ellipsis", n, long[len(long)-4:])
	}
	if short := plainText(strings.Repeat("y", 512)); strings.Contains(short, "…") {
		t.Error("a reason of exactly 512 runes must not be cut")
	}
}

func TestTheReasonJoinsItsWordsWithSingleSpaces(t *testing.T) {
	got := plainText("  one\n\ttwo   three \r\n")
	if got != "one two three" {
		t.Errorf("got %q", got)
	}
}

func TestScrubURLDropsPathParameters(t *testing.T) {
	if got, want := scrubURL("http://host/a;jsessionid=s3cret/b;x=1?q=1"), "http://host/a/b"; got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}
