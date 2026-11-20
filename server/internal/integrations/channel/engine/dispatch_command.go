package engine

import (
	"strings"
	"unicode/utf8"
)

// dispatchCommandPrefix is the literal command token for dispatching work to an
// organization unit. Like /issue it is matched exactly and case-sensitively so
// an inline mention of "/dispatch" in a sentence never starts a dispatch.
const dispatchCommandPrefix = "/dispatch"

// DispatchCommand is the parsed `/dispatch` command. The first line names the
// target department; the body carries the brief the department's squad leader
// decomposes.
//
//	/dispatch <department name or path>
//	<title>
//	目标: ...
//	输入: ...
//	验收: ...
//	边界: ...
//
// English headers (Goal/Input/Acceptance/Boundary) are accepted as
// aliases. Lines after the title and before the first header feed the goal,
// because a sender usually continues the title with a sentence of context.
type DispatchCommand struct {
	Unit       string
	Title      string
	Goal       string
	Input      string
	Acceptance string
	Boundary   string
}

// Valid reports whether the command carries the parts a department needs to
// start: a target, a title, a goal and acceptance criteria.
func (c DispatchCommand) Valid() bool {
	return c.Unit != "" && c.Title != "" && c.Goal != "" && c.Acceptance != ""
}

// Description renders the brief as the same sectioned Markdown the in-app
// dispatch dialog writes, so an agent cannot tell which surface created it.
func (c DispatchCommand) Description() string {
	var b strings.Builder
	sections := []struct{ title, body string }{
		{"目标", c.Goal},
		{"输入", c.Input},
		{"验收", c.Acceptance},
		{"边界", c.Boundary},
	}
	for _, section := range sections {
		body := strings.TrimSpace(section.body)
		if body == "" {
			continue
		}
		if b.Len() > 0 {
			b.WriteString("\n\n")
		}
		b.WriteString("## ")
		b.WriteString(section.title)
		b.WriteString("\n")
		b.WriteString(body)
	}
	return b.String()
}

// ParseDispatchCommand extracts a /dispatch command from a chat-message body.
// It returns (nil, false) unless the first non-empty line is the literal
// token-bounded prefix.
func ParseDispatchCommand(body string) (*DispatchCommand, bool) {
	lines := strings.Split(body, "\n")
	firstIdx := -1
	for i, line := range lines {
		if strings.TrimSpace(line) != "" {
			firstIdx = i
			break
		}
	}
	if firstIdx == -1 {
		return nil, false
	}

	trimmed := strings.TrimLeft(lines[firstIdx], " \t")
	if !strings.HasPrefix(trimmed, dispatchCommandPrefix) {
		return nil, false
	}
	rest := trimmed[len(dispatchCommandPrefix):]
	if rest != "" {
		if r0 := rest[0]; r0 != ' ' && r0 != '\t' {
			return nil, false
		}
	}

	cmd := &DispatchCommand{Unit: strings.TrimSpace(rest)}
	var titleLines []string
	section := ""
	for _, line := range lines[firstIdx+1:] {
		if body, ok := dispatchSectionValue(line); ok {
			section = dispatchSectionKey(line)
			cmd.appendSection(section, body)
			continue
		}
		if strings.TrimSpace(line) == "" {
			continue
		}
		if section == "" {
			titleLines = append(titleLines, strings.TrimSpace(line))
			continue
		}
		cmd.appendSection(section, line)
	}
	if len(titleLines) > 0 {
		cmd.Title = titleLines[0]
		// Context the sender wrote under the title is goal material.
		if len(titleLines) > 1 {
			extra := strings.Join(titleLines[1:], "\n")
			if cmd.Goal == "" {
				cmd.Goal = extra
			} else {
				cmd.Goal = extra + "\n" + cmd.Goal
			}
		}
	}
	cmd.trimSections()
	return cmd, true
}

func (c *DispatchCommand) appendSection(section, line string) {
	switch section {
	case "goal":
		c.Goal = joinLine(c.Goal, line)
	case "input":
		c.Input = joinLine(c.Input, line)
	case "acceptance":
		c.Acceptance = joinLine(c.Acceptance, line)
	case "boundary":
		c.Boundary = joinLine(c.Boundary, line)
	}
}

func (c *DispatchCommand) trimSections() {
	c.Goal = strings.TrimSpace(c.Goal)
	c.Input = strings.TrimSpace(c.Input)
	c.Acceptance = strings.TrimSpace(c.Acceptance)
	c.Boundary = strings.TrimSpace(c.Boundary)
}

func joinLine(current, line string) string {
	if current == "" {
		return line
	}
	return current + "\n" + line
}

// dispatchSectionKey recognizes a section header line ("目标:", "Goal:", with
// an ASCII or full-width colon) and returns its canonical key.
func dispatchSectionKey(line string) string {
	trimmed := strings.TrimSpace(line)
	colon := -1
	for i, r := range trimmed {
		if r == ':' || r == '：' {
			colon = i
			break
		}
	}
	if colon <= 0 {
		return ""
	}
	switch strings.ToLower(strings.TrimSpace(trimmed[:colon])) {
	case "goal", "目标":
		return "goal"
	case "input", "输入":
		return "input"
	case "acceptance", "验收":
		return "acceptance"
	case "boundary", "边界":
		return "boundary"
	}
	return ""
}

// dispatchSectionValue returns the inline text after the header's colon, which
// may be empty when the section body starts on the following line.
func dispatchSectionValue(line string) (string, bool) {
	if dispatchSectionKey(line) == "" {
		return "", false
	}
	trimmed := strings.TrimSpace(line)
	for i, r := range trimmed {
		if r == ':' || r == '：' {
			return strings.TrimSpace(trimmed[i+utf8.RuneLen(r):]), true
		}
	}
	return "", true
}
