//go:build ignore

package certificates_test

import (
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
	"time"

	"blacksmith/internal/certinventory"
	"blacksmith/internal/handlers/certificates"
	"blacksmith/internal/upgrade"
	"blacksmith/pkg/logger"
)

type zzSvc struct {
	fakeInventory
}

func (z *zzSvc) Response(_ context.Context, flt certinventory.Filter) certinventory.InventoryResponse {
	now := time.Date(2026, 11, 14, 8, 0, 0, 0, time.UTC)
	notAfter := now.Add(20 * 24 * time.Hour)
	days := 20
	owner := certinventory.Owner{Kind: certinventory.OwnerKind("instance"), InstanceID: "11111111-aaaa-bbbb-cccc-000000000001", InstanceName: "fixture-redis", Deployment: "redis-small-11111111", ServiceName: "redis", PlanName: "small"}
	crt := certinventory.Certificate{
		ID: "c0ffee01", Key: "/fixture/redis/server_tls", Owners: []certinventory.Owner{owner}, AffectedInstances: 1,
		Variable: "server_tls", CommonName: "fixture.redis.internal", Issuer: "fixture-ca", DNSNames: []string{"fixture.redis.internal"},
		KeyAlgorithm: "RSA", KeyBits: 2048, EffectiveNotAfter: &notAfter, DaysLeft: &days,
		Expiry: certinventory.ExpiryWarning, Health: certinventory.HealthOK, Attention: true,
		Observations: []certinventory.Observation{{Source: certinventory.SourceCredHub, Owner: owner, Locator: "/fixture/redis/server_tls", NotAfter: &notAfter, State: "ok", ObservedAt: now}},
		Rotation:     certinventory.Rotatability{Allowed: true, Modes: []string{"redeploy", "patch"}},
	}
	return certinventory.InventoryResponse{
		GeneratedAt:  now,
		Summary:      certinventory.Summary{Certificates: 1, AffectedInstances: 1, Warning: 1, NeedsAttention: 1},
		Filtered:     certinventory.Summary{Certificates: 1, AffectedInstances: 1, Warning: 1, NeedsAttention: 1},
		Scan:         certinventory.ScanState{ID: "scan-1", State: "idle", NextAt: now.Add(time.Hour)},
		Certificates: []certinventory.Certificate{crt},
	}
}

func (z *zzSvc) Item(id string) (certinventory.ItemDetail, bool) {
	if id != "c0ffee01" {
		return certinventory.ItemDetail{}, false
	}
	r := z.Response(context.Background(), certinventory.Filter{})
	return certinventory.ItemDetail{Certificate: r.Certificates[0], PEM: "-----BEGIN CERTIFICATE-----\nZml4dHVyZQ==\n-----END CERTIFICATE-----\n"}, true
}

type zzPrev struct{}

func (zzPrev) PreviewRotation(_ context.Context, req upgrade.PreviewRequest) (*upgrade.Preview, error) {
	return &upgrade.Preview{
		ID: "fixture-preview-1", ExpiresAt: time.Date(2026, 11, 14, 8, 15, 0, 0, time.UTC),
		Instances: []upgrade.PreviewInstance{{InstanceID: "11111111-aaaa-bbbb-cccc-000000000001", InstanceName: "fixture-redis", Service: "redis", Plan: "small", VMs: 1, Mode: req.Mode, Impact: "restarts one VM", Selected: []upgrade.PreviewVariable{{Name: "server_tls", DaysLeft: 20}}}},
		Warnings:  []string{"fixture warning text"},
	}, nil
}

func TestZZFixtures(t *testing.T) {
	svc := &zzSvc{fakeInventory{enabled: true, scanID: "scan-2", scanRunning: false}}
	h := certificates.NewInventoryHandler(svc, zzPrev{}, logger.NewNoOpLogger())
	do := func(h http.Handler, method, target, body string) *httptest.ResponseRecorder {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequestWithContext(t.Context(), method, target, strings.NewReader(body)))
		return rec
	}
	save := func(name, route string, rec *httptest.ResponseRecorder) {
		zzSave(t, name, route, rec.Code, rec.Body.Bytes())
	}
	save("cert_inventory", "GET /b/certificates/inventory", do(h, "GET", "/b/certificates/inventory", ""))
	save("cert_item", "GET /b/certificates/items/{id}", do(h, "GET", "/b/certificates/items/c0ffee01", ""))
	save("cert_item_missing", "GET /b/certificates/items/{id} (unknown)", do(h, "GET", "/b/certificates/items/nope", ""))
	save("cert_instance", "GET /b/certificates/instances/{id}", do(h, "GET", "/b/certificates/instances/11111111-aaaa-bbbb-cccc-000000000001", ""))
	save("cert_scan", "POST /b/certificates/scan", do(h, "POST", "/b/certificates/scan", `{"instance_ids":["11111111-aaaa-bbbb-cccc-000000000001"]}`))
	save("cert_preview", "POST /b/certificates/rotation/preview", do(h, "POST", "/b/certificates/rotation/preview", `{"mode":"redeploy","selections":[{"instance_id":"11111111-aaaa-bbbb-cccc-000000000001","variables":["server_tls"]}]}`))
	save("cert_bad_filter", "GET /b/certificates/inventory?expiry=soon", do(h, "GET", "/b/certificates/inventory?expiry=soon", ""))
	off := certificates.NewInventoryHandler(&zzSvc{fakeInventory{enabled: false}}, nil, logger.NewNoOpLogger())
	save("cert_disabled", "GET /b/certificates/inventory (inventory off)", do(off, "GET", "/b/certificates/inventory", ""))
	_ = bytes.NewReader
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
