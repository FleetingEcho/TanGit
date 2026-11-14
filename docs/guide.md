# TanGit guide

[English overview](../README.md) · [简体中文概览](../README-CN.md)

[Terminal setup](#open-a-repository-from-the-terminal) · [Workflows](#common-workflows) ·
[Shortcuts](#keyboard) · [Development](#development)

## Open a repository from the terminal

After setup, run `tangit .` or `tangit "path/to/repository"`.
Relative paths use your current directory. Git must be on `PATH`. The command
returns immediately. If TanGit is already running, it focuses that window and
opens the repository in a new tab, or selects its existing tab.

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
| Open an SSH workspace | On Home, choose **Open SSH**, then select or edit a profile. Agent, private-key, and password authentication are supported; secrets use the OS keychain when available and otherwise remain session-only. Unknown host keys require confirmation and changed keys are rejected. Git, files, search, terminals, and transfers execute directly on the remote Linux host without a local mirror or server agent. |
| Stage selected lines | Click diff line numbers, then **Stage**. Shift-click selects a range; Escape clears the selection. |
| Build a Custom Patch | In History or Graph, use `Ctrl+B` on a commit or historical file to add canonical whole-file patches. In its diff, select changed lines and press `Space`, or press `A` for the current hunk. Press `B` to review the basket, preview the combined patch, then export it, apply it atomically to the working tree without staging, or send it to another worktree. This never rewrites history. Different commits cannot contribute changes to the same old/new path; additions, deletions, renames, binary files, and mode-only changes require whole-file selection. |
| Copy changes to another worktree | Open a changed file's menu and choose **Send file to worktree…**, use the diff's **Send current hunk** or **Send selected lines** action, or choose **Send to worktree…** in Custom Patch Builder. Change Courier freezes the selected patch, previews it against an existing destination worktree, and reports conflicts without writing. A clean delivery applies atomically as unstaged destination changes; the source and all commit history remain unchanged. To target an unchecked-out branch, create a worktree for it first. |
| Apply an external patch | In **Changes**, choose **Apply patch…** or press `Ctrl+I`, then select a `.patch` or `.diff` file. Review its files, diagnostics and text before applying. Clean patches apply atomically to the working tree and stay unstaged; conflicted patches cannot be applied. Cancel or Discard Patch changes neither the repository nor the source file. |
| Exchange mailbox patches | Export selected commits from their History context menu or the batch-actions menu into a local folder as `format-patch` files. Import a patch series with **Changes → Apply mailbox patch…**. The import runs like `git am --3way`: authors and messages are preserved, context drift gets a three-way fallback, and conflicts use the normal Continue/Abort controls. |
| Pause and edit an older commit | Select the commit in History or Graph, press `e`, and confirm **Edit commit**. The later commits are replayed up to that point, and the rebase pauses with the chosen commit checked out. Edit or amend it, then press **Continue**; **Abort** restores the original history. |
| Bisect a regression | In History or Graph, right-click a known-bad commit and start bisect, naming a known-good revision. Use **Good** / **Bad** / **Skip** for each tested revision from the banner or commit menu. Git reports the first bad commit. **Reset bisect** returns to your original checkout. |
| Move work to a new branch | In Branches, right-click the checked-out branch and choose **Move unpushed commits to new branch…**. TanGit creates the new branch with those commits, rewinds the original branch to the fork point from its upstream or the default branch, and checks out the new branch. Commit or stash tracked changes first. This requires an upstream or a recognizable default branch. |
| Create a pull request and read checks | Authenticate `gh` for GitHub or `glab` for GitLab, then open the hosting menu in the toolbar or the current branch's menu. Choose **Create pull request**, **Open pull request in browser**, or **View pull request checks**. The CLIs run in the repository, including over SSH; authentication and other CLI output appear in the command log. |
| Add custom commands | Use **Settings → Commands**. Each command runs in the repository root and can have an optional shortcut and workspace scopes. Placeholders such as `{{branch}}`, `{{commit}}`, `{{file}}`, `{{tag}}`, `{{stash}}`, `{{remote}}`, and `{{root}}` expand to shell-quoted values. Press `:` for a one-off command (locally, not for an SSH workspace); output is recorded in the command log. |
| Edit files in TanGit | Open **Editor**, select or quick-open a file, and edit its persistent tab. Use Ctrl/Cmd+S to save; a dot marks unsaved tabs. Closing asks whether to save or discard changes. |
| Manage editor tabs | Right-click a file tab to close it, other tabs, tabs to the right, or all tabs, or to copy its absolute or repository-relative path. |
| Manage files and folders | Use **Editor → New File / New Folder**, or right-click an explorer row to create, rename, delete, view Git file history, or copy paths. Creation starts in the selected folder; deleting asks for confirmation. **Open in explorer** uses the host file manager locally and launches `Narwhal` with `DISPLAY=:0` on an SSH host. |
| Move, copy, and transfer files and folders | Drag an explorer file or folder onto another folder to move it. Drop native files or folders anywhere in the Explorer: local workspaces copy sources recursively without overwriting; SSH workspaces upload through SFTP. Both show aggregate byte/item progress and **Cancel**. Cancelling a local import stops between I/O chunks and removes only incomplete outputs owned by that import; closing the workspace also cancels its imports. Remote files and folders can be downloaded from their context menus, with progress, collision choices, cancellation, and retries. Open editor tabs and unsaved buffers follow renamed or moved files. |
| Use terminal tabs and maximize the dock | Open the status-bar terminal icon, then use **+** for an independent tab or the split buttons for panes within a tab. Tabs show their shell and working directory instead of generic numbers. Use the pencil or right-click menu to rename; the menu also closes the selected tab. Closing a tab asks for confirmation only when that tab contains multiple shells. An individual terminal’s **X** asks whenever any other terminal exists; the last terminal closes directly. Maximize fills the content area and hides redundant per-shell title bars; restore returns to the previous dock height. |
| Use an external editor | Set **Settings → Editor**, then click its editor button. Neovim opens in a terminal. |
| Edit several commits | Select rows with checkboxes or `Ctrl+Space`, then use **Batch actions** or the right-click menu. Rewrites require consecutive commits on the current branch. |
| Resolve conflicts | Choose or edit hunks, stage each resolved file, then continue the interrupted operation. |
| Recover work | Use Reflog to inspect earlier states and create a branch, check out, or reset after confirmation. In **Saved work → Safety Journal**, review discards, resets, rebases, branch deletions, dropped stashes, and patch applications. Successful operations offer immediate notification Undo and remain listed durably. Undo uses the exact operation's pinned object, saved work, or patch bytes and stops if HEAD or newer working-tree changes no longer match. Interrupted or failed entries stay visible without an unsafe Undo action. |
| Adjust the workspace | Drag dividers; use expand/shrink icons for full-window views. The folder button toggles directory grouping. |

All workspace sections are visible by default; choose which ones to show in **Settings → Workspace sections**. Use
**Settings → Updates** to check the installed version and available updates.
The Editor virtualizes large file trees, rebuilds them only when their contents or folding changes, and renders only the active editor tab.

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
| Terminal | ``Ctrl+` `` or `Ctrl+~` shows or hides the bottom panel from any workspace or form without changing sections; its tabs, splits, search and scrollback stay alive while hidden |
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

Use `go test -race ./...` when changing shared state. Wrap new UI strings in
`tr(...)` and add translations to every catalog in `internal/app/i18n*.go`.

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
