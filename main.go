package main

import (
	"os"
	"math/rand"
	"time"
	"strings"

	fmt "github.com/jhunt/go-ansi"
	"github.com/jhunt/go-cli"
	env "github.com/jhunt/go-envirotron"
	"github.com/jhunt/go-table"
)

func bail(e error) {
	if e != nil {
		fmt.Fprintf(os.Stderr, "@R{!!! %s}\n", e)
		os.Exit(1)
	}
}

var opt struct {
	Debug bool `cli:"-D, --debug"`
	Trace bool `cli:"-T, --trace"`
	Help  bool `cli:"-h, --options"`

	URL      string `cli:"-U, --url" env:"BLACKSMITH_URL"`
	Username string `cli:"-u, --username" env:"BLACKSMITH_USERNAME"`
	Password string `cli:"-p, --password" env:"BLACKSMITH_PASSWORD"`

	List struct {
		Long bool `cli:"-l, --long"`
	} `cli:"list, ls"`

	Catalog struct {
		Long bool `cli:"-l, --long"`
	} `cli:"catalog, cat"`

	Create struct {
		ID string `cli:"-i, --id"`
	} `cli:"create, new"`

	Delete   struct{} `cli:"delete, rm"`
	Task     struct{} `cli:"task"`
	Manifest struct{} `cli:"manifest"`
	Creds    struct{} `cli:"creds"`
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
	fmt.Printf("  @G{create}    Deploy a new instance of a service + plan.\n")
	fmt.Printf("  @G{delete}    Delete a deployed service instance.\n")
	fmt.Printf("\n")
	fmt.Printf("  @G{creds}     Print out credentials for a service instance.\n")
	fmt.Printf("  @G{manifest}  Print an instance's BOSH deployment manifest.\n")
	fmt.Printf("  @G{task}      Show the BOSH deployment task for an instance.\n")
	fmt.Printf("\n")
}

func options() {
	fmt.Printf("Options:\n")
	fmt.Printf("\n")
	fmt.Printf("  (these can go anywhere on the command line, by the way...)\n")
	fmt.Printf("\n")
	fmt.Printf("  -h, --options      Show options and usage.  Can be set on a\n")
	fmt.Printf("                  per-command basis for more options.\n")
	fmt.Printf("\n")
	fmt.Printf("  -D, --debug     Enable debugging output.\n")
	fmt.Printf("  -T, --trace     Trace HTTP(s) calls.  Implies --debug.\n")
	fmt.Printf("\n")
	fmt.Printf("  -U, --url       (@Y{required}) URL of Blacksmith\n")
	fmt.Printf("                  Defaults to @W{$BLACKSMITH_URL}\n")
	fmt.Printf("\n")
	fmt.Printf("  -u, --username  (@Y{required}) Blacksmith username.\n")
	fmt.Printf("                  Defaults to @W{$BLACKSMITH_USERNAME}\n")
	fmt.Printf("\n")
	fmt.Printf("  -p, --password  (@Y{required}) Blacksmith password.\n")
	fmt.Printf("                  Defaults to @W{$BLACKSMITH_PASSWORD}\n")
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
		URL:      opt.URL,
		Username: opt.Username,
		Password: opt.Password,
		Debug:    opt.Debug,
		Trace:    opt.Trace,
	}
}

func main() {
	env.Override(&opt)
	command, args, err := cli.Parse(&opt)
	bail(err)

	if opt.Trace {
		opt.Debug = true
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

	case "list":
		if opt.Help {
			usage("@C{list}")
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
			usage("@C{catalog}")
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
			usage("@C{create} @M{service/plan}")
			options()
			os.Exit(0)
		}

		if len(args) != 1 {
			bad("create", "@R{The `service/plan' argument is required.}")
			os.Exit(1)
		}
		l := strings.SplitN(args[0], "/", 2)
		if len(l) != 2 {
			os.Exit(1)
		}

		id := opt.Create.ID
		if id == "" {
			rand.Seed(time.Now().UTC().UnixNano())
			id = RandomName()
		}

		c := connect()
		service, plan, err := c.Plan(l[0], l[1])
		bail(err)
		_, err = c.Create(id, service.ID, plan.ID)
		bail(err)
		fmt.Printf("@G{%s}/@Y{%s} instance @M{%s} created.\n", l[0], l[1], id)
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
			usage("@C{task} @M{instance}")
			options()
			os.Exit(0)
		}

		if len(args) != 1 {
			bad("task", "@R{The `instance' argument is required.}")
			os.Exit(1)
		}

		c := connect()
		id, err := c.Resolve(args[0])
		bail(err)
		creds, err := c.Task(id)
		bail(err)
		fmt.Printf("# @M{%s}\n", id)
		fmt.Printf("%s\n", creds)
		os.Exit(0)

	case "manifest":
		if opt.Help {
			usage("@C{manifest} @M{instance}")
			options()
			os.Exit(0)
		}

		if len(args) != 1 {
			bad("manifest", "@R{The `instance' argument is required.}")
			os.Exit(1)
		}

		c := connect()
		id, err := c.Resolve(args[0])
		bail(err)
		creds, err := c.Manifest(id)
		bail(err)
		fmt.Printf("# @M{%s}\n", id)
		fmt.Printf("%s\n", creds)
		os.Exit(0)

	case "creds":
		if opt.Help {
			usage("@C{creds} @M{instance}")
			options()
			os.Exit(0)
		}

		if len(args) != 1 {
			bad("creds", "@R{The `instance' argument is required.}")
			os.Exit(1)
		}

		c := connect()
		id, err := c.Resolve(args[0])
		bail(err)
		creds, err := c.Creds(id)
		bail(err)
		fmt.Printf("# @M{%s}\n", id)
		fmt.Printf("%s\n", creds)
		os.Exit(0)
	}
}
