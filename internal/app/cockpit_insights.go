package app

import (
	"context"
	"sort"
	"strings"
	"time"

	"lazygitapp/internal/repository"
)

// Three views of the repository's history for the Overview: a heat map of
// commit days, the lines added and deleted over the chosen period, and the
// health of the branches. The first is counted from the contributor read; the
// other two have their own reads, run in the background and cached.

// --- commit heat map -------------------------------------------------------

type heatDay struct {
	Date    time.Time
	Count   int
	InRange bool
}

// commitHeatmap is the commits of the last weeks, one column per week (Monday
// to Sunday), oldest first.
type commitHeatmap struct {
	Weeks [][7]heatDay
	Max   int
	Total int
	// ActiveDays counts days with a commit; LongestStreak the longest run of
	// such days in a row; Busiest is the day with the most commits.
	ActiveDays    int
	LongestStreak int
	Busiest       heatDay
}

const heatmapMaxWeeks = 53

func dayStart(at time.Time) time.Time {
	year, month, day := at.Date()
	return time.Date(year, month, day, 0, 0, 0, 0, at.Location())
}

// weekdayIndex is 0 for Monday through 6 for Sunday.
func weekdayIndex(day time.Time) int { return (int(day.Weekday()) + 6) % 7 }

// buildCommitHeatmap counts commits per local day over the last weeks weeks.
func buildCommitHeatmap(summary repository.ContributorSummary, weeks int, now time.Time) commitHeatmap {
	weeks = max(1, min(weeks, heatmapMaxWeeks))
	today := dayStart(now)
	// The last column is the week of today; the first starts weeks-1 weeks
	// before this week's Monday.
	thisMonday := today.AddDate(0, 0, -weekdayIndex(today))
	start := thisMonday.AddDate(0, 0, -7*(weeks-1))
	counts := map[time.Time]int{}
	for _, contributor := range summary.Contributors {
		for _, at := range contributor.CommitTimes {
			moment := time.Unix(at, 0).In(now.Location())
			if moment.Before(start) {
				continue
			}
			counts[dayStart(moment)]++
		}
	}
	heat := commitHeatmap{Weeks: make([][7]heatDay, weeks)}
	streak := 0
	for week := 0; week < weeks; week++ {
		for index := 0; index < 7; index++ {
			date := start.AddDate(0, 0, 7*week+index)
			day := heatDay{Date: date, InRange: !date.After(today)}
			if day.InRange {
				day.Count = counts[date]
			}
			heat.Weeks[week][index] = day
			if !day.InRange {
				continue
			}
			heat.Total += day.Count
			heat.Max = max(heat.Max, day.Count)
			if day.Count > 0 {
				heat.ActiveDays++
				streak++
				heat.LongestStreak = max(heat.LongestStreak, streak)
				if day.Count > heat.Busiest.Count {
					heat.Busiest = day
				}
			} else {
				streak = 0
			}
		}
	}
	return heat
}

// heatLevel is the shade of a day, 0 for none and 1 to 4 by its share of the
// busiest day.
func heatLevel(count, most int) int {
	if count <= 0 || most <= 0 {
		return 0
	}
	return max(1, (count*4+most-1)/most)
}

// --- code volume -----------------------------------------------------------

// codeVolumeStats is the period's lines changed, counted into chart buckets.
type codeVolumeStats struct {
	Added, Deleted, Commits int
	BucketAdded             []int
	BucketDeleted           []int
	BucketStart             int64
	BucketSeconds           int64
	Limited                 bool
}

// codeVolumeSince is the Unix time a period's read starts at, 0 for the whole
// history.
func codeVolumeSince(period int, now time.Time) int64 {
	spec := contributorRanges[period]
	if spec.days == 0 {
		return 0
	}
	return now.Unix() + 1 - int64(spec.buckets)*spec.bucketSeconds
}

// codeVolumeLimit is how many commits a read keeps: the whole history is read
// only for its newest commits.
func codeVolumeLimit(period int) int {
	if contributorRanges[period].days == 0 {
		return 5000
	}
	return 20000
}

