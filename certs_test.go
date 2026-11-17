package main

import (
	"errors"
	"strings"
	"testing"
)

func TestCertListShowsSummaryAndFilters(t *testing.T) {
	b := newBroker(t, map[string]string{"GET /b/certificates/inventory": "cert_inventory.json"})
	r, out, _ := testRunner(b.client(), "", true)

	err := r.certList(nil, CertFilter{"expiry": "critical", "instance": "x", "attention": "true", "health": ""}, false)
	if err != nil {
		t.Fatal(err)
	}

	for _, want := range []string{"1 certificates for 1 instances", "1 warning", "c0ffee01", "fixture.redis.internal", "20", "Matching your filters: 1 certificates for 1 instances"} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("output lacks %q:\n%s", want, out)
		}
	}
	path := b.requests()[0].Path
	for _, want := range []string{"expiry=critical", "instance=x", "attention=true"} {
		if !strings.Contains(path, want) {
			t.Errorf("request %s lacks %s", path, want)
		}
	}
	if strings.Contains(path, "health") {
		t.Errorf("a blank filter was sent: %s", path)
	}
}

func TestCertListLeavesOutTheFilteredLineWithoutFilters(t *testing.T) {
	b := newBroker(t, map[string]string{"GET /b/certificates/inventory": "cert_inventory.json"})
	r, out, _ := testRunner(b.client(), "", true)

	if err := r.certList(nil, CertFilter{}, false); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(out.String(), "Matching your filters") {
		t.Errorf("output: %s", out)
	}
}

func TestCertListSummaryOnly(t *testing.T) {
	b := newBroker(t, map[string]string{"GET /b/certificates/inventory": "cert_inventory.json"})
	r, _, _ := testRunner(b.client(), "", true)

	if err := r.certList(nil, CertFilter{}, true); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(b.requests()[0].Path, "summary_only=true") {
		t.Errorf("path: %s", b.requests()[0].Path)
	}
}

func TestCertListReportsADisabledInventory(t *testing.T) {
	b := newBroker(t, nil)
	b.status("GET /b/certificates/inventory", 403, "cert_disabled.json")
	r, _, _ := testRunner(b.client(), "", true)

	err := r.certList(nil, CertFilter{}, false)
	if err == nil || !strings.Contains(err.Error(), "inventory_disabled") {
		t.Fatalf("got %v", err)
	}
}

func TestCertListReportsABadFilter(t *testing.T) {
	b := newBroker(t, nil)
	b.status("GET /b/certificates/inventory", 400, "cert_bad_filter.json")
	r, _, _ := testRunner(b.client(), "", true)

	err := r.certList(nil, CertFilter{"expiry": "nope"}, false)
	if err == nil || !strings.Contains(err.Error(), "invalid_filter") {
		t.Fatalf("got %v", err)
	}
}

func TestCertShowPrintsTheCertificateButNotThePEM(t *testing.T) {
	b := newBroker(t, map[string]string{"GET /b/certificates/items/c0ffee01": "cert_item.json"})
	r, out, _ := testRunner(b.client(), "", true)

	if err := r.certShow([]string{"c0ffee01"}); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"Name:      fixture.redis.internal", "20 days left", "allowed=true modes=redeploy", "Expiry:    warning"} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("output lacks %q:\n%s", want, out)
		}
	}
	if strings.Contains(out.String(), "BEGIN CERTIFICATE") {
		t.Error("the PEM was printed")
	}
}

func TestCertInstanceResolvesAndPrints(t *testing.T) {
	b := newBroker(t, map[string]string{
		"GET /b/status": "status.json",
		"GET /b/certificates/instances/" + fixtureID: "cert_instance.json",
	})
	r, out, _ := testRunner(b.client(), "", true)

	if err := r.certInstance([]string{fixtureID}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "Rotatable variables: server_tls") {
		t.Errorf("output: %s", out)
	}
}

func TestCertScanAsksThenScans(t *testing.T) {
	b := newBroker(t, map[string]string{"GET /b/status": "status.json"})
	b.status("POST /b/certificates/scan", 202, "cert_scan.json")

	r, _, errb := testRunner(b.client(), "n\n", false)
	if err := r.certScan(nil, []string{fixtureID}); !errors.Is(err, errAborted) {
		t.Fatalf("got %v", err)
	}
	if len(only(b.requests(), "POST")) != 0 {
		t.Error("a scan started after no")
	}
	if !strings.Contains(errb.String(), fixtureID) {
		t.Errorf("prompt: %s", errb)
	}

	r, out, _ := testRunner(b.client(), "", true)
	if err := r.certScan(nil, []string{fixtureID}); err != nil {
		t.Fatal(err)
	}
	if got := only(b.requests(), "POST")[0].Body; got != `{"instance_ids":["`+fixtureID+`"]}` {
		t.Errorf("body: %s", got)
	}
	if !strings.Contains(out.String(), "Scan scan-2 is running.") {
		t.Errorf("output: %s", out)
	}
}

func TestCertScanOfEverythingSendsNoBody(t *testing.T) {
	b := newBroker(t, nil)
	b.status("POST /b/certificates/scan", 202, "cert_scan.json")
	r, _, _ := testRunner(b.client(), "", true)

	if err := r.certScan(nil, nil); err != nil {
		t.Fatal(err)
	}
	if got := b.requests()[0].Body; got != "" {
		t.Errorf("body: %q", got)
	}
}

func TestCertPreviewSendsSelectionsAndChangesNothing(t *testing.T) {
	b := newBroker(t, map[string]string{"GET /b/status": "status.json"})
	b.status("POST /b/certificates/rotation/preview", 200, "cert_preview.json")
	r, out, _ := testRunner(b.client(), "", false) // no --yes, and no question

	if err := r.certPreview([]string{fixtureID}, []string{"server_tls"}, "redeploy", "pending"); err != nil {
		t.Fatal(err)
	}

	want := `{"mode":"redeploy","converge":"pending","selections":[{"instance_id":"` + fixtureID + `","variables":["server_tls"]}]}`
	if got := only(b.requests(), "POST")[0].Body; got != want {
		t.Errorf("body: %s", got)
	}
	for _, w := range []string{"Preview fixture-preview-1", "Nothing was changed", "rotates server_tls (20 days left)", "restarts one VM", "Warning: fixture warning text"} {
		if !strings.Contains(out.String(), w) {
			t.Errorf("output lacks %q:\n%s", w, out)
		}
	}
}

func TestCertPreviewNeedsVariablesAndAKnownMode(t *testing.T) {
	r, _, _ := testRunner(&Client{}, "", true)

	var ue usageError
	if err := r.certPreview([]string{"x"}, nil, "redeploy", ""); !errors.As(err, &ue) {
		t.Errorf("no variables: %v", err)
	}
	if err := r.certPreview([]string{"x"}, []string{"v"}, "bogus", ""); !errors.As(err, &ue) {
		t.Errorf("bad mode: %v", err)
	}
}
