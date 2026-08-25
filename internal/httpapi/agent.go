package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/yypyyd/longhub-manager/internal/manageragent"
)

func (s *Server) handleInventory(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED")
		return
	}
	kind := strings.TrimPrefix(r.URL.Path, "/api/v1/inventory/")
	if kind == "" || strings.Contains(kind, "/") {
		writeError(w, http.StatusNotFound, "INVENTORY_NOT_FOUND")
		return
	}
	data, err := s.adapter.Inventory(r.Context(), kind)
	if err != nil {
		writeError(w, http.StatusUnprocessableEntity, "OPENCLAW_INVENTORY_FAILED")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"kind": kind, "data": data})
}

func (s *Server) handleAgentConfig(w http.ResponseWriter, r *http.Request) {
	if s.agentConfig == nil {
		writeError(w, http.StatusServiceUnavailable, "MANAGER_AGENT_NOT_CONFIGURED")
		return
	}
	w.Header().Set("cache-control", "no-store")
	switch r.Method {
	case http.MethodGet:
		config, err := s.agentConfig.Public()
		if err != nil {
			writeError(w, http.StatusServiceUnavailable, "MANAGER_AGENT_CONFIG_UNAVAILABLE")
			return
		}
		writeJSON(w, http.StatusOK, config)
	case http.MethodPut:
		var body struct {
			BaseURL  string `json:"base_url"`
			Model    string `json:"model"`
			Protocol string `json:"protocol"`
			APIKey   string `json:"api_key"`
		}
		decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 16*1024))
		decoder.DisallowUnknownFields()
		if err := decoder.Decode(&body); err != nil || !jsonEOF(decoder) {
			writeError(w, http.StatusBadRequest, "INVALID_AGENT_CONFIG")
			return
		}
		config, err := s.agentConfig.SaveWithProtocol(body.BaseURL, body.Model, body.Protocol, body.APIKey)
		if err != nil {
			writeError(w, http.StatusUnprocessableEntity, "INVALID_AGENT_CONFIG")
			return
		}
		writeJSON(w, http.StatusOK, config)
	case http.MethodDelete:
		if err := s.agentConfig.Delete(); err != nil {
			writeError(w, http.StatusServiceUnavailable, "MANAGER_AGENT_CONFIG_DELETE_FAILED")
			return
		}
		writeJSON(w, http.StatusOK, map[string]bool{"deleted": true})
	default:
		writeError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED")
	}
}

func (s *Server) handleAgentConfigTest(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED")
		return
	}
	if s.agentEngine == nil {
		writeError(w, http.StatusServiceUnavailable, "MANAGER_AGENT_NOT_CONFIGURED")
		return
	}
	w.Header().Set("cache-control", "no-store")
	if err := s.agentEngine.TestConnection(r.Context()); err != nil {
		writeJSON(w, http.StatusUnprocessableEntity, map[string]string{
			"code":    "MANAGER_AGENT_CONNECTION_FAILED",
			"message": manageragent.PublicErrorMessage(err),
		})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"ok": true, "message": "模型连接正常，地址、凭据、模型和工具调用协议均可用。",
	})
}

func (s *Server) handleAgentTurn(w http.ResponseWriter, r *http.Request) {
	if s.agentEngine == nil || r.Method != http.MethodPost {
		if r.Method != http.MethodPost {
			writeError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED")
		} else {
			writeError(w, http.StatusServiceUnavailable, "MANAGER_AGENT_NOT_CONFIGURED")
		}
		return
	}
	var body struct {
		SessionID string `json:"session_id"`
		Message   string `json:"message"`
	}
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 12*1024))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&body); err != nil || !jsonEOF(decoder) {
		writeError(w, http.StatusBadRequest, "INVALID_AGENT_TURN")
		return
	}
	response, err := s.agentEngine.Turn(r.Context(), body.SessionID, body.Message)
	if err != nil {
		writeJSON(w, http.StatusUnprocessableEntity, map[string]string{
			"code":    "MANAGER_AGENT_TURN_FAILED",
			"message": manageragent.PublicErrorMessage(err),
		})
		return
	}
	writeJSON(w, http.StatusOK, response)
}

