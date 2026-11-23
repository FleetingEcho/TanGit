package app

import (
	"sort"
	"time"

	"lazygitapp/internal/repository"
)

// The Overview's contributor card answers "who has been working lately" for a
// period the user picks. The history is read once with every contributor's
// commit times; the period is counted here, so switching is instant.

type contributorRange struct {
	label string
	// days is the period; 0 is the whole history.
	days int
	// buckets and bucketSeconds shape the chart: the period is cut into
	// buckets bars, each bucketSeconds long.
	buckets       int
	bucketSeconds int64
}

const (
	contributorRangeWeek = iota
	contributorRangeMonth
	contributorRangeQuarter
	contributorRangeHalf
	contributorRangeYear
	contributorRangeAll
)

const secondsPerDay = int64(24 * 3600)

// contributorRanges are the periods the card offers, the first being the
// default.
var contributorRanges = []contributorRange{
	contributorRangeWeek:    {label: "Last week", days: 7, buckets: 7, bucketSeconds: secondsPerDay},
	contributorRangeMonth:   {label: "Last month", days: 30, buckets: 30, bucketSeconds: secondsPerDay},
	contributorRangeQuarter: {label: "Last 3 months", days: 91, buckets: 13, bucketSeconds: 7 * secondsPerDay},
	contributorRangeHalf:    {label: "Last 6 months", days: 182, buckets: 26, bucketSeconds: 7 * secondsPerDay},
	contributorRangeYear:    {label: "Last year", days: 365, buckets: 12, bucketSeconds: 365 * secondsPerDay / 12},
	contributorRangeAll:     {label: "All time"},
}

// contributorShare is one contributor's commits within the period.
type contributorShare struct {
	Contributor repository.Contributor
	Commits     int
}

// contributorRangeStats is everything the card shows for one period.
type contributorRangeStats struct {
	Range int
	// Commits and PreviousCommits count the period and the equal period before
	// it, for the trend; the previous count is 0 for the whole history.
	Commits         int
	PreviousCommits int
	// Active is how many contributors committed in the period; Total how many
	// there are in all.
	Active int
	Total  int
	// Top is everyone active in the period, most commits first.
	Top []contributorShare
	// TopFiveShare is the percentage of the period's commits made by the five
	// most active contributors.
	TopFiveShare int
	// Buckets are the commits per chart bar, oldest first; BucketStart is the
	// Unix time of the first bar's start and BucketSeconds the width of a bar.
	Buckets       []int
	BucketStart   int64
	BucketSeconds int64
}

// countTimesBetween counts the sorted times in [from, to).
func countTimesBetween(times []int64, from, to int64) int {
	low := sort.Search(len(times), func(i int) bool { return times[i] >= from })
	high := sort.Search(len(times), func(i int) bool { return times[i] >= to })
	return high - low
}

// computeContributorRange counts one period of the history ending at now.
func computeContributorRange(summary repository.ContributorSummary, period int, now time.Time) contributorRangeStats {
	if period < 0 || period >= len(contributorRanges) {
		period = contributorRangeWeek
	}
	spec := contributorRanges[period]
	stats := contributorRangeStats{Range: period, Total: len(summary.Contributors)}
	end := now.Unix() + 1
	start := int64(0)
	buckets, bucketSeconds := spec.buckets, spec.bucketSeconds
	if spec.days == 0 {
		// The whole history, drawn as 24 bars of equal width.
		start = summary.FirstCommit
		span := end - start
		buckets = 24
		bucketSeconds = max(span/int64(buckets)+1, secondsPerDay)
	} else {
		start = end - int64(buckets)*bucketSeconds
	}
	stats.Buckets = make([]int, buckets)
	stats.BucketStart, stats.BucketSeconds = start, bucketSeconds
	for _, contributor := range summary.Contributors {
		inPeriod := countTimesBetween(contributor.CommitTimes, start, end)
		if spec.days == 0 {
			inPeriod = len(contributor.CommitTimes)
		}
		if inPeriod == 0 {
			continue
		}
		stats.Commits += inPeriod
		stats.Active++
		stats.Top = append(stats.Top, contributorShare{Contributor: contributor, Commits: inPeriod})
		for _, at := range contributor.CommitTimes {
			if at < start || at >= end {
				if spec.days != 0 {
					continue
				}
			}
			index := int((at - start) / bucketSeconds)
			if index < 0 {
				index = 0
			}
			if index >= buckets {
				index = buckets - 1
			}
			stats.Buckets[index]++
		}
		if spec.days != 0 {
			span := int64(buckets) * bucketSeconds
			stats.PreviousCommits += countTimesBetween(contributor.CommitTimes, start-span, start)
		}
	}
	sort.SliceStable(stats.Top, func(i, j int) bool { return stats.Top[i].Commits > stats.Top[j].Commits })
	topCommits := 0
	for index, share := range stats.Top {
		if index == 5 {
			break
		}
		topCommits += share.Commits
	}
	if stats.Commits > 0 {
		stats.TopFiveShare = topCommits * 100 / stats.Commits
	}
	return stats
}

// recomputeContributorStats counts the chosen period of the loaded history.
func (a *app) recomputeContributorStats() {
	state := &a.cockpit.contributors
	if !state.loaded {
		return
	}
	now := time.Now()
	state.stats = computeContributorRange(state.summary, state.period, now)
	a.cockpit.insights.heatmap = buildCommitHeatmap(state.summary, heatmapMaxWeeks, now)
}
