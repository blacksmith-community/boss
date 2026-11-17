BOSS - Blacksmith On-demand ServiceS
====================================

`boss` is a command-line utility for interacting with [Blacksmith][bs], in case you don't have a Cloud Foundry or Kubernetes cluster handy. It's great fun at demos.

![boss is not in any way affiliate with THE BOSS, Bruce Springsteen](boss.jpg)

## Connecting

Boss needs the broker's URL and its basic-auth credentials. Each setting has a flag and an environment variable.

| Flag | Environment variable | Meaning |
|------|----------------------|---------|
| `-U`, `--url` | `BLACKSMITH_URL` | Broker URL |
| `-u`, `--username` | `BLACKSMITH_USERNAME` | Broker username |
| `-p`, `--password` | `BLACKSMITH_PASSWORD` | Broker password |
| `-k`, `--skip-ssl-validation` | `BLACKSMITH_SKIP_VERIFY` | Accept a self-signed certificate |
| `--org` | `BLACKSMITH_ORG` | Organization sent by `create`, which defaults to `boss` |
| `--space` | `BLACKSMITH_SPACE` | Space sent by `create`, which defaults to `boss` |
| `--command-key` | `BLACKSMITH_COMMAND_KEY` | Second credential for batch command jobs |
| `--command-key-file` | `BLACKSMITH_COMMAND_KEY_FILE` | File whose first line holds the command key |
| `--timeout` | `BOSS_TIMEOUT` | How long one request may take, such as `90s` or `5m` |
| `--deployment` | none | A director deployment named outright, for the deployment commands |
| `-y`, `--yes` | none | Skip the confirmation questions |

A command key given on the command line stays in the shell history and shows in the process list, so we prefer the environment variable or the key file. Boss sends the key only on the batch command routes, and never writes it to a log.

Boss gives a read 30 seconds. It gives a call that waits on BOSH, which means an errand, a restart, a recreate, a stop, a start, or a delete, 15 minutes. The `--timeout` flag or `BOSS_TIMEOUT` sets one limit for every call, as a duration such as `90s` or `5m`, or as a number of seconds. When a call that changes something times out, is cancelled with Ctrl-C, or loses its connection after it was sent, Boss says the outcome is unknown and points us to `boss tasks list`, because the broker may have started the work. When the connection fails before anything is sent, Boss says that nothing was sent, so running the command again is safe.

## Commands

Every command accepts `-h` for its own help. Where a command takes an instance, we give its full ID or its exact name. Boss never matches a prefix or part of a name. A name that no instance has, a name that two instances share, an empty argument, or a failed lookup stops the command before it sends anything.

| Command | What it does |
|---------|--------------|
| `boss catalog` | Prints the services and plans the broker offers |
| `boss list` | Lists the service instances |
| `boss log` | Prints the broker's own log, with `--file`, `--limit`, and `--offset` |
| `boss create SERVICE/PLAN` | Provisions an instance, with `-i ID`, `-c PARAMS`, and `-f` |
| `boss update INSTANCE` | Changes parameters with `-c PARAMS`, or the plan with `--plan NAME` |
| `boss delete INSTANCE` | Deletes an instance |
| `boss task INSTANCE` | Prints the latest BOSH deployment task of an instance, and follows it with `-f` |
| `boss manifest INSTANCE` | Prints the BOSH manifest of an instance |
| `boss creds INSTANCE` | Prints the credentials of an instance |
| `boss redeploy INSTANCE` | Recreates the VMs of an instance, or restarts its jobs with `--restart`, and waits for the task with `-f` |
| `boss tasks list` | Lists BOSH tasks, with `--type` set to `recent`, `current`, or `all`, plus `--state`, `--limit`, and `--offset` |
| `boss tasks output TASK` | Prints the output of a BOSH task, with `--type` set to `task`, `debug`, `result`, or `event` |
| `boss tasks cancel TASK` | Cancels a BOSH task, and `--force` passes the broker's force option |
| `boss batch list` | Lists the broker's batch jobs |
| `boss batch show JOB` | Shows one batch job and its instances |
| `boss batch pause JOB` | Pauses a running batch job |
| `boss batch resume JOB` | Resumes a paused batch job |
| `boss batch cancel JOB` | Cancels a running or paused batch job |
| `boss certificates list` | Prints the certificate inventory, with filters such as `--expiry`, `--health`, and `--instance` |
| `boss certificates show ID` | Shows one certificate |
| `boss certificates instance INSTANCE` | Shows the certificates of one instance |
| `boss certificates scan` | Starts a certificate scan, of one or more instances with `--instance` |
| `boss certificates preview INSTANCE` | Shows what a rotation would do, using `--variable` and `--mode`, and changes nothing |
| `boss deployment restart INSTANCE` | Restarts the jobs on every VM of a deployment |
| `boss deployment recreate INSTANCE` | Recreates every VM of a deployment |
| `boss deployment stop INSTANCE` | Stops every VM of a deployment |
| `boss deployment start INSTANCE` | Starts the VMs of a deployment again |
| `boss deployment errands INSTANCE` | Lists the errands of a deployment |
| `boss deployment errand INSTANCE ERRAND` | Runs an errand, on chosen instances with `--instance` |
| `boss resurrection on INSTANCE` | Turns BOSH resurrection on |
| `boss resurrection off INSTANCE` | Turns BOSH resurrection off |
| `boss resurrection reset INSTANCE` | Removes the setting, so the director's default applies |
| `boss monitor` | Prints the VM monitor summary |
| `boss orphans` | Lists orphaned instances and unclaimed deployments |

