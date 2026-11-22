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
| Stage selected lines | Click diff line numbers, then **Stage**. Shift-click selects a range; Escape clears the selection. **Select changes** (or `m` in the diff) shows the hunk actions and the line row together; the selected lines are listed by number, highlighted all at once, and can also be discarded (after confirmation) or copied as a patch. |
| Build a Custom Patch | In History or Graph, use `Ctrl+B` on a commit or historical file to add canonical whole-file patches. In its diff, select changed lines and press `Space`, or press `A` for the current hunk. Press `B` to review the basket, preview the combined patch, then export it, apply it atomically to the working tree without staging, or send it to another worktree. This never rewrites history. Different commits cannot contribute changes to the same old/new path; additions, deletions, renames, binary files, and mode-only changes require whole-file selection. |
| Copy changes to another worktree | Open a changed file's menu and choose **Send file to worktree…**, or choose **Send to worktree…** in Custom Patch Builder. Change Courier freezes the selected patch, previews it against an existing destination worktree, and reports conflicts without writing. A clean delivery applies atomically as unstaged destination changes; the source and all commit history remain unchanged. To target an unchecked-out branch, create a worktree for it first. |
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
| Recover work | Use Reflog to inspect earlier states and create a branch, check out, or reset after confirmation. In **Saved work → Safety Journal**, review supported local commits, merges, checkouts, discards, resets, rebases, branch operations, stash changes and patch applications. Recoverable operations offer immediate notification Undo and remain listed durably. Undo compares immutable before/after restore points across raw files, index, operation metadata, local refs and configuration, and refuses when newer content or Git state does not match. Interrupted restoration keeps both recovery points without pretending its rollback completed. |
| Adjust the workspace | Drag dividers; use expand/shrink icons for full-window views. The folder button toggles directory grouping. |

All workspace sections are visible by default; choose which ones to show in **Settings → Workspace sections**. Fresh profiles can also choose whether repositories open on **Overview** under **Settings → Workspace → Repository start**; profiles saved before Overview existed are moved onto Overview once, and you can change the choice back.
The Header and Status bar buttons are also all visible by default and can be hidden independently under
**Settings → Workspace → Toolbar buttons**, including hiding every button in either group. The repository path
and its copy button always remain in the Status bar. Hiding a button only removes that entry point; it does not
stop terminal sessions, background refresh, or other activity.
Use **Settings → Updates** to check the installed version and available updates.
The Editor virtualizes large file trees, rebuilds them only when their contents or folding changes, and renders only the active editor tab.

Author avatars load in the background from GitHub, GitLab.com, or self-managed GitLab, using the configured remote host (or its custom web URL). Public lookups work without a hosting CLI; restricted lookups can reuse an existing local `gh` or `glab` login. Images are cached locally. Missing accounts, unavailable images, network failures, and authentication failures silently keep the colored author initial. TanGit never starts a login flow for an avatar lookup. In SSH workspaces, avatar requests still run on the desktop; a company GitLab server must be reachable from that machine.

## Workflow entry points

### Command Palette

Press **Ctrl/Cmd+Shift+P**, or use the **Command Palette** button, the first
button in the repository toolbar. The palette is available on Home, in Git
repositories and in Editor-only workspaces. Search indexes translated labels,
canonical English commands, command IDs, aliases and chords. Empty search shows
sections for pinned commands, current-context suggestions, recent commands and
navigation; commands you run often rise in the list. Unavailable actions remain
visible, dimmed, with the reason; when a reason has an obvious fix (for example
"No staged changes to commit"), **Tab** offers it. Use Up/Down, PageUp/PageDown
and Enter, or click a result. **Ctrl+B** pins the selected command. Pins,
recents, usage counts and shell history are stored with your settings.

Commands execute by stable ID and are re-resolved immediately before running,
after any tab switch or background refresh. Selecting a destructive action only
opens its existing confirmation, preview or form—it never confirms the Git
mutation itself. Hunk and conflict actions use the same canonical patch,
selection and availability rules as their toolbars. While the palette is open,
ordinary repository single-letter shortcuts stay out of its text field. Escape
closes it and MyGo restores the previous control or the section's primary pane.

#### Search modes

Start the query with a character to search something other than commands:

