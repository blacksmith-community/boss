//go:build ignore

package blacksmith_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	handlerpkg "blacksmith/internal/handlers/blacksmith"
	"blacksmith/pkg/logger"
	"blacksmith/pkg/reconciler"
	"blacksmith/pkg/testutil"
	vaultPkg "blacksmith/pkg/vault"
)

type zzFailDir struct{ *resurrectionDirector }

func (zzFailDir) EnableResurrection(context.Context, string, bool) error {
	return errors.New("director refused")
}

func TestZZFixtures(t *testing.T) {
	logDir := os.Getenv("FIXLOGS")
	if logDir == "" {
		t.Skip("FIXLOGS not set")
	}
	var logText strings.Builder
	for i := 1; i <= 6; i++ {
		fmt.Fprintf(&logText, "2026-11-14T08:%02d:00Z fixture log line %d\n", i, i)
	}
	if err := os.WriteFile(filepath.Join(logDir, "blacksmith.stdout.log"), []byte(logText.String()), 0o644); err != nil {
		t.Fatal(err)
	}
	save := func(name, route string, rec *httptest.ResponseRecorder) {
		zzSave(t, name, route, rec.Code, rec.Body.Bytes())
	}
	vault := &orphanVault{index: map[string]interface{}{
		"22222222-aaaa-bbbb-cccc-000000000009": orphanData(20*time.Hour, map[string]interface{}{
			"instance_name": "fixture-cache", "deployment_name": "valkey-standalone-22222222", "orphaned_reason": "no_bosh_deployment",
		}),
	}}
	rc := &unclaimedReconciler{report: reconciler.UnclaimedReport{Known: true, AsOf: time.Date(2026, 11, 14, 8, 0, 0, 0, time.UTC), Deployments: []reconciler.UnclaimedDeployment{{InstanceID: "33333333-aaaa-bbbb-cccc-000000000010", DeploymentName: "redis-small-33333333", FirstSeen: time.Date(2026, 11, 13, 8, 0, 0, 0, time.UTC)}}}}
	h := handlerpkg.NewHandler(handlerpkg.Dependencies{Logger: logger.Get().Named("test"), Config: createTestConfig(), Vault: vault, Reconciler: rc})
	rec := httptest.NewRecorder()
	h.GetOrphans(rec, httptest.NewRequestWithContext(t.Context(), "GET", "/b/orphans", nil))
	save("orphans", "GET /b/orphans", rec)
	empty := handlerpkg.NewHandler(handlerpkg.Dependencies{Logger: logger.Get().Named("test"), Config: createTestConfig(), Vault: &orphanVault{index: map[string]interface{}{}}})
	rec = httptest.NewRecorder()
	empty.GetOrphans(rec, httptest.NewRequestWithContext(t.Context(), "GET", "/b/orphans", nil))
	save("orphans_empty", "GET /b/orphans (nothing orphaned)", rec)

	for _, c := range []struct{ name, route, target string }{
		{"logs", "GET /b/blacksmith/logs?limit=3", "/b/blacksmith/logs?limit=3"},
		{"logs_more", "GET /b/blacksmith/logs?limit=3&offset=3", "/b/blacksmith/logs?limit=3&offset=3"},
		{"logs_denied", "GET /b/blacksmith/logs?file=/etc/passwd", "/b/blacksmith/logs?file=/etc/passwd"},
		{"logs_missing", "GET /b/blacksmith/logs?file=<allowed file that does not exist>", "/b/blacksmith/logs?file=" + logDir + "/vault.stdout.log"},
	} {
		rec = httptest.NewRecorder()
		h.GetLogs(rec, httptest.NewRequestWithContext(t.Context(), "GET", c.target, nil))
		save(c.name, c.route, rec)
	}

	rv := &testutil.InstanceVault{Instances: map[string]vaultPkg.Instance{"11111111-aaaa-bbbb-cccc-000000000001": {ServiceID: "redis", PlanID: "redis-plan", DeploymentName: "redis-small-11111111"}}}
	rd := &resurrectionDirector{ScriptedBOSHDirector: testutil.NewScriptedBOSHDirector()}
	rh := handlerpkg.NewHandler(handlerpkg.Dependencies{Logger: logger.Get().Named("test"), Config: createTestConfig(), Vault: rv, Director: rd})
	rec = httptest.NewRecorder()
	rh.ToggleResurrection(rec, httptest.NewRequestWithContext(t.Context(), "PUT", "/b/11111111-aaaa-bbbb-cccc-000000000001/resurrection", strings.NewReader(`{"enabled": false}`)))
	save("resurrection_off", "PUT /b/{id}/resurrection {enabled:false}", rec)
	rec = httptest.NewRecorder()
	rh.ToggleResurrection(rec, httptest.NewRequestWithContext(t.Context(), "PUT", "/b/11111111-aaaa-bbbb-cccc-000000000001/resurrection", strings.NewReader(`{"enabled": true}`)))
	save("resurrection_on", "PUT /b/{id}/resurrection {enabled:true}", rec)
	rec = httptest.NewRecorder()
	rh.DeleteResurrectionConfig(rec, httptest.NewRequestWithContext(t.Context(), "DELETE", "/b/11111111-aaaa-bbbb-cccc-000000000001/resurrection", nil))
	save("resurrection_reset", "DELETE /b/{id}/resurrection", rec)
	rec = httptest.NewRecorder()
	rh.ToggleResurrection(rec, httptest.NewRequestWithContext(t.Context(), "PUT", "/b/nope/resurrection", strings.NewReader(`{"enabled": true}`)))
	save("resurrection_missing", "PUT /b/{id}/resurrection (unknown instance)", rec)
	fh := handlerpkg.NewHandler(handlerpkg.Dependencies{Logger: logger.Get().Named("test"), Config: createTestConfig(), Vault: rv, Director: zzFailDir{rd}})
	rec = httptest.NewRecorder()
	fh.ToggleResurrection(rec, httptest.NewRequestWithContext(t.Context(), "PUT", "/b/11111111-aaaa-bbbb-cccc-000000000001/resurrection", strings.NewReader(`{"enabled": true}`)))
	save("resurrection_failed", "PUT /b/{id}/resurrection (director refuses)", rec)
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
