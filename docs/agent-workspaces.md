# Agent workspaces roadmap

TanGit's worktree support is the foundation for isolated coding-agent work. The
product goal is a complete loop: **create → observe → review → revise → accept
→ archive**. A worktree is an execution environment, not the task itself.

## Shared model and invariants

An Agent Task belongs to a repository's common worktree family and has its own
stable ID. It records a title, request, base ref and resolved base commit,
branch, worktree path, agent profile, initialization/test commands, lifecycle
state and timestamps. Later phases add session, run, checkpoint, review and
integration records without changing that identity.

- Git remains the source of truth for commits and file changes.
- Agent state comes from structured events when available. Terminal titles or
  output must not be presented as reliable task completion.
- Every automated operation names an explicit task/worktree target. Selection
  in the UI is never implicit authorization for a background operation.
- Review includes commits, the index, tracked working-tree edits and untracked
  files relative to the recorded base. It never hides dirty agent output.
- Closing or archiving a task does not delete its worktree. Removal checks
  running processes, unsaved buffers, uncommitted work and unpushed commits.
- Existing confirmations, Safety Journal and destructive-action policies also
  apply to agent-originated operations.
- Repository-wide fetches and object-database operations run once per common
  repository, not redundantly once per linked worktree.
- Worktree status is cached and refreshed independently in the background; a
  dashboard must not synchronously scan every worktree during each UI snapshot.

## Phase 1 — the complete single-task loop

1. **New Agent Task**: create an isolated branch/worktree from a selected base,
   save the request and resolved base SHA, and use a configurable command
   template (`{worktree}`, `{task}`, `{promptFile}`) rather than tying the app to
   one agent vendor.
2. **Agent profiles**: launch/resume, setup and test commands. Generic local
   terminals work first; structured adapters can be added without changing the
   task model.
3. **Mission Control**: task-centric rows for running, needs input, testing,
   ready for review, blocked, failed and done; recent activity, changes,
   ahead/behind state and test outcome. Unknown terminal state remains unknown.
4. **Review Center**: base-to-final-result diff plus commits-only and dirty-only
   views, test results and agent summary. Request Changes resumes the same task;
   Accept offers explicit commit/cherry-pick/merge or selective Change Courier
   delivery. Archive is distinct from worktree removal.
5. Focused repository, persistence and native-UI tests for every transition.

## Phase 2 — recovery, notifications and automation

1. **Task Checkpoints**: preserve base and WIP state before accept, cleanup,
   rerun or bulk history operations; restore without silently discarding newer
   work.
2. **Attention delivery**: desktop notification and dashboard badges for needs
   input, completion and failure, including background repository tabs.
3. **Activity and run history**: lifecycle events, commands, test results,
   resumable agent session IDs and bounded terminal/artifact references.
4. **Local automation**: CLI and structured local RPC/MCP adapter for task
   creation, explicit-target status/diff reads, progress/test reporting, review
   requests, comments and patch delivery. Reads default to safe access; writes
   follow policies, expected revisions and existing destructive confirmations.

## Phase 3 — coordinated parallel work

1. **Overlap analysis**: compare committed and dirty changes between tasks;
   show file and neighboring-hunk overlap, delete/modify hazards and generated
   or migration-file risks without claiming overlap is always a conflict.
2. **Task dependency graph**: base relationships, explicit dependencies and an
   integration worktree. Dry-run integration and stop at the first conflict.
3. **Coordinated updates**: one repository fetch, independent behind-base
   calculations, selected rebase/pull operations and independent conflict
   recovery.
4. **Workspace templates**: reusable setup/test/run commands, sparse checkout,
   explicit ignored-file selection and per-task nonsecret environment values.
   Secrets and arbitrary `.env` files are never copied implicitly.
5. **Resource isolation**: per-task development ports, database names and cache
   directories where the profile supports them; coordinated test/review batch
   actions with independent results.

## Implementation seams

- `agentwork`: persistent tasks, runs, state transitions and checkpoints. It
  owns invariants and emits domain events; it does not render UI or guess agent
  state from terminal text.
- `repository`: arbitrary-worktree reads, final-result diffs, checks and Git
  recovery operations. No agent process policy belongs here.
- Agent adapters: translate generic terminal and structured agent events into
  task events. A basic command adapter is the first implementation.
- `app`: task forms, Mission Control, Review Center and integration actions.
  Background updates are generation/task-ID guarded and published on the UI
  thread.

Each phase is delivered as tested, usable increments inside its complete
workflow; persistent schema versions remain backward-compatible as later
phases add fields.
