package main

import (
	"context"
	"errors"
	"os"
	"os/signal"

	fmt "github.com/jhunt/go-ansi"
	"github.com/jhunt/go-cli"
	env "github.com/jhunt/go-envirotron"
	"github.com/jhunt/go-table"
)

var Version = "(dev)"

func bail(e error) {
	if e != nil {
		fmt.Fprintf(os.Stderr, "@R{!!! %s}\n", e)
		os.Exit(1)
	}
}

var opt struct {
	Debug bool `cli:"-D, --debug"`
	Trace bool `cli:"-T, --trace"`
	Help  bool `cli:"-h, --help"`

	Version bool `cli:"-v, --version"`

	URL               string `cli:"-U, --url" env:"BLACKSMITH_URL"`
	SkipSSLValidation bool   `cli:"-k, --skip-ssl-validation" env:"BLACKSMITH_SKIP_VERIFY"`
	Username          string `cli:"-u, --username" env:"BLACKSMITH_USERNAME"`
	Password          string `cli:"-p, --password" env:"BLACKSMITH_PASSWORD"`

	Org   string `cli:"--org" env:"BLACKSMITH_ORG"`
	Space string `cli:"--space" env:"BLACKSMITH_SPACE"`

	Yes bool `cli:"-y, --yes"`

	Log struct {
		File   string `cli:"--file"`
		Limit  int    `cli:"--limit"`
		Offset int    `cli:"--offset"`
	} `cli:"log, logs"`

	List struct {
		Long bool `cli:"-l, --long"`
	} `cli:"list, ls"`

	Catalog struct {
		Long bool `cli:"-l, --long"`
	} `cli:"catalog, cat"`

	Create struct {
		ID     string `cli:"-i, --id"`
		Follow bool   `cli:"-f, --follow"`
		Params string `cli:"-c, --params"`
	} `cli:"create, new"`

	Update struct {
		Follow bool   `cli:"-f, --follow"`
		Params string `cli:"-c, --params"`
		Plan   string `cli:"--plan"`
	} `cli:"update"`

	Delete struct{} `cli:"delete, rm"`

	Task struct {
		Follow bool `cli:"-f, --follow"`
	} `cli:"task"`

	Manifest struct{} `cli:"manifest"`

	Creds struct{} `cli:"creds"`

	Redeploy struct {
		Restart bool `cli:"--restart"`
	} `cli:"redeploy"`
}

func usage(f string, args ...interface{}) {
	if f == "" {
		fmt.Printf("Usage: @G{boss} [options] @C{COMMAND} [options]\n\n")
	} else {
		fmt.Printf("Usage: @G{boss} [options] "+f+"\n\n", args...)
	}
}

func commands() {
	fmt.Printf("Commands:\n")
	fmt.Printf("\n")
	fmt.Printf("  @G{list}      Show all deployed service instances.\n")
	fmt.Printf("  @G{catalog}   Print the catalog of services / plans.\n")
	fmt.Printf("  @G{log}       Print the Blacksmith Service Broker log file.\n")
	fmt.Printf("\n")
	fmt.Printf("  @G{create}    Deploy a new instance of a service + plan.\n")
	fmt.Printf("  @G{update}    Update a service instance of a service + plan.\n")
	fmt.Printf("  @G{delete}    Delete a deployed service instance.\n")
	fmt.Printf("\n")
	fmt.Printf("  @G{creds}     Print out credentials for a service instance.\n")
	fmt.Printf("  @G{manifest}  Print an instance's BOSH deployment manifest.\n")
	fmt.Printf("  @G{redeploy}  Recreate the VMs of an instance, or restart its jobs.\n")
	fmt.Printf("  @G{task}      Show the BOSH deployment task for an instance.\n")
	fmt.Printf("\n")
}

