package main

import (
	"errors"
	"strings"
	"testing"
)

const leakyText = "bad token=hunter2"

// leaks reports whether the text shows the secret instead of the fixed
// sentence.
func leaks(s string) bool { return strings.Contains(s, "hunter2") }

func TestServerMessagesPassThroughTheReasonRule(t *testing.T) {
	body := `{"success":false,"message":"` + leakyText + `","error":"` + leakyText + `","text":""}`

	t.Run("resurrection", func(t *testing.T) {
		b := newBroker(t, nil)
		b.route("PUT /b/fixture-id/resurrection", func(int) reply { return reply{Status: 200, Body: body} })
		b.route("DELETE /b/fixture-id/resurrection", func(int) reply { return reply{Status: 200, Body: body} })
		for name, call := range map[string]func() (string, error){
			"set":   func() (string, error) { return b.client().Resurrection("fixture-id", true) },
			"reset": func() (string, error) { return b.client().ResetResurrection("fixture-id") },
		} {
			got, err := call()
			if err != nil || leaks(got) || got != reasonWithheld {
				t.Errorf("%s: got %q, %v", name, got, err)
			}
		}
	})

	t.Run("batch action", func(t *testing.T) {
		b := newBroker(t, nil)
		b.route("POST /b/upgrade/tasks/fixture-job/pause", func(int) reply { return reply{Status: 200, Body: body} })
		got, err := b.client().BatchAction("fixture-job", "pause")
		if err != nil || leaks(got) || got != reasonWithheld {
			t.Errorf("got %q, %v", got, err)
		}
	})

	t.Run("task cancel", func(t *testing.T) {
		b := newBroker(t, nil)
		b.route("POST /b/tasks/7/cancel", func(int) reply { return reply{Status: 200, Body: body} })
		r, out, _ := testRunner(b.client(), "", true)
		if err := r.tasksCancel([]string{"7"}, false); err != nil {
			t.Fatal(err)
		}
		if leaks(out.String()) || !strings.Contains(out.String(), reasonWithheld) {
			t.Errorf("output %q", out)
		}
	})

	t.Run("log and manifest errors", func(t *testing.T) {
		b := newBroker(t, nil)
		b.route("GET /b/blacksmith/logs", func(int) reply { return reply{Status: 200, Body: body} })
		b.route("GET /b/fixture-id/manifest", func(int) reply { return reply{Status: 200, Body: body} })
		_, err := b.client().Log(LogOptions{})
		if err == nil || leaks(err.Error()) {
			t.Errorf("log error %v", err)
		}
		_, err = b.client().Manifest("fixture-id")
		if err == nil || leaks(err.Error()) {
			t.Errorf("manifest error %v", err)
		}
	})

	t.Run("monitor", func(t *testing.T) {
		b := newBroker(t, nil)
		b.route("GET /b/bosh/vm-monitor", func(int) reply {
			return reply{Status: 200, Body: `{"running":false,"error":"` + leakyText + `"}`}
		})
		r, out, _ := testRunner(b.client(), "", true)
		if err := r.monitor(nil); err != nil {
			t.Fatal(err)
		}
		if leaks(out.String()) {
			t.Errorf("output %q", out)
		}
	})
}

func TestAnAPIErrorDescriptionPassesThroughTheReasonRule(t *testing.T) {
	msg := APIError{Code: "bad", Description: leakyText}.Error()
	if leaks(msg) || !strings.Contains(msg, reasonWithheld) {
		t.Errorf("got %q", msg)
	}
	var e error = APIError{Code: "bad", Description: "the plan is unknown", Status: 400}
	var api APIError
	if !errors.As(e, &api) || !strings.Contains(e.Error(), "the plan is unknown") {
		t.Errorf("an ordinary description must show: %q", e)
	}
}

func TestTheAPIErrorCodeIsCheckedLikeTheDescription(t *testing.T) {
	leaky := `bad token=hunter2 at https://u:pw@h.example/p?k=hunter2`
	b := newBroker(t, nil)
	b.route("GET /b/certificates/instances/fixture-id", func(int) reply {
		return reply{Status: 500, Body: `{"success":false,"error":"` + strings.ReplaceAll(leaky, `"`, `\"`) + `"}`}
	})
	_, err := b.client().CertsOfInstance("fixture-id")
	if err == nil || leaks(err.Error()) || strings.Contains(err.Error(), "u:pw@") || !strings.Contains(err.Error(), reasonWithheld) {
		t.Fatalf("got %v", err)
	}
}

func TestAMachineCodeAndTheBrokersFixedMessagesPrintInFull(t *testing.T) {
	for _, code := range []string{
		"credentials_error",
		"batch_credential_required",
		"deployment_not_managed",
		"credentials are not available because Vault is not configured",
		"credentials are not available because Vault did not answer",
		"credentials are not available because nothing is stored for this instance",
		"This broker needs a valid command key in the X-Blacksmith-Command-Key header before it previews or runs a command.",
		"CredHub refused the broker's credentials.",
	} {
		if got := (APIError{Code: code}).Error(); got != code {
			t.Errorf("got %q, want %q", got, code)
		}
	}
	for _, code := range []string{"Token_error", "token_" + strings.Repeat("x", 64), "token_error "} {
		if got := (APIError{Code: code}).Error(); got != reasonWithheld {
			t.Errorf("%q printed as %q", code, got)
		}
	}
}

func TestABadFilterNamesTheFilterTheOperatorPassed(t *testing.T) {
	b := newBroker(t, nil)
	b.status("GET /b/certificates/inventory", 400, "cert_bad_filter.json")
	_, err := b.client().CertInventory(CertFilter{"expiry": "soon"})
	if err == nil {
		t.Fatal("no error")
	}
	for _, want := range []string{"invalid_filter", `expiry "soon"`} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("%q lacks %q", err, want)
		}
	}
	if strings.Contains(err.Error(), "use one of") {
		t.Errorf("the broker's text was shown: %q", err)
	}
	var api APIError
	if !errors.As(err, &api) || api.Status != 400 {
		t.Errorf("the API error is lost: %v", err)
	}
}