The deployment commands act on the deployment of the instance we name. The broker does not check who owns a deployment on these routes, so Boss never takes a bare deployment name as an argument. To act on a director deployment that no instance owns, we leave the instance out and pass `--deployment NAME`, and the confirmation question names that deployment. The resurrection commands also accept `blacksmith` for the broker's own deployment.

The word `redeploy` covers two different actions. By default it recreates every VM of the instance, which calls the broker's `recreate` route. With `--restart` it restarts the jobs and leaves the VMs alone, which calls the `restart` route. The help text for `boss redeploy -h` says the same.

## Creating and updating

The `-c` option takes parameters for the service. We can give the JSON itself, or the path of a JSON or YAML file.

```
boss create redis/small -c '{"maxmemory": "256mb"}'
boss create redis/small -c params.yml
boss update relaxed-tesla -c params.yml
boss update relaxed-tesla --plan large
```

Both `create` and `update` ask for confirmation before they send anything. The plan option names a plan of the instance's own service, and Boss sends the old plan as the OSB `previous_values` so the broker can tell what changed. Boss refuses a plan change when the catalog does not mark the service `plan_updateable`, and when the instance already has the plan we name.

## Safety

Every command that changes something asks first. That covers create, update, delete, redeploy, task cancel, the batch actions, certificate scans, the deployment actions and errands, and resurrection. We answer `y` or `yes` to go ahead, and anything else, including end of input, stops the command with nothing changed. The `--yes` flag skips the question for scripts.

Boss prints credentials only when we run `boss creds`. Debug and trace output hide the `Authorization` header, the command key, the parameters of create and update, and the bodies of credentials, manifests, BOSH task output, batch job detail, and errand results. Boss never writes credentials to a log file.

Boss repeats a request after a server error or a dropped connection only when the request reads, which means a GET or a HEAD. A request that changes something is never sent a second time, with one exception. When the connection is refused before anything reaches the broker, any request may go again. When the broker refuses a deployment action because another task holds the deployment, or because the outcome is unknown, Boss reports the refusal and does not send the request again.

A delete resolves the instance first and sends its service and plan. When the broker answers that the instance is gone, Boss says it was not found and exits with an error, because we cannot tell a deleted instance from a mistyped one. An errand that ran and exited with a non-zero code prints its output and the code, and then exits with an error.

## What the broker must provide

Boss talks to the Blacksmith broker and expects the Open Service Broker API at version 2.16 or later for `create`, `update`, `delete`, and `catalog`.

The other commands follow the broker's own `/b/` routes, and the table shows how each one behaves against the older v1.8.2 broker and against the development line that follows it.

| Command | v1.8.2 | Newer line |
|---------|--------|------------|
| `tasks`, `monitor`, and the `deployment` commands | Works, and we checked the response shapes. The task list is a bare array with no paging hints. | Works, with paging hints |
| `batch`, `certificates`, and `resurrection` | The routes exist, but we checked response shapes only against the newer line | Works |
| `redeploy` | Falls back to the deployment recreate and restart routes, using the deployment name from the broker's index | Uses the per-instance routes |
| `orphans` | Not served, so Boss says the broker is too old | Works |

