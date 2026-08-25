package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"github.com/yypyyd/longhub-manager/internal/runtime"
)

type manageRequest struct {
	Resource     string `json:"resource"`
	Action       string `json:"action"`
	Confirm      bool   `json:"confirm"`
	ID           string `json:"id,omitempty"`
	Model        string `json:"model,omitempty"`
	Name         string `json:"name,omitempty"`
	Message      string `json:"message,omitempty"`
	ScheduleType string `json:"schedule_type,omitempty"`
	Schedule     string `json:"schedule,omitempty"`
	AgentID      string `json:"agent_id,omitempty"`
	Disabled     bool   `json:"disabled,omitempty"`
	Force        bool   `json:"force,omitempty"`
}

func (s *Server) handleManage(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED")
		return
	}
	var body manageRequest
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 16*1024))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&body); err != nil || !jsonEOF(decoder) {
		writeError(w, http.StatusBadRequest, "INVALID_MANAGEMENT_REQUEST")
		return
	}
	if !body.Confirm {
		writeError(w, http.StatusPreconditionRequired, "USER_CONFIRMATION_REQUIRED")
		return
	}
	result, status := s.executeManagement(r.Context(), body)
	writeJSON(w, status, result)
}

func (s *Server) executeManagement(ctx context.Context, body manageRequest) (map[string]any, int) {
	resource := strings.TrimSpace(body.Resource)
	action := strings.TrimSpace(body.Action)
	switch resource + "/" + action {
	case "models/set_default":
		return s.runConfigMutation(ctx, resource, action, func() error {
			return s.adapter.SetDefaultModel(ctx, body.Model)
		}, map[string]any{"model": strings.TrimSpace(body.Model)})
	case "plugins/enable", "plugins/disable":
		enabled := action == "enable"
		return s.runConfigMutation(ctx, resource, action, func() error {
			return s.adapter.SetPluginEnabled(ctx, body.ID, enabled)
		}, map[string]any{"plugin_id": strings.TrimSpace(body.ID), "enabled": enabled})
	case "cron/add":
		err := s.runDirectMutation(func() error {
			return s.adapter.AddCronMessage(ctx, runtime.CronMessageRequest{
				Name: body.Name, Message: body.Message, ScheduleType: body.ScheduleType,
				Schedule: body.Schedule, AgentID: body.AgentID, Disabled: body.Disabled,
			})
		})
		return directManagementResult(resource, action, err, map[string]any{
			"name": strings.TrimSpace(body.Name), "schedule_type": body.ScheduleType,
			"schedule": strings.TrimSpace(body.Schedule), "disabled": body.Disabled,
		})
	case "cron/enable", "cron/disable":
		enabled := action == "enable"
		err := s.runDirectMutation(func() error { return s.adapter.SetCronEnabled(ctx, body.ID, enabled) })
		return directManagementResult(resource, action, err, map[string]any{
			"job_id": strings.TrimSpace(body.ID), "enabled": enabled,
		})
	case "cron/remove":
		err := s.runDirectMutation(func() error { return s.adapter.RemoveCron(ctx, body.ID) })
		return directManagementResult(resource, action, err, map[string]any{"job_id": strings.TrimSpace(body.ID)})
	case "memory/reindex":
		err := s.runDirectMutation(func() error { return s.adapter.ReindexMemory(ctx, body.AgentID, body.Force) })
		return directManagementResult(resource, action, err, map[string]any{
			"agent_id": strings.TrimSpace(body.AgentID), "force": body.Force,
		})
	default:
		return map[string]any{"code": "UNSUPPORTED_MANAGEMENT_ACTION"}, http.StatusBadRequest
	}
}

func (s *Server) runDirectMutation(mutate func() error) error {
	s.managementMu.Lock()
	defer s.managementMu.Unlock()
	return mutate()
}

func (s *Server) runConfigMutation(
	ctx context.Context,
	resource string,
	action string,
	mutate func() error,
	fields map[string]any,
) (map[string]any, int) {
	if s.configBackups == nil {
		return map[string]any{"code": "CONFIG_BACKUP_NOT_CONFIGURED"}, http.StatusServiceUnavailable
	}
	s.configMu.Lock()
	defer s.configMu.Unlock()
	checkpoint, err := s.configBackups.BeginMutation()
	if err != nil {
		return map[string]any{"code": configBackupErrorCode(err)}, http.StatusUnprocessableEntity
	}
	mutationErr := mutate()
	validationFailed := false
	if mutationErr == nil {
		mutationErr = s.configBackups.ValidateActive(func(candidatePath string) error {
			return s.adapter.ValidateConfigCandidate(ctx, candidatePath)
		})
		validationFailed = mutationErr != nil
	}
	if mutationErr == nil {
		result := map[string]any{
			"resource": resource, "action": action, "changed": true,
			"validated": true, "checkpoint": checkpoint,
		}
		for key, value := range fields {
			result[key] = value
		}
		return result, http.StatusOK
	}
	rollbackCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	_, rollbackErr := s.configBackups.RollbackMutation(checkpoint, func(candidatePath string) error {
		return s.adapter.ValidateConfigCandidate(rollbackCtx, candidatePath)
	})
	if rollbackErr != nil {
		return map[string]any{
			"code": "OPENCLAW_CONFIG_MUTATION_ROLLBACK_FAILED", "checkpoint": checkpoint,
		}, http.StatusInternalServerError
	}
	code := "OPENCLAW_MANAGEMENT_FAILED"
	if validationFailed {
		code = "OPENCLAW_CONFIG_MUTATION_VALIDATION_FAILED"
	}
	return map[string]any{
		"code": code, "resource": resource, "action": action,
		"checkpoint": checkpoint, "rolled_back": true,
	}, http.StatusUnprocessableEntity
}

func directManagementResult(resource, action string, err error, fields map[string]any) (map[string]any, int) {
	if err != nil {
		return map[string]any{
			"code": "OPENCLAW_MANAGEMENT_FAILED", "resource": resource, "action": action,
		}, http.StatusUnprocessableEntity
	}
	result := map[string]any{"resource": resource, "action": action, "changed": true}
	for key, value := range fields {
		result[key] = value
	}
	return result, http.StatusOK
}
