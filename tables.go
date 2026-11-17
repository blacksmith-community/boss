package main

import (
	"io"

	"github.com/jhunt/go-table"
)

// lines joins names one to a line, cleaning each name so that a name cannot
// add a line of its own, and shows "(none)" for an empty list.
func lines(names []string) string {
	out := ""
	for _, n := range names {
		out += cell(n) + "\n"
	}
	if out == "" {
		return "(none)"
	}
	return out
}

// instancesTable writes the table of the list command.
func instancesTable(w io.Writer, instances []Instance, long bool) {
	var t table.Table
	if long {
		t = table.NewTable("ID", "Service", "(ID)", "Plan", "(ID)")
	} else {
		t = table.NewTable("ID", "Service", "Plan")
	}
	for _, instance := range instances {
		sid, sname := "-", "(unknown)"
		if instance.Service != nil {
			sid, sname = instance.Service.ID, instance.Service.Name
		}
		pid, pname := "-", "(unknown)"
		if instance.Plan != nil {
			pid, pname = instance.Plan.ID, instance.Plan.Name
		}
		if long {
			t.Row(nil, cell(instance.ID), cell(sname), cell(sid), cell(pname), cell(pid))
		} else {
			t.Row(nil, cell(instance.ID), cell(sname), cell(pname))
		}
	}
	t.Output(w)
}

// catalogTable writes the table of the catalog command.
func catalogTable(w io.Writer, catalog Catalog, long bool) {
	var t table.Table
	if long {
		t = table.NewTable("Service", "(ID)", "Plans", "(IDs)", "Tags")
	} else {
		t = table.NewTable("Service", "Plans", "Tags")
	}
	for _, s := range catalog.Services {
		var plans, ids []string
		for _, p := range s.Plans {
			plans = append(plans, p.Name)
			ids = append(ids, p.ID)
		}
		if long {
			t.Row(nil, cell(s.Name), cell(s.ID), lines(plans), lines(ids), lines(s.Tags))
			t.Row(nil, "", "", "", "", "")
		} else {
			t.Row(nil, cell(s.Name), lines(plans), lines(s.Tags))
			t.Row(nil, "", "", "")
		}
	}
	t.Output(w)
}
