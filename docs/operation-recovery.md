# Operation and workspace recovery

lazygitGo has several deliberately separate recovery systems:

- **Safety Journal / operation Undo** restores an exact local Git operation and its workspace.
- **Reflog/history Undo** is a separate history navigation flow; it is not a claim that side effects of an arbitrary mutation were reversed.
- **V2 single-file recovery and conflict Result recovery** restore exact recorded file/index states for file-level work.
- **Editor undo** belongs to the editor. Text editing does not turn unrelated Git mutation into an editor undo step.

Choose **Saved work → Recovery & checkpoints → Safety Journal** to inspect durable operations. A recoverable operation's immediate notification and its journal row both lead to confirmation against its immutable operation ID. A button, menu item, or record never bypasses that confirmation.

## What a protected operation captures

New protected operations retain **both** the before and after restore points, rather than just a reflog name or a textual diff. A restore point includes:

- **Raw working files:** tracked, ordinary untracked, and included ignored files, including binary payloads. Clean/smudge filters cannot turn an LFS-style pointer or normalized representation into a substitute for the user's raw bytes.
- **Exact regular-file permission bits:** POSIX `0777` permissions, not only Git's executable bit. Symbolic-link targets are saved without following them. Ownership, ACLs, xattrs, timestamps, and hard-link topology are not claimed.
- **The semantic index:** every path's mode, object ID, and stages 0–3. This covers clean staged indexes and conflict stages, rather than flattening an unresolved index to stage 0.
- **Git operation state:** known merge, rebase/AM, cherry-pick, revert, sequencer, and bisect state. `MERGE_HEAD`, `MERGE_MSG`, `CHERRY_PICK_HEAD`, `REVERT_HEAD`, `AUTO_MERGE`, `rebase-merge`, `rebase-apply`, `sequencer`, and selected bisect metadata are preserved as original bytes, not reconstructed from a current view.
- **Local refs and branch configuration:** HEAD and its attached/detached identity, all local `refs/heads`, `refs/stash` including its named reflog, and `refs/bisect`/`refs/rewritten`. The actual common Git `config` and the worktree-specific `config.worktree` are protected when present. Upstream branch settings therefore remain consistent after checkout, rename, or branch deletion.
- **An immutable ignored-file policy:** ordinary protected operations include ignored files. Hard reset instead protects the specific ignored/tracked collision paths it can overwrite, so unrelated caches are not rolled back or treated as part of that reset.

Blobs and metadata are referenced by immutable Git object IDs. The snapshot commit itself retains its ordinary base/index/(optional) untracked parents, plus a recovery-metadata parent. This parent reaches the raw tree, index payload, operation files, references, permissions and scope. Nothing here is uploaded to a hosting service. Automatic pruning excludes restore points still needed by a journal or an interrupted restoration.

`git gc --prune=now`, reopening a repository, and reading old checkpoint formats are covered by tests. A removed or invalid ref, payload, or schema makes recovery fail closed; it is never silently downgraded to a partial file restore described as complete operation Undo.

## Restore checks and write ordering

Before an operation runs, lazygitGo captures and validates its restore point. A failure to capture, store or validate it prevents that protected destructive action from executing. No callback is then dispatched.

After the operation, lazygitGo captures and validates the new post-operation state. Failure to complete that capture makes the journal **interrupted** and removes the unsafe Undo offer. The button never means “protection is complete” when post-state protection could not be established.

Undo revalidates both layers:

1. the existing workspace fingerprint, including raw content and permissions; and
2. the Git-state fingerprint, including HEAD/branch identity, index stages, whitelisted operation files, configuration, and local refs.

The compare-and-swap checks are against the exact operation that was selected, using immutable before/after recovery anchors. New file contents, a chmod-only change, a newer branch or stash, an index-stage change, a different conflict, or changed Git configuration can stop restoration before a write occurs. In particular, a clean filter that hides newer raw bytes does not make that newer work safe to overwrite. A first commit restores the unborn branch and staged files; it does not reset to a nonexistent parent.

