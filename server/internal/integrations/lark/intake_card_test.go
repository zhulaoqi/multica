package lark

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestIntakeCard_HidesExecutorFromRequester(t *testing.T) {
	raw, err := IntakeCard("blue", "做到这里", "做到：退款接口\n还剩：对账", "打开对话", "https://example.test/intake/1")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(raw, "Agent") || strings.Contains(raw, "机器") {
		t.Fatalf("card names an executor: %s", raw)
	}
	var doc map[string]any
	if err := json.Unmarshal([]byte(raw), &doc); err != nil {
		t.Fatal(err)
	}
	elements := doc["body"].(map[string]any)["elements"].([]any)
	button := elements[1].(map[string]any)
	behavior := button["behaviors"].([]any)[0].(map[string]any)
	if behavior["type"] != "open_url" || behavior["default_url"] != "https://example.test/intake/1" {
		t.Fatalf("button = %#v", button)
	}
}

// Inbox cards mirror agent comments, so headings and tables must reach a
// Markdown element that renders them instead of the legacy lark_md text.
func TestIntakeCard_RendersMarkdownBody(t *testing.T) {
	body := "## 已接模型的渠道\n\n| 渠道 | 模型数 |\n|---|---|\n| QWEN | 100 |"
	raw, err := IntakeCard("blue", "有新评论", body, "打开", "https://example.test/inbox")
	if err != nil {
		t.Fatal(err)
	}
	var doc map[string]any
	if err := json.Unmarshal([]byte(raw), &doc); err != nil {
		t.Fatal(err)
	}
	if doc["schema"] != "2.0" {
		t.Fatalf("schema = %v, want 2.0", doc["schema"])
	}
	content := doc["body"].(map[string]any)["elements"].([]any)[0].(map[string]any)
	if content["tag"] != "markdown" || content["content"] != body {
		t.Fatalf("body element = %#v", content)
	}
}
