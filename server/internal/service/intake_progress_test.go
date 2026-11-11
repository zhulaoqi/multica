package service

import "strings"
import "testing"
import "time"

func TestIntakeProgressInterval_DayAndNight(t *testing.T) {
	day := time.Date(2026, 11, 10, 10, 0, 0, 0, intakeShanghai)
	night := time.Date(2026, 11, 10, 23, 0, 0, 0, intakeShanghai)
	if IntakeProgressInterval(day) != 30*time.Minute {
		t.Fatalf("day = %s", IntakeProgressInterval(day))
	}
	if IntakeProgressInterval(night) != 2*time.Hour {
		t.Fatalf("night = %s", IntakeProgressInterval(night))
	}
}

func TestIntakeProgressAnchor_WaitsForStart(t *testing.T) {
	created := time.Date(2026, 11, 10, 1, 0, 0, 0, intakeShanghai)
	start := time.Date(2026, 11, 10, 9, 0, 0, 0, intakeShanghai)
	if _, ready := IntakeProgressAnchor(created, start, time.Time{}, start.Add(-time.Minute)); ready {
		t.Fatal("notice before the agreed start")
	}
	anchor, ready := IntakeProgressAnchor(created, start, time.Time{}, start.Add(30*time.Minute))
	if !ready || !anchor.Equal(start) {
		t.Fatalf("anchor = %s ready %v", anchor, ready)
	}
}

func TestIntakeProgressDue_WaitsForInterval(t *testing.T) {
	start := time.Date(2026, 11, 10, 10, 0, 0, 0, intakeShanghai)
	if IntakeProgressDue(start.Add(29*time.Minute), start) {
		t.Fatal("due before 30 minutes")
	}
	if !IntakeProgressDue(start.Add(30*time.Minute), start) {
		t.Fatal("not due at 30 minutes")
	}
}

func TestIntakeNoticeProgress_UsesState(t *testing.T) {
	done, remain := IntakeNoticeProgress("queued", "", "", "")
	if done != "还在排队" || remain != "排到后开始，之后按同样间隔汇报" {
		t.Fatalf("queued = %q %q", done, remain)
	}
	done, remain = IntakeNoticeProgress("", "", "重试之后继续", "noise")
	if done != "" || remain != "重试之后继续" {
		t.Fatalf("retry kept = %q %q", done, remain)
	}
	done, remain = IntakeNoticeProgress("running", "", "", "做到：导出好了\n还剩：再核对一列")
	if done != "导出好了" || remain != "再核对一列" {
		t.Fatalf("agent lines = %q %q", done, remain)
	}
}

func TestIntakeReviewNotice_OmitsExecutor(t *testing.T) {
	done, remain, card := IntakeReviewNotice("补上导出\n第二行", "", IntakeReviewEnvironment(""))
	if done != "做完了" || remain != "还没有测试环境" {
		t.Fatalf("empty result = %q %q", done, remain)
	}
	if !strings.Contains(card, "需求：补上导出") || !strings.Contains(card, "结果：做完了") || !strings.Contains(card, "测试环境：还没有测试环境") {
		t.Fatalf("card = %q", card)
	}
	_, remain, card = IntakeReviewNotice("补上导出", "导出好了", IntakeReviewEnvironment(" https://test.example "))
	if remain != "测试环境：https://test.example" || strings.Contains(card, "Agent") {
		t.Fatalf("url card = %q %q", remain, card)
	}
}

func TestIntakeProgressStopped(t *testing.T) {
	if !IntakeProgressStopped("in_review", false, false) || !IntakeProgressStopped("done", false, false) {
		t.Fatal("finished work should stop")
	}
	if !IntakeProgressStopped("todo", true, false) || !IntakeProgressStopped("todo", false, true) {
		t.Fatal("blocker and stall should stop")
	}
	if IntakeProgressStopped("todo", false, false) {
		t.Fatal("active work should continue")
	}
}
