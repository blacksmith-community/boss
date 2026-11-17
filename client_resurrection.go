package main

import "fmt"

// Resurrection turns BOSH resurrection on or off for an instance's
// deployment with PUT /b/{id}/resurrection. The ID may be "blacksmith" for
// the broker's own deployment.
func (c Client) Resurrection(id string, enabled bool) (string, error) {
	var out struct {
		Message string `json:"message"`
	}
	in := map[string]bool{"enabled": enabled}
	if _, err := c.request("PUT", instancePath(id, "/resurrection"), in, &out); err != nil {
		return "", fmt.Errorf("failed to set resurrection for %s: %w", id, err)
	}
	return out.Message, nil
}

// ResetResurrection removes the resurrection setting Blacksmith made, so
// the director's default applies, with DELETE /b/{id}/resurrection.
func (c Client) ResetResurrection(id string) (string, error) {
	var out struct {
		Message string `json:"message"`
	}
	if _, err := c.request("DELETE", instancePath(id, "/resurrection"), nil, &out); err != nil {
		return "", fmt.Errorf("failed to reset resurrection for %s: %w", id, err)
	}
	return out.Message, nil
}
