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
| `-y`, `--yes` | none | Skip the confirmation questions |

## Commands

Every command accepts `-h` for its own help. Where a command takes an instance, we can give the full ID or any prefix that matches exactly one instance.

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
| `boss redeploy INSTANCE` | Recreates the VMs of an instance, or restarts its jobs with `--restart` |
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
| `boss deployment restart NAME` | Restarts the jobs on every VM of a deployment |
| `boss deployment recreate NAME` | Recreates every VM of a deployment |
| `boss deployment stop NAME` | Stops every VM of a deployment |
| `boss deployment start NAME` | Starts the VMs of a deployment again |
| `boss deployment errands NAME` | Lists the errands of a deployment |
| `boss deployment errand NAME ERRAND` | Runs an errand, on chosen instances with `--instance` |
| `boss resurrection on INSTANCE` | Turns BOSH resurrection on |
| `boss resurrection off INSTANCE` | Turns BOSH resurrection off |
| `boss resurrection reset INSTANCE` | Removes the setting, so the director's default applies |
| `boss monitor` | Prints the VM monitor summary |
| `boss orphans` | Lists orphaned instances and unclaimed deployments |

The deployment commands take a deployment name, or an instance ID, and the resurrection commands also accept `blacksmith` for the broker's own deployment.

The word `redeploy` covers two different actions. By default it recreates every VM of the instance, which calls the broker's `recreate` route. With `--restart` it restarts the jobs and leaves the VMs alone, which calls the `restart` route. The help text for `boss redeploy -h` says the same.

## Creating and updating

The `-c` option takes parameters for the service. We can give the JSON itself, or the path of a JSON or YAML file.

```
boss create redis/small -c '{"maxmemory": "256mb"}'
boss create redis/small -c params.yml
boss update relaxed-tesla -c params.yml
boss update relaxed-tesla --plan large
```

Both `create` and `update` ask for confirmation before they send anything. The plan option names a plan of the instance's own service, and Boss sends the old plan as the OSB `previous_values` so the broker can tell what changed.

## Safety

Every command that changes something asks first. That covers create, update, delete, redeploy, task cancel, the batch actions, certificate scans, the deployment actions and errands, and resurrection. We answer `y` or `yes` to go ahead, and anything else, including end of input, stops the command with nothing changed. The `--yes` flag skips the question for scripts.

Boss prints credentials only when we run `boss creds`. Debug and trace output hide the `Authorization` header, the command key, the parameters of create and update, and the bodies of credentials, manifests, BOSH task output, batch job detail, and errand results. Boss never writes credentials to a log file.

When the broker refuses a deployment action because another task holds the deployment, or because the outcome is unknown, Boss reports the refusal and does not send the request again.

## What the broker must provide

Boss talks to the Blacksmith broker and expects the Open Service Broker API at version 2.16 or later for `create`, `update`, `delete`, and `catalog`.

The other commands follow the broker's own `/b/` routes. We wrote and tested them against the Blacksmith development line that follows release v1.8.2. The `redeploy` and `orphans` commands need a broker newer than v1.8.2, because v1.8.2 does not serve the per-instance restart and recreate routes or the orphan report. The other commands call routes that v1.8.2 also serves, but we checked response shapes only against the newer line.

The certificate commands need the certificate inventory switched on in the broker. When it is off, the broker answers with an `inventory_disabled` refusal and Boss prints it.

## Limits worth knowing

- The broker does not implement the OSB update call yet, so `boss update --plan` is sent correctly and the broker refuses it. Updating parameters runs into the same limit.

- When the broker has no credentials stored for an instance, it answers with placeholder values in place of an error. Check that the values from `boss creds` look real before using them.

- The broker's `POST /b/cleanup` route answers that it is not implemented, so Boss never calls it. The `boss orphans` command only reads the report. It cannot remove anything.

- Batch command jobs ask for a second credential. Give it with `--command-key` or `BLACKSMITH_COMMAND_KEY` before we run `boss batch show` or `boss batch resume` on one.

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

We can recreate the VMs of an instance, and then follow the task the director started:

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

Boss tests run against recorded broker responses served by `httptest`, kept under `testdata/`. Run `make test` before sending a change.

[bs]: https://github.com/blacksmith-community/blacksmith
