package scheduler

import (
	"testing"
	"time"
)

func TestOrgDirectoryPlanTimesPicksLatestShanghaiMidnight(t *testing.T) {
	// 10:00 in Shanghai on 17 Nov 2026.
	now := time.Date(2026, 11, 17, 2, 0, 0, 0, time.UTC)
	plans, err := orgDirectoryPlanTimes(now, LatestPlanInfo{})
	if err != nil {
		t.Fatal(err)
	}
	if len(plans) != 1 {
		t.Fatalf("plans = %d, want 1", len(plans))
	}
	want := time.Date(2026, 11, 16, 16, 0, 0, 0, time.UTC)
	if !plans[0].Equal(want) {
		t.Fatalf("plan = %s, want %s", plans[0], want)
	}
}

func TestOrgDirectoryPlanTimesSkipsAFinishedMidnight(t *testing.T) {
	now := time.Date(2026, 11, 17, 2, 0, 0, 0, time.UTC)
	plans, err := orgDirectoryPlanTimes(now, LatestPlanInfo{
		Found:    true,
		PlanTime: time.Date(2026, 11, 16, 16, 0, 0, 0, time.UTC),
		Status:   "SUCCESS",
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(plans) != 0 {
		t.Fatalf("plans = %v, want none", plans)
	}
}
