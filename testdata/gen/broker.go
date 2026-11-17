//go:build ignore

package broker_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"blacksmith/internal/broker"
	"blacksmith/internal/services"

	osbapibroker "github.com/fivetwenty-io/osbapi/v2/pkg/broker"
	"github.com/fivetwenty-io/osbapi/v2/pkg/osbapi"
)

type zzGone struct{ *broker.Broker }

func (zzGone) Deprovision(context.Context, string, osbapi.DeprovisionRequest, bool) (osbapi.DeprovisionResponse, bool, error) {
	return osbapi.DeprovisionResponse{}, false, errors.Join(osbapi.ErrGoneError)
}

type zzOK struct{ *broker.Broker }

// zzOps answers last_operation from a script, through the real osbapi handler.
type zzOps struct {
	*broker.Broker
	resp osbapi.LastOperationResponse
	err  error
}

func (o zzOps) LastOperation(context.Context, string, osbapi.LastOperationRequest) (osbapi.LastOperationResponse, error) {
	return o.resp, o.err
}

func (zzOK) Deprovision(context.Context, string, osbapi.DeprovisionRequest, bool) (osbapi.DeprovisionResponse, bool, error) {
	return osbapi.DeprovisionResponse{}, true, nil
}
func (zzOK) Provision(context.Context, string, osbapi.ProvisionRequest, bool) (osbapi.ProvisionResponse, bool, error) {
	return osbapi.ProvisionResponse{}, true, nil
}

func TestZZFixtures(t *testing.T) {
	b := &broker.Broker{Catalog: services.Catalog([]services.Service{{
		ID: "redis", Name: "redis", Description: "fixture redis service", Bindable: true, Tags: []string{"redis"},
		Plans: []services.Plan{{ID: "redis-small", Name: "small", Description: "fixture small plan"}, {ID: "redis-large", Name: "large", Description: "fixture large plan"}, {ID: "redis-plan", Name: "standalone", Description: "fixture standalone plan"}},
	}})}
	srv := httptest.NewServer(osbapibroker.NewHandler(b))
	defer srv.Close()
	call := func(method, path, body string) (int, []byte) {
		req, _ := http.NewRequestWithContext(t.Context(), method, srv.URL+path, strings.NewReader(body))
		req.Header.Set(osbapi.HeaderAPIVersion, "2.17")
		req.Header.Set("Content-Type", "application/json")
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		data, _ := io.ReadAll(resp.Body)
		return resp.StatusCode, data
	}
	code, data := call("GET", "/v2/catalog", "")
	zzSave(t, "catalog", "GET /v2/catalog", code, data)
	code, data = call("PATCH", "/v2/service_instances/11111111-aaaa-bbbb-cccc-000000000001?accepts_incomplete=true", `{"service_id":"redis","plan_id":"redis-large","previous_values":{"plan_id":"redis-small"}}`)
	zzSave(t, "osb_update", "PATCH /v2/service_instances/{id} (always not implemented)", code, data)
	gone := httptest.NewServer(osbapibroker.NewHandler(zzGone{b}))
	defer gone.Close()
	req, _ := http.NewRequestWithContext(t.Context(), "DELETE", gone.URL+"/v2/service_instances/nope?service_id=redis&plan_id=redis-small&accepts_incomplete=true", nil)
	req.Header.Set(osbapi.HeaderAPIVersion, "2.17")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	gb, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	ok := httptest.NewServer(osbapibroker.NewHandler(zzOK{b}))
	defer ok.Close()
	for _, c := range []struct{ name, method, path, body string }{
		{"osb_delete", "DELETE", "/v2/service_instances/x?service_id=redis&plan_id=redis-small&accepts_incomplete=true", ""},
		{"osb_provision", "PUT", "/v2/service_instances/x?accepts_incomplete=true", `{"service_id":"redis","plan_id":"redis-small"}`},
	} {
		rq, _ := http.NewRequestWithContext(t.Context(), c.method, ok.URL+c.path, strings.NewReader(c.body))
		rq.Header.Set(osbapi.HeaderAPIVersion, "2.17")
		rq.Header.Set("Content-Type", "application/json")
		rs, err := http.DefaultClient.Do(rq)
		if err != nil {
			t.Fatal(err)
		}
		bb, _ := io.ReadAll(rs.Body)
		rs.Body.Close()
		zzSave(t, c.name, c.method+" /v2/service_instances/{id} (stub broker, real osbapi handler)", rs.StatusCode, bb)
	}
	for _, c := range []struct {
		name string
		ops  zzOps
	}{
		{"osb_last_operation_progress", zzOps{Broker: b, resp: osbapi.LastOperationResponse{State: osbapi.StateInProgress, Description: "BOSH deployment in progress (task 5103)"}}},
		{"osb_last_operation_succeeded", zzOps{Broker: b, resp: osbapi.LastOperationResponse{State: osbapi.StateSucceeded, Description: "BOSH deployment finished"}}},
		{"osb_last_operation_failed", zzOps{Broker: b, resp: osbapi.LastOperationResponse{State: osbapi.StateFailed, Description: "fixture failure text"}}},
		{"osb_last_operation_gone", zzOps{Broker: b, err: errors.Join(osbapi.ErrGoneError)}},
	} {
		s := httptest.NewServer(osbapibroker.NewHandler(c.ops))
		rq, _ := http.NewRequestWithContext(t.Context(), "GET", s.URL+"/v2/service_instances/x/last_operation", nil)
		rq.Header.Set(osbapi.HeaderAPIVersion, "2.17")
		rs, err := http.DefaultClient.Do(rq)
		if err != nil {
			t.Fatal(err)
		}
		bb, _ := io.ReadAll(rs.Body)
		rs.Body.Close()
		s.Close()
		zzSave(t, c.name, "GET /v2/service_instances/{id}/last_operation (stub broker, real osbapi handler)", rs.StatusCode, bb)
	}
	zzSave(t, "osb_delete_gone", "DELETE /v2/service_instances/{id} (instance not recorded; osbapi library answer for a gone error)", resp.StatusCode, gb)
	_ = bytes.NewReader
	_ = json.Marshal
	_ = fmt.Sprint
	_ = os.Getenv
	_ = filepath.Join
}

func zzSave(t *testing.T, name, route string, code int, body []byte) {
	t.Helper()
	dir := os.Getenv("FIXOUT")
	if dir == "" {
		t.Skip("FIXOUT not set")
	}
	var out bytes.Buffer
	if err := json.Indent(&out, body, "", "  "); err != nil {
		out.Reset()
		out.Write(body)
		name += ".txt"
	} else {
		out.WriteString("\n")
		name += ".json"
	}
	if err := os.WriteFile(filepath.Join(dir, name), out.Bytes(), 0o644); err != nil {
		t.Fatal(err)
	}
	f, err := os.OpenFile(filepath.Join(dir, "MANIFEST.tsv"), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	fmt.Fprintf(f, "%s\t%d\t%s\n", name, code, route)
}
