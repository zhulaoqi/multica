package service

import "testing"
import "time"

func TestPlanIntakeRetry_SamePrimaryThenBackupThenRoster(t *testing.T) {
	if got := PlanIntakeRetry(1, true, true, false); got.Wait != time.Minute || got.SwitchToBackup || got.Redispatch {
		t.Fatalf("first = %+v", got)
	}
	if got := PlanIntakeRetry(2, true, true, false); got.Wait != 5*time.Minute || got.Redispatch {
		t.Fatalf("second = %+v", got)
	}
	if got := PlanIntakeRetry(3, true, true, false); !got.SwitchToBackup {
		t.Fatalf("backup = %+v", got)
	}
	if got := PlanIntakeRetry(3, true, false, false); !got.Redispatch || got.Wait != 15*time.Minute || got.Stop {
		t.Fatalf("first exhausted round = %+v", got)
	}
	if got := PlanIntakeRetry(3, false, true, false); !got.Redispatch {
		t.Fatalf("backup already running = %+v", got)
	}
	if got := PlanIntakeRetry(3, true, false, true); !got.Stop || got.Redispatch {
		t.Fatalf("second exhausted round = %+v", got)
	}
}

func TestIntakeRetryProgress_SameUnitOrBackup(t *testing.T) {
	done, remain, ok := IntakeRetryProgress(PlanIntakeRetry(1, true, true, false))
	if !ok || done != "这一轮没有新输出，正在同一台重试" || remain != "重试之后继续" {
		t.Fatalf("retry line = %q %q %v", done, remain, ok)
	}
	done, remain, ok = IntakeRetryProgress(PlanIntakeRetry(3, true, true, false))
	if !ok || done != "正在换注册单位" {
		t.Fatalf("switch line = %q %v", done, ok)
	}
	done, remain, ok = IntakeRetryProgress(PlanIntakeRetry(3, true, false, false))
	if !ok || remain != "十五分钟后再分一次" {
		t.Fatalf("redispatch line = %q %q", done, remain)
	}
	if _, _, ok = IntakeRetryProgress(PlanIntakeRetry(3, true, false, true)); ok {
		t.Fatal("an exhausted plan has no retry line")
	}
}

func TestIntakeRetryReason_SkipsExplicitStop(t *testing.T) {
	for _, reason := range []string{"idle_watchdog", "timeout", "runtime_offline", "queued_expired", "provider_network", "provider_5xx"} {
		if !intakeRetryReason(reason) {
			t.Fatalf("%s should retry", reason)
		}
	}
	if intakeRetryReason("stopped") || intakeRetryReason("cancelled") {
		t.Fatal("an explicit stop is not restarted")
	}
}
