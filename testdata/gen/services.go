//go:build ignore

package services_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"blacksmith/internal/bosh"
	"blacksmith/internal/handlers/services"
	"blacksmith/pkg/logger"
	"blacksmith/pkg/testutil"
	vaultPkg "blacksmith/pkg/vault"
)

const zzID = "11111111-aaaa-bbbb-cccc-000000000001"

type zzVault2 struct {
	testutil.InstanceVault
	values map[string]interface{}
}

func (v *zzVault2) Get(_ context.Context, path string, out interface{}) (bool, error) {
	val, ok := v.values[path]
	if !ok {
		return false, nil
	}
	enc, _ := json.Marshal(val)
	return true, json.Unmarshal(enc, out)
}

type zzBadVault struct{ *zzVault2 }

func (zzBadVault) Get(context.Context, string, interface{}) (bool, error) {
	return false, errors.New("vault unavailable")
}

type zzDir struct {
	*testutil.ScriptedBOSHDirector
}

func (zzDir) GetTaskOutput(_ context.Context, id int, typ string) (string, error) {
	if typ != "event" {
		return "", nil
	}
	at := time.Date(2026, 11, 14, 8, 0, 0, 0, time.UTC).Unix()
	return fmt.Sprintf(`{"time":%d,"stage":"Updating instance","tags":["redis"],"total":1,"task":"redis/0","index":1,"state":"started","progress":0}`+"\n"+
		`{"time":%d,"stage":"Updating instance","tags":["redis"],"total":1,"task":"redis/0","index":1,"state":"finished","progress":100}`+"\n", at, at+60), nil
}

func TestZZFixtures(t *testing.T) {
	manifest := "name: redis-small-11111111\nreleases:\n- name: redis-forge\n  version: latest\ninstance_groups:\n- name: redis\n  instances: 1\n"
	vault := &zzVault2{
		InstanceVault: testutil.InstanceVault{Instances: map[string]vaultPkg.Instance{zzID: {ID: zzID, ServiceID: "redis", PlanID: "redis-small", DeploymentName: "redis-small-11111111"}}},
		values: map[string]interface{}{
			zzID + "/manifest":    map[string]interface{}{"manifest": manifest},
			zzID + "/credentials": map[string]interface{}{"host": "10.0.0.5", "port": 6379, "username": "fixture-user", "password": "fixture-not-a-secret", "uri": "redis://fixture-user:fixture-not-a-secret@10.0.0.5:6379"},
		},
	}
	director := testutil.NewScriptedBOSHDirector()
	director.GetEventsFn = func(string) ([]bosh.Event, error) {
		return []bosh.Event{{ID: "1", Action: "update", ObjectType: "deployment", ObjectName: "redis-small-11111111", Task: "5201", TaskID: "5201", Deployment: "redis-small-11111111"}}, nil
	}
	h := services.NewHandler(services.Dependencies{Logger: logger.Get().Named("test"), Director: zzDir{director}, Vault: vault})
	empty := services.NewHandler(services.Dependencies{Logger: logger.Get().Named("test"), Director: zzDir{director}, Vault: &zzVault2{InstanceVault: testutil.InstanceVault{Instances: map[string]vaultPkg.Instance{zzID: {ID: zzID, ServiceID: "redis", PlanID: "redis-small"}}}}})
	do := func(h *services.Handler, target string) *httptest.ResponseRecorder {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequestWithContext(t.Context(), "GET", target, nil))
		return rec
	}
	save := func(name, route string, rec *httptest.ResponseRecorder) {
		zzSave(t, name, route, rec.Code, rec.Body.Bytes())
	}
	save("manifest", "GET /b/{id}/manifest", do(h, "/b/"+zzID+"/manifest"))
	save("credentials", "GET /b/{id}/credentials", do(h, "/b/"+zzID+"/credentials"))
	save("credentials_placeholder", "GET /b/{id}/credentials (no stored credentials)", do(empty, "/b/"+zzID+"/credentials"))
	save("task_log", "GET /b/{id}/task/log", do(h, "/b/"+zzID+"/task/log"))
	save("task_log_missing_instance", "GET /b/{id}/task/log (unknown instance)", do(h, "/b/nope/task/log"))
	bad := services.NewHandler(services.Dependencies{Logger: logger.Get().Named("test"), Director: zzDir{director}, Vault: zzBadVault{vault}})
	save("credentials_error", "GET /b/{id}/credentials (vault read fails)", do(bad, "/b/"+zzID+"/credentials"))
	save("manifest_error", "GET /b/{id}/manifest (vault read fails)", do(bad, "/b/"+zzID+"/manifest"))
	_ = bytes.NewReader
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
