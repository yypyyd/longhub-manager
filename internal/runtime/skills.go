package runtime

import (
	"regexp"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"
)

// SkillSummary is the readiness summary reported by OpenClaw.
type SkillSummary struct {
	Ready int `json:"ready"`
	Total int `json:"total"`
}

// SkillRecord is one logical skill reconstructed from the wrapped CLI table.
type SkillRecord struct {
	Status      string `json:"status"`
	StatusLabel string `json:"status_label"`
	Name        string `json:"name"`
	Description string `json:"description"`
	Source      string `json:"source"`
}

// SkillList contains structured skill data suitable for the Manager UI.
type SkillList struct {
	Summary SkillSummary  `json:"summary"`
	Skills  []SkillRecord `json:"skills"`
	Hint    string        `json:"hint,omitempty"`
}

var (
	skillSummaryPattern = regexp.MustCompile(`(?i)(\d+)\s*/\s*(\d+)\s+ready\b`)
	ansiEscapePattern   = regexp.MustCompile("\\x1b\\[[0-?]*[ -/]*[@-~]")
)

// ParseSkillList converts the human-oriented `openclaw skills list` table into
// stable records. Continuation rows are joined to the previous description.
func ParseSkillList(output string) SkillList {
	result := SkillList{Skills: make([]SkillRecord, 0)}
	cleaned := ansiEscapePattern.ReplaceAllString(strings.ReplaceAll(output, "\r\n", "\n"), "")
	summaryFound := false
	inTable := false

	for _, rawLine := range strings.Split(cleaned, "\n") {
		line := strings.TrimSpace(strings.ReplaceAll(rawLine, "│", "|"))
		if line == "" {
			continue
		}
		if !summaryFound {
			if match := skillSummaryPattern.FindStringSubmatch(line); len(match) == 3 {
				result.Summary.Ready, _ = strconv.Atoi(match[1])
				result.Summary.Total, _ = strconv.Atoi(match[2])
				summaryFound = true
			}
		}
		if isSkillTableBorder(line) {
			continue
		}

		cells, ok := parseSkillTableRow(line)
		if ok {
			inTable = true
			statusLabel, name, description, source := cells[0], cells[1], cells[2], cells[3]
			if strings.EqualFold(statusLabel, "status") && strings.EqualFold(name, "skill") {
				continue
			}
			if name != "" {
				result.Skills = append(result.Skills, SkillRecord{
					Status:      normalizeSkillStatus(statusLabel),
					StatusLabel: statusLabel,
					Name:        name,
					Description: description,
					Source:      source,
				})
				continue
			}
			if description != "" && len(result.Skills) > 0 {
				last := &result.Skills[len(result.Skills)-1]
				last.Description = joinWrappedText(last.Description, description)
			}
			continue
		}

		if inTable && (strings.HasPrefix(line, "提示:") || strings.HasPrefix(strings.ToLower(line), "hint:")) {
			result.Hint = line
		}
	}

	if !summaryFound {
		result.Summary.Total = len(result.Skills)
		for _, skill := range result.Skills {
			if skill.Status == "ready" {
				result.Summary.Ready++
			}
		}
	}
	return result
}

func isSkillTableBorder(line string) bool {
	if strings.HasPrefix(line, "+") {
		return true
	}
	return strings.ContainsRune("┌├└┬┼┴", []rune(line)[0])
}

func parseSkillTableRow(line string) ([4]string, bool) {
	var cells [4]string
	if !strings.HasPrefix(line, "|") || !strings.HasSuffix(line, "|") {
		return cells, false
	}
	parts := strings.Split(line, "|")
	if len(parts) < 6 {
		return cells, false
	}
	cells[0] = strings.TrimSpace(parts[1])
	cells[1] = strings.TrimSpace(parts[2])
	cells[2] = strings.TrimSpace(strings.Join(parts[3:len(parts)-2], "|"))
	cells[3] = strings.TrimSpace(parts[len(parts)-2])
	return cells, true
}

func normalizeSkillStatus(label string) string {
	value := strings.ToLower(strings.TrimSpace(label))
	switch {
	case strings.Contains(value, "needs setup") || strings.Contains(value, "needs_setup"):
		return "needs_setup"
	case strings.Contains(value, "ready") || strings.Contains(value, "✓"):
		return "ready"
	case strings.Contains(value, "disabled"):
		return "disabled"
	default:
		return "unknown"
	}
}

func joinWrappedText(current, continuation string) string {
	current = strings.TrimSpace(current)
	continuation = strings.TrimSpace(continuation)
	if current == "" {
		return continuation
	}
	if continuation == "" {
		return current
	}
	last, _ := utf8.DecodeLastRuneInString(current)
	first, _ := utf8.DecodeRuneInString(continuation)
	if strings.ContainsRune("-/_([{", last) || strings.ContainsRune(",.;:!?%)]}，。；：！？、/", first) ||
		(isCJK(last) && isCJK(first)) {
		return current + continuation
	}
	return current + " " + continuation
}

func isCJK(value rune) bool {
	return unicode.Is(unicode.Han, value) || unicode.Is(unicode.Hiragana, value) ||
		unicode.Is(unicode.Katakana, value) || unicode.Is(unicode.Hangul, value)
}
