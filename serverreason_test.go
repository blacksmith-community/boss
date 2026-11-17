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

const directorError = "Failed to find variable '/dir/dep/db_password' from config server"

func TestADirectorErrorInATaskPrintsAsSent(t *testing.T) {
	t.Run("task log", func(t *testing.T) {
		b := newBroker(t, map[string]string{"GET /b/status": "status.json"})
		b.route("GET /b/"+fixtureID+"/task/log", func(int) reply {
			return reply{Status: 200, Body: `[{"time":"2026-01-01T08:00:00Z","stage":"Updating","task":"redis/0","state":"failed","error":{"code":100,"message":"` + directorError + `"}}]`}
		})
		r, out, _ := testRunner(b.client(), "", true)
		if err := r.task([]string{fixtureID}, false); err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(out.String(), directorError) {
			t.Errorf("output %q", out)
		}
	})

	t.Run("followed task failure", func(t *testing.T) {
		for _, field := range []string{"error", "result"} {
			b := followBroker(t)
			body := `{"task":{"id":5104,"state":"error","` + field + `":"` + directorError + `"},"events":[]}`
			b.route("GET /b/tasks/5104", func(int) reply { return reply{Status: 200, Body: body} })
			r, cancel := followRunner(t, b)
			err := r.follow(fixtureID, nil, r.taskProgress(5104))
			cancel()
			if err == nil || !strings.Contains(err.Error(), directorError) {
				t.Errorf("%s: got %v", field, err)
			}
		}
	})
}

// withText swaps one piece of a fixture for the leaky text.
func withText(t *testing.T, file, old, with string) string {
	t.Helper()
	s := fixture(t, file)
	if !strings.Contains(s, old) {
		t.Fatalf("%s lacks %q", file, old)
	}
	return strings.Replace(s, old, with, 1)
}

func TestEveryPlaceThatShowsServerTextAppliesTheReasonRule(t *testing.T) {
	const bad = leakyText
	finding := `{"findings":[{"severity":"warning","code":"x","message":"` + bad + `"}],`
	cases := map[string]func(t *testing.T) string{
		"a finding": func(t *testing.T) string {
			b := newBroker(t, nil)
			b.route("GET /b/certificates/items/c0ffee01", func(int) reply {
				return reply{Status: 200, Body: strings.Replace(fixture(t, "cert_item.json"), "{", finding, 1)}
			})
			r, out, _ := testRunner(b.client(), "", true)
			_ = r.certShow([]string{"c0ffee01"})
			return out.String()
		},
		"a blocked preview": func(t *testing.T) string {
			return previewOutput(t, withText(t, "cert_preview.json", `"blocked": null`, `"blocked": {"code":"x","message":"`+bad+`"}`))
		},
		"a preview warning": func(t *testing.T) string {
			return previewOutput(t, withText(t, "cert_preview.json", "fixture warning text", bad))
		},
		"an orphan reason": func(t *testing.T) string {
			b := newBroker(t, nil)
			body := withText(t, "orphans.json", "no_bosh_deployment", bad)
			b.route("GET /b/orphans", func(int) reply { return reply{Status: 200, Body: body} })
			r, out, _ := testRunner(b.client(), "", true)
			_ = r.orphans(nil)
			return out.String()
		},
		"a failed task in the list": func(t *testing.T) string {
			b := newBroker(t, nil)
			body := withText(t, "bosh_tasks.json", `"error": "fixture failure text"`, `"error": "`+bad+`"`)
			b.route("GET /b/tasks", func(int) reply { return reply{Status: 200, Body: body} })
			r, out, _ := testRunner(b.client(), "", true)
			_ = r.tasksList(nil, TaskFilter{})
			return out.String()
		},
		"a batch pause reason": func(t *testing.T) string {
			return batchShowOutput(t, withText(t, "batch_job.json", `"paused": false,`, `"paused": true, "pause_reason": "`+bad+`",`))
		},
		"a batch instance error": func(t *testing.T) string {
			return batchShowOutput(t, withText(t, "batch_job.json", "fixture deploy failed", bad))
		},
		"a failed operation": func(t *testing.T) string {
			b := followBroker(t)
			body := withText(t, "osb_last_operation_failed.json", "fixture failure text", bad)
			b.route("GET /v2/service_instances/"+fixtureID+"/last_operation", func(int) reply { return reply{Status: 200, Body: body} })
			r, cancel := followRunner(t, b)
			defer cancel()
			err := r.follow(fixtureID, nil, r.operationProgress(fixtureID, OperationRef{}))
			if err == nil {
				return ""
			}
			return err.Error()
		},
	}
	for name, run := range cases {
		t.Run(name, func(t *testing.T) {
			got := run(t)
			if got == "" || leaks(got) || !strings.Contains(got, reasonWithheld) {
				t.Errorf("got %q", got)
			}
		})
	}
}

func previewOutput(t *testing.T, body string) string {
	t.Helper()
	b := newBroker(t, map[string]string{"GET /b/status": "status.json"})
	b.route("POST /b/certificates/rotation/preview", func(int) reply { return reply{Status: 200, Body: body} })
	r, out, _ := testRunner(b.client(), "", false)
	_ = r.certPreview([]string{fixtureID}, []string{"server_tls"}, "redeploy", "pending")
	return out.String()
}

func batchShowOutput(t *testing.T, body string) string {
	t.Helper()
	b := newBroker(t, nil)
	b.route("GET /b/upgrade/tasks/fixture-batch-0001", func(int) reply { return reply{Status: 200, Body: body} })
	r, out, _ := testRunner(b.client(), "", true)
	_ = r.batchShow([]string{"fixture-batch-0001"})
	return out.String()
}
