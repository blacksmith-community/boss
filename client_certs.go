package main

import (
	"fmt"
	"net/url"
	"time"
)

// CertOwner names what holds a certificate.
type CertOwner struct {
	Kind         string `json:"kind"`
	InstanceID   string `json:"instance_id"`
	InstanceName string `json:"instance_name"`
	Deployment   string `json:"deployment"`
}

// CertFinding is something the inventory noticed about a certificate.
type CertFinding struct {
	Code     string `json:"code"`
	Severity string `json:"severity"`
	Message  string `json:"message"`
}

// Certificate is one entry of the broker's certificate inventory.
type Certificate struct {
	ID                string        `json:"id"`
	Key               string        `json:"key"`
	Owners            []CertOwner   `json:"owners"`
	AffectedInstances int           `json:"affected_instances"`
	Variable          string        `json:"variable"`
	Kind              string        `json:"kind"`
	CommonName        string        `json:"common_name"`
	Issuer            string        `json:"issuer"`
	DNSNames          []string      `json:"dns_names"`
	EffectiveNotAfter *time.Time    `json:"effective_not_after"`
	DaysLeft          *int          `json:"days_left"`
	Expiry            string        `json:"expiry"`
	Health            string        `json:"health"`
	Attention         bool          `json:"attention"`
	Findings          []CertFinding `json:"findings"`
	Rotation          struct {
		Allowed bool     `json:"allowed"`
		Modes   []string `json:"modes"`
		Reasons []string `json:"reasons"`
	} `json:"rotation"`
}

// CertSummary counts what an inventory holds.
type CertSummary struct {
	Certificates       int `json:"certificates"`
	AffectedInstances  int `json:"affected_instances"`
	Expired            int `json:"expired"`
	Critical           int `json:"critical"`
	Warning            int `json:"warning"`
	ExpiryOK           int `json:"expiry_ok"`
	ExpiryUnknown      int `json:"expiry_unknown"`
	HealthErrors       int `json:"health_errors"`
	NeedsAttention     int `json:"needs_attention"`
	Unconverged        int `json:"unconverged"`
	Reverted           int `json:"reverted"`
	Stale              int `json:"stale"`
	IncompleteCoverage int `json:"incomplete_coverage"`
}

// CertInventory is the answer to GET /b/certificates/inventory.
type CertInventory struct {
	GeneratedAt time.Time   `json:"generated_at"`
	Summary     CertSummary `json:"summary"`
	// Filtered counts what the request's filters matched. The broker
	// sends it whether or not a filter was set.
	Filtered     CertSummary   `json:"filtered"`
	Certificates []Certificate `json:"certificates"`
}

// CertFilter holds the inventory filters the broker accepts. A blank
// field is left out.
type CertFilter map[string]string

// CertInventory reads the certificate inventory.
func (c Client) CertInventory(f CertFilter) (CertInventory, error) {
	q := url.Values{}
	for k, v := range f {
		if v != "" {
			q.Set(k, v)
		}
	}

	path := "/b/certificates/inventory"
	if len(q) > 0 {
		path += "?" + q.Encode()
	}

	var out CertInventory
	if _, err := c.request("GET", path, nil, &out); err != nil {
		return out, fmt.Errorf("failed to read the certificate inventory: %w", err)
	}
	return out, nil
}

// CertDetail is one certificate with its PEM and chain.
type CertDetail struct {
	Certificate
	PEM         string   `json:"pem"`
	IssuerChain []string `json:"issuer_chain"`
}

// CertItem reads one certificate from /b/certificates/items/{id}.
func (c Client) CertItem(id string) (CertDetail, error) {
	var out CertDetail
	if _, err := c.request("GET", "/b/certificates/items/"+url.PathEscape(id), nil, &out); err != nil {
		return out, fmt.Errorf("failed to read certificate %s: %w", id, err)
	}
	return out, nil
}

// CertInstance is the certificates of one service instance.
type CertInstance struct {
	Instance     interface{}   `json:"instance"`
	Certificates []Certificate `json:"certificates"`
	Rotatable    []string      `json:"rotatable_variables"`
	Summary      CertSummary   `json:"summary"`
}

// CertsOfInstance reads /b/certificates/instances/{id}.
func (c Client) CertsOfInstance(id string) (CertInstance, error) {
	var out CertInstance
	if _, err := c.request("GET", "/b/certificates/instances/"+url.PathEscape(id), nil, &out); err != nil {
		return out, fmt.Errorf("failed to read certificates of %s: %w", id, err)
	}
	return out, nil
}

// CertScan asks the broker to scan for certificates. With no IDs it scans
// everything.
func (c Client) CertScan(instanceIDs []string) (scanID, state string, err error) {
	var in interface{}
	if len(instanceIDs) > 0 {
		in = map[string][]string{"instance_ids": instanceIDs}
	}

	var out struct {
		ScanID string `json:"scan_id"`
		State  string `json:"state"`
	}
	if _, err := c.request("POST", "/b/certificates/scan", in, &out); err != nil {
		return "", "", fmt.Errorf("failed to start a certificate scan: %w", err)
	}
	return out.ScanID, out.State, nil
}

// RotationPreview is what a rotation would do. Asking for it changes
// nothing.
type RotationPreview struct {
	ID        string    `json:"preview_id"`
	ExpiresAt time.Time `json:"expires_at"`
	Instances []struct {
		InstanceID   string `json:"instance_id"`
		InstanceName string `json:"instance_name"`
		Mode         string `json:"mode"`
		VMs          int    `json:"vms"`
		Impact       string `json:"impact"`
		Selected     []struct {
			Name     string `json:"name"`
			DaysLeft int    `json:"days_left"`
		} `json:"selected"`
		Blocked *struct {
			Code    string `json:"code"`
			Message string `json:"message"`
		} `json:"blocked"`
	} `json:"instances"`
	Warnings []string `json:"warnings"`
}

// CertPreview previews a rotation of variables on one instance.
func (c Client) CertPreview(instanceID, mode, converge string, variables []string) (RotationPreview, error) {
	type selection struct {
		InstanceID string   `json:"instance_id"`
		Variables  []string `json:"variables"`
	}
	in := struct {
		Mode       string      `json:"mode"`
		Converge   string      `json:"converge,omitempty"`
		Selections []selection `json:"selections"`
	}{Mode: mode, Converge: converge, Selections: []selection{{InstanceID: instanceID, Variables: variables}}}

	var out RotationPreview
	if _, err := c.request("POST", "/b/certificates/rotation/preview", in, &out); err != nil {
		return out, fmt.Errorf("failed to preview the rotation: %w", err)
	}
	return out, nil
}
