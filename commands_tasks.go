package main

import (
	"io"
	"strconv"
	"strings"
	"time"

	table "github.com/jhunt/go-table"
)

// table prints rows under headers.
func (r *runner) table(headers []string, rows [][]string) {
	t := table.NewTable(headers...)
	for _, row := range rows {
		cells := make([]interface{}, len(row))
		for i, c := range row {
			cells[i] = c
		}
		t.Row(nil, cells...)
	}
	t.Output(r.out)
}

// unixTime shows a unix timestamp, or a dash when there is none.
func unixTime(sec int64) string {
	if sec <= 0 {
		return "-"
	}
	return time.Unix(sec, 0).UTC().Format("2006-01-02 15:04:05")
}

// taskID reads a BOSH task number from an argument.
func taskID(s string) (int, error) {
	n, err := strconv.Atoi(s)
	if err != nil || n <= 0 {
		return 0, usageErr("`%s' is not a BOSH task number.", s)
	}
	return n, nil
}

// tasksList prints BOSH tasks.
func (r *runner) tasksList(args []string, f TaskFilter) error {
	if len(args) != 0 {
		return usageErr("The tasks list command takes no arguments.")
	}

	list, err := r.c.Tasks(f)
	if err != nil {
		return err
	}
	if len(list.Tasks) == 0 {
		r.printf("No BOSH tasks found.\n")
		return nil
	}

	rows := make([][]string, 0, len(list.Tasks))
	for _, t := range list.Tasks {
		rows = append(rows, []string{strconv.Itoa(t.ID), t.State, t.Deployment, t.Description, unixTime(t.StartedAt)})
	}
	r.table([]string{"Task", "State", "Deployment", "Description", "Started"}, rows)

	if list.HasMore {
		r.printf("\nMore tasks follow; raise --offset to see them.\n")
	}
	return nil
}

// tasksOutput prints the output of one BOSH task.
func (r *runner) tasksOutput(args []string, kind string) error {
	if len(args) != 1 {
		return usageErr("The `task' argument is required.")
	}
	id, err := taskID(args[0])
	if err != nil {
		return err
	}

	text, err := r.c.TaskOutput(id, kind)
	if err != nil {
		return err
	}

	_, err = io.WriteString(r.out, strings.TrimRight(text, "\n")+"\n")
	return err
}

// tasksCancel cancels a BOSH task.
func (r *runner) tasksCancel(args []string, force bool) error {
	if len(args) != 1 {
		return usageErr("The `task' argument is required.")
	}
	id, err := taskID(args[0])
	if err != nil {
		return err
	}

	if err := r.confirm("Cancel BOSH task %d?", id); err != nil {
		return err
	}

	res, err := r.c.CancelTask(id, force)
	if err != nil {
		return err
	}

	r.printf("%s\n", res.Message)
	return nil
}