func (s *Server) handleAgentApproval(w http.ResponseWriter, r *http.Request) {
	if s.agentEngine == nil || r.Method != http.MethodPost {
		if r.Method != http.MethodPost {
			writeError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED")
		} else {
			writeError(w, http.StatusServiceUnavailable, "MANAGER_AGENT_NOT_CONFIGURED")
		}
		return
	}
	var body struct {
		SessionID  string `json:"session_id"`
		ApprovalID string `json:"approval_id"`
		Approved   bool   `json:"approved"`
	}
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4*1024))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&body); err != nil || body.SessionID == "" || body.ApprovalID == "" || !jsonEOF(decoder) {
		writeError(w, http.StatusBadRequest, "INVALID_AGENT_APPROVAL")
		return
	}
	response, err := s.agentEngine.ResolveApproval(r.Context(), body.SessionID, body.ApprovalID, body.Approved)
	if err != nil {
		writeError(w, http.StatusUnprocessableEntity, "MANAGER_AGENT_APPROVAL_FAILED")
		return
	}
	writeJSON(w, http.StatusOK, response)
}

func (s *Server) handleAgentReset(w http.ResponseWriter, r *http.Request) {
	if s.agentEngine == nil || r.Method != http.MethodPost {
		if r.Method != http.MethodPost {
			writeError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED")
		} else {
			writeError(w, http.StatusServiceUnavailable, "MANAGER_AGENT_NOT_CONFIGURED")
		}
		return
	}
	var body struct {
		SessionID string `json:"session_id"`
	}
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 2*1024))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&body); err != nil || body.SessionID == "" || !jsonEOF(decoder) {
		writeError(w, http.StatusBadRequest, "INVALID_AGENT_SESSION")
		return
	}
	s.agentEngine.Reset(body.SessionID)
	writeJSON(w, http.StatusOK, map[string]bool{"reset": true})
}

func (s *Server) runRepairTransaction(ctx context.Context) (map[string]any, int) {
	if s.configBackups == nil {
		return map[string]any{"code": "CONFIG_BACKUP_NOT_CONFIGURED"}, http.StatusServiceUnavailable
	}
	s.configMu.Lock()
	defer s.configMu.Unlock()
	checkpoint, err := s.configBackups.BeginMutation()
	if err != nil {
		return map[string]any{"code": configBackupErrorCode(err)}, http.StatusUnprocessableEntity
	}
	_, repairErr := s.adapter.Repair(ctx, true)
	validationFailed := false
	if repairErr == nil {
		repairErr = s.configBackups.ValidateActive(func(candidatePath string) error {
			return s.adapter.ValidateConfigCandidate(ctx, candidatePath)
		})
		validationFailed = repairErr != nil
	}
	if repairErr == nil {
		return map[string]any{
			"action": "repair", "checkpoint": checkpoint, "repaired": true, "validated": true,
		}, http.StatusOK
	}
	rollbackCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	_, rollbackErr := s.configBackups.RollbackMutation(checkpoint, func(candidatePath string) error {
		return s.adapter.ValidateConfigCandidate(rollbackCtx, candidatePath)
	})
	if rollbackErr != nil {
		return map[string]any{
			"code": "OPENCLAW_REPAIR_ROLLBACK_FAILED", "checkpoint": checkpoint,
		}, http.StatusInternalServerError
	}
	code := "OPENCLAW_REPAIR_FAILED"
	if validationFailed {
		code = "OPENCLAW_REPAIR_VALIDATION_FAILED"
	}
	return map[string]any{
		"code": code, "checkpoint": checkpoint, "rolled_back": true,
	}, http.StatusUnprocessableEntity
}

type localAgentTools struct{ server *Server }

