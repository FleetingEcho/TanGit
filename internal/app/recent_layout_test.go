package app

import (
	"fmt"
	"os"
	"testing"
)

func TestRecentCardsHaveEvenCompactGaps(t *testing.T) {
	const (
		wantMargin   = float32(8)
		wantMaxWidth = float32(480)
	)
	for _, test := range []struct {
		width   int
		columns int
	}{
		{width: 1280, columns: 2},
		{width: 720, columns: 1},
	} {
		t.Run(fmt.Sprintf("%dpx", test.width), func(t *testing.T) {
			repositories := []recentRepository{
				{Path: "/work/one"}, {Path: "/work/two"},
				{Path: "/work/three"}, {Path: "/work/four"},
			}
			cardCount := len(repositories)
			if test.columns == 1 {
				// The fourth single-column card starts beyond the three-row viewport.
				cardCount = 3
			}
			_, tt, _ := recentTester(t, repositories...)
			tt.SetSize(test.width, 800)
			tt.Frame()
			tt.Frame()

			cards := make([][4]float32, cardCount)
			for i, repo := range repositories[:cardCount] {
				card, found := tt.Find("Open " + repo.Path)
				if !found {
					t.Fatalf("missing card %q", repo.Path)
				}
				cards[i] = [4]float32{card.X, card.Y, card.W, card.H}
				if card.H != recentRowHeight {
					t.Errorf("card %d height = %v, want an unclipped %v", i, card.H, recentRowHeight)
				}
			}
			for i := test.columns; i < len(cards); i++ {
				above := cards[i-test.columns]
				gap := cards[i][1] - (above[1] + above[3])
				if gap != wantMargin {
					t.Errorf("vertical gap before card %d = %v, want %v", i, gap, wantMargin)
				}
			}
			if dir := os.Getenv("MYGO_SHOTS"); dir != "" {
				saveScreenshot(t, dir, fmt.Sprintf("recent-gaps-%d", test.width), tt)
			}
			if test.columns == 2 {
				for i := 0; i < len(cards); i += 2 {
					gap := cards[i+1][0] - (cards[i][0] + cards[i][2])
					if gap != wantMargin {
						t.Errorf("horizontal gap in row %d = %v, want %v", i/2, gap, wantMargin)
					}
					for column := range 2 {
						if width := cards[i+column][2]; width > wantMaxWidth {
							t.Errorf("card %d width = %v, want at most %v in the wide two-column layout", i+column, width, wantMaxWidth)
						}
					}
				}
			}
		})
	}
}

func TestRecentRepositoriesFrameStartsWithTheSeventhCard(t *testing.T) {
	for _, test := range []struct {
		count int
		frame bool
	}{
		{count: 1}, {count: 3}, {count: 4}, {count: 6},
		{count: 7, frame: true}, {count: 8, frame: true},
	} {
		t.Run(fmt.Sprintf("%d_cards", test.count), func(t *testing.T) {
			repositories := make([]recentRepository, test.count)
			for i := range repositories {
				repositories[i].Path = fmt.Sprintf("/work/repository-%d", i+1)
			}
			_, tt, _ := recentTester(t, repositories...)
			tt.SetSize(1280, 800)
			tt.Frame()
			tt.Frame()

			// The section heading always has this accessible name. The inner
			// scroll region is the second occurrence, and only exists once
			// the cards overflow their three-row viewport and need a frame.
			labels := 0
			for _, info := range tt.AccessAll() {
				if info.Name == "Recent repositories" {
					labels++
				}
			}
			if framed := labels == 2; framed != test.frame {
				t.Fatalf("recent repositories framed = %v, want %v for %d cards (accessible labels=%d)",
					framed, test.frame, test.count, labels)
			}
		})
	}
}