| Type | Searches | Enter | **Tab** actions |
| --- | --- | --- | --- |
| `>` | Commands only | Runs the command | — |
| `@` | Local and remote branches | Checks the branch out | List its commits, copy the name, merge into the current branch, rebase onto it, delete it |
| `#` | Commits, by message, hash, author and branch | Lists every action for the commit | Show in History, copy the hash |
| `/` | Changed files, and every file the Editor has indexed | Selects or opens the file | Stage or unstage, copy the path |
| `~` | Recent repositories | Opens the repository in a tab | — |
| `!` | A shell command line | Runs it in the repository root | — |

Pasting a commit hash (4–40 hexadecimal digits) in the default mode finds that
commit without a prefix. The search runs in Git across every branch, so it
reaches commits that are not in the loaded history list.

**Searching commits.** `#` takes the same fields as the History filter. Plain
words match the message or hash and are also tried as an author name, so `#alice`
finds Alice's commits. Name a field to be exact: `by:` or `author:` for the
author and `branch:` or `on:` for the branch, for example
`#fix by:alice branch:main`. Quote a value that contains a space:
`by:"Jane Doe"`.

**Author suggestions.** In the `#` mode, type `by:` or `author:` and the palette
lists the repository's authors. Keep typing to narrow the list by substring,
for example `#by:ali`. Choosing an author completes the word, for example
`#by:"Alice Example" `, and goes on to show that author's commits. The list
holds up to 30 names.

**Branch suggestions.** `branch:` (or `on:`) works the same way with branches:
the palette lists local and remote branches, narrowed by what you type, and
choosing one completes the word, for example `#fix branch:origin/release/1.0 `.
A half-typed `by:` or `branch:` word never starts a Git search.

**Commit actions.** Choosing a commit row lists everything that can be done with
that commit, the same set as its context menu: copy the hash or subject, mark it
as the rebase base, edit, reword, amend, create or apply fixups, squash, fixup,
move, drop, check it out, undo the latest commit, soft/mixed/hard reset, export
a mailbox patch, create a tag or branch, bisect, cherry-pick, revert, add it to
Custom Patch, and compare it with HEAD or its parent. Type to filter the list;
**Escape** steps back to the commits before it closes the palette. Actions that
rewrite the checked-out branch (rebase, reset, drop, undo) are offered only for
commits in that branch's loaded history, as the context menu does for another
branch's commits. Each action opens its usual confirmation or form.

**Shell commands.** Type `!` and a command line, for example
`!git log --oneline -5`, and press Enter. The palette stays open and shows the
command's progress and output below the list; output streams in as it is
printed. **Cancel command** stops the whole process tree. Closing the palette
does not cancel the command: it keeps running and finishes in the command log,
and reopening the palette shows its state again. When it finishes, **Copy
output**, **Run again** and **Save as command** (adds it to your custom commands)
are available; output beyond 16 KB is cut off. Up and Down recall earlier lines.
`{{branch}}`, `{{commit}}`, `{{file}}`, `{{root}}`, `{{tag}}`, `{{stash}}` and
`{{remote}}` expand to the current selection, shell-quoted. Shell commands run
locally and are unavailable in SSH workspaces. The older `:` prompt is still
available.

Other palette commands include switching themes, toggling side-by-side diffs,
line wrapping and whitespace changes, switching to another open tab, applying or
popping a stash, fetching or browsing a remote, retrying or showing the latest
failed command, customizing commands and opening the Activity Center.

### Activity Center

Press **Ctrl/Cmd+Shift+A**, choose **Open Activity Center** in the palette, or
use **View all activity** on Overview. It lists commands, Safety Journal
records, Agent task activity, transfers and notices across every open
repository, newest first, with their state and time. Search by title,
repository or state; filter by **Needs attention** (warnings and errors), by
source, and—when several repositories are open—by repository. Up/Down and
Enter open a row, and Escape closes the center. A row opens its original viewer
by stable identity (the command log, Safety Journal record, Agent task or
transfer) and closes the center; an evicted record is reported instead of
matching similar text. Session items disappear when the session ends; Safety
Journal records are durable.

The **Contributors** view (or **Show contributors** in the palette) lists who
authored the active repository's commits, across every branch: name, email,
commit count with a bar against the top contributor, and the date of their last
commit, most commits first. People who commit under several names but one email
are one contributor, shown under their newest name. Search by name or email.
Choosing a contributor shows their 20 most recent commits; **Show in History**
narrows the History list to them, **Search in palette** opens the palette on
`#by:"name"`, and a commit opens its full action list in the palette. The list
is read from Git once per opening (at most the newest 200,000 commits) and is
unavailable for non-Git folders. A repository with hundreds of contributors
draws 50 at a time (**Show 50 more** adds the next page); search covers the whole
list, and the list can be ordered by most commits, most recent or name.

