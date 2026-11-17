//go:build ignore

package deployments_test

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
	"testing"

	"blacksmith/internal/bosh"
	"blacksmith/internal/bosh/boshtest"
	"blacksmith/internal/handlers/deployments"
	"blacksmith/internal/mutation"
	"blacksmith/pkg/logger"
	"blacksmith/pkg/testutil"
	vaultPkg "blacksmith/pkg/vault"
	"time"
)

type zzDirector struct {
	*testutil.MockBOSHDirector
	errandResult *bosh.ErrandResult
	errandErr    error
	errands      []bosh.Errand
}

func (d *zzDirector) task(op string) (*bosh.Task, error) {
	return &bosh.Task{ID: 5201, State: "queued", Deployment: "fixture-dep"}, nil
}
func (d *zzDirector) RestartDeployment(context.Context, string, bosh.RestartOpts) (*bosh.Task, error) {
	return d.task("restart")
}
func (d *zzDirector) RecreateDeployment(context.Context, string, bosh.RecreateOpts) (*bosh.Task, error) {
	return d.task("recreate")
}
func (d *zzDirector) StopDeployment(context.Context, string, bosh.StopOpts) (*bosh.Task, error) {
	return d.task("stop")
}
func (d *zzDirector) StartDeployment(context.Context, string, bosh.StartOpts) (*bosh.Task, error) {
	return d.task("start")
}
func (d *zzDirector) ListErrands(context.Context, string) ([]bosh.Errand, error) {
	return d.errands, nil
}
func (d *zzDirector) RunErrand(context.Context, string, string, bosh.ErrandOpts) (*bosh.ErrandResult, error) {
	return d.errandResult, d.errandErr
}

func TestZZFixtures(t *testing.T) {
	save := func(name, route string, rec *httptest.ResponseRecorder) {
		zzSave(t, name, route, rec.Code, rec.Body.Bytes())
	}

	// instance actions
	for _, action := range []string{"recreate", "restart"} {
		setup := newInstanceActionSetup(deployments.Dependencies{})
		rec := serveInstanceAction(t, setup.handler, http.MethodPost, "/b/"+ownedInstance+"/"+action)
		save("action_"+action, "POST /b/{id}/"+action, rec)
	}

	setup := newInstanceActionSetup(deployments.Dependencies{})
	setup.director.running = []bosh.Task{runningTask()}
	save("action_locked", "POST /b/{id}/recreate (director task running)", serveInstanceAction(t, setup.handler, http.MethodPost, "/b/"+ownedInstance+"/recreate"))

	setup = newInstanceActionSetup(deployments.Dependencies{})
	release, _ := setup.guard.Acquire(ownedInstance, "", mutation.Holder{Kind: mutation.KindUpgrade, Description: "upgrade of 7a1b"})
	save("action_busy", "POST /b/{id}/restart (instance held)", serveInstanceAction(t, setup.handler, http.MethodPost, "/b/"+ownedInstance+"/restart"))
	release()

	setup = newInstanceActionSetup(deployments.Dependencies{})
	save("action_not_found", "POST /b/{id}/recreate (not ours)", serveInstanceAction(t, setup.handler, http.MethodPost, "/b/9f9f-not-ours/recreate"))

	setup = newInstanceActionSetup(deployments.Dependencies{})
	setup.director.fail = errInstanceDirectorRefused
	save("action_refused", "POST /b/{id}/restart (director refused)", serveInstanceAction(t, setup.handler, http.MethodPost, "/b/"+ownedInstance+"/restart"))

	adapter := newAdapterSetup(t, func(w http.ResponseWriter, r *http.Request) bool {
		if r.Method != http.MethodPut || r.URL.Path != "/deployments/"+ownedDeployment+"/jobs/*" {
			return false
		}
		boshtest.DropAnswer(w)
		return true
	})
	save("action_unknown", "POST /b/{id}/recreate (answer lost)", serveInstanceAction(t, adapter.handler, http.MethodPost, "/b/"+ownedInstance+"/recreate"))

	// deployment lifecycle and errands
	dir := &zzDirector{MockBOSHDirector: testutil.NewMockBOSHDirector(), errands: []bosh.Errand{{Name: "smoke-tests"}, {Name: "cleanup"}}}
	// The broker's index lists fixture-dep as the deployment of a service
	// instance, so the ownership check lets the actions through.
	index := &testutil.InstanceVault{Instances: map[string]vaultPkg.Instance{
		"7a1b": {ID: "7a1b", ServiceID: "redis", PlanID: "small", DeploymentName: "fixture-dep"},
	}}
	h := deployments.NewHandler(deployments.Dependencies{Logger: logger.NewNoOpLogger(), Director: dir, Vault: index})
	do := func(method, target string) *httptest.ResponseRecorder {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequestWithContext(t.Context(), method, target, nil))
		return rec
	}
	for _, op := range []string{"recreate", "restart", "stop", "start"} {
		save("deployment_"+op, "POST /b/deployments/fixture-dep/"+op, do("POST", "/b/deployments/fixture-dep/"+op))
	}
	save("errands", "GET /b/deployments/fixture-dep/errands", do("GET", "/b/deployments/fixture-dep/errands"))

	guard := mutation.NewGuard(time.Now)
	hg := deployments.NewHandler(deployments.Dependencies{Logger: logger.NewNoOpLogger(), Director: dir, Guard: guard, Vault: index})
	rel, _ := guard.Acquire("", "fixture-dep", mutation.Holder{Kind: mutation.KindDeploymentAction, Description: "restart from the UI"})
	rec := httptest.NewRecorder()
	hg.ServeHTTP(rec, httptest.NewRequestWithContext(t.Context(), "POST", "/b/deployments/fixture-dep/stop", nil))
	save("deployment_busy", "POST /b/deployments/fixture-dep/stop (held)", rec)
	rel()

	passing := bosh.ErrandResult{InstanceGroup: "node", InstanceID: "fixture-uuid-0", ExitCode: 0, Stdout: "all fixture checks passed"}
	failing := bosh.ErrandResult{InstanceGroup: "node", InstanceID: "fixture-uuid-1", ExitCode: 1, Stdout: "fixture check failed", Stderr: "fixture stderr"}
	dir.errandResult = &passing
	save("errand_result", "POST .../errands/smoke-tests (passing)", do("POST", "/b/deployments/fixture-dep/errands/smoke-tests"))

	worst := failing
	worst.Results = []bosh.ErrandResult{passing, failing}
	dir.errandResult = &worst
	save("errand_failed", "POST .../errands/smoke-tests (one instance fails)", do("POST", "/b/deployments/fixture-dep/errands/smoke-tests?instances=node/0,node/1"))

	dir.errandResult = &bosh.ErrandResult{Skipped: true}
	save("errand_skipped", "POST .../errands/smoke-tests?when_changed=true (skipped)", do("POST", "/b/deployments/fixture-dep/errands/smoke-tests?when_changed=true"))

	dir.errandResult, dir.errandErr = nil, errors.New("fixture director could not run the errand")
	save("errand_error", "POST .../errands/smoke-tests (500)", do("POST", "/b/deployments/fixture-dep/errands/smoke-tests"))
	dir.errandErr = fmt.Errorf("instance /abc: %w", bosh.ErrInvalidErrandInstance)
	save("errand_bad_instance", "POST .../errands/smoke-tests (400)", do("POST", "/b/deployments/fixture-dep/errands/smoke-tests?instances=abc"))

	save("deployments_not_found", "GET /b/deployments/fixture-dep/unknown", do("GET", "/b/deployments/fixture-dep/unknown"))
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
