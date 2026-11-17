# Test fixtures

Every file here is a response that a real Blacksmith broker handler wrote. None of them is typed by hand. The Go programs in `gen/` made them. Each program runs the broker's own HTTP handlers, with the broker's own response types, against a scripted director and a stubbed vault, and saves each body exactly as the handler sent it.

The programs ran against two broker checkouts. Files without a prefix came from the broker at commit b61f8696, which is the tip of the broker branch that carries the new routes. Files that start with `v182_` came from the broker at the `v1.8.2` tag, so they show how an older broker answers the same routes. The v1.8.2 files cover the status index, the task list, task output, task cancel, the VM monitor, the errand routes, and the deployment recreate, restart, stop, and start routes. Some of the newer routes do not exist there, and the `v182_` files for those routes hold the plain-text 404 that the old router sends.

Identifiers are fixed so the tests can name them. Instance IDs look like `11111111-aaaa-bbbb-cccc-00000000000N`, and secrets are placeholders such as `fixture-not-a-secret`.

## Regenerating the files

The programs in `gen/` carry the `ignore` build tag, so the Go toolchain skips them here. The script `gen/run.sh` lays each one over the broker with `go test -overlay`, which means it never writes inside either broker checkout. It needs a checkout of the broker at the commit we want, and it takes an optional second checkout at the `v1.8.2` tag.

```
testdata/gen/run.sh /path/to/broker /path/to/output /path/to/broker-at-v1.8.2
```

The script writes the newer responses to `head/` under the output folder, and the v1.8.2 responses to `v182/`. Each folder also holds a `MANIFEST.tsv` that lists the route and the status code behind every file. We copy a wanted file into this folder, and we add the `v182_` prefix to the ones from `v182/`.

A fresh run differs from the committed files in a few values. The broker stamps timestamps from the clock, and the batch list orders jobs by those times, so `status*.json`, `orphans*.json`, and `batch_list.json` change from run to run. The files named `route_*` in the output are probes of routes the tests do not use, and we leave them out.

## What the fixtures leave out

The fixtures cover shapes and status codes. They do not cover timing, because the handlers ran against a scripted director that answers at once. They also cannot show how the broker behaves under load or across a restart.

One file is a slice of a real response. The file `task_log_first.json` holds the first event of `task_log.json`, and the follow test uses it to stand for the answer a task gives early in its run.

A few tests change a recorded response in memory and say so where they do. One adds `plan_updateable` to the catalog, because the broker never sets it, and others mark a step of the task log as failed or move its times to a later day.

The file `params.json` and its twin `params.yml` are inputs that we hand to Boss, so they are not broker responses.

## Where each group came from

- `status*.json`

  The index at `/b/status`. The file `status_many.json` holds four instances, and two of them share a name so the tests can check ambiguity. The file `v182_status.json` comes from the older broker, which does not report an organization, a space, or addresses.

- `bosh_tasks*.json`

  The director task list at `/b/tasks`. Without the `paged` query the broker sends a bare array, and with it the broker sends an object that carries the `has_more`, `exact`, and `ceiling` hints. The older broker answers a bare array even when the request asks for pages.

- `task_log*.json`, `task_output*.json`, and `task_cancel*.json`

  The deployment task log, the output of a task, and the cancel route, including the answers for a finished task and an unknown one.

- `batch_*.json`

  The upgrade batch routes. A single job has no `kind`, while the list summaries do. A request that lacks the command key gets a 401 with the code `command_credential_required`.

- `cert_*.json`

  The certificate inventory, one item, one instance, a scan, a rotation preview, and the error answers.

- `action_*.json` and `deployment_*.json`

  The lifecycle routes for an instance and for a raw director deployment, with the success, locked, busy, refused, and unknown-outcome answers. The `v182_deployment_*` files show that the older broker accepts the deployment routes.

- `errand*.json`

  The errand list and the results of an errand that passes, fails on one instance, is skipped, and fails with a 400 and a 500.

- `resurrection_*.json`, `vm_monitor*.json`, and `orphans*.json`

  The resurrection routes, the VM monitor summary, and the orphan list.

- `osb_*.json` and `catalog.json`

  The service broker routes. A delete or a provision answers 202 with an empty object. An update answers 500 with the description `not implemented`. A delete of an instance the broker no longer has answers 410.

- `credentials*.json`, `manifest*.json`, and `logs*.json`

  The credentials, the manifest, and the broker log. The file `credentials_placeholder.json` is what the broker sends when it holds no stored credentials, and it carries a host, a user, and a password that are not real.

- `task_detail*.json`

  The detail of one BOSH task from `/b/tasks/{id}`, with the task, its events, and for a failed task the `error` field. There is one file each for a queued, a processing, a finished, a failed, and an unknown task.

- `osb_last_operation*.json`

  The answers to the last operation poll for an instance that is in progress, succeeded, failed, or gone.

- `deployment_not_managed.json`

  The 403 a newer broker sends when a deployment action names a deployment it does not manage, built from the broker's own error writer.

- `*.txt`

  Plain-text 404 bodies. The router sends them for a route the broker does not have, which is how Boss can tell an old broker from a missing instance.