### Overview

**Overview** is a workspace section, not a replacement for Changes or History.
A header shows the repository, its branch with ahead/behind counts and its
state; fetching, pulling, pushing and refreshing stay in the toolbar above
rather than being repeated here. **Continue where you left
off** lists what needs doing next, each row striped in its severity color with
one action. Four tiles summarize the working tree (changed files, staged,
modified, untracked and conflicts, with review progress), collaboration
(ahead/behind, upstream and cached pull-request checks), the current
operation and Agent state. A **Contributors** card sits directly under the
header and answers who has been working lately for a period you pick: **Last
week** (the default), **Last month**, **Last 3 months**, **Last 6 months**,
**Last year** or **All time**. It shows how many of the repository's
contributors committed in the period, the period's commit count with the change
against the period before it, a chart of commits over the period, and the five
most active contributors; in a large project it also says how many contributors
were active and what share of the commits the top five made. Choosing a
contributor, or **View all contributors**, opens the full list in the Activity
Center. The history is read from Git once in the background when the Overview
is entered, with every contributor's commit times, and reused for two minutes;
switching the period recounts it instantly. Sessions that cannot read the
history leave the card out. The latest five activity items come last. Attention is ordered from actual failures and Git operations through
conflicts, unsaved buffers, Agent/transfer issues and ordinary sync or review
work. At most three primary items appear. Every action uses the shared Command
Catalog and is checked again before execution.

Repositories open on Overview by default (**Settings → Workspace → Repository
start → Open repositories on Overview**). Profiles saved before Overview existed
are moved onto it once, and the next save writes an explicit marker so a later
choice of another landing page is kept. Overview is the first sidebar section, above Editor, on
fresh profiles; a profile saved before Overview existed is given it once, shown
and first, and made its landing page. Overview remains hidden in
Editor-only folders and can be hidden or reordered like every other Git
section; your choice is kept afterwards. Its two-column layout
follows available content width, so sidebars and terminal docks produce a single
column when necessary. Entering Overview conditionally reuses Agent loading;
it never refreshes Git details redundantly or requests PR checks automatically.
Its **Recent activity** links open the original command, Safety Journal, Agent,
transfer or workspace viewer by stable identity; an evicted record falls back
to that viewer with an explanation instead of selecting a similar text match.

### Command Log filters and incremental history

Open **Command Log** from the status bar, **Overview → Open Command log**, the
palette or `Ctrl+L`. It retains the separate **Background Git commands** and
**Your commands** tabs. Search covers command text and output. Independent
menus select Succeeded, Failed or Running status, recorded invocation sources,
and Last hour, Last 3 hours, Last 24 hours, Last 3 days, Last week or All time.
The initial range is Last 24 hours. Choosing a palette/keyboard/direct source
also selects the corresponding existing tab; choosing Background Git commands
returns to that tab. **Clear filters** removes text/status/source/range limits.

Rows are newest first. A successful command has a green circular-chevron icon
and **Succeeded** badge, with a compact local time; its full timestamp is in
the tooltip. Failed and running records use distinct status indicators. Use
the row menu to show/hide output, copy the command or copy its output. Failures
and filtered matches show output automatically. **Copy log** includes complete
timestamps and all matching output regardless of the current rendered page.

The existing session logger continues to own all command records: pagination
does not repeatedly execute Git, read a new history file or create an activity
database. At first only 20 matching records are displayed. Reaching the end of
the virtualized list reveals the next 20 in memory, until there are no more.
Changing a tab or filter starts a fresh page. Failure-to-log navigation still
finds the exact stable command ID and expands the page sufficiently to reveal
it; ordinary new entries do not unexpectedly reload pages or reset scrolling.

## Operation recovery, history, and assisted review

This section describes behavior in the current implementation. Availability of
an action depends on the live repository state, not merely on the menu or panel
being visible. For complete-workspace restore points, compare-and-swap checks,
interrupted Undo, and explicitly unsupported boundaries, see
[Operation and workspace recovery](operation-recovery.md).

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

### Guided conflict mode

