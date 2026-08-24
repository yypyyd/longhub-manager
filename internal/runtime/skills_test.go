package runtime

import (
	"strings"
	"testing"
)

func TestParseSkillListReconstructsWrappedRows(t *testing.T) {
	output := `技能 (2/3 ready)
+---------------+----------------------+--------------------------------+-------------------+
| Status        | Skill                | Description                    | Source            |
+---------------+----------------------+--------------------------------+-------------------+
| ✓ ready       | 🐙 github            | GitHub CLI for issues, PRs,    | openclaw-bundled  |
|               |                      | reviews, and releases.          |                   |
| △ needs setup | 📝 apple-notes       | Create and search Apple Notes. | openclaw-bundled  |
| disabled      | cmm-api              | 蝉妈妈电商数据查询，支持指定     | openclaw-workspace|
|               |                      | 时间范围和商品分类。             |                   |
+---------------+----------------------+--------------------------------+-------------------+

提示: 使用 openclaw skills search 管理技能。`

	result := ParseSkillList(output)
	if result.Summary.Ready != 2 || result.Summary.Total != 3 {
		t.Fatalf("unexpected summary: %+v", result.Summary)
	}
	if len(result.Skills) != 3 {
		t.Fatalf("expected 3 skills, got %d", len(result.Skills))
	}
	if got := result.Skills[0]; got.Status != "ready" || got.Name != "🐙 github" ||
		got.Description != "GitHub CLI for issues, PRs, reviews, and releases." {
		t.Fatalf("unexpected ready skill: %+v", got)
	}
	if got := result.Skills[1]; got.Status != "needs_setup" || got.StatusLabel != "△ needs setup" {
		t.Fatalf("unexpected setup skill: %+v", got)
	}
	if got := result.Skills[2]; got.Status != "disabled" || got.Description != "蝉妈妈电商数据查询，支持指定时间范围和商品分类。" {
		t.Fatalf("unexpected Chinese description: %+v", got)
	}
	if !strings.HasPrefix(result.Hint, "提示:") {
		t.Fatalf("unexpected hint: %q", result.Hint)
	}
}

func TestParseSkillListHandlesPipesANSIAndMissingSummary(t *testing.T) {
	output := "\x1b[32m| ✓ ready | sample | Uses A | B syntax. | local |\x1b[0m\n"
	result := ParseSkillList(output)
	if result.Summary.Ready != 1 || result.Summary.Total != 1 {
		t.Fatalf("unexpected derived summary: %+v", result.Summary)
	}
	if len(result.Skills) != 1 || result.Skills[0].Description != "Uses A | B syntax." {
		t.Fatalf("unexpected parsed skills: %+v", result.Skills)
	}
}

func TestParseSkillListRestoresSpacesAtWordAndLanguageBoundaries(t *testing.T) {
	output := `技能 (1/1 ready)
| Status  | Skill  | Description                 | Source |
| ✓ ready | sample | 从 Lark-flavored Markdown  | local  |
|          |        | 内容创建文档。App           |        |
|          |        | (2) supports post-          |        |
|          |        | mortem checks。             |        |`
	result := ParseSkillList(output)
	want := "从 Lark-flavored Markdown 内容创建文档。App (2) supports post-mortem checks。"
	if len(result.Skills) != 1 || result.Skills[0].Description != want {
		t.Fatalf("unexpected reconstructed description: %+v", result.Skills)
	}
}

func TestParseSkillListIgnoresMalformedAndEmptyOutput(t *testing.T) {
	for _, output := range []string{"", "not a table", "| only | three | cells |"} {
		result := ParseSkillList(output)
		if result.Skills == nil || len(result.Skills) != 0 {
			t.Fatalf("expected an empty non-nil list for %q, got %#v", output, result.Skills)
		}
	}
}
