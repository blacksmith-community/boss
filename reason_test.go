package main

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestAReasonThatMayHoldASecretIsWithheld(t *testing.T) {
	for name, in := range map[string]string{
		"a spaced sign":                "PASSWORD = hunter2",
		"a colon":                      "password: hunter2",
		"a JSON pair":                  `{"password":"hunter2"}`,
		"a quoted value":               "secret='two words'",
		"a percent-encoded key":        "pass%77ord=hunter2",
		"a percent-encoded first byte": "%70assword=hunter2",
		"pw":                           "the pw is hunter2",
		"pwd":                          "the PWD is hunter2",
		"auth":                         "auth failed for hunter2",
		"a credential":                 "bad credential hunter2",
		"a cookie":                     "Cookie sessionid hunter2",
		"an Authorization header":      "Authorization: Bearer hunter2",
		"a bearer word":                "use the bearer hunter2",
		"a split key":                  "pass\u200bword=hunter2",
		"a key split by a control":     "pa\x00ssword hunter2",
		"a split word with no sign":    "to\u200bken hunter2",
		"a value with a semicolon":     "token=abc;def",
		"a value with commas":          "token=a,b,c",
		"a value with a parenthesis":   "secret=(paren)x",
		"a URL":                        "see https://host.example/p",
		"userinfo":                     "sent to alice:hunter2@host.example",
		"a network path":               "see //alice:hunter2@host.example/p",
		"an at sign":                   "mail alice@host.example",
		"a percent sign":               "100% sure",
		"a quote":                      `he said "hunter2"`,
		"a brace":                      "{hunter2}",
		"a key name":                   "the key is hunter2",
		"an upper case key":            "API_KEY hunter2",
	} {
		shown, withheld := reasonText(in)
		if !withheld || shown != reasonWithheld {
			t.Errorf("%s: got %q (withheld %v), want the fixed sentence", name, shown, withheld)
		}
		if strings.Contains(shown, "hunter2") {
			t.Errorf("%s: %q leaks the secret", name, shown)
		}
	}
}

func TestAReasonWithALookalikeLetterIsWithheld(t *testing.T) {
	for name, in := range map[string]string{
		"fullwidth letters":         "\uff54\uff4f\uff4b\uff45\uff4e hunter2",
		"a Cyrillic a":              "p\u0430ss hunter2",
		"a long s":                  "\u017fecret hunter2",
		"math bold letters":         "\U0001d429\U0001d41a\U0001d42c\U0001d42c hunter2",
		"an accented letter":        "caf\u00e9",
		"a Braille blank split":     "pa\u2800ss hunter2",
		"a Hangul filler split":     "pa\u3164ss hunter2",
		"a Braille blank in a word": "tok\u2800en hunter2",
		"an acute accent split":     "pa\u0301ss hunter2",
		"an enclosing mark split":   "pa\u20ddss hunter2",
		"a variation selector":      "pa\ufe0fss hunter2",
		"a grapheme joiner":         "pa\u034fss hunter2",
		"a double breve split":      "pa\u0361ss hunter2",
	} {
		shown, withheld := reasonText(in)
		if !withheld || shown != reasonWithheld {
			t.Errorf("%s: got %q (withheld %v)", name, shown, withheld)
		}
	}
}

func TestEachMarkerAloneWithholdsAReason(t *testing.T) {
	for name, in := range map[string]string{
		"the word secret": "the secret is out",
		"an equals sign":  "size=10",
		"two slashes":     "go to //host/p",
		"an at sign":      "mail me@example",
		"a percent sign":  "a 50% chance",
		"a quote":         `a "b"`,
		"a brace":         "a {b}",
	} {
		if _, withheld := reasonText(in); !withheld {
			t.Errorf("%s: %q was shown", name, in)
		}
	}
}

func TestAnOrdinaryReasonIsShownInFull(t *testing.T) {
	in := "deployment fixture-dep is not a deployment of one of this broker's service instances, so the broker will not change it"
	shown, withheld := reasonText(in)
	if withheld || shown != in {
		t.Errorf("got %q (withheld %v)", shown, withheld)
	}
}

func TestTheRealBrokerReasonIsShownInFull(t *testing.T) {
	var body struct {
		Error string `json:"error"`
	}
	if err := json.Unmarshal([]byte(fixture(t, "deployment_not_managed.json")), &body); err != nil {
		t.Fatal(err)
	}
	shown, withheld := reasonText(body.Error)
	if withheld || shown != body.Error {
		t.Errorf("the broker's own reason was withheld: %q", body.Error)
	}
}

func TestTheWithheldSentenceReachesTheOperator(t *testing.T) {
	got := NotManagedError{Deployment: "d", Message: "token=abc;def"}.Error()
	if !strings.Contains(got, reasonWithheld) || strings.Contains(got, "abc") || strings.Contains(got, "def") {
		t.Errorf("got %q", got)
	}
	if want := "The broker gave a reason that may hold a secret, so Boss does not show it."; reasonWithheld != want {
		t.Errorf("the sentence is %q, want %q", reasonWithheld, want)
	}
}

func TestTheReasonReplacesFormatCharactersAndIsCapped(t *testing.T) {
	got, _ := reasonText("a\u202eb\u200bc")
	if strings.ContainsAny(got, "\u202e\u200b") || got != "a b c" {
		t.Errorf("got %q", got)
	}

	long, _ := reasonText(strings.Repeat("x", 600))
	if n := len([]rune(long)); n != 513 || !strings.HasSuffix(long, "…") {
		t.Errorf("got %d runes ending %q, want 512 and an ellipsis", n, long[len(long)-4:])
	}
	if short, _ := reasonText(strings.Repeat("y", 512)); strings.Contains(short, "…") {
		t.Error("a reason of exactly 512 runes must not be cut")
	}
}

func TestTheReasonJoinsItsWordsWithSingleSpaces(t *testing.T) {
	got, _ := reasonText("  one\n\ttwo   three \r\n")
	if got != "one two three" {
		t.Errorf("got %q", got)
	}
}

func TestScrubURLDropsPathParameters(t *testing.T) {
	if got, want := scrubURL("http://host/a;jsessionid=s3cret/b;x=1?q=1"), "http://host/a/b"; got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestAFixedBrokerReasonIsShownOnlyWhenItMatchesExactly(t *testing.T) {
	for _, in := range []string{
		"unauthorized log file access",
		"credentials not found",
		"vault_max_versions must be between 0 and 1000 (0 = unlimited)",
		"  credentials not found\n",
	} {
		if shown, withheld := reasonText(in); withheld || shown != strings.TrimSpace(in) {
			t.Errorf("%q: got %q (withheld %v)", in, shown, withheld)
		}
	}
	for name, in := range map[string]string{
		"an extra word":     "unauthorized log file access now",
		"a leading word":    "the unauthorized log file access",
		"another case":      "Unauthorized log file access",
		"an extra marker":   "credentials not found=1",
		"a split character": "credentials not\u200b found",
		"a prefix":          "credentials not",
	} {
		if shown, withheld := reasonText(in); !withheld || shown != reasonWithheld {
			t.Errorf("%s: %q was shown as %q", name, in, shown)
		}
	}
}
