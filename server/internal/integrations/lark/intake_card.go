package lark

import "encoding/json"

// IntakeCard is one personal interactive card. The button opens the chat.
// Requester cards must not include an agent name or a machine name; callers
// put only the progress text in body.
func IntakeCard(template, title, body, button, url string) (string, error) {
	if template == "" {
		template = "blue"
	}
	doc := map[string]any{
		"config": map[string]any{"wide_screen_mode": true},
		"header": map[string]any{
			"template": template,
			"title":    map[string]any{"tag": "plain_text", "content": title},
		},
		"elements": []any{
			map[string]any{
				"tag":  "div",
				"text": map[string]any{"tag": "lark_md", "content": body},
			},
			map[string]any{
				"tag": "action",
				"actions": []any{
					map[string]any{
						"tag":  "button",
						"text": map[string]any{"tag": "plain_text", "content": button},
						"type": "primary",
						"url":  url,
					},
				},
			},
		},
	}
	raw, err := json.Marshal(doc)
	if err != nil {
		return "", err
	}
	return string(raw), nil
}