func bucketCodeVolume(volume repository.CodeVolume, period int, now time.Time) codeVolumeStats {
	spec := contributorRanges[period]
	end := now.Unix() + 1
	buckets, bucketSeconds := spec.buckets, spec.bucketSeconds
	start := end - int64(buckets)*bucketSeconds
	if spec.days == 0 {
		start = end
		for _, commit := range volume.Commits {
			if commit.At > 0 && commit.At < start {
				start = commit.At
			}
		}
		buckets = 24
		bucketSeconds = max((end-start)/int64(buckets)+1, secondsPerDay)
	}
	stats := codeVolumeStats{
		BucketAdded: make([]int, buckets), BucketDeleted: make([]int, buckets),
		BucketStart: start, BucketSeconds: bucketSeconds, Limited: volume.Limited,
	}
	for _, commit := range volume.Commits {
		if commit.At < start || commit.At >= end {
			continue
		}
		index := min(max(int((commit.At-start)/bucketSeconds), 0), buckets-1)
		stats.BucketAdded[index] += commit.Added
		stats.BucketDeleted[index] += commit.Deleted
		stats.Added += commit.Added
		stats.Deleted += commit.Deleted
		stats.Commits++
	}
	return stats
}

// --- branch health ---------------------------------------------------------

const (
	branchStaleDays      = 90
	branchFarBehindAfter = 200
)

const (
	branchCategoryMerged = iota
	branchCategoryStale
	branchCategoryBehind
)

// branchHealthStats groups the branches that may need care. A branch is in at
// most one group, the first that fits: merged, stale, far behind.
type branchHealthStats struct {
	Default string
	Local   int
	Remote  int
	Groups  [3][]repository.BranchHealthEntry
}

func classifyBranchHealth(health repository.BranchHealth, now time.Time) branchHealthStats {
	stats := branchHealthStats{Default: health.Default}
	staleBefore := now.Add(-branchStaleDays * 24 * time.Hour).Unix()
	for _, branch := range health.Branches {
		if branch.Remote {
			stats.Remote++
		} else {
			stats.Local++
		}
		if branch.Current || branch.Name == health.Default || strings.HasSuffix(branch.Name, "/"+health.Default) {
			continue
		}
		switch {
		case branch.Merged:
			stats.Groups[branchCategoryMerged] = append(stats.Groups[branchCategoryMerged], branch)
		case branch.LastCommit > 0 && branch.LastCommit < staleBefore:
			stats.Groups[branchCategoryStale] = append(stats.Groups[branchCategoryStale], branch)
		case branch.Behind >= branchFarBehindAfter:
			stats.Groups[branchCategoryBehind] = append(stats.Groups[branchCategoryBehind], branch)
		}
	}
	// Oldest first for stale branches; most behind first for the others.
	sort.SliceStable(stats.Groups[branchCategoryStale], func(i, j int) bool {
		return stats.Groups[branchCategoryStale][i].LastCommit < stats.Groups[branchCategoryStale][j].LastCommit
	})
	sort.SliceStable(stats.Groups[branchCategoryBehind], func(i, j int) bool {
		return stats.Groups[branchCategoryBehind][i].Behind > stats.Groups[branchCategoryBehind][j].Behind
	})
	return stats
}

// --- state and reads -------------------------------------------------------

type codeVolumeSession interface {
	CodeVolume(ctx context.Context, branch string, since int64, maxCommits int) (repository.CodeVolume, error)
}

type languageSession interface {
	LanguageStats(ctx context.Context) (repository.LanguageStats, error)
}

type branchHealthSession interface {
	BranchHealth(ctx context.Context) (repository.BranchHealth, error)
}

type cockpitInsights struct {
	heatmap commitHeatmap
	// heatmapLine shows the commit activity as a line over the weeks instead
	// of a grid of days.
	heatmapLine bool

	langUnavailable bool
	langLoading     bool
	langLoaded      bool
	langErr         string
	languages       repository.LanguageStats
	langGeneration  uint64
	langLoadedAt    time.Time
	langToken       uint64

	volumeUnavailable bool
	volumeLoading     bool
	volumeLoaded      bool
	volumeErr         string
	volume            codeVolumeStats
	volumeKey         string
	// volumePeriod is the Code changes card's own time range.
	volumePeriod int
	volumeToken  uint64
	volumeCache  map[string]insightsCacheEntry[repository.CodeVolume]

	healthUnavailable bool
	healthLoading     bool
	healthLoaded      bool
	healthErr         string
	health            branchHealthStats
	healthCategory    int
	healthGeneration  uint64
	healthLoadedAt    time.Time
	healthToken       uint64
}

type insightsCacheEntry[T any] struct {
	value      T
	generation uint64
	loadedAt   time.Time
}

func codeVolumeKey(branch string, period int) string {
	return branch + "\x00" + contributorRanges[period].label
}

