//go:build ignore

package upgrade_test

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"blacksmith/internal/apierr"
	"blacksmith/internal/config"
	upgradehandler "blacksmith/internal/handlers/upgrade"
	"blacksmith/internal/upgrade"
	"blacksmith/pkg/logger"
)

func TestZZFixtures(t *testing.T) {
	vault := newMemoryVault()
	at := time.Date(2026, 11, 10, 9, 0, 0, 0, time.UTC)
	end := at.Add(12 * time.Minute)
	done := upgrade.UpgradeTask{
		ID: "fixture-batch-0001", Name: "fixture stemcell upgrade", Status: upgrade.TaskStatusCompleted,
		TargetStemcell: upgrade.StemcellTarget{OS: "ubuntu-noble", Version: "1.2"},
		Instances: []upgrade.InstanceUpgrade{
			{InstanceID: "11111111-aaaa-bbbb-cccc-000000000001", InstanceName: "fixture-redis", DeploymentName: "redis-small-11111111", Status: upgrade.InstanceStatusSuccess, StartedAt: &at, CompletedAt: &end},
			{InstanceID: "11111111-aaaa-bbbb-cccc-000000000002", InstanceName: "fixture-pg", DeploymentName: "pg-small-11111111", Status: upgrade.InstanceStatusFailed, Error: "fixture deploy failed"},
		},
		TotalCount: 2, CompletedCount: 1, FailedCount: 1, CreatedAt: at, StartedAt: &at, CompletedAt: &end,
	}
	paused := done
	paused.ID, paused.Name, paused.Status, paused.CompletedAt = "fixture-batch-0002", "fixture paused upgrade", upgrade.TaskStatusPaused, nil
	running := done
	running.ID, running.Name, running.Status, running.CompletedAt = "fixture-batch-0003", "fixture running upgrade", upgrade.TaskStatusRunning, nil
	storeUpgrades(t, vault, done, paused, running)

	h := upgradehandler.NewHandler(upgradehandler.Dependencies{Logger: logger.NewNoOpLogger(), Vault: vault})
	defer h.Stop()

	time.Sleep(500 * time.Millisecond)
	do := func(method, target, body string) *httptest.ResponseRecorder {
		rec := httptest.NewRecorder()
		var rd *bytes.Reader
		if body != "" {
			rd = bytes.NewReader([]byte(body))
		} else {
			rd = bytes.NewReader(nil)
		}
		h.ServeHTTP(rec, httptest.NewRequestWithContext(t.Context(), method, target, rd))
		return rec
	}
	save := func(name, route string, rec *httptest.ResponseRecorder) {
		zzSave(t, name, route, rec.Code, rec.Body.Bytes())
	}

	save("batch_list", "GET /b/upgrade/tasks", do("GET", "/b/upgrade/tasks", ""))
	save("batch_job", "GET /b/upgrade/tasks/{id}", do("GET", "/b/upgrade/tasks/fixture-batch-0001", ""))
	save("batch_missing", "GET /b/upgrade/tasks/{id} (unknown)", do("GET", "/b/upgrade/tasks/nope", ""))
	save("batch_paused_resume", "POST /b/upgrade/tasks/{id}/resume", do("POST", "/b/upgrade/tasks/fixture-batch-0002/resume", ""))
	save("batch_cancel", "POST /b/upgrade/tasks/{id}/cancel", do("POST", "/b/upgrade/tasks/fixture-batch-0002/cancel", ""))
	{
		auth := upgrade.NewConfigAuthorizer(func() config.CommandsConfig {
			return config.CommandsConfig{Credential: config.CommandCredential{Key: "fixture-key"}}
		})
		rec := httptest.NewRecorder()
		req := httptest.NewRequestWithContext(t.Context(), "POST", "/b/upgrade/tasks", nil)
		apierr.Write(rec, auth.AuthorizeRequest(t.Context(), req, "vcap"), 500)
		save("batch_credential_required", "POST /b/upgrade/tasks (command key missing; real authorizer error through apierr.Write)", rec)
	}
	save("batch_pause", "POST /b/upgrade/tasks/{id}/pause", do("POST", "/b/upgrade/tasks/fixture-batch-0003/pause", ""))
	save("batch_cancel_done", "POST /b/upgrade/tasks/{id}/cancel (finished)", do("POST", "/b/upgrade/tasks/fixture-batch-0001/cancel", ""))
	save("batch_pause_done", "POST /b/upgrade/tasks/{id}/pause (finished)", do("POST", "/b/upgrade/tasks/fixture-batch-0001/pause", ""))
	save("batch_settings", "GET /b/upgrade/settings", do("GET", "/b/upgrade/settings", ""))
	_ = http.StatusOK
	_ = json.Marshal
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

var _ = fmt.Sprint
