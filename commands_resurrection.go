package main

// resurrection turns resurrection on or off, or removes the setting.
// action is on, off, or reset.
func (r *runner) resurrection(args []string, action string) error {
	if len(args) != 1 {
		return usageErr("The `instance' argument is required; use `blacksmith' for the broker itself.")
	}

	id := args[0]
	if id != "blacksmith" {
		resolved, err := r.c.Resolve(id)
		if err != nil {
			return err
		}
		id = resolved
	}

	var (
		msg string
		err error
	)
	switch action {
	case "on", "off":
		if err = r.confirm("Turn resurrection %s for %s?", action, id); err != nil {
			return err
		}
		msg, err = r.c.Resurrection(id, action == "on")
	case "reset":
		if err = r.confirm("Remove the resurrection setting for %s, so the director's default applies?", id); err != nil {
			return err
		}
		msg, err = r.c.ResetResurrection(id)
	default:
		return usageErr("The action must be on, off, or reset.")
	}
	if err != nil {
		return err
	}

	r.printf("%s\n", msg)
	return nil
}
