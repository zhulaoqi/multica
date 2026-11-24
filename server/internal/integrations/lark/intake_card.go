package lark

import "encoding/json"

// IntakeCard is one personal interactive card. The button opens the chat.
// Requester cards must not include an agent name or a machine name; callers
// put only the progress text in body.
//
// The body is Markdown: inbox cards mirror agent comments, which carry headings
// and tables. Schema 2.0's `markdown` element renders those; the legacy
// `lark_md` text shows them as raw characters (see SendMarkdownCard).
func IntakeCard(template, title, body, button, url string) (string, error) {
	if template == "" {
		template = "blue"
	}
	doc := map[string]any{
		"schema": "2.0",
		"config": map[string]any{"update_multi": true},
		"header": map[string]any{
			"template": template,
			"title":    map[string]any{"tag": "plain_text", "content": title},
		},
		"body": map[string]any{
			"elements": []any{
				map[string]any{"tag": "markdown", "content": markdownImageLinks(body)},
				map[string]any{
					"tag":  "button",
					"text": map[string]any{"tag": "plain_text", "content": button},
					"type": "primary_filled",
					"behaviors": []any{
						map[string]any{"type": "open_url", "default_url": url},
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