// loadCockpitCodeVolume counts the lines changed in the chosen period and
// branch: from the cache when a recent read is held, else from Git in the
// background.
func (a *app) loadCockpitCodeVolume() {
	insights := &a.cockpit.insights
	state := &a.cockpit.contributors
	if a.session == nil {
		return
	}
	session, ok := a.session.(codeVolumeSession)
	if !ok {
		insights.volumeUnavailable = true
		return
	}
	insights.volumeUnavailable = false
	branch, period := state.branch, insights.volumePeriod
	key := codeVolumeKey(branch, period)
	now := time.Now()
	if entry, ok := insights.volumeCache[key]; ok && entry.generation == a.generation && now.Sub(entry.loadedAt) < cockpitContributorsMaxAge {
		insights.volume = bucketCodeVolume(entry.value, period, now)
		insights.volumeLoaded, insights.volumeLoading, insights.volumeErr, insights.volumeKey = true, false, "", key
		return
	}
	insights.volumeLoading, insights.volumeLoaded = true, false
	insights.volumeToken++
	token := insights.volumeToken
	generation := a.generation
	ctx := a.repositoryWork.Context()
	since, limit := codeVolumeSince(period, now), codeVolumeLimit(period)
	go func() {
		defer a.recoverBackground()
		volume, err := session.CodeVolume(ctx, branch, since, limit)
		a.update(func() {
			current := &a.cockpit.insights
			if generation != a.generation || current.volumeToken != token {
				return
			}
			current.volumeLoading = false
			if err != nil {
				current.volumeErr = safeError(err)
				return
			}
			if current.volumeCache == nil {
				current.volumeCache = map[string]insightsCacheEntry[repository.CodeVolume]{}
			}
			at := time.Now()
			current.volumeCache[key] = insightsCacheEntry[repository.CodeVolume]{value: volume, generation: generation, loadedAt: at}
			current.volumeErr, current.volumeKey, current.volumeLoaded = "", key, true
			current.volume = bucketCodeVolume(volume, period, at)
		})
	}()
}

// loadCockpitBranchHealth reads the branches against the default branch, unless
// a recent read of this repository is held.
func (a *app) loadCockpitBranchHealth() {
	insights := &a.cockpit.insights
	if insights.healthLoading || a.session == nil {
		return
	}
	session, ok := a.session.(branchHealthSession)
	if !ok {
		insights.healthUnavailable = true
		return
	}
	insights.healthUnavailable = false
	if insights.healthLoaded && insights.healthGeneration == a.generation && time.Since(insights.healthLoadedAt) < cockpitContributorsMaxAge {
		return
	}
	insights.healthLoading = true
	insights.healthToken++
	token := insights.healthToken
	generation := a.generation
	ctx := a.repositoryWork.Context()
	go func() {
		defer a.recoverBackground()
		health, err := session.BranchHealth(ctx)
		a.update(func() {
			current := &a.cockpit.insights
			if generation != a.generation || current.healthToken != token {
				return
			}
			current.healthLoading = false
			if err != nil {
				current.healthErr = safeError(err)
				return
			}
			current.healthErr = ""
			current.health, current.healthLoaded = classifyBranchHealth(health, time.Now()), true
			current.healthGeneration, current.healthLoadedAt = generation, time.Now()
		})
	}()
}

// loadCockpitLanguages counts the repository's source by language, unless a
// recent read of this repository is held.
func (a *app) loadCockpitLanguages() {
	insights := &a.cockpit.insights
	if insights.langLoading || a.session == nil {
		return
	}
	session, ok := a.session.(languageSession)
	if !ok {
		insights.langUnavailable = true
		return
	}
	insights.langUnavailable = false
	if insights.langLoaded && insights.langGeneration == a.generation && time.Since(insights.langLoadedAt) < cockpitContributorsMaxAge {
		return
	}
	insights.langLoading, insights.langLoaded = true, false
	insights.langToken++
	token := insights.langToken
	generation := a.generation
	ctx := a.repositoryWork.Context()
	go func() {
		defer a.recoverBackground()
		stats, err := session.LanguageStats(ctx)
		a.update(func() {
			current := &a.cockpit.insights
			if generation != a.generation || current.langToken != token {
				return
			}
			current.langLoading = false
			if err != nil {
				current.langErr = safeError(err)
				return
			}
			current.langErr = ""
			current.languages, current.langLoaded = stats, true
			current.langGeneration, current.langLoadedAt = generation, time.Now()
		})
	}()
}

// loadCockpitInsights starts the reads of the three views for the Overview.
func (a *app) loadCockpitInsights() {
	a.loadCockpitLanguages()
	a.loadCockpitCodeVolume()
	a.loadCockpitBranchHealth()
}

// revealBranch shows a local branch in the Branches section.
func (a *app) revealBranch(name string) {
	a.setWorkspaceTab(sectionBranches)
	for row, source := range a.visibleBranches() {
		if source >= 0 && source < len(a.snapshot.Branches) && a.snapshot.Branches[source].Name == name {
			a.selectBranchRow(row)
			return
		}
	}
}
