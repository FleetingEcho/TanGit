# Verification

This document describes the checks configured in `.github/workflows/verify.yml`
and what their results do—and do not—establish. A workflow definition is not a
test result. When citing verification for a change, use the logs for the same
commit, and record the tool versions and capabilities actually exercised.

## Automated gates

`Verify` runs on pull requests and pushes to `main`. It is also a reusable
workflow through `workflow_call`. `.github/workflows/release.yml` calls that
same workflow for `v*` tags and makes draft creation wait for it. Consequently,
a release cannot create its draft or start its signed/update builds until every
job in the called verification workflow has passed.

The Ubuntu `Test and vet` job uses `actions/setup-go` with `go-version-file:
go.mod` and runs:

```sh
go tool mygo vet .
CGO_ENABLED=0 go test ./...
go test -race ./internal/repository ./internal/agentwork ./internal/agentcli ./internal/app
```

The full test command runs with `TANGIT_TEST_REQUIRE_OPENSSH=1`; the race
command has the same required-OpenSSH setting at job scope. The job installs
`build-essential`, `openssh-client`, and `openssh-server`, checks that it is not
root, and prints the Go, MyGo, Git, and SSH versions in its log.

### MyGo 0.10 verification and local extensions

The current MyGo dependency is **0.10.0**. Its vendored code, `go.mod`, pruning
script and upstream LICENSE are kept in sync. TanGit preserves its own batch
highlight, Quit/Relaunch, hover-anchor, foreground-process and CJK baseline
behavior while using the newer upstream UI and engine implementation; no
validation-only stub or changed public behavior substitutes for those APIs.

Nested-module checks include `ui`, `editor`, `terminal` and `pty`, including
real shell foreground-process transitions. The whole TanGit suite and the
Linux native build / CLI smoke test also run on the upgraded sources.

Three upstream checks are classified separately rather than silently dropped:

- `TestTwoTestersInParallel` intentionally drives concurrent glyph-atlas
  access documented as **issue #1722** in the upstream MyGo test suite.
- `TestDiffFrameAllocs` and `TestOutlineRowAllocs` compare frame-allocation
  counts whose values change under race-instrumented execution.
- `TestPathContains` is likewise an allocation-count assertion, not an ordinary
  repository functional check.

The race-only run reports these limits explicitly and uses
`-skip '^TestTwoTestersInParallel$|^TestDiffFrameAllocs$|^TestOutlineRowAllocs$|^TestPathContains$'`
to avoid treating multithreaded glyph drawing or instrumentation-sensitive
performance assertions as a correctness failure. They are not product-
compatibility suite skips and must not be used to waive other tests. Standard
non-race UI/terminal checks still execute these upstream assertions.

## Native build and smoke matrix

Each build runs on its native hosted OS. The artifacts deliberately omit
installers, DMGs, notarization, and update uploads so pull requests need no
NSIS installation, Apple credentials, signing identity, or updater private
key. macOS is ad-hoc signed so its bundled executable can be launched.

| Job | Runner and build target | Smoke check | What a passing run establishes | What it does not establish |
| --- | --- | --- | --- | --- |
| Linux | `ubuntu-latest`, `linux/amd64` | Execute `build/linux-amd64/tangit agent help` and check its usage output | MyGo can compile/package the Linux target and its native executable can enter and leave the headless CLI on that runner | GUI behavior, package installation, other Linux distributions/architectures, or external network access |
| macOS | `macos-latest`, `darwin/universal` | Execute `TanGit.app/Contents/MacOS/TanGit agent help` from the universal bundle and check its usage output | Both universal slices compile and the bundle's headless CLI runs on the runner used by that job | Execution of both slices in one run, a DMG, notarization, a Developer ID signature, GUI behavior, or every supported macOS version |
| Windows | `windows-latest`, `windows/amd64` | Execute `TanGit.exe agent help` and check its usage output | MyGo can compile/package the Windows target and its native executable can enter and leave the headless CLI on that runner | The NSIS installer, Windows on ARM, GUI behavior, or every supported Windows version |

The `agent help` route returns before single-instance, updater, native-window,
or terminal initialization. It is a launch and output smoke test, not a native
UI test. Build jobs depend on the Ubuntu test job, and the three operating
systems run independently with fail-fast disabled.

## Version and capability evidence

