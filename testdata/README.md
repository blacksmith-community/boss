# Test fixtures

Every file here is a response that a real Blacksmith broker handler wrote. None of them is typed by hand. We produced them with a throwaway Go program that sat outside this repository and was never committed. The program ran the broker's own HTTP handlers, with the broker's own response types, against a scripted director and a stubbed vault, and it saved each body exactly as the handler sent it.

The program ran twice. Files without a prefix came from the broker at commit b61f8696, which is the tip of the broker branch that carries the new routes. Files that start with `v182_` came from the broker at the `v1.8.2` tag, so they show how an older broker answers the same routes. Some of the newer routes do not exist there, and the `v182_` files for those routes hold the plain-text 404 that the old router sends.

Identifiers are fixed so the tests can name them. Instance IDs look like `11111111-aaaa-bbbb-cccc-00000000000N`, and secrets are placeholders such as `fixture-not-a-secret`.

## What the fixtures leave out

The fixtures cover shapes and status codes. They do not cover timing, because the handlers ran against a scripted director that answers at once. They also cannot show how the broker behaves under load or across a restart.

Two files are slices of a real response. The file `task_log_first.json` holds the first event of `task_log.json`, and the follow test uses it to stand for the answer a task gives early in its run.

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

- `*.txt`

  Plain-text 404 bodies. The router sends them for a route the broker does not have, which is how Boss can tell an old broker from a missing instance.
