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
	config := doc["config"].(map[string]any)
	if config["wide_screen_mode"] != true {
		t.Fatalf("config = %#v", config)
	}
	elements := doc["elements"].([]any)
	action := elements[1].(map[string]any)
	button := action["actions"].([]any)[0].(map[string]any)
	if button["url"] != "https://example.test/intake/1" {
		t.Fatalf("button = %#v", button)
	}
}
