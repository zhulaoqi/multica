package lark

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/multica-ai/multica/server/internal/service"
)

func TestOrgBotFrameDecoderCardAction(t *testing.T) {
	raw := []byte(`{"schema":"2.0","header":{"event_id":"ev_1","event_type":"card.action.trigger","app_id":"cli_x"},"event":{"operator":{"open_id":"ou_user"},"action":{"value":{"action":"target:personal"}},"context":{"open_chat_id":"oc_dm"}}}`)
	msg, ok, err := NewOrgBotFrameDecoder().Decode(raw, Installation{})
	if err != nil || !ok {
		t.Fatalf("decode = ok %v err %v", ok, err)
	}
	if msg.MessageType != orgBotCardActionType || msg.CommandBody != "target:personal" {
		t.Fatalf("message = %+v", msg)
	}
	if msg.ChatID != "oc_dm" || msg.SenderOpenID != "ou_user" || msg.MessageID != "ev_1" {
		t.Fatalf("message = %+v", msg)
	}
}

func TestOrgBotFrameDecoderMenuClick(t *testing.T) {
	raw := []byte(`{"schema":"2.0","header":{"event_id":"ev_menu","event_type":"application.bot.menu_v6","app_id":"cli_x"},"event":{"operator":{"operator_id":{"open_id":"ou_user"}},"event_key":"s","timestamp":1669364458}}`)
	msg, ok, err := NewOrgBotFrameDecoder().Decode(raw, Installation{})
	if err != nil || !ok {
		t.Fatalf("decode = ok %v err %v", ok, err)
	}
	if msg.MessageType != orgBotMenuActionType || msg.CommandBody != "s" || msg.ChatID != "" {
		t.Fatalf("message = %+v", msg)
	}
	if msg.SenderOpenID != "ou_user" || msg.MessageID != "ev_menu" || msg.ChatType != ChatTypeP2P {
		t.Fatalf("message = %+v", msg)
	}
}

func TestOrgBotFrameDecoderChatEntered(t *testing.T) {
	raw := []byte(`{"schema":"2.0","header":{"event_id":"ev_enter","event_type":"im.chat.access_event.bot_p2p_chat_entered_v1","app_id":"cli_x"},"event":{"chat_id":"oc_dm","operator_id":{"open_id":"ou_user"}}}`)
	msg, ok, err := NewOrgBotFrameDecoder().Decode(raw, Installation{})
	if err != nil || !ok {
		t.Fatalf("decode = ok %v err %v", ok, err)
	}
	if msg.MessageType != orgBotChatEnteredType || msg.ChatID != "oc_dm" || msg.SenderOpenID != "ou_user" {
		t.Fatalf("message = %+v", msg)
	}
	if msg.LastVisibleMessageID != "" {
		t.Fatalf("missing last message = %q", msg.LastVisibleMessageID)
	}

	kept := []byte(`{"schema":"2.0","header":{"event_id":"ev_enter","event_type":"im.chat.access_event.bot_p2p_chat_entered_v1","app_id":"cli_x"},"event":{"chat_id":"oc_dm","operator_id":{"open_id":"ou_user"},"last_message_id":"om_kept"}}`)
	msg, ok, err = NewOrgBotFrameDecoder().Decode(kept, Installation{})
	if err != nil || !ok || msg.LastVisibleMessageID != "om_kept" {
		t.Fatalf("kept history = %+v ok %v err %v", msg, ok, err)
	}
}

func TestOrgBotCardsStayCompact(t *testing.T) {
	choice, err := orgBotChoiceCard("发给谁", []orgBotCardButton{{Label: "我的小队", Action: "target:personal"}})
	if err != nil {
		t.Fatalf("choice: %v", err)
	}
	selected, err := orgBotSelectedCard(OrgBotChatContext{
		Kind:        orgBotTargetProject,
		ProjectName: "AIHUB",
		UnitPath:    "企业技术中心/研发部/开发二组",
		ProjectID:   pgtype.UUID{Valid: true},
	}, true)
	if err != nil {
		t.Fatalf("selected: %v", err)
	}
	dispatched, err := orgBotDispatchedCard("项目 AIHUB", "把登录失败的提示改清楚", "AIHU-6", "企业技术中心/研发部/开发二组", "https://yiyan.example/aihub/issues/AIHU-6", "", true)
	if err != nil {
		t.Fatalf("dispatched: %v", err)
	}
	start, err := OrgDispatchStartCard("AIHU-6", "把登录失败的提示改清楚", projectWorker(), "https://yiyan.example/aihub/issues/AIHU-6")
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	for _, card := range []string{choice, selected, dispatched, start} {
		if strings.Contains(card, `"subtitle"`) || strings.Contains(card, "队长会拆解") {
			t.Fatalf("card is not compact: %s", card)
		}
	}
	if !strings.Contains(choice, `"target:personal"`) || !strings.Contains(selected, "已选中 项目 AIHUB") {
		t.Fatalf("choice=%s selected=%s", choice, selected)
	}
	if !strings.Contains(dispatched, "AIHU-6") || !strings.Contains(dispatched, "把登录失败的提示改清楚") || strings.Contains(dispatched, "ECLI-") {
		t.Fatalf("dispatched=%s", dispatched)
	}
	if !strings.Contains(start, "项目开始处理") || !strings.Contains(start, "AIHU-6") {
		t.Fatalf("start=%s", start)
	}
}

