package engine

import "testing"

func TestParseDispatchCommand(t *testing.T) {
	tests := []struct {
		name string
		body string
		want *DispatchCommand
	}{
		{
			name: "chinese sections",
			body: "/dispatch 研发中心/支付组\n支付回调改造\n目标: 支持新版回调\n输入: 接口文档\n验收: 回调成功率 100%\n边界: 不改旧接口",
			want: &DispatchCommand{
				Unit:       "研发中心/支付组",
				Title:      "支付回调改造",
				Goal:       "支持新版回调",
				Input:      "接口文档",
				Acceptance: "回调成功率 100%",
				Boundary:   "不改旧接口",
			},
		},
		{
			name: "english aliases and full-width colon",
			body: "/dispatch 支付组\nRefactor callbacks\nGoal：Ship the new handler\nAcceptance: All tests pass\nBoundary: No API change",
			want: &DispatchCommand{
				Unit:       "支付组",
				Title:      "Refactor callbacks",
				Goal:       "Ship the new handler",
				Acceptance: "All tests pass",
				Boundary:   "No API change",
			},
		},
		{
			name: "multiline section and paragraph under the title",
			body: "/dispatch 支付组\n回调改造\n这是一段补充背景。\n目标: 第一行\n第二行\n验收:\n可以上线",
			want: &DispatchCommand{
				Unit:       "支付组",
				Title:      "回调改造",
				Goal:       "这是一段补充背景。\n第一行\n第二行",
				Acceptance: "可以上线",
			},
		},
		{
			name: "leading blank lines are allowed",
			body: "\n\n  /dispatch 支付组\n任务\n目标: 做\n验收: 好",
			want: &DispatchCommand{
				Unit:       "支付组",
				Title:      "任务",
				Goal:       "做",
				Acceptance: "好",
			},
		},
		{
			name: "organization bot project selector",
			body: "/dispatch 支付组\n回调改造\n项目: 支付台\n目标: 支持新版回调\n验收: 回调成功率 100%",
			want: &DispatchCommand{
				Unit:       "支付组",
				Project:    "支付台",
				Title:      "回调改造",
				Goal:       "支持新版回调",
				Acceptance: "回调成功率 100%",
			},
		},
		{
			name: "english project alias and number",
			body: "/dispatch 支付组\nCallback rework\nProject: 2\nGoal: Ship\nAcceptance: Green",
			want: &DispatchCommand{
				Unit:       "支付组",
				Project:    "2",
				Title:      "Callback rework",
				Goal:       "Ship",
				Acceptance: "Green",
			},
		},
		{
			name: "missing target",
			body: "/dispatch\n任务\n目标: 做\n验收: 好",
			want: &DispatchCommand{
				Title:      "任务",
				Goal:       "做",
				Acceptance: "好",
			},
		},
		{
			name: "missing acceptance",
			body: "/dispatch 支付组\n任务\n目标: 做",
			want: &DispatchCommand{
				Unit:  "支付组",
				Title: "任务",
				Goal:  "做",
			},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := ParseDispatchCommand(tc.body)
			if !ok {
				t.Fatalf("ParseDispatchCommand() did not match")
			}
			if *got != *tc.want {
				t.Fatalf("ParseDispatchCommand() = %+v, want %+v", *got, *tc.want)
			}
		})
	}

	for _, body := range []string{
		"",
		"hello",
		"/dispatchers 支付组",
		"/Dispatch 支付组",
		"hey /dispatch 支付组",
	} {
		if cmd, ok := ParseDispatchCommand(body); ok {
			t.Errorf("ParseDispatchCommand(%q) matched unexpectedly: %+v", body, cmd)
		}
	}
}

func TestDispatchCommand_Valid(t *testing.T) {
	full := DispatchCommand{Unit: "u", Title: "t", Goal: "g", Acceptance: "a"}
	if !full.Valid() {
		t.Fatal("complete command should be valid")
	}
	for _, mut := range []func(*DispatchCommand){
		func(c *DispatchCommand) { c.Unit = "" },
		func(c *DispatchCommand) { c.Title = "" },
		func(c *DispatchCommand) { c.Goal = "" },
		func(c *DispatchCommand) { c.Acceptance = "" },
	} {
		cmd := full
		mut(&cmd)
		if cmd.Valid() {
			t.Errorf("command %+v should be invalid", cmd)
		}
	}
}

func TestDispatchCommand_Description(t *testing.T) {
	cmd := DispatchCommand{
		Goal:       "支持新版回调",
		Acceptance: "回调成功率 100%",
	}
	want := "## 目标\n支持新版回调\n\n## 验收\n回调成功率 100%"
	if got := cmd.Description(); got != want {
		t.Fatalf("Description() = %q, want %q", got, want)
	}
	if got := (DispatchCommand{}).Description(); got != "" {
		t.Fatalf("empty Description() = %q, want empty", got)
	}
}
