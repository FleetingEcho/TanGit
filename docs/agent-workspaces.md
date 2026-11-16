# Agent workspaces: current implementation

This document describes the Agent workspace behavior present in the current
code. It replaces the former Phase 1/2/3 roadmap: those headings were planning
labels, not an accurate inventory of shipped behavior. Nothing in this document
is a promise of provider support or a future integration.

See [Local agent automation](agent-automation.md) for CLI/RPC commands and the
[workflow guide](guide.md#optional-external-assist-helpers) for Conflict
Assistant and Change Splitter.

## Task identity and isolated worktrees

An Agent Task belongs to one repository worktree family and has a durable ID.
Its record includes the title and request, base ref plus resolved full base
commit, new branch, explicit worktree path, command profile, dependencies,
lifecycle state, session/activity/test information, checkpoints, and timestamps.
Git remains the source of truth for commits and files; the task record does not
replace them.

The desktop **New Agent Task** workflow:

1. Resolves the chosen base to a commit SHA.
2. Creates the requested branch and isolated worktree, including explicitly
   listed sparse paths when configured.
3. Persists the durable task and request file.
4. Copies only explicitly named ignored files when requested; it neither
   discovers nor implicitly copies `.env` files or secrets.

If Git creation succeeds but task persistence fails, the error identifies the
remaining branch/worktree for inspection or manual cleanup. Task creation is
therefore not described as an all-or-nothing filesystem transaction.

Profiles can contain launch, resume, setup, test, development/run, and optional
AssistCommand templates. Command placeholders include the explicit worktree,
task/request/prompt file, full base, branch, session ID, and configured port,
database, and cache values. Values are substituted once and shell-quoted.
Reusable workspace templates store independent copies of profiles, so editing
a template does not rewrite existing tasks.

The generic desktop terminal adapter currently requires a **local worktree**
for agent launch/resume, test, and development/run commands. Durable tasks and
remote worktree data remain available when that adapter cannot launch a remote
terminal. Configured commands are trusted user programs, not sandboxed jobs.

## Review, revision, checkpoints, and acceptance

**Review changes** opens a separate session for the explicit task worktree.
Its three views are:

- **Final files**: one direct BaseCommit-to-final comparison including commits,
  staged and unstaged changes, and untracked files.
- **Commit range**: commits in the task branch since its fixed base.
- **Dirty only**: the task's current index/worktree output.

Review never hides dirty output or infers readiness from terminal text, an exit
code, or a passing test. A zero agent exit records `exited`, not
`ready_for_review`; test execution updates the test result without changing the
agent lifecycle. A person or structured integration must explicitly mark a
task ready for review.

**Request changes** appends reviewer feedback to the durable request and resumes
the same task with its configured resume command. It does not silently create a
new worktree or task. Archiving changes task visibility, not the worktree or its
changes.

Checkpoints preserve staged, unstaged, and untracked WIP together with the
captured HEAD identity. Restoring requires the same HEAD, saves current WIP in
a new safety checkpoint first, and then restores the selected work snapshot.
Newer commits stop restoration; failure is not reported as a successful
rollback.

Acceptance is a separate explicit action. It is available only for a task marked
ready for review that has task commits and no remaining staged, unstaged, or
untracked output. **Accept commits** cherry-picks the captured commit range
oldest first; **Merge branch** merges the task branch into the current
foreground branch. Both create a task checkpoint before the confirmed Git
mutation. Conflicts use the normal operation recovery UI. No review view,
notification, or passing test performs these actions automatically.

## Notifications and coordinated task tools

Current task tools also include:

- Durable activity and bounded test/session records, plus toast and native
  desktop notifications for explicit needs-input, ready-for-review, exit,
  failure, and test-result transitions. Native notification delivery failure
  does not rewrite task state.
- **Analyze overlaps**, which compares committed and dirty final results across
  active tasks and labels shared files, exact/nearby hunks, delete/modify
  hazards, and generated/migration risks. Different base SHAs prevent a false
  hunk-position comparison. Findings are review hints, not conflict predictions
  or integration authorization.
- **Integration dry run**, which captures selected task HEAD SHAs and merges
  them in dependency order in an isolated worktree. It stops at real conflicts
  for inspection without pushing, changing task readiness/BaseCommit, or
  modifying foreground/task worktrees. Worktree cleanup is separately
  confirmed.
- **Coordinated updates**, which fetch the foreground repository once, inspect
  explicitly selected task targets independently, and run confirmed task
  rebases with checkpoints and per-task results. A stop prevents scheduling
  additional tasks; it is not described as cancellation of an already-running
  Git process.
- **Batch tests and reviews**, which queue explicitly selected tasks within
  terminal capacity and present independent results or review visits. Merely
  visiting a review does not mark it ready or accepted.

## CLI/RPC automation and advice helpers

The local `tangit agent ...` CLI and newline-delimited JSON RPC share the same
persistent task store. They provide explicit-target list/show/diff/create,
report, comment, test-result, patch preview, and confirmed working-tree patch
application. Existing-task writes require the exact `updatedAt` revision; patch
application consumes its clean, source-bound preview. The service does not
launch profile commands, open a socket, or implicitly fetch, stage, commit,
merge, push, clean up, or mutate the source worktree.

The optional Agent Profile **AssistCommand** is a separate provider-neutral
JSON-file helper for Conflict Assistant and Change Splitter. It runs only after
an explicit suggestion request. The advisor does not open a repository or invoke
Git, and advice previews do not stage or commit automatically. Accepting a
conflict Result and applying one split group to the index are distinct desktop
actions with source/index/operation checks.

AssistCommand is executed through the platform shell with the user's normal
permissions and receives source text or diff content in its request. It is a
trusted helper, **not a sandbox**, and no model or service provider is bundled.
The same distinction applies to coding-agent commands: TanGit does not infer
integration approval from their output, but a command the user deliberately
launches can itself edit or commit inside its assigned worktree.

## Safety invariants across the UI and CLI

- Every background operation names an explicit task and worktree; the selected
  desktop tab is never implicit authorization.
- Commits, the index, tracked working-tree changes, and untracked files all
  participate in task review.
- Store revision checks, full commit IDs, exact patch bytes, and repository
  state checks bound mutations to what was reviewed.
- Existing confirmations, operation recovery, checkpoints, and destructive
  action policies apply to the corresponding Agent actions.
- Overlap, test, exit, and notification observations do not equal review
  approval. Acceptance and integration always require their own actions.

## Ownership in the source

- `internal/agentwork`: durable tasks, state transitions, checkpoints,
  activity/test records, templates, dependency ordering, and overlap analysis.
- `internal/agentcli`: explicit local CLI and stdin/stdout JSON RPC; no GUI,
  listener, MCP server, or implicit command launch.
- `internal/agentassist`: source parsing, identity validation, helper protocol,
  and in-memory previews; no repository access or Git mutation.
- `internal/repository`: arbitrary-worktree reads, final-result diffs, snapshots,
  recovery, and confirmed Git operations.
- `internal/app`: task forms, terminal adapters, review and acceptance,
  notifications, coordinated tools, and assistant dialogs. Repository-bound
  work is guarded by task/repository identity and current source state.