func (t *localAgentTools) Specs() []manageragent.ToolSpec {
	return []manageragent.ToolSpec{
		readTool("inspect_runtime", "读取 OpenClaw 安装与 Node.js 兼容状态"),
		readTool("inspect_gateway", "读取 Gateway 运行、健康和所有权状态"),
		readTool("run_install_preflight", "只读检查安装所需的 Node.js、npm、目录和磁盘条件"),
		readTool("run_diagnostics", "运行机器可读的 OpenClaw 只读诊断"),
		readTool("list_models", "读取已配置模型"),
		readTool("list_agents", "读取 Agent 与渠道绑定"),
		readTool("list_channels", "读取消息渠道"),
		readTool("list_cron", "读取定时任务"),
		readTool("inspect_memory", "读取记忆索引状态"),
		readTool("inspect_security", "运行只读安全审计"),
		readTool("list_skills", "读取 Skills 就绪状态"),
		readTool("list_backups", "读取可恢复配置快照"),
		writeTool("install_openclaw", "安装固定审核版本的 OpenClaw"),
		writeTool("repair_openclaw", "创建恢复点、运行修复、验证并在失败时自动回滚"),
		writeTool("start_gateway", "启动本机 OpenClaw Gateway"),
		writeTool("restart_gateway", "重启本机 OpenClaw Gateway"),
		writeTool("stop_gateway", "停止本机 OpenClaw Gateway"),
		writeTool("create_config_backup", "创建 OpenClaw 配置快照"),
		manageragent.ToolSpec{
			Definition: toolDefinition("set_default_model", "设置 OpenClaw 默认模型", map[string]any{
				"type": "object", "additionalProperties": false,
				"properties": map[string]any{"model": map[string]any{"type": "string"}},
				"required":   []string{"model"},
			}),
			RequiresApproval: true, ApprovalSummary: "修改 OpenClaw 默认模型并验证配置",
		},
		manageragent.ToolSpec{
			Definition: toolDefinition("set_plugin_enabled", "启用或禁用指定 OpenClaw 插件", map[string]any{
				"type": "object", "additionalProperties": false,
				"properties": map[string]any{
					"plugin_id": map[string]any{"type": "string"}, "enabled": map[string]any{"type": "boolean"},
				},
				"required": []string{"plugin_id", "enabled"},
			}),
			RequiresApproval: true, ApprovalSummary: "修改插件启用状态并验证配置",
		},
		manageragent.ToolSpec{
			Definition: toolDefinition("set_cron_enabled", "启用或禁用指定定时任务", map[string]any{
				"type": "object", "additionalProperties": false,
				"properties": map[string]any{
					"job_id": map[string]any{"type": "string"}, "enabled": map[string]any{"type": "boolean"},
				},
				"required": []string{"job_id", "enabled"},
			}),
			RequiresApproval: true, ApprovalSummary: "修改定时任务启用状态",
		},
		manageragent.ToolSpec{
			Definition: toolDefinition("reindex_memory", "重建指定 Agent 的记忆索引", map[string]any{
				"type": "object", "additionalProperties": false,
				"properties": map[string]any{
					"agent_id": map[string]any{"type": "string"}, "force": map[string]any{"type": "boolean"},
				},
			}),
			RequiresApproval: true, ApprovalSummary: "重建本机记忆索引",
		},
		manageragent.ToolSpec{
			Definition: toolDefinition("restore_config_backup", "恢复指定 OpenClaw 配置快照", map[string]any{
				"type": "object", "additionalProperties": false,
				"properties": map[string]any{"backup_id": map[string]any{"type": "string"}},
				"required":   []string{"backup_id"},
			}),
			RequiresApproval: true, ApprovalSummary: "恢复配置会替换当前 OpenClaw 配置",
		},
	}
}