func options() {
	fmt.Printf("Options:\n")
	fmt.Printf("\n")
	fmt.Printf("  (these can go anywhere on the command line, by the way...)\n")
	fmt.Printf("\n")
	fmt.Printf("  -h, --help      Show options and usage.  Can be set on a\n")
	fmt.Printf("                  per-command basis for more help.\n")
	fmt.Printf("\n")
	fmt.Printf("  --org NAME      Organization sent when creating an instance.\n")
	fmt.Printf("                  Defaults to @W{$BLACKSMITH_ORG}, then @W{boss}\n")
	fmt.Printf("  --space NAME    Space sent when creating an instance.\n")
	fmt.Printf("                  Defaults to @W{$BLACKSMITH_SPACE}, then @W{boss}\n")
	fmt.Printf("\n")
	fmt.Printf("  -y, --yes       Do not ask before a command changes anything.\n")
	fmt.Printf("\n")
	fmt.Printf("  -D, --debug     Enable debugging output.\n")
	fmt.Printf("  -T, --trace     Trace HTTP(s) calls.  Implies --debug.\n")
	fmt.Printf("\n")
	fmt.Printf("  -U, --url       (@Y{required}) URL of Blacksmith\n")
	fmt.Printf("                  Defaults to @W{$BLACKSMITH_URL}\n")
	fmt.Printf("\n")
	fmt.Printf("  -k, --skip-ssl-validation\n")
	fmt.Printf("                  Skip verification of the API endpoint\n")
	fmt.Printf("                  Defaults to @W{$BLACKSMITH_SKIP_VERIFY}\n")
	fmt.Printf("\n")
	fmt.Printf("  -u, --username  (@Y{required}) Blacksmith username.\n")
	fmt.Printf("                  Defaults to @W{$BLACKSMITH_USERNAME}\n")
	fmt.Printf("\n")
	fmt.Printf("  -p, --password  (@Y{required}) Blacksmith password.\n")
	fmt.Printf("                  Defaults to @W{$BLACKSMITH_PASSWORD}\n")
	fmt.Printf("\n")
}

func list_options() {
	fmt.Printf("Command Options:\n")
	fmt.Printf("\n")
	fmt.Printf("  -l, --long      Display additonal details about service instances\n")
	fmt.Printf("\n")
}

func catalog_options() {
	fmt.Printf("Command Options:\n")
	fmt.Printf("\n")
	fmt.Printf("  -l, --long      Display additonal details about catalog plans\n")
	fmt.Printf("\n")
}

func create_options() {
	fmt.Printf("Command Options:\n")
	fmt.Printf("\n")
	fmt.Printf("  -i, --id        Service instance id\n")
	fmt.Printf("  -f, --follow    Actively display the service log\n")
	fmt.Printf("  -c, --params    Provisioning parameters, as JSON or the path of\n")
	fmt.Printf("                  a JSON or YAML file\n")
	fmt.Printf("\n")
}

func update_options() {
	fmt.Printf("Command Options:\n")
	fmt.Printf("\n")
	fmt.Printf("  -f, --follow    Actively display the service log\n")
	fmt.Printf("  -c, --params    Update parameters, as JSON or the path of\n")
	fmt.Printf("                  a JSON or YAML file\n")
	fmt.Printf("  --plan NAME     Move the instance to another plan of its service\n")
	fmt.Printf("\n")
}

func task_options() {
	fmt.Printf("Command Options:\n")
	fmt.Printf("\n")
	fmt.Printf("  -f, --follow    Actively display the service log\n")
	fmt.Printf("\n")
}

func log_options() {
	fmt.Printf("Command Options:\n")
	fmt.Printf("\n")
	fmt.Printf("  --file PATH     Read one of the broker's allowed log files\n")
	fmt.Printf("                  (default is blacksmith.stdout.log)\n")
	fmt.Printf("  --limit N       Show at most N lines\n")
	fmt.Printf("  --offset N      Skip the first N lines\n")
	fmt.Printf("\n")
}