Go and MyGo are selected from module metadata, rather than from unrelated tools
preinstalled on a runner. Git is supplied by each hosted image and is **not
pinned** by this workflow.

| Tool or capability | Configured source | Supported verification claim |
| --- | --- | --- |
| Go | The `go` directive in `go.mod` (currently `1.27.3`) | Checks using `actions/setup-go` ran with the version recorded in their logs |
| MyGo | The `tool github.com/egoist/mygo/cmd/mygo` dependency in `go.mod` (currently MyGo `0.10.0`) | The `go tool mygo` command selected by that checkout ran successfully for the completed job |
| Git in the Ubuntu test job | The hosted image's installed Git; `git --version` is printed | Tests exercised exactly that logged version, not an implied minimum or broad version range |
| OpenSSH in the Ubuntu test job | Ubuntu packages installed by the job; `ssh -V` is printed | Loopback tests exercised the logged client/server package family on that image |
| Hosted operating systems | Moving `ubuntu-latest`, `macos-latest`, and `windows-latest` labels | Results apply to the concrete runner image recorded by GitHub, not to every OS release |
| Git 2.32 | No Git 2.32 job or container is configured | **Not verified. Do not claim Git 2.32 compatibility from this workflow.** |
| Other Git versions, distributions, CPU architectures, installers, remote services, or desktop environments | Not pinned or exercised unless added explicitly and shown in logs | **Unverified until a matching run is recorded.** Real-host compatibility claims need additional evidence |

In particular, compiling a universal macOS binary is not the same as running
both slices, and a successful Linux test does not prove macOS or Windows tests
passed. The normal tests and race tests run on Ubuntu only; the other two
platforms have the build/headless-smoke scope shown above.

## Feature-specific evidence and limits

