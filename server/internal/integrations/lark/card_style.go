package lark

import "strings"

func cardMarkdownWithTitle(title, body string) string {
	title = cardPlain(title)
	body = strings.TrimSpace(body)
	switch {
	case title == "":
		return body
	case body == "":
		return "**" + title + "**"
	default:
		return "**" + title + "**\n\n" + body
	}
}
