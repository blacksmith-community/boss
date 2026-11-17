package main

import (
	"errors"
	"io"
	"os"
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

func TestABrokerErrorCodeAndTheBrokersFixedMessagesPrintInFull(t *testing.T) {
	for _, code := range []string{
		"AsyncRequired",
		"ConcurrencyError",
		"MaintenanceInfoConflict",
		"RequiresApp",
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
	for _, code := range []string{
		"Token_error", "token_" + strings.Repeat("x", 64), "token_error ",
		"hunter2", "0123456789abcdef0123456789abcdef", "db_password_hunter2", "sk_" + "live_" + "4eC39HqLyjWDarjtT1zdp7dc",
		"asyncrequired", "AsyncRequired2",
	} {
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

func TestAFailedTaskInTheListPrintsTheDirectorErrorAsSent(t *testing.T) {
	b := newBroker(t, nil)
	body := withText(t, "bosh_tasks.json", `"error": "fixture failure text"`, `"error": "`+directorError+`"`)
	b.route("GET /b/tasks", func(int) reply { return reply{Status: 200, Body: body} })
	r, out, _ := testRunner(b.client(), "", true)
	if err := r.tasksList(nil, TaskFilter{}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), directorError) {
		t.Errorf("output %q", out)
	}
}

func TestDirectorTextThatPrintsAsSentLosesItsControlCharacters(t *testing.T) {
	dirty := "bad password \x1b[31mred\x1b[0m \u202eevil\u200b\x07 done\nsecond\tline"
	clean := "bad password [31mred[0m evil done\nsecond\tline"
	jsonText := `bad password \u001b[31mred\u001b[0m \u202eevil\u200b\u0007 done\nsecond\tline`

	if got := asSent(dirty); got != clean {
		t.Errorf("asSent: got %q", got)
	}

	t.Run("task log", func(t *testing.T) {
		b := newBroker(t, map[string]string{"GET /b/status": "status.json"})
		b.route("GET /b/"+fixtureID+"/task/log", func(int) reply {
			return reply{Status: 200, Body: `[{"time":"2026-01-01T08:00:00Z","stage":"S","task":"t","state":"failed","error":{"code":1,"message":"` + jsonText + `"}}]`}
		})
		r, out, _ := testRunner(b.client(), "", true)
		if err := r.task([]string{fixtureID}, false); err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(out.String(), clean) || strings.ContainsAny(out.String(), "\x1b\u202e\u200b\x07") {
			t.Errorf("output %q", out)
		}
	})

	t.Run("followed task", func(t *testing.T) {
		b := followBroker(t)
		body := `{"task":{"id":5104,"state":"error","error":"` + jsonText + `"},"events":[]}`
		b.route("GET /b/tasks/5104", func(int) reply { return reply{Status: 200, Body: body} })
		r, cancel := followRunner(t, b)
		err := r.follow(fixtureID, nil, r.taskProgress(5104))
		cancel()
		if err == nil || !strings.Contains(err.Error(), clean) || strings.ContainsAny(err.Error(), "\x1b\u202e\u200b\x07") {
			t.Errorf("got %q", err)
		}
	})

	t.Run("task list", func(t *testing.T) {
		b := newBroker(t, nil)
		body := withText(t, "bosh_tasks.json", `"error": "fixture failure text"`, `"error": "`+jsonText+`"`)
		b.route("GET /b/tasks", func(int) reply { return reply{Status: 200, Body: body} })
		r, out, _ := testRunner(b.client(), "", true)
		if err := r.tasksList(nil, TaskFilter{}); err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(out.String(), clean) || strings.ContainsAny(out.String(), "\x1b\u202e\u200b\x07") {
			t.Errorf("output %q", out)
		}
	})
}

func TestTheCodeIsCheckedWhenADescriptionFollowsIt(t *testing.T) {
	got := APIError{Code: leakyText, Description: "the plan is unknown"}.Error()
	if leaks(got) || !strings.Contains(got, reasonWithheld) || !strings.Contains(got, "the plan is unknown") {
		t.Errorf("got %q", got)
	}
}

func TestOnlyABrokerCodePrintsInParentheses(t *testing.T) {
	for _, code := range []string{"deployment_locked", "instance_not_found", "vault_unavailable", "internal_error", "command_credential_required", "E008"} {
		got := APIError{Code: "a message", BrokerCode: code}.Error()
		if got != "a message ("+code+")" {
			t.Errorf("%s: got %q", code, got)
		}
	}
	for _, code := range []string{"hunter2", "0123456789abcdef0123456789abcdef", "db_password_hunter2", "two words", "tab\there", "esc\x1b[31m", "Vault_Unavailable", "vault_unavailable "} {
		got := APIError{Code: "a message", BrokerCode: code}.Error()
		if got != "a message" {
			t.Errorf("%q: got %q", code, got)
		}
	}
}

func TestOnlyAListedCodePrintsForAFindingOrABlockedPreview(t *testing.T) {
	for _, code := range []string{"weak_key", "chain_broken", "instance_busy", "instance_not_found"} {
		if got := listedCode(code); got != code {
			t.Errorf("%s printed as %q", code, got)
		}
	}
	for _, code := range []string{"hunter2", "esc\x1b[31m", "db_password_hunter2", "two words", ""} {
		if got := listedCode(code); got != codeWithheld {
			t.Errorf("%q printed as %q", code, got)
		}
	}

	finding := `{"findings":[{"severity":"warning","code":"hunter2","message":"a plain message"}],`
	b := newBroker(t, nil)
	b.route("GET /b/certificates/items/c0ffee01", func(int) reply {
		return reply{Status: 200, Body: strings.Replace(fixture(t, "cert_item.json"), "{", finding, 1)}
	})
	r, out, _ := testRunner(b.client(), "", true)
	_ = r.certShow([]string{"c0ffee01"})
	if leaks(out.String()) || !strings.Contains(out.String(), "[warning] "+codeWithheld+": a plain message") {
		t.Errorf("finding output %q", out)
	}

	got := previewOutput(t, withText(t, "cert_preview.json", `"blocked": null`, `"blocked": {"code":"hunter2","message":"a plain message"}`))
	if leaks(got) || !strings.Contains(got, "BLOCKED ["+codeWithheld+"]: a plain message") {
		t.Errorf("preview output %q", got)
	}
}

func TestCredsErrorBodyAndStatusTextPassThroughTheReasonRule(t *testing.T) {
	b := newBroker(t, nil)
	b.route("GET /b/fixture-id/credentials", func(int) reply { return reply{Status: 200, Body: `{"error":"` + leakyText + `"}`} })
	_, err := b.client().Creds("fixture-id")
	if err == nil || leaks(err.Error()) || !strings.Contains(err.Error(), reasonWithheld) {
		t.Errorf("got %v", err)
	}
}

func TestEveryPrintedStringLosesItsControlCharacters(t *testing.T) {
	esc := "\x1b[31m\u202e"
	bad := func(s string) string { return strings.NewReplacer("\x1b", `\u001b`, "\u202e", `\u202e`).Replace(s) }
	dirty := func(out string) bool { return strings.ContainsAny(out, "\x1b\u202e") }

	t.Run("printf and table", func(t *testing.T) {
		b := newBroker(t, nil)
		body := withText(t, "batch_list.json", "fixture stemcell upgrade", "name"+bad(esc)+"\\nnext")
		b.route("GET /b/upgrade/tasks", func(int) reply { return reply{Status: 200, Body: body} })
		r, out, _ := testRunner(b.client(), "", true)
		if err := r.batchList(nil); err != nil {
			t.Fatal(err)
		}
		if dirty(out.String()) || !strings.Contains(out.String(), "name[31m next") {
			t.Errorf("output %q", out)
		}
	})

	t.Run("a printed name", func(t *testing.T) {
		r, out, _ := testRunner(newBroker(t, nil).client(), "", true)
		r.printf("Deployment %s\n", "dep"+esc)
		if dirty(out.String()) {
			t.Errorf("output %q", out)
		}
	})

	t.Run("task output", func(t *testing.T) {
		b := newBroker(t, nil)
		b.route("GET /b/tasks/7/output", func(int) reply { return reply{Status: 200, Body: `{"output":"line` + bad(esc) + `"}`} })
		b.route("GET /b/tasks/7/output/result", func(int) reply { return reply{Status: 200, Body: `{"output":"line` + bad(esc) + `"}`} })
		r, out, _ := testRunner(b.client(), "", true)
		_ = r.tasksOutput([]string{"7"}, "result")
		if dirty(out.String()) {
			t.Errorf("output %q", out)
		}
	})

	t.Run("the final error", func(t *testing.T) {
		old := os.Stderr
		rd, wr, _ := os.Pipe()
		os.Stderr = wr
		code := -1
		oldExit := exit
		exit = func(c int) { code = c }
		finish("list", errors.New("failed"+esc))
		exit = oldExit
		os.Stderr = old
		_ = wr.Close()
		got, _ := io.ReadAll(rd)
		if code == -1 || dirty(string(got)) || !strings.Contains(string(got), "failed") {
			t.Errorf("stderr %q", got)
		}
	})
}

func TestAnEnumeratedFieldPrintsOnlyTheValuesTheBrokerSends(t *testing.T) {
	for _, v := range []string{"expired", "ok"} {
		if got := oneOf(v, expiryValues...); got != v {
			t.Errorf("%s printed as %q", v, got)
		}
	}
	for _, v := range []string{"hunter2", "esc\x1b[31m", "Expired", "two words"} {
		if got := oneOf(v, expiryValues...); got != valueWithheld {
			t.Errorf("%q printed as %q", v, got)
		}
	}
	if oneOf("", expiryValues...) != "" {
		t.Error("an empty value must stay empty")
	}

	b := newBroker(t, nil)
	t.Run("a certificate", func(t *testing.T) {
		body := fixture(t, "cert_item.json")
		for _, r := range [][2]string{{`"kind": "",`, `"kind": "hunter2",`}, {`"expiry": "warning"`, `"expiry": "hunter2"`}, {`"health": "ok"`, `"health": "hunter2"`}, {`"attention": true,`, `"attention": true, "findings": [{"severity": "hunter2", "code": "weak_key", "message": "m"}],`}, {`"redeploy"`, `"hunter2"`}} {
			if !strings.Contains(body, r[0]) {
				t.Fatalf("fixture lacks %s", r[0])
			}
			body = strings.Replace(body, r[0], r[1], 1)
		}
		b.route("GET /b/certificates/items/c0ffee01", func(int) reply { return reply{Status: 200, Body: body} })
		r, out, _ := testRunner(b.client(), "", true)
		_ = r.certShow([]string{"c0ffee01"})
		if leaks(out.String()) || strings.Count(out.String(), valueWithheld) < 5 {
			t.Errorf("output %q", out)
		}
	})
	t.Run("a batch job", func(t *testing.T) {
		body := withText(t, "batch_job.json", `"paused": false,`, `"paused": false, "kind": "hunter2",`)
		body = strings.Replace(body, `"status": "completed"`, `"status": "hunter2"`, 1)
		out := batchShowOutput(t, body)
		if leaks(out) || !strings.Contains(out, valueWithheld) {
			t.Errorf("output %q", out)
		}
	})
}

func sameSet(t *testing.T, name string, got map[string]bool, want []string) {
	t.Helper()
	seen := map[string]bool{}
	for _, w := range want {
		seen[w] = true
		if !got[w] {
			t.Errorf("%s lost %q", name, w)
		}
	}
	for g := range got {
		if !seen[g] {
			t.Errorf("%s gained %q", name, g)
		}
	}
}

func TestTheCodeListsMatchTheBrokerExactly(t *testing.T) {
	sameSet(t, "brokerErrorCodes", brokerErrorCodes, []string{
		"AsyncRequired",
		"ConcurrencyError",
		"HTTPError",
		"MaintenanceInfoConflict",
		"RequiresApp",
	})
	sameSet(t, "brokerCodes", brokerCodes, []string{
		"E007",
		"E008",
		"approval_mismatch",
		"candidate_invalid",
		"cf_auth_failed",
		"cf_not_cloud_foundry",
		"cf_tls_failed",
		"cf_unreachable",
		"cleanup_not_started",
		"command_credential_required",
		"command_too_long",
		"commands_disabled",
		"credhub_not_configured",
		"credhub_unavailable",
		"deployment_locked",
		"deployment_not_managed",
		"director_unavailable",
		"director_unsupported",
		"instance_busy",
		"instance_not_found",
		"internal_error",
		"invalid_filter",
		"invalid_mode",
		"invalid_request",
		"invalid_run_user",
		"invalid_settings",
		"invalid_task_state",
		"inventory_disabled",
		"max_in_flight_too_high",
		"no_health_gate",
		"no_selections",
		"not_awaiting_decision",
		"not_found",
		"not_rotatable",
		"outcome_unknown",
		"patch_disabled",
		"patch_unsupported",
		"preview_bound",
		"preview_expired",
		"preview_mismatch",
		"request_id_conflict",
		"root_disabled",
		"rotation_disabled",
		"signing_ca_changed",
		"ssh_disabled",
		"tasks_loading",
		"timeout_out_of_range",
		"unknown_variable",
		"unsupported_media_type",
		"upgrade_preview_required",
		"vault_unavailable",
		"vms_not_running",
	})
	sameSet(t, "findingCodes", findingCodes, []string{
		"chain_broken",
		"deploy_pending",
		"issuer_expires_first",
		"patch_reverted",
		"pinned_ca_mismatch",
		"probe_failed",
		"san_mismatch",
		"served_mismatch",
		"services_ca_drift",
		"signing_ca_changed",
		"unconverged",
		"weak_key",
		"weak_signature",
	})
	vmWant := []string{"running", "failing", "unresponsive agent", "unresponsive", "provisioning", "error", "unknown", "stopping", "starting", "stopped"}
	if got := strings.Join(vmStatuses, ","); got != strings.Join(vmWant, ",") {
		t.Errorf("vmStatuses is %s", got)
	}
	if got := strings.Join(severityValues, ","); got != "error,warning,info" {
		t.Errorf("severityValues is %s", got)
	}
}

func TestEveryMonitorStatusTheBrokerWritesPrintsAsSent(t *testing.T) {
	for _, status := range []string{"running", "failing", "unresponsive agent", "unresponsive", "provisioning", "error", "unknown", "stopping", "starting", "stopped"} {
		t.Run(status, func(t *testing.T) {
			body := strings.Replace(fixture(t, "vm_monitor.json"), `"last_status": "running"`, `"last_status": "`+status+`"`, 1)
			b := newBroker(t, nil)
			b.route("GET /b/bosh/vm-monitor", func(int) reply { return reply{Status: 200, Body: body} })
			r, out, _ := testRunner(b.client(), "", true)
			if err := r.monitor(nil); err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(out.String(), status) || strings.Contains(out.String(), valueWithheld) {
				t.Errorf("status %q printed as %q", status, out)
			}
		})
	}
}
