package app

import "time"

// Tests open many repositories; none of them wants a background file read to
// start by itself a moment later.
func init() {
	scheduleCodeSearchIndexOnOpen = func(time.Duration, func()) {}
}
