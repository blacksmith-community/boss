package main

import (
	"fmt"
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

// clock shows a time in UTC, or a dash when the time is not set.
func clock(t time.Time) string {
	if t.IsZero() {
		return "-"
	}
	return t.UTC().Format("2006-01-02 15:04:05")
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
	if len(list.Tasks) == 0 && !list.HasMore {
		r.printf("No BOSH tasks found.\n")
		return nil
	}

	if len(list.Tasks) > 0 {
		rows := make([][]string, 0, len(list.Tasks))
		for _, t := range list.Tasks {
			rows = append(rows, []string{strconv.Itoa(t.ID), t.State, t.Deployment, t.Description, clock(t.StartedAt)})
		}
		r.table([]string{"Task", "State", "Deployment", "Description", "Started"}, rows)

		for _, t := range list.Tasks {
			if t.Error != "" {
				// The error is director output for a task, so it prints as sent.
				r.printf("\nTask %d failed: %s\n", t.ID, asSent(t.Error))
			}
		}
	}

	switch {
	case list.HasMore && list.Ceiling:
		r.printf("\nThe broker lists no deeper than this, so older tasks are out of reach. Narrow the list with --state or --type.\n")
	case list.HasMore && !list.Exact:
		r.printf("\nThe broker stopped counting, so more tasks may follow; raise --offset to look further.\n")
	case list.HasMore:
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

	r.printf("%s\n", shownReason(res.Message))
	return nil
}

func progress(done, total int) string {
	return fmt.Sprintf("%d/%d", done, total)
}

// batchKind names a batch job's kind. The broker leaves the kind out for a
// plain stemcell or release upgrade, which was the only kind there was.
func batchKind(kind string) string {
	if kind == "" {
		return "upgrade"
	}
	return kind
}

// batchList prints the batch jobs.
func (r *runner) batchList(args []string) error {
	if len(args) != 0 {
		return usageErr("The batch list command takes no arguments.")
	}

	jobs, err := r.c.BatchTasks()
	if err != nil {
		return err
	}
	if len(jobs) == 0 {
		r.printf("No batch jobs found.\n")
		return nil
	}

	rows := make([][]string, 0, len(jobs))
	for _, j := range jobs {
		rows = append(rows, []string{j.ID, j.Name, batchKind(j.Kind), j.Status, j.Target,
			progress(j.CompletedCount, j.TotalCount), strconv.Itoa(j.FailedCount),
			j.CreatedAt.UTC().Format("2006-01-02 15:04:05")})
	}
	r.table([]string{"ID", "Name", "Kind", "Status", "Target", "Done", "Failed", "Created"}, rows)
	return nil
}

// batchShow prints one batch job and its instances.
func (r *runner) batchShow(args []string) error {
	if len(args) != 1 {
		return usageErr("The `job' argument is required.")
	}
	if strings.TrimSpace(args[0]) == "" {
		return errEmptyJob
	}

	j, err := r.c.BatchTask(args[0])
	if err != nil {
		return err
	}

	state := j.Status
	if j.Paused {
		state += " (paused)"
	}
	r.printf("Job:     %s\n", j.ID)
	if j.Name != "" {
		r.printf("Name:    %s\n", j.Name)
	}
	r.printf("Kind:    %s\n", batchKind(j.Kind))
	r.printf("Status:  %s\n", state)
	if j.PauseReason != "" {
		r.printf("Paused:  %s\n", shownReason(j.PauseReason))
	}
	r.printf("Target:  %s\n", j.target())
	r.printf("Counts:  %d total, %d completed, %d failed, %d unknown, %d skipped, %d cancelled\n",
		j.TotalCount, j.CompletedCount, j.FailedCount, j.UnknownCount, j.SkippedCount, j.CancelledCount)
	created := j.CreatedAt.UTC().Format("2006-01-02 15:04:05")
	if j.CreatedBy != "" {
		created += " by " + j.CreatedBy
	}
	r.printf("Created: %s\n\n", created)

	rows := make([][]string, 0, len(j.Instances))
	for _, i := range j.Instances {
		task := "-"
		if i.BOSHTaskID > 0 {
			task = strconv.Itoa(i.BOSHTaskID)
		}
		rows = append(rows, []string{i.InstanceID, i.DeploymentName, i.Status, task, shownReason(i.Error)})
	}
	r.table([]string{"Instance", "Deployment", "Status", "BOSH task", "Error"}, rows)
	return nil
}

// batchAction pauses, resumes, or cancels a batch job, after asking.
func (r *runner) batchAction(args []string, action string) error {
	if len(args) != 1 {
		return usageErr("The `job' argument is required.")
	}
	if strings.TrimSpace(args[0]) == "" {
		return errEmptyJob
	}

	if err := r.confirm("%s batch job %s?", strings.ToUpper(action[:1])+action[1:], args[0]); err != nil {
		return err
	}

	msg, err := r.c.BatchAction(args[0], action)
	if err != nil {
		return err
	}
	r.printf("%s\n", msg)
	return nil
}
