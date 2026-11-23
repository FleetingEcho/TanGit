# Local agent automation

The `internal/agentcli` package provides a local CLI/RPC service over the same
`agentwork.Store` and repository review operations used by lazygitGo. It has no GUI,
MyGo, socket, network, or MCP dependency. `app.Main` routes the explicit
`lazygitGo agent ...` subcommand to `agentcli.Execute` with `os.Stdin`, `os.Stdout`,
and `os.Stderr` before starting any GUI, launcher, or single-instance handling.
The newline-delimited JSON interface below is local structured RPC; it is not a
complete MCP implementation and creates no listener or network connection.

This document describes the CLI/RPC boundary in the current implementation,
not a delivery phase or future provider integration. The desktop task and
review workflow is documented in [Agent workspaces](agent-workspaces.md#agent-workspaces-current-implementation).
The CLI does not infer a task, selected tab, destination, review approval, or
integration action from UI state.

## Shared data directory

Every CLI command accepts `--data-dir DIR`. Resolution is, in order:

1. the explicit `--data-dir` value;
2. `LAZYGITGO_AGENT_DATA_DIR`;
3. the legacy `TANGIT_AGENT_DATA_DIR`, if set;
4. the desktop app's default data directory.

On Linux, fresh installations default to `${XDG_CONFIG_HOME:-~/.config}/lazygitGo`.
Upgraded installations keep their existing data directory.
Tasks are stored in `agent-tasks.json` and `agent-tasks/TASK_ID/request.md` under
that directory.

**The CLI directory must be the desktop lazygitGo user-data directory, not a
repository directory.** If the desktop uses a non-default user-data location,
pass that exact location with `--data-dir` or the environment variable. Using a
second directory creates a separate task store rather than synchronizing two
stores. Likewise, list/create repository filters use lazygitGo's opaque repository
ID. For a local repository this is its canonical main-worktree path; use the same
path spelling for CLI-created tasks so they appear with desktop-created tasks.

On success, ordinary text or requested JSON is written to stdout. Failures are
written as `agent COMMAND: ERROR` to stderr. `--json` emits one indented JSON
value followed by a newline and does not place diagnostics on stdout. `Execute`
returns `0` for success, `1` for operation/output errors, and `2` for usage
errors; it never calls `os.Exit` itself.

## Read operations

```sh
# All tasks, or exactly one repository family.
lazygitGo agent list --json
lazygitGo agent list --repository /work/project --json

# Explicit IDs only; no desktop selection is consulted.
lazygitGo agent show --id 0123456789abcdef0123456789abcdef --json

# The direct stored BaseCommit -> current index/worktree final result.
lazygitGo agent diff --id 0123456789abcdef0123456789abcdef --json
lazygitGo agent diff --id 0123456789abcdef0123456789abcdef \
  --file path/within/worktree.go --json
```

`show` returns the full task, including `checkpoints` and `lastTest`. `diff`
returns `taskId`, `baseCommit`, and `files`; with `--file` it also returns the
selected `FinalFileDiff` (`patch`, `oldText`, `newText`, and `kind` as
applicable). Final files include commits since base, staged and unstaged tracked
changes, and untracked files. The tracked comparison is a single direct
base-to-final diff, not a sum of cached and uncached diffs. A file not present
in that final review is an error. `--context N` and `--ignore-whitespace` apply
the repository's normal `DiffOptions` consistently to file lists and patches.

These commands do not create worktrees, update task records, apply patches, or
infer a target from the currently selected desktop tab.

## Create a task

```sh
lazygitGo agent create \
  --repository /work/project \
  --path /work/project-agent-search \
  --branch agent/search \
  --base main \
  --title "Implement search" \
  --request "Add repository search and preserve existing behavior." \
  --profile local-agent \
  --launch-command 'agent --prompt-file {promptFile} --cd {worktree}' \
  --resume-command 'agent resume {sessionId} --cd {worktree}' \
  --setup-command 'make setup' \
  --test-command 'go test ./...' \
  --env PORT=4100 --env LOG_LEVEL=debug \
  --yes --json
```

All target fields are explicit and title/request must be nonempty. Command
profile fields are stored, not launched by this service. Environment entries
use repeatable `KEY=VALUE` flags and should contain only nonsecret values.

Creation resolves `--base` through `repository.Opener` to an immutable commit
SHA, creates the new branch/worktree from that SHA, and only then persists the
task. **`--yes` is mandatory**, including before any Git mutation. If persistence
fails after Git succeeds, the error identifies the worktree and branch and says
that they remain for inspection/manual cleanup; no automatic deletion is
attempted.

## Reports, comments, and test results

`list`/`show` expose the current `updatedAt`. Every mutation of an existing task
requires that exact revision as RFC3339 (fractional seconds are accepted):

```sh
TASK=0123456789abcdef0123456789abcdef
REV='2026-11-13T19:30:00.123456789Z'

lazygitGo agent report --id "$TASK" --expected-updated-at "$REV" \
  --session-id agent-session-42 --summary 'Implemented and checked.' \
  --status ready_for_review --json

# Use the newly returned updatedAt for each subsequent mutation.
lazygitGo agent comment --id "$TASK" --expected-updated-at "$REV" \
  --text 'Please add a regression test for empty input.' --json

lazygitGo agent test-result --id "$TASK" --expected-updated-at "$REV" \
  --status passed --command 'go test ./...' --exit-code 0 \
  --started-at '2026-11-13T19:31:00Z' \
  --finished-at '2026-11-13T19:31:04Z' --json
```

Report fields are optional individually, but at least one of `--session-id`,
`--summary`, or `--status` must be supplied. Explicitly supplying an empty
session ID or summary clears that field. Allowed report lifecycle values are
`needs_input`, `ready_for_review`, `blocked`, and `failed`. Unchanged fields are
preserved, and the underlying store still enforces every legal transition.

`comment` appends nonempty text between
`<!-- LAZYGITGO AGENT COMMENT START -->` and
`<!-- LAZYGITGO AGENT COMMENT END -->` in the durable request. It does not resume an
agent or change lifecycle state.

`test-result` requires `passed` or `failed`, a nonempty command, and an explicit
numeric `--exit-code` (including zero). Start/finish timestamps should be
supplied when a harness has them; omitted timestamps are filled consistently
from those supplied or the current UTC time. Finishing before starting is an
error. **A passing test never changes lifecycle status or implies
`ready_for_review`.**

Revision comparison occurs inside `Store.Update`'s serialized callback, before
any field is changed. A stale update fails with no request/task write; callers
must show/list again and resubmit against the new `updatedAt`.

## Patch preview and delivery

```sh
# Preview only: the default is read-only.
lazygitGo agent patch --destination /work/integration --patch /tmp/change.patch
lazygitGo agent preview-patch --destination /work/integration --patch /tmp/change.patch --json

# Apply requires both a clean preview and explicit confirmation.
lazygitGo agent patch --destination /work/integration --patch /tmp/change.patch --yes
lazygitGo agent apply-patch --destination /work/integration --patch /tmp/change.patch --yes --json
```

There is no default or inferred destination. Preview opens exactly the supplied
worktree and calls `PreviewPatch` on the supplied file. Results include `clean`,
`diagnostic`, public preview text/truncation state, file metadata, and line
counts. Application requires `--yes`, rejects a non-clean preview, and passes
the **same bound preview object** to `ApplyPatch`; repository integrity and
state checks therefore remain authoritative. Ordinary patch application changes
only the destination working tree and does not stage files. Neither preview nor
apply modifies the patch source.

## Newline-delimited JSON RPC

Run the stream server with:

```sh
lazygitGo agent rpc --data-dir /path/to/lazygitGo-user-data
```

Input is one JSON object per line on stdin; output is one compact JSON response
per nonempty request line on stdout. EOF closes cleanly. No listener or network
connection is created.

```json
{"id":"read-1","method":"list","params":{"dataDir":"/config/lazygitGo","repository":"/work/project"}}
{"id":"read-2","method":"show","params":{"dataDir":"/config/lazygitGo","taskId":"0123456789abcdef0123456789abcdef"}}
```

Successful response:

```json
{"id":"read-1","result":[{"id":"0123456789abcdef0123456789abcdef","repositoryId":"/work/project","title":"Implement search","request":"...","baseRef":"main","baseCommit":"...","branch":"agent/search","worktreePath":"/work/project-agent-search","profile":{},"status":"running","createdAt":"2026-11-13T19:00:00Z","updatedAt":"2026-11-13T19:00:00Z","lastActiveAt":"2026-11-13T19:00:00Z"}]}
```

Error response:

```json
{"id":"read-1","error":{"message":"agent task ... was not found"}}
```

`id` can be any JSON value and is copied to the response (`null` if absent or if
the request cannot be parsed). `dataDir` is optional in every method and follows
the same explicit/environment/default resolution described above; subsequent
requests can select different stores. Params reject unknown fields.

### Methods and params

All method results are the same task/array/diff/preview values as the service
operations.

| Method | Params (besides optional `dataDir`) |
| --- | --- |
| `list` | optional `repository` |
| `show` | `taskId` |
| `diff` | `taskId`; optional `file`, `ignoreWhitespace`, `contextLines` |
| `create` | `repository`, `path`, `branch`, `base`, `title`, `request`, optional `profile`, and required `yes: true` |
| `report` | `taskId`, `expectedUpdatedAt`; optional `sessionId`, `summary`, `status` (at least one) |
| `comment` | `taskId`, `expectedUpdatedAt`, `text` |
| `test_result` | `taskId`, `expectedUpdatedAt`, `status`, `command`, numeric `exitCode`; optional RFC3339 `startedAt`, `finishedAt` |
| `preview_patch` | `destination`, `patch` |
| `apply_patch` | `destination`, `patch`, required `yes: true` |

Example mutation:

```json
{"id":42,"method":"report","params":{"dataDir":"/config/lazygitGo","taskId":"0123456789abcdef0123456789abcdef","expectedUpdatedAt":"2026-11-13T19:30:00.123456789Z","summary":"Implemented","status":"ready_for_review"}}
```

Example test result:

```json
{"id":43,"method":"test_result","params":{"dataDir":"/config/lazygitGo","taskId":"0123456789abcdef0123456789abcdef","expectedUpdatedAt":"2026-11-13T19:31:10Z","status":"failed","command":"go test ./...","exitCode":1,"startedAt":"2026-11-13T19:30:30Z","finishedAt":"2026-11-13T19:31:09Z"}}
```

Malformed JSON, invalid params, unknown methods, stale revisions, and operation
failures produce error responses without terminating the stream. Requests are
bounded to **1 MiB per line**; an oversized line receives an error and is
discarded through its newline so later requests can proceed. Responses are
bounded to **8 MiB**; an oversized result is replaced by a size error. Actual
stdin/stdout I/O failures are returned to `Execute` and reported on stderr.

## Relation to in-app assist helpers

The desktop's Agent Profile can also contain an optional **AssistCommand** for
Conflict Assistant and Change Splitter. That JSON-file advisor is separate
from `agentcli`: `lazygitGo agent ...` neither launches it nor turns its output
into task status or a Git mutation. Its protocol and mutation boundaries are
described in the [workflow guide](guide.md#optional-external-assist-helpers).

Assist responses are proposals only. Merely requesting or previewing one does
not stage, commit, continue an operation, or accept a task. Conflict Result
acceptance and split-group staging are separate explicit desktop actions, and
lazygitGo generates and validates the exact data used by those actions. An
AssistCommand is a user-trusted helper run through the platform shell with
ordinary process permissions; private request files and response validation do
not make it a sandbox. No model or service provider is built in.

This differs deliberately from `agent create` and `agent patch --yes`: those
commands perform their documented worktree or working-tree mutation only after
explicit confirmation. Even confirmed CLI patch application does not stage or
commit files. Task reports and passing test results similarly record
observations, not approval to integrate.

Desktop **Continue preflight** is separate too. `lazygitGo agent ...` does not run
the Required/Optional commands from **Settings → Behavior**, and
`agent test-result` does not satisfy those checks, create their checkpoint, or
approve the final operation CAS. Those desktop commands are fully trusted local
shell commands, not sandboxed; they may use the network and write files, and
Continue is disabled for SSH workspaces. See
[Continue preflight and trusted checks](guide.md#continue-preflight-and-trusted-checks)
for the exact Required/Optional refusal and Optional-failure override rules.

## Safety policy

- Every task/worktree target is explicit; UI selection is never read.
- Reads and patch preview have no intentional Git or task mutations.
- Worktree/branch creation and patch application require affirmative `--yes`
  (or RPC `yes: true`). Existing-task writes instead require an exact revision
  and preserve store transition invariants.
- Tests report observations only; passing tests do not authorize review or
  integration. Comments are durable request text only and launch no command.
- Patch previews remain bound to their exact bytes, destination, and observed
  repository state through the repository API.
- The service launches no configured agent/setup/test/assist command, opens no
  socket, and adds no implicit fetch, commit, stage, merge, push, cleanup, or
  source worktree mutation.
- Suggestions and previews are not authorization to stage or commit. Desktop
  acceptance/integration actions have their own explicit confirmations and
  source-state checks.
- See [Verification](verification.md#feature-specific-evidence-and-limits) for
  what the automated tests establish; passing them does not certify arbitrary
  external helpers, hosting providers, Git versions, or SSH deployments.
