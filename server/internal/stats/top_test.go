package stats

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestTopByPlaysAndByTime(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	short := f.album(1, "Short Album", "A", `["Rock"]`, `["Punk"]`, now)
	long := f.album(1, "Long Album", "B", `["Jazz"]`, `["Modal", "Hard Bop"]`, now)
	f.listen(1, short, "counted", now.AddDate(0, 0, -1), 30, 1)
	f.listen(1, short, "counted", now.AddDate(0, 0, -2), 30, 1)
	f.listen(1, short, "counted", now.AddDate(0, 0, -3), 30, 1)
	f.listen(1, long, "counted", now.AddDate(0, 0, -1), 200, 1)

	byPlays, err := f.st.Top(ctx, 1, "30d", "plays", 10)
	if err != nil {
		t.Fatal(err)
	}
	if byPlays.Albums[0].AlbumID != short || byPlays.Albums[0].Value != 3 {
		t.Fatalf("by plays %+v", byPlays.Albums)
	}
	byTime, _ := f.st.Top(ctx, 1, "30d", "time", 10)
	if byTime.Albums[0].AlbumID != long || byTime.Albums[0].Value != 200*60 {
		t.Fatalf("by time %+v", byTime.Albums)
	}
	if byPlays.Summary.Plays != 4 || byPlays.Summary.ListenedSec != 290*60 || byPlays.Summary.Albums != 2 {
		t.Fatalf("summary %+v", byPlays.Summary)
	}
	if byTime.Genres[0].Name != "Jazz" || byPlays.Genres[0].Name != "Rock" {
		t.Fatalf("genres %+v / %+v", byTime.Genres, byPlays.Genres)
	}
	styles := map[string]int64{}
	for _, s := range byPlays.Styles {
		styles[s.Name] = s.Value
	}
	if styles["Modal"] != 1 || styles["Hard Bop"] != 1 || styles["Punk"] != 3 {
		t.Fatalf("an album with several styles should count towards each: %+v", byPlays.Styles)
	}
}

func TestRepeatedListenCountsEachPlay(t *testing.T) {
	f := newFixture(t)
	a := f.album(1, "Loop", "A", `[]`, `[]`, now)
	f.listen(1, a, "counted", now.AddDate(0, 0, -1), 180, 4)
	top, _ := f.st.Top(context.Background(), 1, "all", "plays", 10)
	if top.Albums[0].Value != 4 {
		t.Fatalf("plays %d", top.Albums[0].Value)
	}
}

func TestPeriodExcludesOlderListens(t *testing.T) {
	f := newFixture(t)
	a := f.album(1, "A", "A", `[]`, `[]`, now)
	f.listen(1, a, "counted", now.AddDate(0, 0, -31), 40, 1)
	f.listen(1, a, "counted", now.AddDate(0, 0, -29), 40, 1)
	top, _ := f.st.Top(context.Background(), 1, "30d", "plays", 10)
	if top.Summary.Plays != 1 {
		t.Fatalf("plays in 30 days %d", top.Summary.Plays)
	}
	all, _ := f.st.Top(context.Background(), 1, "all", "plays", 10)
	if all.Summary.Plays != 2 {
		t.Fatalf("all time %d", all.Summary.Plays)
	}
}

func TestUnconfirmedTimeIsReported(t *testing.T) {
	f := newFixture(t)
	a := f.album(1, "A", "A", `[]`, `[]`, now)
	id := f.listen(1, a, "pending_confirmation", now.AddDate(0, 0, -1), 180, 1)
	f.st.DB.W.Exec(`UPDATE sessions SET duration_confirmed = 0 WHERE id = ?`, id)
	top, _ := f.st.Top(context.Background(), 1, "all", "time", 10)
	if top.Summary.UnconfirmedSec != 180*60 {
		t.Fatalf("summary %+v", top.Summary)
	}
}

func TestEmptyStats(t *testing.T) {
	f := newFixture(t)
	top, err := f.st.Top(context.Background(), 1, "all", "plays", 10)
	if err != nil || len(top.Albums) != 0 || len(top.Genres) != 0 || top.Summary.Plays != 0 {
		t.Fatalf("top %+v err %v", top, err)
	}
	weeks, err := f.st.Timeline(context.Background(), 1, 4)
	if err != nil || len(weeks) != 4 {
		t.Fatalf("weeks %+v err %v", weeks, err)
	}
	if _, err := f.st.Top(context.Background(), 1, "week", "plays", 10); !errors.Is(err, ErrBadQuery) {
		t.Fatalf("bad period: %v", err)
	}
}

