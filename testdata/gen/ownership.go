//go:build ignore

package deployments_test

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

// TestZZFixtures asks the real ownership check to change a deployment
// that no service instance of the broker owns.
func TestZZFixtures(t *testing.T) {
	handler, _ := ownershipHandler(managedVault())

	req := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/b/deployments/fixture-dep/restart", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	zzSave(t, "deployment_not_managed", "POST /b/deployments/fixture-dep/restart (not ours)", rec.Code, rec.Body.Bytes())
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
