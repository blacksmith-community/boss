package main

import (
	"context"
	"testing"

	env "github.com/jhunt/go-envirotron"
)

func TestCreateSendsTheConfiguredOrgAndSpace(t *testing.T) {
	b := osbBroker(t)
	c := b.client()
	c.Org, c.Space = "fixture-org", "fixture-space"
	r, _, _ := testRunner(c, "", true)

	if err := r.create([]string{"redis/small"}, "new-one", "", false); err != nil {
		t.Fatal(err)
	}

	body := lastBody(t, b, "PUT")
	if body["organization_guid"] != "fixture-org" || body["space_guid"] != "fixture-space" {
		t.Errorf("body: %v", body)
	}
}

func TestOrgAndSpaceDefaultToBoss(t *testing.T) {
	t.Setenv("BLACKSMITH_ORG", "")
	t.Setenv("BLACKSMITH_SPACE", "")
	saved := opt
	t.Cleanup(func() { opt = saved })
	opt.Org, opt.Space = "", ""
	env.Override(&opt)

	c := connect(context.Background())
	if c.org() != "boss" || c.space() != "boss" {
		t.Errorf("got %q and %q", c.org(), c.space())
	}
}

func TestOrgAndSpaceComeFromTheEnvironment(t *testing.T) {
	t.Setenv("BLACKSMITH_ORG", "env-org")
	t.Setenv("BLACKSMITH_SPACE", "env-space")
	saved := opt
	t.Cleanup(func() { opt = saved })
	opt.Org, opt.Space = "", ""
	env.Override(&opt)

	c := connect(context.Background())
	if c.Org != "env-org" || c.Space != "env-space" {
		t.Errorf("got %q and %q", c.Org, c.Space)
	}
}
