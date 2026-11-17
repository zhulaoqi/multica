package scheduler

import (
	"context"
	"time"

	"github.com/multica-ai/multica/server/internal/service"
)

// JobNameOrgDirectorySync is the daily Feishu directory refresh.
const JobNameOrgDirectorySync = "org_directory_sync"

const orgDirectoryCron = "0 0 * * *"
const orgDirectoryTimezone = "Asia/Shanghai"

// OrgDirectorySyncJob refreshes every workspace's Feishu directory at
// 00:00 Asia/Shanghai. A missed midnight is caught up once on the next tick.
func OrgDirectorySyncJob(sync func(context.Context) (int, error)) JobSpec {
	return JobSpec{
		Name:              JobNameOrgDirectorySync,
		RunTimeout:        10 * time.Minute,
		StaleTimeout:      15 * time.Minute,
		HeartbeatInterval: 30 * time.Second,
		AllowStaleReentry: true,
		MaxAttempts:       3,
		MaxPlansPerTick:   1,
		RetryBackoff:      []time.Duration{5 * time.Minute, 30 * time.Minute},
		Scopes:            StaticScopes(ScopeGlobal),
		PlansForScope: func(_ context.Context, _ Scope, now time.Time, latest LatestPlanInfo) ([]time.Time, error) {
			return orgDirectoryPlanTimes(now, latest)
		},
		Handler: func(ctx context.Context, _ HandlerInput) (HandlerResult, error) {
			synced, err := sync(ctx)
			return HandlerResult{RowsAffected: int64(synced)}, err
		},
	}
}

func orgDirectoryPlanTimes(now time.Time, latest LatestPlanInfo) ([]time.Time, error) {
	if latest.RetryEligible(now) {
		return []time.Time{latest.PlanTime}, nil
	}
	after := now.Add(-48 * time.Hour)
	if latest.Found {
		after = latest.PlanTime
	}
	occurrences, err := service.NextOccurrencesUTC(orgDirectoryCron, orgDirectoryTimezone, after, now)
	if err != nil || len(occurrences) == 0 {
		return nil, err
	}
	return []time.Time{occurrences[len(occurrences)-1]}, nil
}