Choose **Settings → Behavior → Conflict and hunk buttons → Guided**. **Guided**
is the third persisted presentation alongside **Icons** and **Icons + labels**
(the stored value is `conflictActionStyle: guided`). It keeps the same conflict
controls and operation drivers, while adding labeled actions and two kinds of
live help:

- the operation area lists **Read conflict**, **Choose or edit Result**, **Mark
  resolved**, **Review staged**, and **Continue** with **Earlier**, **Current**,
  **Next**, or **Available** where applicable;
- the conflict pane explains the selected **Base**, **Current**, **Incoming**,
  or editable **Result** view, adapting the explanation to the pane width.

The checklist is derived from the current operation, actual conflict count,
selected file, Result markers, staged files, and available operation actions.
**Earlier** denotes position, not a durable completion attestation; an
unresolved file never receives a false resolved tick. Guided help does not
choose a side, stage a file, or run Continue for you. Hide or reopen the guide
with **Hide guide** / **Show guide** without changing the repository.

A normal pass is therefore: read the operation progress and current file/hunk,
compare the three read-only stages, edit **Result**, choose **Mark resolved**,
inspect the staged diff under **Review staged**, and then follow the Continue
preflight below. The underlying controls and operation panel are implemented in
[`internal/app/guided_conflict.go`](../internal/app/guided_conflict.go); the
three persisted styles are defined in
[`internal/app/action_controls.go`](../internal/app/action_controls.go).

Recovery guidance is record-driven. With a real selected-path record it offers
**Restore unresolved…** or **Retry…** through the ordinary confirmation path.
Without one it says **No recovery is available for the selected file** and does
not render either action for that file. It never invents a successful restore.

### Continue preflight and trusted checks

For a local workspace, configure **Settings → Behavior → Continue preflight**
before choosing **Continue**. Typical entries are:

| Setting | Example | Blank behavior | Failure policy |
| --- | --- | --- | --- |
| **Required check command** | `go test ./...` | Blocks Continue; Optional does not run | Must pass; failure or cancellation cannot be overridden |
| **Optional check command** | `golangci-lint run` | Shown as **skipped** | A completed **failed** result alone may receive an explicit override |

Required must pass before Optional runs. If Optional fails, inspect its output
and either **Retry checks** or deliberately choose **Override optional failure
and continue**. That override appears for an Optional **failed** state, not for
Optional cancellation. No override bypasses a missing/failed/cancelled Required
check, incomplete or stale source capture, a source-recheck error, checkpoint
failure, or the final operation comparison.

These commands have the same execution trust boundary as Custom Commands.
[`runTrustedShellCommand`](../internal/app/custom_commands.go) runs them through
`sh -c` or `cmd /C` in the repository root, with the desktop user's full
permissions, environment, filesystem access, and cancellation support. **They
are not sandboxed**, may access the network, and may create or modify files.
For an SSH workspace, the **Continue action itself is disabled** rather than
running a local command against a remote checkout. Output, including a string
such as `__IT_IS_SAFE__`, never changes an exit status or grants approval by
itself.

Preflight captures immutable `OperationState` and operation fingerprint data,
plus canonical SHA-256 hashes of index records and the content/type of tracked
paths and ordinary non-ignored untracked paths. Same-size or same-line-count
edits therefore still invalidate this comparison. After the commands it
recaptures those inputs and displays changed paths, output, cancellation, and
errors. Retry captures a fresh source; a stale retry uses the operation that
was just shown rather than repeatedly expecting the old fingerprint.

The comparison is not a sandbox or a complete record of process side effects:
standard ignored files, writes outside the checkout, and network effects are
not part of these changed-file hashes. When the captured operation is unchanged
but its tracked/index/non-ignored content changed, that detected change needs
a successful checkpoint before Continue becomes available. If nothing in the
captured source changed, checkpoint is **skipped**. Capture and recheck do not
stage, restore, or create an operation commit; a configured shell command may
invoke Git or mutate anything else its user permissions allow.

Finally, both ordinary approval and the Optional-failure override capture the
source once more, compare it with what was reviewed, and call the ordinary
operation driver's expected-fingerprint CAS. A fresh mismatch refuses Continue;
there is no second unchecked Git Continue path. The repository source capture
is in [`internal/repository/continue_review.go`](../internal/repository/continue_review.go),
and the UI gates are in
[`internal/app/continue_preflight.go`](../internal/app/continue_preflight.go).

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

