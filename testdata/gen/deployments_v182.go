//go:build ignore

package deployments_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"blacksmith/internal/bosh"
	"blacksmith/internal/handlers/deployments"
	"blacksmith/pkg/logger"
	"blacksmith/pkg/testutil"
)

type zzDirector struct{ *testutil.MockBOSHDirector }

func (zzDirector) StopDeployment(context.Context, string, bosh.StopOpts) (*bosh.Task, error) {
	return &bosh.Task{ID: 5201, State: "queued", Deployment: "fixture-dep"}, nil
}
func (zzDirector) StartDeployment(context.Context, string, bosh.StartOpts) (*bosh.Task, error) {
	return &bosh.Task{ID: 5201, State: "queued", Deployment: "fixture-dep"}, nil
}
func (zzDirector) ListErrands(context.Context, string) ([]bosh.Errand, error) {
	return []bosh.Errand{{Name: "smoke-tests"}, {Name: "cleanup"}}, nil
}
func (zzDirector) RunErrand(context.Context, string, string, bosh.ErrandOpts) (*bosh.ErrandResult, error) {
	return &bosh.ErrandResult{InstanceGroup: "node", InstanceID: "fixture-uuid-0", ExitCode: 0, Stdout: "all fixture checks passed"}, nil
}

func (zzDirector) task() (*bosh.Task, error) {
	return &bosh.Task{ID: 5201, State: "queued", Deployment: "fixture-dep"}, nil
}
func (d zzDirector) RestartDeployment(context.Context, string, bosh.RestartOpts) (*bosh.Task, error) {
	return d.task()
}
func (d zzDirector) RecreateDeployment(context.Context, string, bosh.RecreateOpts) (*bosh.Task, error) {
	return d.task()
}

func TestZZFixtures(t *testing.T) {
	h := deployments.NewHandler(deployments.Dependencies{Logger: logger.NewNoOpLogger(), Director: zzDirector{testutil.NewMockBOSHDirector()}})
	for _, op := range []string{"recreate", "restart", "stop", "start"} {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequestWithContext(t.Context(), "POST", "/b/deployments/fixture-dep/"+op, nil))
		zzSave(t, "deployment_"+op, "POST /b/deployments/fixture-dep/"+op, rec.Code, rec.Body.Bytes())
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequestWithContext(t.Context(), "GET", "/b/deployments/fixture-dep/errands", nil))
	zzSave(t, "errands", "GET /b/deployments/fixture-dep/errands", rec.Code, rec.Body.Bytes())
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequestWithContext(t.Context(), "POST", "/b/deployments/fixture-dep/errands/smoke-tests", nil))
	zzSave(t, "errand_result", "POST /b/deployments/fixture-dep/errands/smoke-tests", rec.Code, rec.Body.Bytes())
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
