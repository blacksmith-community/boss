package main

import (
	"strconv"
	"strings"
)

func certDays(c Certificate) string {
	if c.DaysLeft == nil {
		return "-"
	}
	return strconv.Itoa(*c.DaysLeft)
}

func certOwners(c Certificate) string {
	names := make([]string, 0, len(c.Owners))
	for _, o := range c.Owners {
		switch {
		case o.InstanceName != "":
			names = append(names, o.InstanceName)
		case o.InstanceID != "":
			names = append(names, o.InstanceID)
		default:
			names = append(names, o.Kind)
		}
	}
	if len(names) == 0 {
		return "-"
	}
	return strings.Join(names, ", ")
}

func (r *runner) certTable(certs []Certificate) {
	rows := make([][]string, 0, len(certs))
	for _, c := range certs {
		name := c.CommonName
		if name == "" {
			name = c.Variable
		}
		rows = append(rows, []string{c.ID, c.Kind, name, c.Expiry, certDays(c), c.Health, certOwners(c)})
	}
	r.table([]string{"ID", "Kind", "Name", "Expiry", "Days", "Health", "Owners"}, rows)
}

func (r *runner) certSummary(s CertSummary) {
	r.printf("%d certificates for %d instances: %d expired, %d critical, %d warning, %d ok, %d unknown; %d need attention.\n",
		s.Certificates, s.AffectedInstances, s.Expired, s.Critical, s.Warning, s.ExpiryOK, s.ExpiryUnknown, s.NeedsAttention)
}

// certList prints the certificate inventory.
func (r *runner) certList(args []string, f CertFilter, summaryOnly bool) error {
	if len(args) != 0 {
		return usageErr("The certificates list command takes no arguments.")
	}
	if summaryOnly {
		f["summary_only"] = "true"
	}

	inv, err := r.c.CertInventory(f)
	if err != nil {
		return err
	}

	r.certSummary(inv.Summary)
	if len(inv.Certificates) > 0 {
		r.printf("\n")
		r.certTable(inv.Certificates)
	}
	return nil
}

// certShow prints one certificate.
func (r *runner) certShow(args []string) error {
	if len(args) != 1 {
		return usageErr("The `certificate' argument is required.")
	}

	c, err := r.c.CertItem(args[0])
	if err != nil {
		return err
	}

	r.printf("ID:        %s\n", c.ID)
	r.printf("Key:       %s\n", c.Key)
	r.printf("Kind:      %s\n", c.Kind)
	r.printf("Name:      %s\n", c.CommonName)
	r.printf("Issuer:    %s\n", c.Issuer)
	if c.EffectiveNotAfter != nil {
		r.printf("Expires:   %s (%s days left)\n", c.EffectiveNotAfter.UTC().Format("2006-01-02"), certDays(c.Certificate))
	}
	r.printf("Expiry:    %s\n", c.Expiry)
	r.printf("Health:    %s\n", c.Health)
	r.printf("Owners:    %s\n", certOwners(c.Certificate))
	r.printf("Rotation:  allowed=%t modes=%s\n", c.Rotation.Allowed, strings.Join(c.Rotation.Modes, ","))
	for _, f := range c.Findings {
		r.printf("Finding:   [%s] %s: %s\n", f.Severity, f.Code, f.Message)
	}
	return nil
}

// certInstance prints the certificates of one instance.
func (r *runner) certInstance(args []string) error {
	if len(args) != 1 {
		return usageErr("The `instance' argument is required.")
	}

	id, err := r.c.Resolve(args[0])
	if err != nil {
		return err
	}

	res, err := r.c.CertsOfInstance(id)
	if err != nil {
		return err
	}

	r.printf("# %s\n", id)
	r.certSummary(res.Summary)
	if len(res.Certificates) > 0 {
		r.printf("\n")
		r.certTable(res.Certificates)
	}
	if len(res.Rotatable) > 0 {
		r.printf("\nRotatable variables: %s\n", strings.Join(res.Rotatable, ", "))
	}
	return nil
}

// certScan asks the broker to scan for certificates.
func (r *runner) certScan(args, instances []string) error {
	if len(args) != 0 {
		return usageErr("The certificates scan command takes no arguments; use --instance.")
	}

	ids := make([]string, 0, len(instances))
	for _, i := range instances {
		id, err := r.c.Resolve(i)
		if err != nil {
			return err
		}
		ids = append(ids, id)
	}

	target := "every instance"
	if len(ids) > 0 {
		target = strings.Join(ids, ", ")
	}
	if err := r.confirm("Start a certificate scan of %s?", target); err != nil {
		return err
	}

	scan, state, err := r.c.CertScan(ids)
	if err != nil {
		return err
	}
	r.printf("Scan %s is %s.\n", scan, state)
	return nil
}

// certPreview shows what rotating variables of an instance would do.
func (r *runner) certPreview(args, variables []string, mode, converge string) error {
	if len(args) != 1 {
		return usageErr("The `instance' argument is required.")
	}
	if len(variables) == 0 {
		return usageErr("Name at least one variable with --variable.")
	}
	switch mode {
	case "redeploy", "patch", "converge":
	default:
		return usageErr("The --mode must be redeploy, patch, or converge.")
	}

	id, err := r.c.Resolve(args[0])
	if err != nil {
		return err
	}

	p, err := r.c.CertPreview(id, mode, converge, variables)
	if err != nil {
		return err
	}

	r.printf("Preview %s, good until %s. Nothing was changed.\n", p.ID, p.ExpiresAt.UTC().Format("2006-01-02 15:04:05"))
	for _, i := range p.Instances {
		r.printf("\nInstance %s (%s), mode %s, %d VMs\n", i.InstanceID, i.InstanceName, i.Mode, i.VMs)
		for _, v := range i.Selected {
			r.printf("  rotates %s (%d days left)\n", v.Name, v.DaysLeft)
		}
		if i.Impact != "" {
			r.printf("  impact: %s\n", i.Impact)
		}
		if i.Blocked != nil {
			r.printf("  BLOCKED [%s]: %s\n", i.Blocked.Code, i.Blocked.Message)
		}
	}
	for _, w := range p.Warnings {
		r.printf("\nWarning: %s\n", w)
	}
	return nil
}