V2 input is fail-closed. Malformed JSON, binary/NUL bytes in a manifest, or
trailing garbage, unsupported or unknown fields, invalid progress, unsafe
paths, and inconsistent digests do not become a ready restore. Restore also validates content-addressed
payload bytes—including binary content—before its file/index CAS. Corrupt or
truncated payloads are rejected without overwriting newer same-path work or
changing its index; **Retry…** remains an explicit later attempt, not an
automatic repair or a success claim. If optional recovery metadata cannot be
discovered, the rest of DetailsSnapshot remains available, no stale
restore/retry action is published from that discovery, and the diagnostic is
written to the command log. The exact corruption, concurrency, and fuzz scope
is listed under [verification evidence](verification.md#feature-specific-evidence-and-limits).

### Changes hunks and Custom Patch history

In an ordinary textual **Changes** diff, press **Select changes** (or `m` while
the diff is focused) to show the hunk actions, then move the caret into a hunk
and use **Stage hunk**, **Unstage hunk**, or **Discard hunk**. The tools sit in a
bordered group, apart from the previous/next file buttons; `[` and `]` move
between hunks and `s`/`d` stage or discard the current hunk with or without the
mode on. The toolbar derives a
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

1. Read the operation card's actual type and current step. With **Guided**
   enabled, use its live checklist, but still compare Base, Current, and
   Incoming before editing Result with the real conflict controls. Optionally
   request and inspect Conflict Assistant suggestions; accepting them still
   writes Result only.
2. Choose **Mark resolved**, inspect the staged result, and repeat for all
   conflicted files. If necessary, restore the original unresolved file and
   stages from durable Conflict recovery. If a newer same-path edit causes a
   CAS refusal, keep that work and review it instead of retrying blindly.
3. Return to **Rebase TODO** and review current/pending entries. Continue only
   after conflicts are resolved and staged. In the local preflight, review the
   Required/Optional output, changed captured paths, and checkpoint result;
   then choose **Continue operation**, or explicitly override only an Optional
   failure you intend to accept. Use Skip only when offered and you intentionally
   want to discard the stopped step; use Abort to abandon the operation.
4. For unrelated ordinary edits, use the Changes hunk toolbar to stage only
   what belongs in the next commit, then review and commit normally after the
   interrupted operation ends.
5. If a later Stack Sync or Custom Patch history edit is needed, create a new
   preview for that purpose and review its exact source and destination. Do
   not infer success from the preview: apply results, CAS refusals, conflicts,
   and independent push outcomes are reported separately.

[Verification scope and current limits →](verification.md#feature-specific-evidence-and-limits)

## Large repositories and refresh

No separate mode or setting is required. Open **History**, **Graph**, and
**Changes** normally. The shared numstat reader advances through each
NUL-delimited record instead of splitting the entire output into temporary
record and path slices. It preserves commit boundaries and rename path fields;
binary `-` entries remain zero textual line additions/deletions. See the helpers in
[`internal/repository/commit_stats.go`](../internal/repository/commit_stats.go)
and their use for status/commit files in
[`internal/repository/session.go`](../internal/repository/session.go).

Selecting a different commit cancels obsolete `CommitFiles` and
`CommitFileDiff` reads; selecting a different Changes file cancels the obsolete
worktree diff. Monotonic request generations also protect both file-list and
diff publication: if selection goes A → B → A, a late result from the first A
cannot replace the current A view even when a repository reader ignores
cancellation. Canceled or stale
callbacks do not request a repaint. These paths are in
[`internal/app/actions.go`](../internal/app/actions.go) and
[`internal/app/view_history.go`](../internal/app/view_history.go).

A quiet Changes refresh still performs its repository/Git read. For the same
request key and generation, an existing textual state, and the same full patch
and kind, TanGit reuses that `DiffState` and requests no repaint instead of parsing and
laying out identical bytes again. Initial loads, changed patches, binary/media,
and patchless paths keep their existing behavior. This is a narrow unchanged-
patch fast path, not a claim that Git reads disappear or that `CoreSnapshot`
is uniformly faster. The fixed fixture, stage boundaries, competing benchmark
runs, host details, and negative results are in
[Large-repository performance evidence](performance.md#scope-and-outcome).

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
| Tools | `Ctrl/Cmd+Shift+P` Command Palette; `Ctrl+L` command log with user/background sections and highlighted search; `:` one-off command, `Ctrl+,` Settings |

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
