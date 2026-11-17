package main

import "testing"

// A 202 means the broker accepted the work, whatever its body holds.
func TestAnAcceptedAnswerWithAnUnreadableBodyIsStillAccepted(t *testing.T) {
	for name, body := range map[string]string{
		"not JSON":            "accepted, thanks",
		"a numeric operation": `{"operation":12345}`,
		"an object operation": `{"operation":{"id":1}}`,
		"a JSON string only":  `"ok"`,
		"an operation string": `{"operation":"op-1"}`,
	} {
		want := ""
		if name == "an operation string" {
			want = "op-1"
		}

		b := osbBroker(t)
		b.route("PUT /v2/service_instances/new-one", func(int) reply { return reply{Status: 202, Body: body, ContentType: "text/plain"} })
		b.route("PATCH /v2/service_instances/"+fixtureID, func(int) reply { return reply{Status: 202, Body: body, ContentType: "text/plain"} })
		c := b.client()

		created, err := c.Create("new-one", "redis", "small", nil)
		if err != nil || created.Operation != want {
			t.Errorf("%s: create gave %q and %v, want %q and no error", name, created.Operation, err, want)
		}
		updated, err := c.Update(fixtureID, "redis", "small", "", map[string]interface{}{"k": "v"})
		if err != nil || updated.Operation != want {
			t.Errorf("%s: update gave %q and %v, want %q and no error", name, updated.Operation, err, want)
		}
	}
}
