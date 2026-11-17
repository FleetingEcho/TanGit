# TanGit guide

[English overview](../README.md) · [简体中文概览](../README-CN.md)

[Terminal setup](#open-a-repository-from-the-terminal) · [Workflows](#common-workflows) ·
[Recovery and assisted review](#operation-recovery-history-and-assisted-review) ·
[Shortcuts](#keyboard) · [Development](#development)

## Open a repository from the terminal

After setup, run `tangit .` or `tangit "path/to/repository"`.
Relative paths use your current directory. Git must be on `PATH`. The command
returns immediately. If TanGit is already running, it focuses that window and
opens the path in a new tab, or selects its existing tab. A Git repository opens
the full workspace; a plain folder opens the Editor-only workspace without
running `git init`.

### macOS

Move `TanGit.app` to Applications, then install the bundled launcher once:

```sh
sudo mkdir -p /usr/local/bin
sudo install -m 755 /Applications/TanGit.app/Contents/Resources/tangit /usr/local/bin/tangit
command -v tangit
tangit .
```

The v1.0.2 DMG has no bundled launcher. From a source checkout, use
`sudo install -m 755 resources/darwin/tangit /usr/local/bin/tangit` instead.
For installation without administrator access, use `~/.local/bin` if it is on your `PATH`.

The launcher returns immediately and opens the repository in the running app, or starts TanGit when it is not running.
It finds TanGit in `/Applications` or `~/Applications`. For another location:

```sh
TANGIT_APP="/path/to/TanGit.app" tangit .
```

### Windows

Current installers add `%LOCALAPPDATA%\Programs\TanGit` to your user `PATH`.
Open a new terminal after installing:

```powershell
tangit .
tangit "C:\Projects\Repository with spaces"
```

If the command is missing, check `Get-Command tangit` in PowerShell or
`where tangit` in Command Prompt. You can always launch by its full path:

```powershell
& "$env:LOCALAPPDATA\Programs\TanGit\TanGit.exe" .
```

For older installers or extracted archives, add the folder containing
`TanGit.exe` to PATH once; adjust `$dir` for a custom location:

```powershell
$dir = Join-Path $env:LOCALAPPDATA 'Programs\TanGit'
$userPath = [Environment]::GetEnvironmentVariable('Path', 'User')
if (($userPath -split ';') -notcontains $dir) {
    $newPath = if ($userPath) { "$userPath;$dir" } else { $dir }
    [Environment]::SetEnvironmentVariable('Path', $newPath, 'User')
}
if (($env:Path -split ';') -notcontains $dir) { $env:Path += ";$dir" }
Get-Command tangit
```

### Linux

Packages provide `tangit`; the standalone installer uses `~/.local/bin`.
If upgrading from v1.0.0 through the in-app updater, reinstall a newer package
once to install the launcher. System-managed packages should be updated with
their package installer.

## Common workflows

| Task | Steps |
| --- | --- |
| Clone and open a repository | On Home, choose **Clone** or press `C`. Enter an HTTPS, SSH, or scp-like URL, optionally choose a branch, and select a native destination parent. **Clone and open** shows Git's real progress and opens the completed repository. Escape cancels Git without deleting the destination. Credential-bearing URLs are rejected; authentication uses your configured Git credential helpers or SSH agent. |
| Open a plain folder | At the top of Home, switch **Open mode** to **Editor**, then enter or browse to a folder. The workspace shows only **Editor**, with the file tree, content search and replace, editing and saving, structural file operations, and the local terminal. TanGit does not initialize Git or change your saved section visibility. **Recent folders** is independent of **Recent repositories**, including when the same path appears in both; Editor recents persist across restarts. If the selected folder already belongs to a Git repository, it opens the full Git workspace instead. |
| Open an SSH workspace | On Home, choose **Open SSH**, then select or edit a profile. Agent, private-key, and password authentication are supported; secrets use the OS keychain when available and otherwise remain session-only. Unknown host keys require confirmation and changed keys are rejected. Git, files, search, terminals, and transfers execute directly on the remote Linux host without a local mirror or server agent. |
| Stage selected lines | Click diff line numbers, then **Stage**. Shift-click selects a range; Escape clears the selection. |
| Build a Custom Patch | In History or Graph, use `Ctrl+B` on a commit or historical file to add canonical whole-file patches. In its diff, select changed lines and press `Space`, or press `A` for the current hunk. Press `B` to review the basket, preview the combined patch, then export it, apply it atomically to the working tree without staging, or send it to another worktree. This never rewrites history. Different commits cannot contribute changes to the same old/new path; additions, deletions, renames, binary files, and mode-only changes require whole-file selection. |
| Copy changes to another worktree | Open a changed file's menu and choose **Send file to worktree…**, use the diff's **Send current hunk** or **Send selected lines** action, or choose **Send to worktree…** in Custom Patch Builder. Change Courier freezes the selected patch, previews it against an existing destination worktree, and reports conflicts without writing. A clean delivery applies atomically as unstaged destination changes; the source and all commit history remain unchanged. To target an unchecked-out branch, create a worktree for it first. |
| Maintain linked worktrees | Open **Worktrees** to create, search, open, lock, unlock, or remove linked checkouts. Locked and stale records are marked in both the list and details. **Repair metadata** fixes worktree links after repositories or checkouts move; **Prune stale records…** removes only administrative records whose directories are gone. **Force remove…** is separately confirmed and can delete a dirty or locked checkout, so review its path and changes first. |
| Apply an external patch | In **Changes**, choose **Apply patch…** or press `Ctrl+I`, then select a `.patch` or `.diff` file. Review its files, diagnostics and text before applying. Clean patches apply atomically to the working tree and stay unstaged; conflicted patches cannot be applied. Cancel or Discard Patch changes neither the repository nor the source file. |
| Exchange mailbox patches | Export selected commits from their History context menu or the batch-actions menu into a local folder as `format-patch` files. Import a patch series with **Changes → Apply mailbox patch…**. The import runs like `git am --3way`: authors and messages are preserved, context drift gets a three-way fallback, and conflicts use the normal Continue/Abort controls. |
| Pause and edit an older commit | Select the commit in History or Graph, press `e`, and confirm **Edit commit**. The later commits are replayed up to that point, and the rebase pauses with the chosen commit checked out. Edit or amend it, then press **Continue**; **Abort** restores the original history. |
| Bisect a regression | In History or Graph, right-click a known-bad commit and start bisect, naming a known-good revision. Use **Good** / **Bad** / **Skip** for each tested revision from the banner or commit menu. Git reports the first bad commit. **Reset bisect** returns to your original checkout. |
| Move work to a new branch | In Branches, right-click the checked-out branch and choose **Move unpushed commits to new branch…**. TanGit creates the new branch with those commits, rewinds the original branch to the fork point from its upstream or the default branch, and checks out the new branch. Commit or stash tracked changes first. This requires an upstream or a recognizable default branch. |
| Create a pull request and read checks | Authenticate `gh` for GitHub or `glab` for GitLab, then open the hosting menu in the toolbar or the current branch's menu. Choose **Create pull request**, **Open pull request in browser**, or **View pull request checks**. The CLIs run in the repository, including over SSH; authentication and other CLI output appear in the command log. |
| Add custom commands | Use **Settings → Commands**. Each command runs in the repository root and can have an optional shortcut and workspace scopes. Placeholders such as `{{branch}}`, `{{commit}}`, `{{file}}`, `{{tag}}`, `{{stash}}`, `{{remote}}`, and `{{root}}` expand to shell-quoted values. Press `:` for a one-off command (locally, not for an SSH workspace); output is recorded in the command log. |
| Edit files in TanGit | Open **Editor**, select or quick-open a file, and edit its persistent tab. Use Ctrl/Cmd+S to save; a dot marks unsaved tabs. Closing asks whether to save or discard changes. Right-click the code editor and choose **Format Document**, or select one non-empty range and choose **Format Selection**. Configure an optional **Formatter command** for the file's language in **Settings → Editor**—for example, `gofmt` or `prettier --stdin-filepath {{file}}`. Install the formatter yourself; TanGit never downloads it implicitly. The command is parsed without a shell, receives the document or selection on stdin, and replaces that range with stdout. `{{file}}` expands to the current absolute path for parser and configuration discovery. Formatter settings remain available when the language server is disabled. An external command takes precedence; otherwise TanGit uses an enabled language server advertising document or range formatting. Formatting runs asynchronously with a 10-second timeout. Errors, unexpected empty output, and further edits leave the buffer unchanged; successful formatting is one undoable edit. External commands run only in local workspaces. |
| Search the workspace | Press `Ctrl+Shift+F` in **Editor**. Results appear after the first scan page, then continue loading in the background until all matching files and occurrences are included. Partial counts carry `+`; completed counts are exact. Use case, whole-word, regular-expression, folder, and include/exclude filters to narrow the search. |
| Manage editor tabs | Right-click a file tab to close it, other tabs, tabs to the right, or all tabs, or to copy its absolute or repository-relative path. |
| Manage files and folders | Use **Editor → New File / New Folder**, or right-click an explorer row to create, rename, delete, view Git file history, or copy paths. Creation starts in the selected folder; deleting asks for confirmation. **Open in explorer** uses the host file manager locally and launches `Narwhal` with `DISPLAY=:0` on an SSH host. |
| Move, copy, and transfer files and folders | Drag an explorer file or folder onto another folder to move it. Drop native files or folders anywhere in the Explorer: local workspaces copy sources recursively without overwriting; SSH workspaces upload through SFTP. Both show aggregate byte/item progress and **Cancel**. Cancelling a local import stops between I/O chunks and removes only incomplete outputs owned by that import; closing the workspace also cancels its imports. Remote files and folders can be downloaded from their context menus, with progress, collision choices, cancellation, and retries. Open editor tabs and unsaved buffers follow renamed or moved files. |
| Use and position the terminal | Open the status-bar terminal icon. Its toolbar position menu docks the same live terminal at the **Bottom**, **Left**, **Right**, or **Top**, including beside the Editor. Choose the default for new docks in **Settings → Workspace**. Moving the dock preserves running shells, tabs, splits, search, and scrollback. Use **+** for an independent tab or the split buttons for panes within a tab. Use the pencil or right-click menu to rename; the menu also closes the selected tab. Closing a tab asks for confirmation only when that tab contains multiple shells. An individual terminal’s **X** asks whenever any other terminal exists; the last terminal closes directly. Maximize fills the content area and hides redundant per-shell title bars; restore returns to the previous dock width or height. |
| Use an external editor | Set **Settings → Editor**, then click its editor button. Neovim opens in a terminal. |
| Edit several commits | Select rows with checkboxes or `Ctrl+Space`, then use **Batch actions** or the right-click menu. Rewrites require consecutive commits on the current branch. |
| Resolve conflicts | Choose or edit hunks, stage each resolved file, then continue the interrupted operation. |
| Recover work | Use Reflog to inspect earlier states and create a branch, check out, or reset after confirmation. In **Saved work → Safety Journal**, review discards, resets, rebases, branch deletions, dropped stashes, and patch applications. Successful operations offer immediate notification Undo and remain listed durably. Undo uses the exact operation's pinned object, saved work, or patch bytes and stops if HEAD or newer working-tree changes no longer match. Interrupted or failed entries stay visible without an unsafe Undo action. |
| Adjust the workspace | Drag dividers; use expand/shrink icons for full-window views. The folder button toggles directory grouping. |

All workspace sections are visible by default; choose which ones to show in **Settings → Workspace sections**.
The Header and Status bar buttons are also all visible by default and can be hidden independently under
**Settings → Workspace → Toolbar buttons**, including hiding every button in either group. The repository path
and its copy button always remain in the Status bar. Hiding a button only removes that entry point; it does not
stop terminal sessions, background refresh, or other activity.
Use **Settings → Updates** to check the installed version and available updates.
The Editor virtualizes large file trees, rebuilds them only when their contents or folding changes, and renders only the active editor tab.

Author avatars load in the background from GitHub, GitLab.com, or self-managed GitLab, using the configured remote host (or its custom web URL). Public lookups work without a hosting CLI; restricted lookups can reuse an existing local `gh` or `glab` login. Images are cached locally. Missing accounts, unavailable images, network failures, and authentication failures silently keep the colored author initial. TanGit never starts a login flow for an avatar lookup. In SSH workspaces, avatar requests still run on the desktop; a company GitLab server must be reachable from that machine.

## Operation recovery, history, and assisted review

This section describes behavior in the current implementation. Availability of
an action depends on the live repository state, not merely on the menu or panel
being visible.

### Real operation context and conflict controls

When a merge, rebase, pull-with-rebase, mailbox `am`, cherry-pick, or revert
stops, the operation card above **Changes** reads Git's own metadata. It shows
the effective operation type, known full refs and object IDs, and actual step
progress. It does not infer this context from the selected branch or history
rows. During an interactive rebase, **Back to Rebase TODO** returns to Git's
real current, pending, and completed steps.

The header and rebase action area offer **Continue**, **Skip**, and **Abort**
only when the current operation reports those transitions. In particular,
Skip is not universal. Resolve and stage every conflict before Continue; Skip
explicitly discards the stopped step; Abort asks Git to abandon the operation.
The action is bound to the reviewed operation fingerprint, which includes its
metadata and unmerged index stages. A fingerprint change or an unsupported
transition is refused rather than approximated against different state.

The conflict pane groups controls by purpose:

- **Hunk**: navigate conflict regions and see the current region or whole-file
  conflict status.
- **View**: read immutable **Base** (stage 1), **Current** (stage 2), and
  **Incoming** (stage 3) versions beside the editable working-tree **Result**.
  **Review both sides** compares Current with Incoming. Conflict Assistant is
  also in this group.
- **Apply**: in Result, use Current, Base when present, Incoming, or Both for
  the selected region; edit text directly; undo a region choice; or restore the
  initial conflict. Whole-file add/delete conflicts offer meaningful side
  choices rather than nonexistent hunk actions. Choose **Mark resolved** to
  stage the reviewed file.

The groups wrap or become compact menus as the pane narrows. Disabled controls
report their actual reason, and switching to an immutable stage does not turn
a Result mutation into a write to that stage.

### Durable single-file conflict recovery

The first resolution mutation on a supported path creates a V2 record before
changing that path. It preserves the original working-tree bytes, executable
mode or deletion, and exact unmerged index stages 1/2/3; later choices and
staging update the record's after-state without replacing that baseline. Raw
payloads are content-addressed, so restoring index stages does not depend only
on those blobs still being reachable. Records and two-phase progress survive
an application restart. Symbolic links, gitlinks/submodules, and non-regular
file states are refused rather than recovered approximately.

Open **Recovery & checkpoints → Recovery**, select the file's **Conflict
recovery** entry, and explicitly choose **Restore unresolved…** (or **Retry…**
after interruption). Restore compares the current path with the recorded
before/after worktree and index states before each phase. It can resume a
verified file-only step without blindly writing it again. New work on the same
path causes a CAS conflict and is preserved; unrelated paths do not block
restore. This is separate from whole-worktree checkpoints and the older Safety
Journal.

### Changes hunks and Custom Patch history

In an ordinary textual **Changes** diff, move the caret into a hunk and use
**Stage hunk**, **Unstage hunk**, or **Discard hunk**. The toolbar derives a
canonical patch from that hunk—not rendered row positions—and previews the
exact patch against the source state before applying it. Staging affects the
index only; unstaging leaves the edit in the working tree; discarding asks for
confirmation, reverses only that hunk in the working tree, and leaves the index
untouched. Combined conflict diffs, binary and mode-only changes do not have
hunk actions; a staged deletion must be unstaged as a whole file. The inline
reason explains when a whole-file action or conflict resolution is required.

Custom Patch keeps direct patch application separate from history rewriting.
Direct **Preview Custom Patch** binds the patch bytes and selected target,
direction, and three-way option. **History actions…** instead supports:

- **Remove from original commit…**
- **Move out into index…**
- **Move to selected commit…**
- **New commit before original…** and **New commit after original…**

Every history edit requires selections from exactly one non-merge source
commit, a patch checked against that source, and the expected full HEAD SHA
and branch ref. Move destination must be another non-merge commit in current
HEAD history. The journal saves full hashes, patch digest, replay stage, and
stash/index progress before potentially stopping actions, so the ordinary
Operation UI can resume after conflicts or a restart. Direct Apply target,
reverse, and three-way options do not silently become history policy.

History editing does not support multiple source commits in one replay,
new-before on a root commit, or `commit.gpgsign=true`. The last case is refused
before the resumable driver creates a stash, object, or rebase; an interactive
signing/pinentry flow must not be mistaken for a recoverable non-interactive
replay.

### Visual Rebase TODO and marked-base workflows

In **History → Rebase TODO**, prepare a plan for an explicit target ref. TanGit
asks Git to generate the actual interactive todo, captures it, and aborts the
capture-only rebase before showing the draft. It does not execute the picks
while preparing the preview. With no marked base, this is an ordinary rebase
preview. Use a commit's **Mark as base commit for rebase** menu action to pin a
full commit SHA; the draft then corresponds to `git rebase --onto <target>
<full-base-SHA>`. Preparing another fresh plan is required after the target or
source changes.

**Edit & start** binds the edited draft to its original todo, source HEAD, and
target. Commit lines may be reordered or have their actions changed; use `drop`
for an unwanted commit rather than deleting or duplicating a line. Non-commit
directives such as `exec`, `update-ref`, `reset`, and `label` retain their exact
identity. For an active rebase, **Edit TODO** compares the complete remaining
todo and operation fingerprint, and Git's own sequence-editor handoff compares
the todo again before replacing it. Malformed plans remain visible while Abort
stays available. A conflict stop returns to the same real todo and operation
driver rather than creating a parallel synthetic operation.

### Stack Sync plans and publication

Choose **Branches → Local branches → Sync stack…**. Planning fetches first,
then freezes live local/remote OIDs, topology, upstreams, worktree
observations, and other open operations into a fresh per-branch plan. Choose
the rebase or merge divergence strategy and whether to enable **Publish changed
branches** before reviewing the exact local action and expected remote OID.

Apply validates the complete plan SourceID again. Independent non-checked-out
ref updates use an expected-old-OID `update-ref` transaction; a rejected CAS
rejects that whole transaction. Checked-out branches run real Git merges or
rebases. When `rebase.updateRefs` is enabled, Git's generated TODO owns the
refs it actually lists, rather than TanGit inventing a second set of updates.
A stale plan must be refreshed, not reused.

The batch transaction does not make every later merge, rebase, and push atomic.
Per-branch outcomes and local progress remain inspectable when a real Git
operation stops. Publication then attempts eligible branches independently
with `--force-with-lease=<ref>:<preview-remote-OID>`, including fast-forward
publication. One push failure does not roll back local updates or another
remote branch. There is no remote stack transaction, and no failed publication
is reported as success.

### Optional external assist helpers

Conflict Assistant and Change Splitter use the current Agent Profile's optional
**AssistCommand**. Opening either dialog does not run a helper. Configure a
trusted command such as the following shape, then explicitly request
suggestions:

```sh
advisor {requestFile} {responseFile} {artifactDir}
```

`{requestFile}` and `{responseFile}` must each occur exactly once;
`{artifactDir}` is optional and may occur once. TanGit writes a versioned JSON
request to a private per-run directory, runs the expanded command through the
platform shell in its artifact directory, validates the response's source and
identity hashes, and removes the temporary directory after the run. This
helper is an arbitrary user-configured program with the user's normal process
permissions and may itself use the network. The request also contains the
conflict text or canonical diff being reviewed. **Private files and response
validation define the protocol; they are not an OS sandbox.** Use only a helper
you trust with that source and with your account. There is no built-in model or
service-provider dependency.

The `agentassist` advisor has no repository or Git write capability:

- Conflict suggestions replace identified regions on a cloned Result. All
  original regions need validated suggestions before **Accept Result only** is
  available; exclusions remain visible and make the Result incomplete. Accept
  rechecks raw path bytes, unmerged stages, and operation fingerprint, then
  writes Result only with V2 recovery. It does not stage, continue, commit, or
  run a hook.
- Change Splitter accepts groups of original file/hunk/line identities, not an
  AI-authored patch. TanGit constructs and Git-previews each generated patch
  against a frozen source. **Apply group to index** is a separate explicit
  action using that same CAS preview; it stages one group, leaves the working
  tree unchanged, and never creates a commit automatically. Uncovered source
  identities require visible exclusions. Binary input stays local and is only
  an indivisible whole-file group.

Closing or replacing the owning dialog invalidates late responses. Changed
bytes, index state, operation state, source selection, or preview ownership
also prevent an old suggestion from being accepted or applied. For the
distinction between these previews and task CLI mutations, see
[Local agent automation](agent-automation.md#relation-to-in-app-assist-helpers).

### Example normal workflow

A typical interrupted-rebase workflow is:

1. Read the operation card's actual type and current step. In **Changes**,
   compare Base, Current, and Incoming before editing Result with the grouped
   conflict controls. Optionally request and inspect Conflict Assistant
   suggestions; accepting them still writes Result only.
2. Choose **Mark resolved**, inspect the staged result, and repeat for all
   conflicted files. If necessary, restore the original unresolved file and
   stages from durable Conflict recovery. If a newer same-path edit causes a
   CAS refusal, keep that work and review it instead of retrying blindly.
3. Return to **Rebase TODO** and review current/pending entries. Continue only
   after conflicts are resolved and staged. Use Skip only when offered and you
   intentionally want to discard the stopped step; use Abort to abandon the
   operation.
4. For unrelated ordinary edits, use the Changes hunk toolbar to stage only
   what belongs in the next commit, then review and commit normally after the
   interrupted operation ends.
5. If a later Stack Sync or Custom Patch history edit is needed, create a new
   preview for that purpose and review its exact source and destination. Do
   not infer success from the preview: apply results, CAS refusals, conflicts,
   and independent push outcomes are reported separately.

[Verification scope and current limits →](verification.md#feature-specific-evidence-and-limits)

## Keyboard

`?` shows context help; `F1` opens the complete, searchable shortcut reference.
Commands follow the focused panel. Uppercase letters mean **Shift + letter**.

| Context | Keys |
| --- | --- |
| Controls / panels | `Tab` / `Shift+Tab`; `F6` / `Shift+F6`; `Enter` activates; `Escape` returns |
| Lists / search | Arrows or `j` / `k`; `/` searches; `F3` / `Shift+F3` moves between matches; `Shift+F10` opens the row menu |
| Repository tabs | `Cmd/Ctrl+T` new, `Cmd/Ctrl+W` close; `Ctrl+Tab` / `Ctrl+Shift+Tab` switch |
| Home | `C` opens remote clone; `Escape` closes the form or cancels cloning |
| Global Git actions | `p` pull, `P` push, `z` / `Z` undo / redo, `F5` refresh |
| Changes | `Space` stage/unstage, `a` stage all, `c` compose, `A` amend, `s` stash, `d` discard, `b` blame, `Ctrl+I` import patch, `M` import mailbox patch |
| Commit form | `Enter` submits the summary; `Ctrl+Enter` commits; `Escape` closes |
| Editor | `Ctrl+P` quick open; `Ctrl+Shift+F` searches workspace content; `Ctrl+W` closes the editor tab |
| History / Graph | `e` pauses a rebase to edit a commit; `r` reword, `s` squash, `f` fixup, `d` drop, `t` revert; `Ctrl+J` / `Ctrl+K` reorder; `Ctrl+B` adds the selected commit/file to Custom Patch; `B` reviews it |
| Bisect | `b` start / mark bad; `g` good; `k` skip; `R` reset |
| Cherry-pick | `C` copies selected commits; `V` confirms applying them |
| Branches | `Space` checkout, `n` create, `M` merge, `r` rebase, `u` unset upstream, `d` delete; `Shift+O` / `Shift+G` / `Shift+K` create / open / check a pull request |
| Stash / tags | Stash: `Space` apply, `g` pop, `d` drop. Tags: `n` create, `P` push, `Shift+A` push all, `d` delete |
| Diff review | `[` / `]` changes hunk; `v` starts line selection; `Shift+Up/Down` extends it; `Space` stages/unstages in Changes or adds selected lines to a historical Custom Patch; `A` adds the current historical hunk |
| Diff display | `W` compares refs; `Alt+W` toggles whitespace; `Alt+U` cycles context |
| Conflicts | `F7` / `Shift+F7` moves; `Alt+1`–`Alt+4` selects current/ancestor/incoming/both; `Alt+Z` undoes a choice |
| Terminal | ``Ctrl+` `` or `Ctrl+~` shows or hides the dock from any workspace or form without changing sections; its position, tabs, splits, search and scrollback stay alive while hidden |
| Tools | `Ctrl+L` command log with user/background sections and highlighted search; `:` one-off command, `Ctrl+,` Settings |

**Background fetching:** **Settings → Behavior** controls how often every remote is
fetched and whether stale remote-tracking branches are pruned. The Branches
header offers bulk deletion of local branches whose upstream is gone. Pushing
also offers dependent branches stacked below the current branch, oldest first.

## Development

Requires Go **1.27.3** and Git. Dependencies are downloaded from `go.mod`.
macOS builds require macOS 12 or newer; Linux packages use GTK 3 and AppIndicator.

```sh
git clone https://github.com/FleetingEcho/TanGit.git
cd TanGit
go tool mygo dev
```

Before submitting changes:

```sh
gofmt -l .
go vet ./...
go tool mygo vet .
CGO_ENABLED=0 go test ./...
```

Use `go test -race ./...` when changing shared state. See
[Verification](verification.md) for the CI matrix, required OpenSSH mode, smoke
scope, and the limits of local checks. Wrap new UI strings in `tr(...)` and add
translations to every catalog in `internal/app/i18n*.go`.

## Publishing signed updates

```sh
go tool mygo build -platform darwin/universal,windows/amd64,windows/arm64,linux/amd64,linux/arm64
```

macOS signing and DMGs need macOS; Windows installers need NSIS (`makensis`).
Build outputs are grouped by platform under `build/`.

1. Bump `mygo.json`'s version and commit the change.
2. Push the matching version tag to trigger the release workflow.
3. Check installers, signed archives, deltas, and all platform `update-*.json` manifests before publishing the draft.

The workflow needs the `MYGO_UPDATER_PRIVATE_KEY` repository secret. Keep the
same signing key so existing installations accept new updates. In-app updates
require write access to the installation folder.