func TestTimelineFillsEmptyWeeks(t *testing.T) {
	f := newFixture(t)
	a := f.album(1, "A", "A", `[]`, `[]`, now)
	f.listen(1, a, "counted", now, 40, 1)
	f.listen(1, a, "counted", now.AddDate(0, 0, -14), 40, 2)
	weeks, _ := f.st.Timeline(context.Background(), 1, 4)
	got := []int64{weeks[0].Plays, weeks[1].Plays, weeks[2].Plays, weeks[3].Plays}
	if got[0] != 0 || got[1] != 2 || got[2] != 0 || got[3] != 1 {
		t.Fatalf("plays per week %v (%+v)", got, weeks)
	}
	if weeks[3].Start != "2026-09-28" {
		t.Fatalf("current week starts %s", weeks[3].Start)
	}
}

func TestTimelineUsesLocalTimeZone(t *testing.T) {
	f := newFixture(t)
	warsaw, err := time.LoadLocation("Europe/Warsaw")
	if err != nil {
		t.Skip("no tzdata")
	}
	f.st.Loc = warsaw
	mondayAfterMidnight := time.Date(2026, 9, 28, 0, 30, 0, 0, warsaw)
	a := f.album(1, "A", "A", `[]`, `[]`, now)
	f.listen(1, a, "counted", mondayAfterMidnight, 40, 1)
	weeks, _ := f.st.Timeline(context.Background(), 1, 2)
	if weeks[1].Start != "2026-09-28" || weeks[1].Plays != 1 || weeks[0].Plays != 0 {
		t.Fatalf("Monday 00:30 in Warsaw (Sunday in UTC) landed in the wrong week: %+v", weeks)
	}
}

func TestLongerPeriodsExcludeOlderListens(t *testing.T) {
	f := newFixture(t)
	a := f.album(1, "A", "A", `[]`, `[]`, now)
	f.listen(1, a, "counted", now.AddDate(0, 0, -366), 40, 1)
	f.listen(1, a, "counted", now.AddDate(0, 0, -364), 40, 1)
	f.listen(1, a, "counted", now.AddDate(0, 0, -91), 40, 1)
	f.listen(1, a, "counted", now.AddDate(0, 0, -89), 40, 1)
	for period, want := range map[string]int64{"90d": 1, "year": 3, "all": 4} {
		top, err := f.st.Top(context.Background(), 1, period, "plays", 10)
		if err != nil {
			t.Fatal(err)
		}
		if top.Summary.Plays != want {
			t.Fatalf("plays in %s %d, want %d", period, top.Summary.Plays, want)
		}
	}
}

func TestTopLimitFallsBackToTenOutsideOneToFifty(t *testing.T) {
	f := newFixture(t)
	a := f.album(1, "A", "A", `["G01","G02","G03","G04","G05","G06","G07","G08","G09","G10","G11"]`, `[]`, now)
	f.listen(1, a, "counted", now.AddDate(0, 0, -1), 40, 1)
	for limit, want := range map[int]int{-1: 10, 0: 10, 1: 1, 50: 11, 51: 10} {
		top, err := f.st.Top(context.Background(), 1, "all", "plays", limit)
		if err != nil {
			t.Fatal(err)
		}
		if len(top.Genres) != want {
			t.Fatalf("limit %d returned %d genres, want %d", limit, len(top.Genres), want)
		}
	}
}

func TestTimelineWeeksFallBackToTwentySixOutsideOneToFiveHundredTwenty(t *testing.T) {
	f := newFixture(t)
	for weeks, want := range map[int]int{-1: 26, 0: 26, 1: 1, 520: 520, 521: 26} {
		got, err := f.st.Timeline(context.Background(), 1, weeks)
		if err != nil {
			t.Fatal(err)
		}
		if len(got) != want {
			t.Fatalf("weeks %d returned %d weeks, want %d", weeks, len(got), want)
		}
	}
}

func TestTimelineCountsListenAtMidnightStartingTheFirstWeek(t *testing.T) {
	f := newFixture(t)
	a := f.album(1, "A", "A", `[]`, `[]`, now)
	f.listen(1, a, "counted", time.Date(2026, 9, 21, 0, 0, 0, 0, time.UTC), 40, 1)
	weeks, _ := f.st.Timeline(context.Background(), 1, 2)
	if weeks[0].Start != "2026-09-21" || weeks[0].Plays != 1 {
		t.Fatalf("weeks %+v", weeks)
	}
}

func TestTimelineWithoutTimeZoneUsesLocalTime(t *testing.T) {
	f := newFixture(t)
	f.st.Loc = nil
	weeks, err := f.st.Timeline(context.Background(), 1, 1)
	if err != nil || len(weeks) != 1 || weeks[0].Start != "2026-09-28" {
		t.Fatalf("weeks %+v err %v", weeks, err)
	}
}
