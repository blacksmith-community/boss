package main

import (
	"errors"
	"net/http"
	"testing"
)

func TestOnlyAnInstanceDeleteTreatsA410AsGone(t *testing.T) {
	b := newBroker(t, nil)
	b.route("DELETE /b/other", func(int) reply { return reply{Status: 410, Body: `{}`} })
	b.route("DELETE /v2/service_instances/"+fixtureID, func(int) reply { return reply{Status: 410, Body: `{}`} })
	c := b.client()

	_, _, err := c.rawRequest(http.MethodDelete, "/b/other", nil)
	var api APIError
	if !errors.As(err, &api) || api.Status != 410 {
		t.Errorf("a 410 on any other delete is an error, got %v", err)
	}

	err = c.Delete(InstanceRef{ID: fixtureID, PlanID: "p", ServiceID: "s"})
	if !errors.Is(err, ErrInstanceGone) {
		t.Errorf("an instance delete that gets 410 reports it gone, got %v", err)
	}
}