func TestOrgBotCardActionsShareOneRow(t *testing.T) {
	card, err := orgBotDispatchedCard("项目 AIHUB", "输出一个皇后算法", "AIHU-13", "企业技术中心/研发部/开发二组", "https://yiyan.example/aihub/issues/AIHU-13", "", true)
	if err != nil {
		t.Fatalf("dispatched: %v", err)
	}
	open, switchBtn := footerButtons(t, card)
	if open["type"] != "primary_filled" || open["width"] != "default" || buttonText(t, open) != "打开任务" {
		t.Fatalf("open button = %#v", open)
	}
	if switchBtn["type"] != "default" || switchBtn["width"] != "default" || buttonText(t, switchBtn) != "切换" {
		t.Fatalf("switch button = %#v", switchBtn)
	}
	if strings.Contains(card, `"width":"fill"`) {
		t.Fatalf("footer actions must keep their content width: %s", card)
	}

	selected, err := orgBotSelectedCard(OrgBotChatContext{Kind: orgBotTargetPersonal}, true)
	if err != nil {
		t.Fatalf("selected: %v", err)
	}
	if strings.Contains(selected, "primary_filled") || strings.Contains(selected, `"width":"fill"`) {
		t.Fatalf("lone switch should be a content-width default button: %s", selected)
	}

	choice, err := orgBotChoiceCard("发给谁", []orgBotCardButton{
		{Label: "我的小队", Action: "target:personal"},
		{Label: "开发二组", Action: "unit:dev"},
	})
	if err != nil {
		t.Fatalf("choice: %v", err)
	}
	if strings.Count(choice, `"width":"fill"`) != 2 {
		t.Fatalf("choice buttons should fill the row: %s", choice)
	}
}

func TestOrgBotCardFieldsAlignOnAFixedLabelColumn(t *testing.T) {
	card, err := OrgDispatchStartCard("AIHU-6", "把登录失败的提示改清楚", projectWorker(), "https://yiyan.example/aihub/issues/AIHU-6")
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	elements := cardBodyElements(t, card)
	if len(elements) != 5 {
		t.Fatalf("elements = %d, want four fields plus one action row", len(elements))
	}
	for _, row := range elements[:4] {
		columns, _ := row["columns"].([]any)
		if len(columns) != 2 {
			t.Fatalf("field columns = %#v", row["columns"])
		}
		label, _ := columns[0].(map[string]any)
		value, _ := columns[1].(map[string]any)
		if label["width"] != orgBotFieldLabelWidth || value["width"] != "weighted" {
			t.Fatalf("field row = %#v", row)
		}
	}
	if !strings.Contains(card, "color='grey'") {
		t.Fatalf("labels are not muted: %s", card)
	}
}

func projectWorker() OrgDispatchWorker {
	return NewOrgDispatchWorker(service.DispatchHandoff{Kind: service.DispatchHandoffProject, Name: "AIHUB"}, true, "hzg的cursor")
}

// Every receipt after 已交给 names who is doing the work, and the start card
// is titled after the kind of target instead of always saying 部门.
func TestOrgDispatchReceiptsNameWhoIsDoingTheWork(t *testing.T) {
	cases := []struct {
		name     string
		handoff  service.DispatchHandoff
		title    string
		handedTo string
	}{
		{"project", service.DispatchHandoff{Kind: service.DispatchHandoffProject, Name: "AIHUB"}, "项目开始处理", "项目 AIHUB"},
		{"personal squad", service.DispatchHandoff{Kind: service.DispatchHandoffSquad, Name: "hzg的cursor"}, "小队开始处理", "我的小队"},
		{"department", service.DispatchHandoff{Kind: service.DispatchHandoffDepartment, Name: "开发二组"}, "部门开始处理", "开发二组"},
		{"unknown target", service.DispatchHandoff{}, "开始处理", ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			worker := NewOrgDispatchWorker(tc.handoff, true, "hzg的cursor")
			start, err := OrgDispatchStartCard("AIHU-6", "对账", worker, "https://yiyan.example/aihub/issues/AIHU-6")
			if err != nil {
				t.Fatalf("start: %v", err)
			}
			done, err := OrgDispatchDoneCard("AIHU-6", "对账", "in_review", worker, "https://yiyan.example/aihub/issues/AIHU-6")
			if err != nil {
				t.Fatalf("done: %v", err)
			}
			failed, err := OrgDispatchFailedCard("AIHU-6", "对账", worker, "runtime offline", "https://yiyan.example/aihub/issues/AIHU-6")
			if err != nil {
				t.Fatalf("failed: %v", err)
			}
			if got := cardTitle(t, start); got != tc.title {
				t.Fatalf("start title = %q, want %q", got, tc.title)
			}
			for name, card := range map[string]string{"start": start, "done": done, "failed": failed} {
				fields := cardFields(t, card)
				if fields["交给"] != tc.handedTo || fields["智能体"] != "hzg的cursor" {
					t.Fatalf("%s fields = %#v, want 交给 %q and the agent", name, fields, tc.handedTo)
				}
				if _, ok := fields["部门"]; ok {
					t.Fatalf("%s still has a 部门 field: %#v", name, fields)
				}
			}
			if fields := cardFields(t, done); fields["状态"] != "待验收" {
				t.Fatalf("done fields = %#v", fields)
			}
			if fields := cardFields(t, failed); fields["原因"] != "runtime offline" {
				t.Fatalf("failed fields = %#v", fields)
			}
		})
	}
}

