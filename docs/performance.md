# Large-repository performance evidence

## Scope and outcome

Baseline: `45cc31a3224f4de6020306f52c592eeadd01271e` (`main`). Benchmarks were committed separately as `1e3408a`, then only measured performance paths were changed:

- `69d9b89`: stream NUL-delimited numstat records and reject canceled cache waiters before another Git read;
- `20f5d27`: cancel obsolete worktree/commit reads, reject A-B-A publications with request tokens, and skip rebuilding an unchanged quiet textual patch.

There is deliberately **no** new Snapshot or Session caching layer, no general pagination/session rewrite, no sparse-view semantic rewrite, and no principal-list renderer replacement. A proposed combined `git diff --raw --numstat` CommitFiles read was benchmarked and reverted because it did not improve time and increased allocated bytes. It is not present in the final code.

All comparisons are on this checkout's Linux/amd64 host (Intel Core Ultra 7 265), Go 1.27.3, Git 2.43.0. They are **local comparisons, not cross-machine speed claims**. Benchmarks have no hard latency or percentage-improvement CI assertions.

## Real fixture and stage boundaries

The opt-in benchmarks use an ordinary non-bare Git repository, not a fake filesystem or synthetic Session:

- 12,000 tracked files beneath `tree/`, plus `history.txt` and `long.go` (the root `CommitFiles` result has 12,002 entries);
- 512 ordinary subsequent history commits, then one long-hunk commit (514 commits including the root and long commit);
- an edit to every one of the 12,000 `tree/` files for `CoreSnapshot` status/numstat;
- 8,000 generated Go declarations per side in `long.go`, with a real **1,309,914-byte** patch and one large hunk.

`LAZYGITAPP_PERF_REPO` points at a reusable fixture. If it names a non-existent repository, the benchmark creates it; if unset, it uses a temporary repository. The measured loops reuse exactly the same stored fixture for old and new code. If supplying an existing path, it must have the fixture shape described above, since benchmarks select its root, latest commit, and dirty worktree by content.

Costs are intentionally separate:

| Stage | Final after median | What it includes |
| --- | ---: | --- |
| `LongHunk_GitReadOnly` | 6.44 ms | `Session.CommitFileDiff` and real Git output only |
| app `ParsePatchOnly` | 0.474 ms | `editor.ParsePatch` only |
| repository `MyGoSetPatchOnly` | 1.94 ms | MyGo buffers, ropes and `DiffState.SetPatch`, no `ParsePatch` |
| app `BuildDiffState_ParsePlusMyGo` | 3.34 ms | `ParsePatch`, `SetPatch`, moved-line annotations and app setup; do not add its stages again |
| repository `MyGoScrollingFrame` | 2.04 ms | headless MyGo rendering of an already-built state, 1,800 DIP viewport |
| `QuietRefreshSamePatch_GitExcluded` | 1.06 us | actual `requestQuietFileDiff`, immediate Session response, no Git wall time, unchanged state and no repaint |

The quiet benchmark reports `diff_bytes` as context but no MB/s, because the reuse path does not parse that byte stream. The Git-read benchmark remains separate: skipping a rebuild does not remove the required Git read.

## Benchmark order and statistical semantics

Baseline logs use `-benchtime=5x -count=3` for repository variants and `-benchtime=5x -count=3` for the initial app capture. Final app/quiet controls use `20x`, so raw allocations or timings across different warm-up counts are not treated as exact paired deltas.

Decision comparisons are:

1. **Parser component pair:** `split_original` then `streaming`, same real 300,031-byte / 12,002-numstat-file output, within the final benchmark. The original split implementation exists only in `_test.go` as a benchmark control.
2. **Saved old/new binaries:** A is a test binary built at `45cc31a` plus benchmark-only `1e3408a`; B contains the final production changes. `A1 B1 A2 B2 A3 B3` runs the same variants, each with `-test.count=3`.
3. **Core order control:** `A1 B1 B2 A2 A3 B3 B4 A4` (ABBA twice), three benchmark repetitions in each run, to balance predecessor/order effects.
4. **Quiet after-only product-path benchmark:** it is compared with the original complete builder cost control, not represented as a full old/new Git+UI end-to-end ratio. Git is excluded explicitly.
5. `same_position_control` precedes `scroll_distinct`; it demonstrates cache/no-op cost but is not a scroll-speed result.

Reported medians are sample medians, ranges include every sample; they are not confidence intervals. Repeated noisy captures with concurrent unrelated Go suites were rejected for decision-making. The checked-in final interleaved logs were captured without another active Go test process.

## What changed and what did not

### Numstat and Snapshot reads

Same real 12k-file numstat component, final three-run pair:

| Variant | Median ns/op | Range | B/op | allocs/op |
| --- | ---: | --- | ---: | ---: |
| original strings split | 1,244,358 | 1,193,795–1,405,565 | 2,082,163 | 12,086 |
| streaming NUL scanner | 844,093 | 718,937–874,864 | 1,308,328 | 81 |

This is direct evidence for the retained parser change. It reduces temporary record allocation without changing binary/rename/path handling, order, or values.

End-to-end Git wall time is not given an invented improvement percentage:

- Baseline `CoreSnapshot`: 648 ms median (599–742 ms); final capture: 587 ms (585–612 ms). Those runs had different surrounding load, so they are not a causal speedup claim.
- Balanced ABBA `CoreSnapshot`: A 557 ms median (546–586 ms), B 567 ms (528–582 ms), 12 samples each. Ranges overlap; the correct conclusion is **no clear wall-time advantage or regression**, while streaming reduces parser cost and allocation work.
- `DetailsSnapshot` cold numstat stayed approximately 71 ms.
- Interleaved root `CommitFiles`: A 30.45 ms median, B 30.82 ms, with overlapping ranges. Allocated bytes in the final interleave changed from about 6.68 MB to 5.91 MB and allocations from about 24.4k to 12.4k. The combined-read alternative is logged separately in `rejected-combined-read.txt` and was removed because its time did not improve and its bytes rose to about 8.2 MB.