func redeploy_options() {
	fmt.Printf("Redeploying asks the broker to change the instance's BOSH deployment.\n")
	fmt.Printf("By default it @W{recreates} every VM (POST /b/INSTANCE/recreate).\n")
	fmt.Printf("With @W{--restart} it only restarts the jobs on the VMs instead\n")
	fmt.Printf("(POST /b/INSTANCE/restart).  It asks first unless @W{--yes} is given.\n")
	fmt.Printf("\n")
	fmt.Printf("Command Options:\n")
	fmt.Printf("\n")
	fmt.Printf("  --restart       Restart the jobs, instead of recreating the VMs\n")
	fmt.Printf("\n")
}

func bad(command, msg string, args ...interface{}) {
	fmt.Printf(msg+"\n", args...)
	if command == "" {
		fmt.Printf("Try @W{boss} @C{--help} for more information.\n\n")
	} else {
		fmt.Printf("Try @W{boss} @C{%s -h} for more information.\n\n", command)
	}
}

func connect() *Client {
	return &Client{
		URL:                opt.URL,
		Username:           opt.Username,
		Password:           opt.Password,
		InsecureSkipVerify: opt.SkipSSLValidation,
		Debug:              opt.Debug,
		Trace:              opt.Trace,
		Org:                opt.Org,
		Space:              opt.Space,
	}
}

