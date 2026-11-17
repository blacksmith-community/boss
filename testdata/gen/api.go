//go:build ignore

package api_test

import (
	"blacksmith/internal/interfaces"
	"blacksmith/pkg/testutil"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"blacksmith/internal/api"
	pkgservices "blacksmith/pkg/services"
)

type zzV struct{ interfaces.Vault }

func (zzV) Get(context.Context, string, interface{}) (bool, error) { return false, nil }
func (zzV) Put(context.Context, string, interface{}) error         { return nil }

func TestZZFixtures(t *testing.T) {
	deps := api.Dependencies{
		Config: &mockConfig{}, Vault: zzV{}, Director: testutil.NewScriptedBOSHDirector(), Logger: &mockLogger{}, Broker: &mockBroker{},
		ServicesManager: &pkgservices.Manager{}, CFManager: &mockCFManager{}, SecurityMiddleware: &pkgservices.SecurityMiddleware{},
	}
	h := api.NewInternalAPI(deps)
	defer h.UpgradeHandler().Stop()
	for _, c := range []struct{ name, method, path, body string }{
		{"route_restart", "POST", "/b/11111111-aaaa-bbbb-cccc-000000000001/restart", ""},
		{"route_recreate", "POST", "/b/11111111-aaaa-bbbb-cccc-000000000001/recreate", ""},
		{"route_orphans", "GET", "/b/orphans", ""},
		{"route_deployment_recreate", "POST", "/b/deployments/fixture-dep/recreate", ""},
		{"route_unknown", "GET", "/b/no-such-endpoint-at-all", ""},
		{"route_upgrade_unknown", "GET", "/b/upgrade/no-such-endpoint", ""},
		{"route_tasks", "GET", "/b/tasks", ""},
		{"route_tasks_paged", "GET", "/b/tasks?paged=1", ""},
		{"route_task_output", "GET", "/b/tasks/1/output", ""},
		{"route_task_cancel", "POST", "/b/tasks/1/cancel", ""},
		{"route_cleanup", "POST", "/b/cleanup", ""},
		{"route_vm_monitor", "GET", "/b/bosh/vm-monitor", ""},
	} {
		func() {
			defer func() {
				if r := recover(); r != nil {
					t.Logf("%s panicked: %v", c.name, r)
				}
			}()
			rec := httptest.NewRecorder()
			h.ServeHTTP(rec, httptest.NewRequestWithContext(t.Context(), c.method, c.path, strings.NewReader(c.body)))
			zzSave(t, c.name, c.method+" "+c.path, rec.Code, rec.Body.Bytes())
		}()
	}
	_ = bytes.NewReader
	_ = json.Marshal
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