// Someone else reading about a personal squad must not be told it is theirs.
func TestOrgDispatchWorkerNamesAnotherPersonsSquad(t *testing.T) {
	handoff := service.DispatchHandoff{Kind: service.DispatchHandoffSquad, Name: "hzg的cursor"}
	if got := NewOrgDispatchWorker(handoff, false, "").HandedTo; got != "小队 hzg的cursor" {
		t.Fatalf("handed to = %q", got)
	}
	if got := NewOrgDispatchWorker(handoff, true, "").HandedTo; got != "我的小队" {
		t.Fatalf("handed to = %q", got)
	}
}

func cardTitle(t *testing.T, card string) string {
	t.Helper()
	var doc struct {
		Header struct {
			Title struct {
				Content string `json:"content"`
			} `json:"title"`
		} `json:"header"`
	}
	if err := json.Unmarshal([]byte(card), &doc); err != nil {
		t.Fatalf("card json: %v", err)
	}
	return doc.Header.Title.Content
}

// cardFields reads the label/value rows of a notice card.
func cardFields(t *testing.T, card string) map[string]string {
	t.Helper()
	fields := map[string]string{}
	for _, row := range cardBodyElements(t, card) {
		columns, _ := row["columns"].([]any)
		if row["flex_mode"] != "none" || len(columns) != 2 {
			continue
		}
		label := strings.TrimSuffix(strings.TrimPrefix(columnMarkdown(columns[0]), "<font color='grey'>"), "</font>")
		fields[label] = columnMarkdown(columns[1])
	}
	return fields
}

func columnMarkdown(column any) string {
	col, _ := column.(map[string]any)
	elements, _ := col["elements"].([]any)
	if len(elements) != 1 {
		return ""
	}
	element, _ := elements[0].(map[string]any)
	content, _ := element["content"].(string)
	return content
}

func footerButtons(t *testing.T, card string) (open, switchBtn map[string]any) {
	t.Helper()
	elements := cardBodyElements(t, card)
	if len(elements) == 0 {
		t.Fatalf("card has no body elements: %s", card)
	}
	row := elements[len(elements)-1]
	columns, _ := row["columns"].([]any)
	if row["flex_mode"] != "flow" || len(columns) != 2 {
		t.Fatalf("action row = %#v", row)
	}
	for _, column := range columns {
		if col, _ := column.(map[string]any); col["width"] != "auto" {
			t.Fatalf("action column = %#v", column)
		}
	}
	return columnButton(t, columns[0]), columnButton(t, columns[1])
}

func cardBodyElements(t *testing.T, card string) []map[string]any {
	t.Helper()
	var doc struct {
		Body struct {
			Elements []map[string]any `json:"elements"`
		} `json:"body"`
	}
	if err := json.Unmarshal([]byte(card), &doc); err != nil {
		t.Fatalf("card json: %v", err)
	}
	return doc.Body.Elements
}

func columnButton(t *testing.T, column any) map[string]any {
	t.Helper()
	col, _ := column.(map[string]any)
	elements, _ := col["elements"].([]any)
	if len(elements) != 1 {
		t.Fatalf("column elements = %#v", col["elements"])
	}
	button, _ := elements[0].(map[string]any)
	return button
}

func buttonText(t *testing.T, button map[string]any) string {
	t.Helper()
	text, _ := button["text"].(map[string]any)
	content, _ := text["content"].(string)
	return content
}

func TestLarkJSONFrameDecoderStillDropsCardAction(t *testing.T) {
	raw := []byte(`{"schema":"2.0","header":{"event_id":"ev_1","event_type":"card.action.trigger","app_id":"cli_x"},"event":{"operator":{"open_id":"ou_user"},"action":{"value":{"action":"target:personal"}},"context":{"open_chat_id":"oc_dm"}}}`)
	_, ok, err := NewLarkJSONFrameDecoder().Decode(raw, Installation{})
	if err != nil || ok {
		t.Fatalf("shared decoder accepted a card action: ok %v err %v", ok, err)
	}
}
