package main

import (
	"bytes"
	"errors"
	"io"
	"net"
	"os"
	"strings"
	"testing"
)

const dirtyBytes = "\x1b[31m\u202e"

func isDirty(s string) bool { return strings.ContainsAny(s, "\x1b\u202e") }

func TestACellIsOneLine(t *testing.T) {
	if got := cell("a\nb\tc  d" + dirtyBytes); got != "a b c  d[31m" {
		t.Errorf("cell is %q", got)
	}
}

func TestBailPrintsTheErrorWithoutControlCharacters(t *testing.T) {
	old, oldExit := os.Stderr, exit
	rd, wr, _ := os.Pipe()
	os.Stderr = wr
	code := -1
	exit = func(c int) { code = c }
	bail(errors.New("failed" + dirtyBytes))
	exit, os.Stderr = oldExit, old
	_ = wr.Close()
	got, _ := io.ReadAll(rd)
	if code != 1 || isDirty(string(got)) || !strings.Contains(string(got), "failed") {
		t.Errorf("code %d, stderr %q", code, got)
	}
}

func TestErrandOutputLosesControlCharactersAndKeepsLines(t *testing.T) {
	b := deployBroker(t)
	body := `{"deployment":"d","errand":"smoke","exit_code":0,"stdout":"out1` + `\u001b[31m\n` + `out2","stderr":"err1\u001b[31m\n` + `err2","results":[{"instance":"n","instance_id":"i","exit_code":0,"stdout":"rs\u001b[31m\nrs2","stderr":"re\u001b[31m\nre2"}]}`
	b.route("POST /b/deployments/"+fixtureDeployment+"/errands/smoke", func(int) reply { return reply{Status: 200, Body: body} })
	r, out, _ := testRunner(b.client(), "", true)
	if err := r.errand([]string{fixtureID, "smoke"}, nil); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"out1[31m\nout2", "err1[31m\nerr2", "rs[31m\nrs2", "re[31m\nre2"} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("output lacks %q:\n%q", want, out)
		}
	}
	if isDirty(out.String()) {
		t.Errorf("output %q", out)
	}
}

func TestTheListAndCatalogTablesStayCleanAndKeepPlanLines(t *testing.T) {
	svc := &Service{ID: "s" + dirtyBytes, Name: "svc\nsplit" + dirtyBytes}
	plan := &Plan{ID: "p" + dirtyBytes, Name: "plan" + dirtyBytes}
	instances := []Instance{{ID: "i\n" + dirtyBytes, Service: svc, Plan: plan}}
	catalog := Catalog{Services: []Service{{ID: "s1" + dirtyBytes, Name: "svc" + dirtyBytes, Tags: []string{"t1" + dirtyBytes, "t2"}, Plans: []Plan{{ID: "pa", Name: "alpha" + dirtyBytes}, {ID: "pb", Name: "beta"}}}}}
	for _, long := range []bool{false, true} {
		var buf bytes.Buffer
		instancesTable(&buf, instances, long)
		if isDirty(buf.String()) || strings.Contains(buf.String(), "svc\nsplit") {
			t.Errorf("instances long=%v: %q", long, buf.String())
		}
		buf.Reset()
		catalogTable(&buf, catalog, long)
		lines := strings.Split(buf.String(), "\n")
		alpha, beta := -1, -1
		for i, l := range lines {
			if strings.Contains(l, "alpha") {
				alpha = i
			}
			if strings.Contains(l, "beta") {
				beta = i
			}
		}
		if isDirty(buf.String()) || alpha < 0 || beta != alpha+1 {
			t.Errorf("catalog long=%v keeps plans on one line each: %q", long, buf.String())
		}
	}
}

func TestAnUnexpectedStatusShowsTheStandardTextNotTheServers(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = ln.Close() }()
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			buf := make([]byte, 4096)
			_, _ = c.Read(buf)
			_, _ = io.WriteString(c, "HTTP/1.1 300 "+leakyText+"\r\nContent-Length: 0\r\nConnection: close\r\n\r\n")
			_ = c.Close()
		}
	}()
	c := &Client{URL: "http://" + ln.Addr().String(), Username: "u", Password: "p", MaxRetries: -1}
	_, err = c.Instances()
	if err == nil || leaks(err.Error()) || !strings.Contains(err.Error(), "Multiple Choices") {
		t.Errorf("got %v", err)
	}
}
