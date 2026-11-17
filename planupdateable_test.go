package main

import (
	"strings"
	"testing"
)

func catalogWith(service, plan *bool) Catalog {
	return Catalog{Services: []Service{{
		ID: "svc", Name: "redis", PlanUpdateable: service,
		Plans: []Plan{{ID: "p1", Name: "small", PlanUpdateable: plan}, {ID: "p2", Name: "large"}},
	}}}
}

func yes() *bool { v := true; return &v }
func no() *bool  { v := false; return &v }

func TestAPlanLevelPlanUpdateableWinsOverTheServiceLevel(t *testing.T) {
	// The broker sets neither today, but the OSB spec lets either one be set.
	cases := []struct {
		name          string
		service, plan *bool
		allowed       bool
	}{
		{"neither", nil, nil, false},
		{"service true", yes(), nil, true},
		{"service false", no(), nil, false},
		{"plan true over service false", no(), yes(), true},
		{"plan true over nothing", nil, yes(), true},
		{"plan false over service true", yes(), no(), false},
	}
	for _, tc := range cases {
		_, err := findPlan(catalogWith(tc.service, tc.plan), "svc", "p1", "large")
		if tc.allowed && err != nil {
			t.Errorf("%s: want the change allowed, got %v", tc.name, err)
		}
		if !tc.allowed && err == nil {
			t.Errorf("%s: want the change refused", tc.name)
		}
	}
}

func TestAMissingPlanUpdateableSaysWhatTheCatalogLeftOut(t *testing.T) {
	_, err := findPlan(catalogWith(nil, nil), "svc", "p1", "large")
	if err == nil {
		t.Fatal("want a refusal")
	}
	for _, want := range []string{"does not mark", "plan_updateable", "missing value as false"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("message lacks %q: %v", want, err)
		}
	}
	if strings.Contains(err.Error(), "is false") {
		t.Errorf("the broker sent no value, so the message must not say it is false: %v", err)
	}
}

func TestAnExplicitFalseSaysTheCatalogSaysFalse(t *testing.T) {
	_, err := findPlan(catalogWith(no(), nil), "svc", "p1", "large")
	if err == nil || !strings.Contains(err.Error(), "plan_updateable is false") {
		t.Errorf("want the explicit false named, got %v", err)
	}
}
