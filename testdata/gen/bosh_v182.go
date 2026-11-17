//go:build ignore

package bosh_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"blacksmith/internal/bosh"
	boshhandler "blacksmith/internal/handlers/bosh"
	"blacksmith/internal/interfaces"
	"blacksmith/internal/vmmonitor"
	"blacksmith/pkg/logger"
)

type zzVault struct {
	interfaces.Vault
	db      map[string]interface{}
	records map[string]map[string]interface{}
}

func (v *zzVault) Get(_ context.Context, path string, result interface{}) (bool, error) {
	var data interface{}
	if path == "db" {
		data = v.db
	} else if r, ok := v.records[path]; ok {
		data = r
	} else {
		return false, nil
	}
	enc, _ := json.Marshal(data)
	return true, json.Unmarshal(enc, result)
}

type zzMonitor struct {
	interfaces.VMMonitor
}

func (zzMonitor) GetStatus() vmmonitor.MonitorStatus {
	at := time.Date(2026, 11, 14, 8, 0, 0, 0, time.UTC)
	return vmmonitor.MonitorStatus{
		Running: true, ServiceCount: 1, NormalInterval: "1h0m0s", FailedInterval: "5m0s",
		Services: []vmmonitor.ServiceStatus{{ServiceID: "11111111-aaaa-bbbb-cccc-000000000001", DeploymentName: "redis-small-11111111", LastStatus: "running", LastCheck: at, NextCheck: at.Add(time.Hour), IsHealthy: true}},
		Summary:  map[string]interface{}{"healthy": 1, "failing": 0},
	}
}

func (zzMonitor) GetServiceVMStatus(_ context.Context, id string) (*vmmonitor.VMStatus, error) {
	at := time.Date(2026, 11, 14, 8, 0, 0, 0, time.UTC)
	return &vmmonitor.VMStatus{Status: "running", VMCount: 1, HealthyVMs: 1, LastUpdated: at, NextUpdate: at.Add(time.Hour), VMs: []bosh.VM{}}, nil
}

func TestZZFixtures(t *testing.T) {
	vault := &zzVault{
		db: map[string]interface{}{
			"11111111-aaaa-bbbb-cccc-000000000001": map[string]interface{}{"service_id": "redis", "plan_id": "redis-plan"},
		},
		records: map[string]map[string]interface{}{
			"11111111-aaaa-bbbb-cccc-000000000001": {"instance_name": "fixture-redis", "deployment_name": "redis-small-11111111", "service_id": "redis", "plan_id": "redis-plan", "organization_name": "fixture-org", "space_name": "fixture-space"},
		},
	}
	build := func(mon interfaces.VMMonitor) *boshhandler.Handler {
		return boshhandler.NewHandler(boshhandler.Dependencies{
			Logger: &logger.NoOpLogger{}, Config: statusConfig{}, Director: &infoDirector{}, Broker: planBroker{releases: []string{"redis-forge"}},
			Vault: vault, VMMonitor: mon, VaultHealth: func(context.Context) error { return nil },
			Version: "fixture-1.8.4", Commit: "0000000", Now: func() time.Time { return time.Date(2026, 11, 14, 8, 0, 0, 0, time.UTC) },
		})
	}
	save := func(name, route string, rec *httptest.ResponseRecorder) {
		zzSave(t, name, route, rec.Code, rec.Body.Bytes())
	}
	h := build(zzMonitor{})
	rec := httptest.NewRecorder()
	h.GetStatus(rec, httptest.NewRequestWithContext(t.Context(), "GET", "/b/status", nil))
	save("status", "GET /b/status", rec)
	rec = httptest.NewRecorder()
	h.GetVMMonitorStatus(rec, httptest.NewRequestWithContext(t.Context(), "GET", "/b/bosh/vm-monitor", nil))
	save("vm_monitor", "GET /b/bosh/vm-monitor", rec)
	rec = httptest.NewRecorder()
	build(nil).GetVMMonitorStatus(rec, httptest.NewRequestWithContext(t.Context(), "GET", "/b/bosh/vm-monitor", nil))
	save("vm_monitor_off", "GET /b/bosh/vm-monitor (monitor not configured)", rec)
	_ = bytes.NewReader
	_ = http.StatusOK
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
