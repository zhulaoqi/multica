package lark

import (
	"encoding/json"
	"testing"
)

func TestSecondDevelopedCardsUseHeaderlessSchema2(t *testing.T) {
	intake, err := IntakeCard("等你补充", "需求：补充验收标准", "打开对话", "https://example.test/intake")
	if err != nil {
		t.Fatal(err)
	}
	inbox, err := InboxCard("**有新评论**\n\n评论内容", "打开任务", "https://example.test/inbox")
	if err != nil {
		t.Fatal(err)
	}
	org, err := orgBotChoiceCard("发给谁", []orgBotCardButton{{Label: "我的小队", Action: "target:personal"}})
	if err != nil {
		t.Fatal(err)
	}

	for name, raw := range map[string]string{
		"intake": intake,
		"inbox":  inbox,
		"org":    org,
	} {
		t.Run(name, func(t *testing.T) {
			var doc map[string]any
			if err := json.Unmarshal([]byte(raw), &doc); err != nil {
				t.Fatal(err)
			}
			if doc["schema"] != "2.0" {
				t.Fatalf("schema = %#v, want 2.0: %s", doc["schema"], raw)
			}
			if _, ok := doc["header"]; ok {
				t.Fatalf("custom card still has a header: %s", raw)
			}
			body, _ := doc["body"].(map[string]any)
			elements, _ := body["elements"].([]any)
			if len(elements) == 0 {
				t.Fatalf("card has no body elements: %s", raw)
			}
			first, _ := elements[0].(map[string]any)
			if first["tag"] != "markdown" {
				t.Fatalf("first body element is not markdown: %#v", first)
			}
		})
	}
}