For **new full-workspace records**, restoration does not start with a broad `git reset --hard` or replay only the files changed in a patch. It compares the complete **current raw tree with the target raw tree**, restores differences, reconciles extra files, restores index and Git metadata, and restores the correct attached or detached HEAD. Existing older recovery records retain their explicitly legacy restore behavior. The current raw comparison also restores files that were unchanged relative to HEAD but were then modified by apply, checkout, or another supported operation.

Afterwards, lazygitGo verifies the restored HEAD/branch and both target fingerprints again. A hook or I/O anomaly cannot make a partial restore count as a completed Undo.

## Protected workflows

These routes use the shared journal and raw-workspace protection. Their availability always remains subject to the actual state and applicable unsupported-type checks.

| Workflow | Recovery behavior |
| --- | --- |
| Direct commit, amend/HEAD reword, and fixup creation | Restore the old HEAD and the exact working/index state, including staged/unstaged splits and unborn HEAD for a first commit. Older history edits retain their existing history coordinators. |
| Merge, cherry-pick, and revert, including selected sequences | Restore the pre-operation workspace, refs, index and metadata. Where supported, a conflict stop has an exact recoverable partial state; newer conflict edits remain protected by CAS. |
| Ordinary merge/cherry-pick/revert/AM Continue and Abort | Each step has before/after restoration points. Continued progress is not mistaken for the initial conflict. Abort can restore the exact conflict/index/metadata that it abandoned. Rebase continues through its own history/History Patch coordinator instead of a second parallel undo system. |
| Checkout and force-checkout | Restore the correct attached or detached HEAD, file contents, index, permissions and operation metadata without a blanket hard reset. |
| Remote-branch checkout | Remove or restore the created local ref as appropriate and preserve its upstream configuration. Read-only remote-tracking refs and fetched objects are not presented as a remote-server rollback. |
| Branch create/rename/delete | Preserve local branch refs and related Git configuration while restoring HEAD and the workspace without switching the current branch unexpectedly. |
| Stash save/apply/pop, selected-line stash, rename/rewrite, file-level stash operations, and drop | Restore files/index plus the original stash ref and saved reflog. Pop/drop is not approximated by inserting a duplicate stash at the top. |
| Restore a historical commit file or apply an individual stash file | Restore the raw content and index while retaining the saved stash object and the no-staging contract. |

The ordinary Continue/Abort tests specifically verify a return to the **resolved, pre-commit merge** and to the **original unresolved merge**, respectively. Different Testers and fixtures cover a first commit, reword/amend, conflicted merge, dirty force-checkout, detached HEAD transitions, stash save/apply/pop, branch rename, and ordinary operation transitions. They use real Git repositories, not a simulated success result.

## Unsupported and explicitly bounded cases

The following are **not** automatically reversible by the operation-Undo button. They are not silently declared complete merely because another mutation is recoverable.

- **Tags and unsigned/signed tag workflows, remote administration, and arbitrary ref namespaces.**
- **Creating, removing, moving, or repairing other worktrees.** Journal worktree identity and checkout scope still apply; a restore cannot casually apply a snapshot from a different worktree.
- **Dirty or conflicted submodules, gitlink SHA transitions, and other special files.** Clean gitlinks at the same recorded OID are supported. A change requiring a safe nested-repository plan is refused. Directories and special files are not recursively deleted just to force a target shape.
- **Arbitrary custom shell commands, external process/network effects, and effects outside the recorded repository scope.** Local Git hooks also retain normal Git execution semantics; arbitrary external side effects are not sandboxes waiting to be reversed.
- **Bulk editor replace, file create/rename/move/delete, or local/SSH editor save as a single general-purpose filesystem transaction.** These retain their file-level or editor recovery paths rather than a misleading general operation-Undo claim.
- **Rebase steps already owned by the History Patch coordinator.** They use that coordinator's source, replay and recoverable transitions, not parallel competing journals.

Where a case cannot be fully protected, the appropriate behavior is to refuse capture or refuse Undo and explain that its state cannot be restored safely. An action which already executed but cannot complete its safety capture must not present an enabled Undo button.

### Remote effects

A remote operation is **not** an ordinary local Undo, and lazygitGo never automatically force-pushes to pretend a server effect was reversed.

`RemoteRefEffect` holds a specific remote name/ref, its previous OID, and the resulting OID, without retaining a credential-bearing URL. When such an effect is recorded, ordinary Undo is refused with `ErrRemoteCompensationRequired`.