func (t *localAgentTools) Execute(ctx context.Context, name string, args json.RawMessage) (string, error) {
	emptyArgs := func() error {
		var body map[string]any
		if err := json.Unmarshal(args, &body); err != nil || len(body) != 0 {
			return errors.New("工具不接受参数")
		}
		return nil
	}
	marshal := func(value any) (string, error) {
		encoded, err := json.Marshal(value)
		return string(encoded), err
	}
	switch name {
	case "inspect_runtime":
		if err := emptyArgs(); err != nil {
			return "", err
		}
		status := t.server.adapter.Discover(ctx)
		return marshal(map[string]any{"state": status.State, "version": status.Version, "node_version": status.NodeVersion, "node_compatible": status.NodeOK, "message": status.Message})
	case "inspect_gateway":
		if err := emptyArgs(); err != nil {
			return "", err
		}
		status := t.server.adapter.GatewayStatus(ctx)
		return marshal(map[string]any{"state": status.State, "health": status.Health, "installed": status.Installed, "running": status.Running, "managed": status.Managed, "version": status.Version, "message": status.Message, "error_code": status.ErrorCode})
	case "run_install_preflight":
		if err := emptyArgs(); err != nil {
			return "", err
		}
		report, err := t.server.adapter.InstallPreflight(ctx)
		result, _ := marshal(report)
		return result, err
	case "run_diagnostics":
		return t.inventory(ctx, args, "diagnostics")
	case "list_models":
		return t.inventory(ctx, args, "models")
	case "list_agents":
		return t.inventory(ctx, args, "agents")
	case "list_channels":
		return t.inventory(ctx, args, "channels")
	case "list_cron":
		return t.inventory(ctx, args, "cron")
	case "inspect_memory":
		return t.inventory(ctx, args, "memory")
	case "inspect_security":
		return t.inventory(ctx, args, "security")
	case "list_skills":
		return t.inventory(ctx, args, "skills")
	case "list_backups":
		if err := emptyArgs(); err != nil {
			return "", err
		}
		if t.server.configBackups == nil {
			return "", errors.New("配置备份不可用")
		}
		items, err := t.server.configBackups.List()
		if err != nil {
			return "", err
		}
		return marshal(map[string]any{"backups": items})
	case "install_openclaw":
		if err := emptyArgs(); err != nil {
			return "", err
		}
		_, err := t.server.adapter.InstallNative(ctx)
		if err != nil {
			return "", err
		}
		return `{"installed":true,"package":"openclaw@2026.7.1-2"}`, nil
	case "repair_openclaw":
		if err := emptyArgs(); err != nil {
			return "", err
		}
		result, status := t.server.runRepairTransaction(ctx)
		encoded, _ := marshal(result)
		if status != http.StatusOK {
			return encoded, errors.New("修复未成功，系统已按结果执行回滚")
		}
		return encoded, nil
	case "start_gateway", "restart_gateway", "stop_gateway":
		if err := emptyArgs(); err != nil {
			return "", err
		}
		action := strings.TrimSuffix(name, "_gateway")
		status, err := t.server.adapter.GatewayControl(ctx, action, true)
		result, _ := marshal(status)
		return result, err
	case "create_config_backup":
		if err := emptyArgs(); err != nil {
			return "", err
		}
		if t.server.configBackups == nil {
			return "", errors.New("配置备份不可用")
		}
		t.server.configMu.Lock()
		backup, err := t.server.configBackups.Backup()
		t.server.configMu.Unlock()
		result, _ := marshal(backup)
		return result, err
	case "set_default_model":
		var body struct {
			Model string `json:"model"`
		}
		if err := decodeToolArgs(args, &body); err != nil || body.Model == "" {
			return "", errors.New("模型 ID 无效")
		}
		result, status := t.server.runConfigMutation(ctx, "models", "set_default", func() error {
			return t.server.adapter.SetDefaultModel(ctx, body.Model)
		}, map[string]any{"model": strings.TrimSpace(body.Model)})
		encoded, _ := marshal(result)
		if status != http.StatusOK {
			return encoded, errors.New("默认模型修改失败并已按结果执行回滚")
		}
		return encoded, nil
	case "set_plugin_enabled":
		var body struct {
			PluginID string `json:"plugin_id"`
			Enabled  *bool  `json:"enabled"`
		}
		if err := decodeToolArgs(args, &body); err != nil || body.PluginID == "" || body.Enabled == nil {
			return "", errors.New("插件参数无效")
		}
		result, status := t.server.runConfigMutation(ctx, "plugins", "set_enabled", func() error {
			return t.server.adapter.SetPluginEnabled(ctx, body.PluginID, *body.Enabled)
		}, map[string]any{"plugin_id": strings.TrimSpace(body.PluginID), "enabled": *body.Enabled})
		encoded, _ := marshal(result)
		if status != http.StatusOK {
			return encoded, errors.New("插件状态修改失败并已按结果执行回滚")
		}
		return encoded, nil
	case "set_cron_enabled":
		var body struct {
			JobID   string `json:"job_id"`
			Enabled *bool  `json:"enabled"`
		}
		if err := decodeToolArgs(args, &body); err != nil || body.JobID == "" || body.Enabled == nil {
			return "", errors.New("定时任务参数无效")
		}
		err := t.server.runDirectMutation(func() error {
			return t.server.adapter.SetCronEnabled(ctx, body.JobID, *body.Enabled)
		})
		result, status := directManagementResult("cron", "set_enabled", err, map[string]any{
			"job_id": strings.TrimSpace(body.JobID), "enabled": *body.Enabled,
		})
		encoded, _ := marshal(result)
		if status != http.StatusOK {
			return encoded, errors.New("定时任务状态修改失败")
		}
		return encoded, nil
	case "reindex_memory":
		var body struct {
			AgentID string `json:"agent_id"`
			Force   bool   `json:"force"`
		}
		if err := decodeToolArgs(args, &body); err != nil {
			return "", errors.New("记忆索引参数无效")
		}
		err := t.server.runDirectMutation(func() error {
			return t.server.adapter.ReindexMemory(ctx, body.AgentID, body.Force)
		})
		result, status := directManagementResult("memory", "reindex", err, map[string]any{
			"agent_id": strings.TrimSpace(body.AgentID), "force": body.Force,
		})
		encoded, _ := marshal(result)
		if status != http.StatusOK {
			return encoded, errors.New("记忆索引重建失败")
		}
		return encoded, nil
	case "restore_config_backup":
		if t.server.configBackups == nil {
			return "", errors.New("配置备份不可用")
		}
		var body struct {
			BackupID string `json:"backup_id"`
		}
		decoder := json.NewDecoder(strings.NewReader(string(args)))
		decoder.DisallowUnknownFields()
		if err := decoder.Decode(&body); err != nil || body.BackupID == "" || !jsonEOF(decoder) {
			return "", errors.New("备份 ID 无效")
		}
		t.server.configMu.Lock()
		defer t.server.configMu.Unlock()
		result, err := t.server.configBackups.Restore(body.BackupID, func(candidatePath string) error { return t.server.adapter.ValidateConfigCandidate(ctx, candidatePath) })
		encoded, _ := marshal(result)
		return encoded, err
	default:
		return "", errors.New("工具不存在或未授权")
	}
}

