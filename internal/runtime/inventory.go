package runtime

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/url"
	"regexp"
	"strings"
)

var inventoryCommands = map[string][]string{
	"models":         {"models", "list", "--json"},
	"model_status":   {"models", "status", "--json"},
	"agents":         {"agents", "list", "--bindings", "--json"},
	"channels":       {"channels", "list", "--json"},
	"channel_status": {"channels", "status", "--json"},
	"cron":           {"cron", "list", "--all", "--json"},
	"memory":         {"memory", "status", "--json"},
	"plugins":        {"plugins", "list", "--json"},
	"usage":          {"status", "--json", "--usage"},
	"skills":         {"skills", "list", "--json"},
	"sessions":       {"sessions", "list", "--all-agents", "--limit", "100", "--json"},
	"security":       {"security", "audit", "--json"},
	"diagnostics":    {"doctor", "--lint", "--json", "--non-interactive"},
}

var inventorySecretPattern = regexp.MustCompile(`(?i)(bearer\s+|(?:api[_-]?key|token|password|secret)\s*[:=]\s*)[^\s,;]+`)
var inventoryCredentialPattern = regexp.MustCompile(`(?i)\b(?:sk|xoxb|xoxp|ghp|github_pat)-[a-z0-9_-]{12,}`)
var inventoryWindowsHomePattern = regexp.MustCompile(`(?i)[a-z]:\\users\\[^\\/\s]+`)
var inventoryUnixHomePattern = regexp.MustCompile(`(?:/home|/Users)/[^/\s]+`)

// Inventory runs one fixed machine-readable OpenClaw query and returns
// validated, recursively redacted JSON. It never accepts CLI arguments from a
// page or model.
func (a *NativeAdapter) Inventory(ctx context.Context, kind string) (json.RawMessage, error) {
	args, ok := inventoryCommands[kind]
	if !ok {
		return nil, fmt.Errorf("不允许的 OpenClaw inventory: %s", kind)
	}
	command, err := findOpenClaw(ctx, a.runner)
	if err != nil {
		return nil, errors.New("未发现原生 OpenClaw")
	}
	output, runErr := a.run(ctx, command, args...)
	if runErr != nil && (errors.Is(runErr, context.Canceled) || errors.Is(runErr, context.DeadlineExceeded) || errors.Is(runErr, ErrCommandOutputLimit)) {
		return nil, fmt.Errorf("读取 OpenClaw %s 失败: %s", kind, safeCommandError(runErr, output))
	}
	decoder := json.NewDecoder(strings.NewReader(strings.TrimSpace(output)))
	decoder.UseNumber()
	var value any
	if err := decoder.Decode(&value); err != nil {
		if runErr != nil {
			return nil, fmt.Errorf("读取 OpenClaw %s 失败: %s", kind, safeCommandError(runErr, output))
		}
		return nil, fmt.Errorf("OpenClaw %s 没有返回有效 JSON", kind)
	}
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		return nil, fmt.Errorf("OpenClaw %s 返回了多余数据", kind)
	}
	if runErr != nil && kind != "diagnostics" && kind != "security" {
		return nil, fmt.Errorf("读取 OpenClaw %s 失败: %s", kind, safeCommandError(runErr, output))
	}
	redactInventory(value)
	encoded, err := json.Marshal(value)
	if err != nil {
		return nil, errors.New("编码 OpenClaw inventory 失败")
	}
	return json.RawMessage(encoded), nil
}

func redactInventory(value any) {
	switch typed := value.(type) {
	case map[string]any:
		for key, child := range typed {
			if sensitiveInventoryKey(key) {
				typed[key] = "[REDACTED]"
				continue
			}
			if text, ok := child.(string); ok {
				typed[key] = redactInventoryString(text)
				continue
			}
			redactInventory(child)
		}
	case []any:
		for _, child := range typed {
			redactInventory(child)
		}
	}
}

func redactInventoryString(value string) string {
	redacted := inventorySecretPattern.ReplaceAllString(value, "${1}[REDACTED]")
	redacted = inventoryCredentialPattern.ReplaceAllString(redacted, "[REDACTED]")
	redacted = inventoryWindowsHomePattern.ReplaceAllString(redacted, "~")
	redacted = inventoryUnixHomePattern.ReplaceAllString(redacted, "~")
	parsed, err := url.Parse(redacted)
	if err != nil || parsed.RawQuery == "" {
		return redacted
	}
	query := parsed.Query()
	changed := false
	for key := range query {
		if sensitiveInventoryKey(key) {
			query.Set(key, "[REDACTED]")
			changed = true
		}
	}
	if !changed {
		return redacted
	}
	parsed.RawQuery = query.Encode()
	return parsed.String()
}

func sensitiveInventoryKey(key string) bool {
	normalized := strings.ToLower(strings.NewReplacer("_", "", "-", "", ".", "").Replace(key))
	for _, marker := range []string{"apikey", "token", "password", "secret", "credential", "authorization", "cookie"} {
		if strings.Contains(normalized, marker) {
			return true
		}
	}
	return false
}