When a broker answers a route with the plain-text 404 `endpoint not found`, Boss names the command and says the broker is too old for it. An ordinary JSON not-found answer, such as an unknown instance, is reported as it is.

The certificate commands need the certificate inventory switched on in the broker. When it is off, the broker answers with an `inventory_disabled` refusal and Boss prints it.

## Limits worth knowing

- The broker does not implement the OSB update call yet, so it answers every update with a 500 that says `not implemented`. Boss turns that into the message that the broker does not support updates yet, and it sends the update only once.

- Brokers that hold no credentials for an instance used to answer with placeholder values in place of an error. Boss recognizes that placeholder set, refuses to print it, and exits with an error.

- The broker's `POST /b/cleanup` route answers that it is not implemented, so Boss never calls it. The `boss orphans` command only reads the report. It cannot remove anything.

- Batch command jobs ask for a second credential. Give it with `BLACKSMITH_COMMAND_KEY` or `--command-key-file` before we run `boss batch show` or `boss batch resume` on one.

- Following with `-f` waits for the real state of the work and never guesses from the log. A create or an update reads the broker's last operation answer, and a redeploy or `boss task -f` reads the BOSH task itself until it is done, failed, cancelled, or timed out. The task log only shows what is happening, so a task that waits behind a lock, or goes quiet between stages, keeps the follow running. The command exits with an error when the work fails, and it exits with status 130 when we press Ctrl-C, because the work is then still running at the broker.

## Not yet supported

Boss does not start new batch jobs of any kind. That includes upgrades, certificate rotations, converges, and run-command jobs. Previewing a rotation works, and starting one does not. We can still list, show, pause, resume, and cancel jobs that someone started in the broker's web interface.

## Examples

We can view the Blacksmith catalog:

```
→ boss catalog
Service     Plans                 Tags
=======     =====                 ====
mariadb     standalone            blacksmith
                                  dedicated
                                  mariadb

redis       dedicated-cache       blacksmith
            dedicated-persistent  dedicated
                                  redis
```

Or see what we've provisioned so far:

```
→ boss ls -l
ID                 Service   (ID)      Plan       (ID)
==                 =======   ====      ====       ====
relaxed-tesla      rabbitmq  rabbitmq  dedicated  rabbitmq-dedicated
agitated-jennings  rabbitmq  rabbitmq  dedicated  rabbitmq-dedicated
```

We can create a service and watch its deployment task:

```
→ boss create rabbitmq/dedicated -f
Create instance ecstatic-yonath of rabbitmq/dedicated in org boss and space boss? [y/N] y
rabbitmq/dedicated instance ecstatic-yonath created.

tailing deployment task log...
03:25:01 | Preparing deployment: Preparing deployment started
03:25:03 | Preparing deployment: Preparing deployment finished
03:26:18 | Creating missing vms: standalone/58bdb1b3 (0) finished
```

We can recreate the VMs of an instance, and then follow the task the director started. The `boss redeploy --follow` form does both steps in one command.

```
→ boss redeploy relaxed-tesla
Recreate the VMs of instance relaxed-tesla? [y/N] y
recreate of deployment rabbitmq-dedicated-relaxed-tesla started as BOSH task 10731 (queued).
→ boss task -f relaxed-tesla
```

And we can look at certificates that need attention:

```
→ boss certificates list --attention
```

## Troubleshooting

- **Connection errors.** Check the Blacksmith URL and the credentials.

- **SSL errors.** Use `--skip-ssl-validation` for a self-signed certificate.

- **The broker returns a code such as `deployment_locked`.** The code comes from the broker. For that one, we wait for the task it names and then run the command again.

Debug mode prints each request and a redacted view of each response.

```
boss --debug catalog
```

Trace mode adds the HTTP status and headers, with credentials hidden.

```
boss --trace catalog
```

## How Do I Contribute?

1. Fork this repo.

2. Create your feature branch (`git checkout -b my-new-feature`).

3. Commit your changes (`git commit -am 'Added some feature'`).

4. Push to the branch (`git push origin my-new-feature`).

5. Create a new Pull Request in Github.

6. Profit!

Boss tests run against responses that the broker's own handlers produced, served by `httptest` and kept under `testdata/`. The README in that folder says how we made each file and which broker version it came from. Run `make test` before sending a change.

[bs]: https://github.com/blacksmith-community/blacksmith