func decodeToolArgs(args json.RawMessage, target any) error {
	decoder := json.NewDecoder(strings.NewReader(string(args)))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil || !jsonEOF(decoder) {
		return errors.New("工具参数无效")
	}
	return nil
}

func (t *localAgentTools) inventory(ctx context.Context, args json.RawMessage, kind string) (string, error) {
	var body map[string]any
	if err := json.Unmarshal(args, &body); err != nil || len(body) != 0 {
		return "", errors.New("工具不接受参数")
	}
	data, err := t.server.adapter.Inventory(ctx, kind)
	return string(data), err
}

func readTool(name, description string) manageragent.ToolSpec {
	return manageragent.ToolSpec{Definition: toolDefinition(name, description, emptyToolSchema()), ApprovalSummary: description}
}

func writeTool(name, summary string) manageragent.ToolSpec {
	return manageragent.ToolSpec{Definition: toolDefinition(name, summary, emptyToolSchema()), RequiresApproval: true, ApprovalSummary: summary}
}

func toolDefinition(name, description string, parameters map[string]any) manageragent.ToolDefinition {
	return manageragent.ToolDefinition{Type: "function", Function: manageragent.ToolFunction{Name: name, Description: description, Parameters: parameters}}
}

func emptyToolSchema() map[string]any {
	return map[string]any{"type": "object", "properties": map[string]any{}, "additionalProperties": false}
}