The operation, recovery, and assisted-review behaviors described in the
[workflow guide](guide.md#operation-recovery-history-and-assisted-review) have
focused checks in the current test tree. The table identifies evidence to run
and inspect; it is not a claim that a particular commit passed on a particular
host without the corresponding log.

| Area | Current source-level and headless checks | Scope that must not be inferred |
| --- | --- | --- |
| Real operation context and Continue/Skip/Abort | `internal/repository/operation_test.go` exercises Git operation metadata, actions, progress, and CAS refusal; `internal/app/operation_test.go` and the operation UI tests check operation-dependent controls | Every Git version, every unusual sequencer state, or every remote SSH transition; unsupported transitions are expected to fail closed |
| Guided conflict mode | [`action_controls_test.go`](../internal/app/action_controls_test.go) checks the third `guided` style value and control layouts; [`guided_conflict_test.go`](../internal/app/guided_conflict_test.go) checks live stage help, localization, and narrow/wide reflow; [`operation_view_test.go`](../internal/app/operation_view_test.go) checks real step/current/next/available positions and that a selected path with no recovery record gets neither Restore nor Retry | Persisted completion attestations (the checklist deliberately derives positions), automatic resolution/Continue, or manual visual approval on every native OS and display size |
| Trusted Continue preflight | [`continue_review_test.go`](../internal/repository/continue_review_test.go) covers canonical index/worktree and untracked-content identity; [`continue_execute_test.go`](../internal/app/continue_execute_test.go) executes the real local shell helper and a real merge review with same-length edits; [`continue_preflight_test.go`](../internal/app/continue_preflight_test.go) drives fake and UI gates for missing/failing Required, Optional skip/failure override, output, cancellation, retry, checkpoints, stale generations, shutdown, and final driver CAS | A command sandbox (none is provided or claimed), network availability for particular configured commands, every external process's cancellation behavior, or Continue over SSH (the action is explicitly disabled). The digest covers tracked and ordinary non-ignored untracked content, not ignored files, outside writes, or network side effects |
| Changes hunks and patch CAS | `internal/app/hunk_actions_test.go`, `hunk_actions_real_test.go`, and the repository patch request/import tests cover canonical hunk patches, explicit targets, stale previews, and real temporary repositories | Binary/mode-only hunk support, combined-conflict hunk staging, or arbitrary third-party patch compatibility |
| Single-file durable recovery and conflict Result publication | [`file_recovery_test.go`](../internal/repository/file_recovery_test.go), [`file_recovery_workflow_test.go`](../internal/repository/file_recovery_workflow_test.go), [`conflict_result_test.go`](../internal/repository/conflict_result_test.go), and [`file_recovery_integration_test.go`](../internal/app/file_recovery_integration_test.go) cover exact text/binary file/index states, before/after CAS, interrupted progress, explicit retries, and Result-only writes | Repository-wide transactions, recovery of symlinks/gitlinks/non-regular paths, or immunity to uncoordinated external Git processes |
| V2 structure, corruption, concurrent sessions, and degraded discovery | [`file_recovery_hardening_test.go`](../internal/repository/file_recovery_hardening_test.go) covers the fixed V2 golden, unknown/malformed fields, binary and trailing-garbage rejection, payload digest/truncation corruption before CAS with newer work preserved, three-session update/restore CAS, and degraded metadata logging without stale actions. [`file_recovery_fuzz_test.go`](../internal/repository/file_recovery_fuzz_test.go) adds structured manifest/status/digest fuzzing, whole-input binary/malformed/truncated fuzzing, and canonical path-containment fuzzing | An open-ended fuzz campaign or proof against every malformed input and damaged-store recovery strategy; normal `go test` runs seed corpora unless fuzzing is explicitly enabled. Unknown future V2 fields are rejected, not guessed |
| Numstat streaming and refresh publication | [`commit_stats_test.go`](../internal/repository/commit_stats_test.go) checks commit boundaries, renames, binary dashes, cache behavior, and canceled cache waiters; [`performance_regression_test.go`](../internal/app/performance_regression_test.go) checks obsolete-read cancellation, A-B-A publication refusal, no stale repaint, and exact same-patch quiet state/byte reuse | A blanket `CoreSnapshot` speedup, removal of the quiet Git read, or an end-to-end UI percentage. The fixed host/workload and both positive and negative results are in [performance evidence](performance.md#large-repository-performance-evidence) |
| Visual Rebase TODO and Custom Patch history | `internal/repository/rebase_plan_test.go`, `rebase_plan_safety_test.go`, `history_patch_test.go`, and related app tests cover full-todo identity, marked-base plans, operation CAS, replay/resume, and invalid-source refusal | New-before a root commit, multi-source history replay, merge source/destination editing, or `commit.gpgsign=true`; those are intentional current limitations |
| Stack Sync plan/apply and publication | `internal/repository/stack_sync_test.go` uses local/bare repositories for expected-old-OID updates, frozen-plan invalidation, real `rebase.updateRefs` TODO ownership, preview-approved publication only, preview-OID lease rejection, and independent push outcomes | Remote stack atomicity, automatic rollback after later failures, or successful publication to external hosting providers |
| Conflict Assistant and Change Splitter | `internal/agentassist` tests use real controlled helper processes and malformed/late/cross-source responses; app tests check source/index/operation digests, exact private preview consumption, group-only index changes, unchanged HEAD/worktree, and late-dialog invalidation | Correctness or safety of an arbitrary installed helper, model/provider quality, network behavior, or an OS sandbox boundary |
| Grouped conflict and operation header layouts | `internal/app/responsive_clipping_test.go` and action-control tests exercise wide/compact groups, menus, disabled reasons, and parent geometry in MyGo's headless tester across specified sizes and themes | Manual visual approval on every native OS, font, display scale, or window size |
| Agent task persistence and coordinated tools | `internal/agentwork`, `internal/agentcli`, and focused app agent tests cover revision guards, task/review identities, checkpoints, isolated integration, coordinated updates, and independent results | Provider-specific agent correctness, remote generic-terminal launch, or automatic review approval/integration |

The recovery fuzz targets are part of the ordinary test inventory, so a normal
`go test` run exercises their checked-in seeds and corpus. That is different
from a sustained fuzzing result. To deliberately fuzz one target on a local
host, run one target at a time and retain its command, duration, tool versions,
and output, for example:

```sh
go test ./internal/repository -run '^$' -fuzz '^FuzzReadFileRecoveryRecord$' -fuzztime 30s
```

The other targets are `FuzzValidateFileRecoveryRecord` and
`FuzzFileRecoveryPath`. A seed-corpus pass, or a 30-second local campaign, must
not be presented as exhaustive validation on every platform or Git version.

Run the complete required suite rather than selecting only these examples when
validating a release candidate. A focused documentation/source check can use:

```sh
CGO_ENABLED=0 go test ./internal/repository ./internal/agentassist ./internal/agentwork ./internal/agentcli ./internal/app
go test -race ./internal/repository ./internal/agentwork ./internal/agentcli ./internal/app
```

These focused checks do not expand the platform/version evidence above. In
particular, **Git 2.32 remains unverified**, the configured CI Git is the logged
hosted-image version, and no broad statement such as "all Git versions are
supported" follows from them. OpenSSH required mode exercises the logged
loopback client/server setup; it is not a test of every SSH server,
authentication method, hosting service, or remote desktop layout. Controlled
helper fixtures do not contact or certify a real model/service provider.
Headless geometry checks do not replace manual native-GUI review where that
distinction matters.

## Requiring local OpenSSH tests

`internal/sshintegration.Start` starts an isolated sshd on an ephemeral
loopback port. Historically, environmental limitations skipped its callers.
Those skips remain convenient for an ordinary developer run, but are not
acceptable when OpenSSH coverage is a CI requirement.

Set the following variable to turn every environment-driven skip in this
helper into an explicit test failure:

```sh
TANGIT_TEST_REQUIRE_OPENSSH=1
```

For example, on a supported POSIX host with OpenSSH installed:

```sh
TANGIT_TEST_REQUIRE_OPENSSH=1 CGO_ENABLED=0 go test ./internal/sshintegration ./internal/sshworkspace ./internal/repository ./internal/app
```

Use an unprivileged account. The helper needs `/usr/sbin/sshd`, `ssh-keygen`, a
usable current username, loopback TCP, a secure test-state location, and the
host's sshd privilege-separation facilities. The variable does not install
software, grant privileges, relax `StrictModes`, or bypass account policy.
Unsupported non-POSIX platforms also fail rather than skip when the variable
is `1`.

Authentication, generated-key, configuration, protocol, command-output, and
other product failures are fatal in either mode. Required mode only prevents
an unavailable **environment capability** from becoming a green skipped test.

## Local checks and their limits

Before submitting a change, run the commands that are feasible for the change
and the local host:

```sh
gofmt -l cmd internal
go tool mygo vet .
CGO_ENABLED=0 go test ./...
go test -race ./internal/repository ./internal/agentwork ./internal/agentcli ./internal/app
go tool mygo build -platform linux/amd64 -skip-dmg -skip-installer -skip-notarize
build/linux-amd64/tangit agent help
```

On macOS or Windows, select the corresponding native target and executable
instead. A local shell can check source behavior and parse workflow YAML, but
it does not execute GitHub Actions triggers, reusable-workflow semantics,
branch protection, hosted runner images, apt setup, secrets, or release
permissions. A YAML parse also does not validate every GitHub expression or
prove that third-party actions and downloads are available. The authoritative
result for those behaviors must come from GitHub Actions on the same commit.

Likewise, a headless CLI check cannot validate GUI rendering or terminal
output. External ICMP reachability, public network behavior, terminal/native
visual output, and platform-specific window behavior need their own suitable
hosted or manual checks; do not infer them from this workflow or from a local
Linux run.

OpenSSH's `StrictModes` checks the generated state's parent directories. A
checkout beneath an unsuitable temporary-directory hierarchy can therefore
fail authentication even when sshd starts. Treat a message such as `bad
ownership or modes` as a real local environment failure to diagnose, not as a
passing skip or as proof that a hosted Ubuntu runner has the same layout.

## Extending the real-operation matrix

A future matrix for real Git operations should be added to `verify.yml` (or to
a focused reusable workflow called from it), with explicit, reproducible
version provisioning. For each new required dimension:

1. Pin or otherwise identify the OS and Git/SSH implementation actually used;
   print their versions in the log.
2. Keep unit tests and the required OpenSSH loopback tests separate from tests
   needing credentials, external networks, ICMP, or a display server.
3. Use a narrow real-repository fixture helper where it reduces duplication,
   without coupling unrelated packages or changing production behavior merely
   to serve the matrix.
4. Fail clearly when a required provisioner or capability is missing instead of
   allowing the required job to pass entirely through skips.
5. Record the resulting capability and limits in this document. Do not infer
   support for adjacent Git versions or platforms from one passing row.

Because `release.yml` calls the same `Verify` workflow, new required jobs in
that workflow automatically gate releases as well. The release workflow still
has its own tag/version check, native cache preparation, NSIS setup, updater
signing secret, artifact upload, and draft publication checks; verification
does not replace those release-specific assertions.