// finish ends a command that failed. A command line the operator got wrong
// prints the short hint, and anything else prints the error.
func finish(command string, err error) {
	if err == nil {
		return
	}

	var ue usageError
	if errors.As(err, &ue) {
		bad(command, "@R{%s}", ue.msg)
		os.Exit(1)
	}

	fmt.Fprintf(os.Stderr, "@R{!!! %s}\n", err)
	os.Exit(1)
}

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()

	env.Override(&opt)
	command, args, err := cli.Parse(&opt)
	bail(err)

	if opt.Trace {
		opt.Debug = true
	}

	if opt.Version {
		fmt.Printf("boss %s\n", Version)
		os.Exit(0)
	}

	if command == "" && len(args) == 0 {
		opt.Help = true
	}

	if opt.Help && command == "" {
		usage("")
		commands()
		options()
		os.Exit(0)
	}

	switch command {
	default:
		bad("", "@R{Unrecognized command `%s'...}", command)
		os.Exit(1)

	case "":
		bad("", "@R{Unrecognized command `%s'...}", args[0])
		os.Exit(1)

	case "log":
		if opt.Help {
			usage("@C{log} [command_options]|[options]")
			log_options()
			options()
			os.Exit(0)
		}

		finish("log", newRunner(connect(), ctx).log(args, LogOptions{
			File: opt.Log.File, Limit: opt.Log.Limit, Offset: opt.Log.Offset,
		}))
		os.Exit(0)

	case "list":
		if opt.Help {
			usage("@C{list} [command_options]|[options]")
			list_options()
			options()
			os.Exit(0)
		}

		if len(args) != 0 {
			bad("list", "@R{The list command takes no arguments.}")
			os.Exit(1)
		}

		c := connect()
		instances, err := c.Instances()
		bail(err)

		if len(instances) == 0 {
			fmt.Printf("@Y{No Blacksmith service instances found.}\n")
			os.Exit(0)
		}

		if opt.List.Long {
			t := table.NewTable("ID", "Service", "(ID)", "Plan", "(ID)")
			for _, instance := range instances {
				sid := "-"
				sname := "(unknown)"
				if instance.Service != nil {
					sid = instance.Service.ID
					sname = instance.Service.Name
				}

				pid := "-"
				pname := "(unknown)"
				if instance.Plan != nil {
					pid = instance.Plan.ID
					pname = instance.Plan.Name
				}

				t.Row(nil, instance.ID, sname, sid, pname, pid)
			}
			t.Output(os.Stdout)

		} else {
			t := table.NewTable("ID", "Service", "Plan")
			for _, instance := range instances {
				sname := "(unknown)"
				if instance.Service != nil {
					sname = instance.Service.Name
				}

				pname := "(unknown)"
				if instance.Plan != nil {
					pname = instance.Plan.Name
				}

				t.Row(nil, instance.ID, sname, pname)
			}
			t.Output(os.Stdout)

		}

	case "catalog":
		if opt.Help {
			usage("@C{catalog} [command_options]|[options]")
			catalog_options()
			options()
			os.Exit(0)
		}

		if len(args) != 0 {
			bad("catalog", "@R{The catalog command takes no arguments.}")
			os.Exit(1)
		}

		c := connect()
		catalog, err := c.Catalog()

		if opt.Catalog.Long {
			t := table.NewTable("Service", "(ID)", "Plans", "(IDs)", "Tags")
			for _, s := range catalog.Services {

				plans := ""
				ids := ""
				for _, p := range s.Plans {
					plans += fmt.Sprintf("%s\n", p.Name)
					ids += fmt.Sprintf("%s\n", p.ID)
				}
				if plans == "" {
					plans = "(none)"
				}

				tags := ""
				for _, t := range s.Tags {
					tags += fmt.Sprintf("%s\n", t)
				}
				if tags == "" {
					tags = "(none)"
				}

				t.Row(nil, s.Name, s.ID, plans, ids, tags)
				t.Row(nil, "", "", "", "", "")
			}
			t.Output(os.Stdout)

		} else {
			t := table.NewTable("Service", "Plans", "Tags")
			for _, s := range catalog.Services {

				plans := ""
				for _, p := range s.Plans {
					plans += fmt.Sprintf("%s\n", p.Name)
				}
				if plans == "" {
					plans = "(none)"
				}

				tags := ""
				for _, t := range s.Tags {
					tags += fmt.Sprintf("%s\n", t)
				}
				if tags == "" {
					tags = "(none)"
				}

				t.Row(nil, s.Name, plans, tags)
				t.Row(nil, "", "", "")
			}
			t.Output(os.Stdout)
		}
		bail(err)
		os.Exit(0)

	case "create":
		if opt.Help {
			usage("@C{create} @M{service/plan} [command_options]|[options]")
			create_options()
			options()
			os.Exit(0)
		}

		c := connect()
		finish("create", newRunner(c, ctx).create(args, opt.Create.ID, opt.Create.Params, opt.Create.Follow))
		os.Exit(0)

	case "update":
		if opt.Help {
			usage("@C{update} @M{instance} [command_options]|[options]")
			update_options()
			options()
			os.Exit(0)
		}

		c := connect()
		finish("update", newRunner(c, ctx).update(args, opt.Update.Params, opt.Update.Plan, opt.Update.Follow))
		os.Exit(0)

	case "delete":
		if opt.Help {
			usage("@C{delete} @M{instance}")
			options()
			os.Exit(0)
		}

		if len(args) != 1 {
			bad("delete", "@R{The `instance' argument is required.}")
			os.Exit(1)
		}

		c := connect()
		err := c.Delete(args[0])
		bail(err)
		fmt.Printf("@C{%s} instance deleted.\n", args[0])
		os.Exit(0)

	case "task":
		if opt.Help {
			usage("@C{task} @M{instance} [command_options]|[options]")
			task_options()
			options()
			os.Exit(0)
		}

		finish("task", newRunner(connect(), ctx).task(args, opt.Task.Follow))
		os.Exit(0)

	case "manifest":
		if opt.Help {
			usage("@C{manifest} @M{instance}")
			options()
			os.Exit(0)
		}

		finish("manifest", newRunner(connect(), ctx).manifest(args))
		os.Exit(0)

	case "redeploy":
		if opt.Help {
			usage("@C{redeploy} @M{instance} [command_options]|[options]")
			redeploy_options()
			options()
			os.Exit(0)
		}

		finish("redeploy", newRunner(connect(), ctx).redeploy(args, opt.Redeploy.Restart))
		os.Exit(0)

	case "creds":
		if opt.Help {
			usage("@C{creds} @M{instance}")
			options()
			os.Exit(0)
		}

		finish("creds", newRunner(connect(), ctx).creds(args))
		os.Exit(0)
	}
}