### RecentCommits / requestMoreCommits

Warm-total reads (so numstat cache hits do not hide the log read) still use the existing `limit+skip` behavior. Final samples were approximately 12.6 ms at skip 0 and 17.8 ms at skip 480. Interleaved A/B ranges overlap. The measured allocation growth with skip is real, but the upstream `GetCommitsOptions` at the pinned Lazygit revision has no paging `Skip` field. Replacing the loader's all-ref filters, rebase handling, hash pools and ordering merely to chase that allocation would be a broad session rewrite, not a narrow evidence-backed change. **No RecentCommits production optimization was retained.** Existing page order, totals and `EndReached` behavior remain under their original tests.

### Principal commit list / lazy-visible statistics

The real app view is benchmarked with 20,000 commits and a 1,800 DIP-high viewport. `scroll_distinct` walks non-adjacent screens with stride 997; it is not a short-screen, same-frame cached benchmark. Both baseline and final report `lazy_visible_rows=29` and `total_commits=20000` on the final viewport. A same-position control costs roughly 0.77 ms and must not be substituted for scrolling.

Final interleaved `scroll_distinct` results: A 10.289 ms median (9.459–10.894 ms), B 10.219 ms (9.337–10.579 ms), nine samples each. This is parity, not a renderer speedup. Focused baseline pprof attributed most list time to text shaping/layout and normal visible row construction; there was no sufficient evidence for a special principal-list paint rewrite, so no such rewrite was retained.

### Cancellation, ABA and no repaints

The old behavior had publication guards but did not cancel obsolete CommitFiles/CommitFileDiff reads and could accept an old result after selection A-B-A because only hash/path identities were compared. Worktree diff publication already had generations, but replacement did not cancel the previous read.

The final code uses request-scoped contexts plus monotonic tokens. Selecting a new commit/file or resetting details cancels obsolete work. Even a Session that ignores cancellation cannot publish an old A result into a new A selection. Stale/canceled callbacks return `false` through `updateChanged`, so they do not request a full repaint. A directory selection also cancels its former diff read.

`test-first-red-app.txt` records the four original failures (missing cancellation, CommitFiles ABA and CommitDiff ABA) and the already-green same-patch/no-repaint guard. The final tests pass both normally and under `-race`. Commit activities and ordinary repository writes were not changed.

### Quiet unchanged patches / long hunks

Originally, quiet worktree refresh called `buildDiffState` before its UI publication callback compared the raw patch and discarded identical output. `RepeatedBuildSamePatch` remains as an explicit two-build cost control (about 6.81 ms for two final builds). The final `QuietRefreshSamePatch_GitExcluded` runs the actual request path and waits every publication, costing about 1.06 us, preserving the exact state pointer and all 1,309,914 patch bytes, with no repaint.

The shortcut is narrow: quiet mode, same key/generation and same kind/full patch bytes, with an existing textual `DiffState`. Binary/media and patchless paths keep their original behavior. Initial builds, moved-line annotations, scrolling geometry, hunk order, old/new buffers and patch export bytes were not changed.

There was no `SparseBuildDiffState` symbol at baseline. The measured initial builder and long-hunk scroll path did not justify introducing a new sparse public state model with lazy-geometry semantics. **No speculative `SparseBuildDiffState` rewrite or lazy-visible diff annotation change was added.**

## Verification

Final successful checks (logs retained under `docs/performance/`):

```sh
go test ./... -skip 'SSH|OpenSSH' -count=1 -timeout=300s
go test -race ./internal/app ./internal/repository -run '<targeted cancellation/ABA/cache/publication tests>' -count=1 -timeout=120s
go tool mygo vet .
```

The all-package non-SSH suite passed, including existing behavior suites for snapshot files, CommitFiles/numstat totals, commit ordering/pagination, commit activities, diff hunk/state behavior and UI publication. The targeted race suite passed. MyGo vet passed.

An earlier unfiltered repository run reached four credential-dependent failures in `TestOpenSSHWorkspaceVisualRebasePlanRealOpenSSH`, `TestOpenSSHWorkspaceRealOpenSSH`, `TestOpenSSHWorkspaceRecoveryRealOpenSSH`, and `TestSSHUnbornBranchHistoryIsEmpty`, all during local OpenSSH handshake authentication. They are not represented as passing; final all-package verification explicitly excludes SSH/OpenSSH integration. No production SSH code was changed.

## Raw evidence index

- `baseline-repository.txt`, `baseline-app.txt`: saved baseline benchmark runs with commands and variant order.
- `baseline-repository-pprof.txt`, `baseline-app-pprof.txt`, `baseline-focused-pprof.txt`: CPU/allocation and focused list/diff baseline profiles.
- `after-repository.txt`, `after-app.txt`, corresponding `*-pprof.txt`: final complete benchmark/profile captures.
- `ab-repository-interleaved.txt`, `ab-app-interleaved.txt`, `ab-core-balanced.txt`: final old/new comparisons with explicit order in each header.
- `test-first-red-app.txt`, `test-first-red-repository.txt`: pre-implementation red results.
- `rejected-combined-read.txt`: rejected combined CommitFiles candidate; not the response implementation.
- `go-test-nonssh.txt`, `race-target.txt`, `mygo-vet.txt`, `verification-status.txt`: final verification results.