`RemoteCompensation` is only a review plan containing the expected current remote OID, target OID, whether deletion is involved, and an explicit **confirmation required** marker. Before executing a compensation, the caller must obtain renewed user confirmation, re-read the remote ref, and protect it with expected-old-value/force-with-lease semantics. Missing, created, updated, and deleted refs therefore have distinct expected states; a nonexistent ref is not the same as an empty OID. A compensation plan is not itself authorization, and never silently drives a push.

## Failure, cancellation, interruption, and retention

- **Capture or protection failure fails closed.** A later Git lock error remains the underlying error; lazygitGo does not obscure it with a generic checkpoint error. An existing Git `index.lock` is honored before snapshot/mutation work and is never removed as a convenience.
- **Cancellation before mutation is fail-closed.** Once the underlying Git command begins, its normal command/lifecycle and cancellation behavior is respected. Recovery work is not exposed as an instantly cancellable global transaction.
- **Undo first writes `undoing` durably.** On restart, a restoration that did not complete cannot be blindly replayed as though no work had occurred. Its original before/after identities and fingerprints are retained.
- **Interruption preserves both restore points.** A failed restoration is not marked undone, and the original post-operation state is not overwritten by recalculating it from the partial result. Review the preserved points rather than letting another one-click operation overwrite uncertain or newer data.
- **Journal writes and published recovery refs are atomic at their respective publication points.** Recovery payload OIDs are pinned or made reachable through existing recovery/journal refs, not an unpinned sidecar pretending to be durable.
- **Retention remains bounded.** The recovery system preserves restore points referenced by active or interrupted operations; old independent points still follow the documented recovery retention policy.

The mutation lock serializes lazygitGo sessions that use the same checkout. It is not an exclusive lock on every editor, terminal, or Git process on the machine. External Git concurrency still depends on Git checks and expected-old-value ref updates; external file changes are protected by CAS rather than by pretending those writers can be globally suspended.

## Cost, logs, and notifications

Full operation protection requires reading and hashing the selected workspace, refs, configuration, and Git metadata, with before/after and validation passes. Any changed regular file still needs a separate raw payload. Git's object store deduplicates identical blobs, but CPU, disk I/O and large ignored trees still affect cost. Large-repository protection can therefore take noticeable time; it must not be presented as an un measured or free background action.

The Command Log records immutable shortcut provenance independently of Git output: canonical chord, semantic action ID, and human label. Text typed into an input does not become a keyboard action, mouse approval does not become a shortcut, and mouse clicks are not re-labeled as a future keyboard command.

**Shortcut acknowledgement**, **operation result/error**, and **recovery notifications** are independent. Disabling shortcut acknowledgement toasts does not remove Command Log causality and does not suppress operation results, failed operations, recovered-work warnings, or other actions that require attention.

## Evidence and test commands

The operation and UI tests use real temporary Git repositories, real Git output and `ui.Tester`. Current coverage includes:

- staged/unstaged and exact conflict-index restoration, ignored/untracked/binary data, symlinks, executable and non-executable permission bits;
- clean/smudge filters, same-clean/different-raw CAS, garbage collection, and restart durability;
- first-commit/unborn recovery, conflicted merge, amend/reword, stash/reflog, checkout/detached states, remote checkout config, branch rename/delete, and Continue/Abort state transitions;
- failed capture, post-operation/current CAS failures, existing `index.lock`, interrupted permission writes, interruption/restart, and an injected stash-state change; and
- a real app commit form followed by journal Undo confirmation, source-level `c` shortcut provenance, file/index assertions, binary permissions, and symbolic-link preservation.

Once the current change is finalized, record fresh results for:

```sh
CGO_ENABLED=0 go test ./...
go test -race ./internal/repository -run '^(TestOperationWorkspace|TestWorkspaceSnapshot|TestInterruptedUndo|TestUndo|TestRemoteEffects|TestDroppedStash|TestDiscardRecovery)'
go tool mygo vet .
```

These commands are evidence commands, not a built-in promise of a green result. Use the logs for the actual commit under review, and keep the same distinction between focused real-Git/UI tests, a full local suite, and CI's platform-specific build matrix.
