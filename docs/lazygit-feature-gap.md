# Lazygit feature-gap audit

Audit date: 2026-11-06

This compares the current TanGit workspace against upstream Lazygit at commit
[`bbca3df`](https://github.com/jesseduffield/lazygit/tree/bbca3df2cd064729bd66b909717cb4709ecd7408).
TanGit currently pins Lazygit's Git engine in [`go.mod`](../go.mod), but its UI
only exposes operations declared by TanGit's own
[`repository.Session`](../internal/repository/session.go). Upstream behavior is
documented by Lazygit's [feature overview](https://github.com/jesseduffield/lazygit/blob/bbca3df2cd064729bd66b909717cb4709ecd7408/README.md),
[generated action/keybinding catalog](https://github.com/jesseduffield/lazygit/blob/bbca3df2cd064729bd66b909717cb4709ecd7408/docs/keybindings/Keybindings_en.md),
and [configuration reference](https://github.com/jesseduffield/lazygit/blob/bbca3df2cd064729bd66b909717cb4709ecd7408/docs/Config.md).

## Executive summary

TanGit already covers the everyday happy path well: files and line staging,
committing/amending HEAD, stash basics, commit browsing, branch checkout and
basic merge/rebase, remotes, tags, worktrees, submodules, conflicts, blame,
reflog browsing, file history, and native desktop conveniences.

The largest parity gaps are Lazygit's custom patch workflow, rich
remote/provider workflows, custom commands and configuration, and the long tail
of actions available in each Lazygit panel. Core one-shot history editing and
automatic reflog-based undo/redo are now covered; the native interactive-rebase
todo editor remains a gap.

## Missing or partial

### 1. Advanced interactive history rewriting

TanGit now rewords HEAD or an older commit with its full message body, drops
commits (including merges), reorders non-merge commits, squashes or fixes a
commit into its older neighbor, amends HEAD or an older commit with staged
changes, and creates and applies fixup commits. These operations use Lazygit's
interactive-rebase command layer and TanGit's early daemon entry point
([`history_editing.go`](../internal/repository/history_editing.go),
[`commit_history_actions.go`](../internal/app/commit_history_actions.go)).

The remaining history-editing gaps are:

- a native mid-rebase todo editor with edit, pick, stop, and range operations;
- selecting and operating on multiple commits at once;
- change author or add co-authors;
- mark a base commit and rebase with `--onto`;
- Git bisect (good/bad/skip/reset).

Upstream's full surface is documented in the
[README](https://github.com/jesseduffield/lazygit/blob/bbca3df2cd064729bd66b909717cb4709ecd7408/README.md#features)
and the [Commits action table](https://github.com/jesseduffield/lazygit/blob/bbca3df2cd064729bd66b909717cb4709ecd7408/docs/keybindings/Keybindings_en.md#commits).

### 2. Custom patches and multi-commit selection

Lazygit can build a patch from selected files/lines of an old commit, remove
or reverse that patch, split commits, and copy/paste multiple selected commits.
TanGit supports partial staging of the working tree but has no old-commit
custom-patch builder, range selection, multi-commit cherry-pick queue, or
commit-file patch inclusion controls. See Lazygit's
[custom-patch feature](https://github.com/jesseduffield/lazygit/blob/bbca3df2cd064729bd66b909717cb4709ecd7408/README.md#rebase-magic-custom-patches)
and [Commit files actions](https://github.com/jesseduffield/lazygit/blob/bbca3df2cd064729bd66b909717cb4709ecd7408/docs/keybindings/Keybindings_en.md#commit-files).

### 3. Git-operation undo and redo — supported, with bounded scope

TanGit now undoes and redoes eligible commits, resets, pulls, checkouts, and
completed rebases with `Ctrl+Z` / `Ctrl+Shift+Z`. It reads a fresh unlimited HEAD
reflog, writes Lazygit-compatible `[lazygit undo]` / `[lazygit redo]` markers,
and protects tracked work around hard resets with an auto-stash. Replay state
survives restarts and manual reflog recovery remains available
([`history_undo.go`](../internal/repository/history_undo.go)).

As upstream also notes, replay is refused while a Git operation is in progress.
Root commits and reflog actions outside the supported set cannot be inferred and
are skipped. See Lazygit's [Undo feature](https://github.com/jesseduffield/lazygit/blob/bbca3df2cd064729bd66b909717cb4709ecd7408/README.md#undo)
and [global `z`/`Z` actions](https://github.com/jesseduffield/lazygit/blob/bbca3df2cd064729bd66b909717cb4709ecd7408/docs/keybindings/Keybindings_en.md#global-keybindings).

### 4. Pull-request and forge integration

TanGit has no GitHub/GitLab/Bitbucket/Azure/Gitea/Codeberg integration for:

- creating a pull request from a branch;
- opening a branch, commit, or selected diff line in a browser;
- showing pull-request status/icons;
- copying pull-request URLs;
- configuring custom forge host mappings.

Upstream documents these actions in its
[feature overview](https://github.com/jesseduffield/lazygit/blob/bbca3df2cd064729bd66b909717cb4709ecd7408/README.md#show-github-pull-requests),
[branch/commit action tables](https://github.com/jesseduffield/lazygit/blob/bbca3df2cd064729bd66b909717cb4709ecd7408/docs/keybindings/Keybindings_en.md#local-branches),
and [service mapping configuration](https://github.com/jesseduffield/lazygit/blob/bbca3df2cd064729bd66b909717cb4709ecd7408/docs/Config.md#custom-pull-request-urls).

### 5. Custom commands, command prompt, and keybinding configuration

TanGit includes an embedded terminal and a fixed native shortcut map, but not
Lazygit's `:` command prompt, context-aware custom commands, YAML custom
keybindings, shell-function file, or per-repository/parent-directory config
overrides. Lazygit documents these in
[Custom commands](https://github.com/jesseduffield/lazygit/blob/bbca3df2cd064729bd66b909717cb4709ecd7408/docs/Custom_Command_Keybindings.md)
and [Config](https://github.com/jesseduffield/lazygit/blob/bbca3df2cd064729bd66b909717cb4709ecd7408/docs/Config.md).

### 6. Diffing and external tools

TanGit offers built-in unified/side-by-side layouts, wrapping, fullscreen, and
patch export. It does not expose:

- compare arbitrary refs or two selected commits, reverse comparison, or diff
  against a selected ref;
- external diff renderers and renderer cycling;
- `git difftool` for files/commits/branches/tags;
- toggle whitespace changes;
- change diff context size or rename-similarity threshold at runtime;
- jump to a file inside a multi-file diff;
- open a pull request at the selected diff line.

See Lazygit's [global diff actions](https://github.com/jesseduffield/lazygit/blob/bbca3df2cd064729bd66b909717cb4709ecd7408/docs/keybindings/Keybindings_en.md#global-keybindings)
and [main diff panel actions](https://github.com/jesseduffield/lazygit/blob/bbca3df2cd064729bd66b909717cb4709ecd7408/docs/keybindings/Keybindings_en.md#main-panel-normal).

### 7. Missing actions by panel

The following are exposed by Lazygit's current action catalog but absent from
TanGit's [`Session`](../internal/repository/session.go), menus, and
[`shortcuts.go`](../internal/app/shortcuts.go):

| Panel | Missing Lazygit actions |
| --- | --- |
| Working files | Open with OS default app; add to `.gitignore` or `.git/info/exclude`; commit without hooks; commit using Git editor; find fixup base; per-file `git difftool`; upstream reset options. |
| Commit files | Checkout a file from that commit; discard that file from the commit through rebase; open with default app; edit; `git difftool`; include one/all files in a custom patch. |
| Local branches | Git-flow actions; checkout by typed name/previous branch; move unpushed commits to a new branch; create worktree from selected branch; create/open/copy PR; squash merge; sort branches; choose a new upstream from the local-branch panel or reset upstream; mixed reset. |
| Remote branches | Create worktree directly; sorting; reset; browse its commits; `git difftool`. TanGit can check out locally or detached, merge/rebase, delete on the remote, set the current branch's upstream, and copy the full name. |
| Remotes | Add a fork remote. |
| Stash | Create a branch or worktree from a stash. |
| Tags | Checkout detached; create worktree from tag; delete a remote tag; reset to tag; browse commits; `git difftool`. |
| Worktrees | Open selected worktree directly in the external editor (TanGit instead switches its repository tab/session to that worktree). |
| Submodules | Enter/navigate a submodule repository; initialize separately; update URL; bulk initialize/update actions; copy submodule name. |
| Reflog | Copy rich commit attributes; open in browser; create a worktree; multi-commit cherry-pick; `git difftool`. |

The authoritative upstream inventory is the generated
[keybinding/action catalog](https://github.com/jesseduffield/lazygit/blob/bbca3df2cd064729bd66b909717cb4709ecd7408/docs/keybindings/Keybindings_en.md).

### 8. General list/navigation features

TanGit has native list navigation and `/` quick search on Changes, local and
remote branches, remotes, stash, tags, worktrees, submodules, and reflog.
Searches use trimmed, case-insensitive substring matching, visible/total counts,
first-match selection, and explicit clear actions; row actions retain their
unfiltered repository targets. History and Graph open their structured commit
filter (message, author, and branch) instead. Changes, commit, branch-commit,
and stash file lists also switch between flat paths and sorted directory trees
with per-pane folding and expand/collapse-all controls; directory rows never
become file-action targets.

The remaining navigation gaps are generic range selection, multi-row
operations, and next/previous search-result movement.

### 9. Lazygit configuration compatibility

TanGit intentionally has its own JSON-backed native settings
([`settings.go`](../internal/app/settings.go)); it does not load Lazygit's global,
repository, or parent-directory YAML files. Therefore it lacks most Lazygit
knobs, including custom Git log commands/order, external pagers/renderers,
author/branch colors, custom filename icons, commit-message prefixes,
per-panel layout settings, screen modes, scroll-off behavior, key remapping,
and custom copy/paste/open commands. The full upstream surface is in
[Config.md](https://github.com/jesseduffield/lazygit/blob/bbca3df2cd064729bd66b909717cb4709ecd7408/docs/Config.md).

### 10. Smaller workflow gaps

- No “skip” action for an in-progress rebase/cherry-pick; TanGit offers
  continue and abort.
- No “nuke working tree” flow matching Lazygit's tracked/untracked reset
  options, although TanGit can discard all visible changes.
- No screen-mode cycling (normal/half/fullscreen panels); only fullscreen diff.
- No command-log options/focus mode; TanGit has a command-log modal.
- No recent-repository switcher action inside a repository view; TanGit uses
  desktop tabs and its picker instead.

## Partial or intentionally different

| Area | TanGit today | Difference from Lazygit |
| --- | --- | --- |
| History editing | Reword, drop, reorder, squash, fixup, amend-old, create/apply fixups, and branch rebase. | No native mid-rebase todo editor, range operations, `--onto` base marking, author editing, or skip. |
| Partial changes | Select diff lines and stage/unstage them. | No hunk-edit mode or old-commit custom patches. |
| Conflicts | Pick current/incoming content, edit, undo a resolution, and stage. | Lazygit additionally exposes pick-both and broader merge-conflict option menus. |
| Reset | Soft/mixed/hard reset to a commit or reflog entry. | Fewer reset targets/options than Lazygit. |
| Stash | Stash all or staged changes; apply, pop, rename, drop; inspect/apply/discard one stashed file. | No stash-unstaged-only option or branch/worktree-from-stash. |
| Worktrees | Create from a typed base, switch the app to one, and remove it. | No one-key creation from the selected branch/commit/tag/stash and no direct editor action. |
| Commit graph | Native graph page with lanes. | No Lazygit classic/detailed graph modes, graph visibility/log-order menus, or whole-graph toggles. |
| Search/filter | Commit filter by message, author, and branch; Changes file search. | No uniform `/` filtering across branches, remotes, stash, tags, submodules, reflog, etc. |
| Settings | Native themes, accent, font, spacing, radius, language, editor, diff defaults, visible sections. | Not compatible with Lazygit YAML and covers a much smaller set of Git/UI behaviors. |
| Terminal/commands | Persistent embedded terminal and command log. | Different from Lazygit's one-shot shell prompt and context-aware custom-command system. |

## Major Lazygit capabilities already supported

These should **not** be counted as gaps:

- stage/unstage files and all files;
- select individual diff lines for partial staging;
- commit, add a description, and amend HEAD;
- discard a file or all visible changes with confirmation;
- fetch, pull, and push;
- browse paged history and a commit graph;
- filter commits by message, author, and branch;
- reword any commit, drop and reorder commits, squash/fixup into an older
  commit, amend an older commit, and create/apply fixup commits;
- automatic reflog-based undo/redo for commits, resets, pulls, checkouts, and
  completed rebases, with persistent markers and auto-stash protection;
- cherry-pick, revert, soft/mixed/hard reset, create branch/tag at a commit;
- branch create/checkout/force-checkout/rename/delete/merge/rebase/pull/fast-forward;
- add/edit/fetch/remove remotes, check out a remote branch locally or detached,
  merge or rebase onto it, set it as the current branch's upstream, copy its
  full name, and delete it on the remote with confirmation;
- unset a local branch's upstream without changing its commits;
- stash all/staged, apply, pop, rename, drop, and inspect individual stash files;
- create/push/delete local tags;
- create/switch/remove worktrees;
- add/update/delete submodules;
- check out commits in detached HEAD mode; browse reflog entries, copy hashes,
  create recovery branches, and reset to an entry;
- conflict resolution, file editing, blame, and file history;
- unified/side-by-side/wrapped/fullscreen diffs and patch export;
- command log, recent repositories, multi-repository tabs, editor integration,
  terminal pane, themes, localization, and keyboard shortcut reference.

The local evidence is TanGit's [README feature list](../README.md), operation
surface in [`internal/repository/session.go`](../internal/repository/session.go),
menus and views under `internal/app/view_*.go`, and fixed shortcut map in
[`shortcuts.go`](../internal/app/shortcuts.go).

## Suggested implementation order

1. **Uniform search plus file-tree views** — broad daily usability improvement.
2. **Remote branch/upstream/remote editing actions** — closes common Git
   workflow gaps without requiring provider APIs.
3. **Arbitrary-ref comparisons and external difftool/renderer support.**
4. **Native mid-rebase todo editing, range operations, and `--onto` base marking.**
5. **PR/forge integration.**
6. **Custom commands and configurable keybindings.**
7. **Custom patch building and multi-selection**, which is powerful but the
   most state-heavy UI to reproduce safely.

