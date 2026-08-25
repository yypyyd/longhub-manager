package runtime

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"unicode/utf8"
)

var managementIDPattern = regexp.MustCompile(`^(?:[A-Za-z0-9]|@[A-Za-z0-9])[A-Za-z0-9._:/@+-]{0,198}$`)

type CronMessageRequest struct {
	Name         string
	Message      string
	ScheduleType string
	Schedule     string
	AgentID      string
	Disabled     bool
}

// SetDefaultModel changes only the documented OpenClaw default model. The
// caller owns the surrounding config checkpoint, validation and rollback.
func (a *NativeAdapter) SetDefaultModel(ctx context.Context, model string) error {
	model = strings.TrimSpace(model)
	if err := validateManagementID("模型 ID", model, false); err != nil {
		return err
	}
	return a.runReviewedMutation(ctx, "models", "set", model)
}

// SetPluginEnabled toggles one documented plugin id without accepting command
// fragments or additional flags from the page/model.
func (a *NativeAdapter) SetPluginEnabled(ctx context.Context, pluginID string, enabled bool) error {
	pluginID = strings.TrimSpace(pluginID)
	if err := validateManagementID("插件 ID", pluginID, false); err != nil {
		return err
	}
	action := "disable"
	if enabled {
		action = "enable"
	}
	return a.runReviewedMutation(ctx, "plugins", action, pluginID)
}

func (a *NativeAdapter) AddCronMessage(ctx context.Context, request CronMessageRequest) error {
	request.Name = strings.TrimSpace(request.Name)
	request.Message = strings.TrimSpace(request.Message)
	request.Schedule = strings.TrimSpace(request.Schedule)
	request.AgentID = strings.TrimSpace(request.AgentID)
	if err := validateManagementText("任务名称", request.Name, 120, false); err != nil {
		return err
	}
	if strings.ContainsAny(request.Name, "\r\n\t") {
		return errors.New("任务名称不能包含控制字符")
	}
	if err := validateManagementText("任务消息", request.Message, 8*1024, false); err != nil {
		return err
	}
	if err := validateManagementText("调度表达式", request.Schedule, 160, false); err != nil {
		return err
	}
	if strings.ContainsAny(request.Schedule, "\r\n") {
		return errors.New("调度表达式不能换行")
	}
	if request.AgentID != "" {
		if err := validateManagementID("Agent ID", request.AgentID, true); err != nil {
			return err
		}
	}
	var scheduleFlag string
	switch request.ScheduleType {
	case "every":
		scheduleFlag = "--every"
	case "cron":
		scheduleFlag = "--cron"
	case "at":
		scheduleFlag = "--at"
	default:
		return errors.New("调度类型无效")
	}
	args := []string{
		"cron", "add", "--name=" + request.Name, "--message=" + request.Message,
		"--session=isolated", scheduleFlag + "=" + request.Schedule, "--json",
	}
	if request.AgentID != "" {
		args = append(args, "--agent="+request.AgentID)
	}
	if request.Disabled {
		args = append(args, "--disabled")
	}
	return a.runReviewedMutation(ctx, args...)
}

func (a *NativeAdapter) SetCronEnabled(ctx context.Context, jobID string, enabled bool) error {
	jobID = strings.TrimSpace(jobID)
	if err := validateManagementID("任务 ID", jobID, false); err != nil {
		return err
	}
	flag := "--disable"
	if enabled {
		flag = "--enable"
	}
	return a.runReviewedMutation(ctx, "cron", "edit", jobID, flag)
}

func (a *NativeAdapter) RemoveCron(ctx context.Context, jobID string) error {
	jobID = strings.TrimSpace(jobID)
	if err := validateManagementID("任务 ID", jobID, false); err != nil {
		return err
	}
	return a.runReviewedMutation(ctx, "cron", "rm", jobID, "--json")
}

func (a *NativeAdapter) ReindexMemory(ctx context.Context, agentID string, force bool) error {
	agentID = strings.TrimSpace(agentID)
	if agentID != "" {
		if err := validateManagementID("Agent ID", agentID, true); err != nil {
			return err
		}
	}
	args := []string{"memory", "index"}
	if agentID != "" {
		args = append(args, "--agent="+agentID)
	}
	if force {
		args = append(args, "--force")
	}
	return a.runReviewedMutation(ctx, args...)
}

func (a *NativeAdapter) runReviewedMutation(ctx context.Context, args ...string) error {
	command, err := findOpenClaw(ctx, a.runner)
	if err != nil {
		return errors.New("未发现原生 OpenClaw")
	}
	output, runErr := a.runWithTimeout(ctx, a.mutationTimeout, command, args...)
	if runErr != nil {
		return fmt.Errorf("OpenClaw 管理操作失败: %s", safeCommandError(runErr, output))
	}
	return nil
}

func validateManagementID(label, value string, allowEmpty bool) error {
	value = strings.TrimSpace(value)
	if value == "" && allowEmpty {
		return nil
	}
	if !managementIDPattern.MatchString(value) {
		return fmt.Errorf("%s 格式无效", label)
	}
	return nil
}

func validateManagementText(label, value string, maxBytes int, allowEmpty bool) error {
	if value == "" && allowEmpty {
		return nil
	}
	if value == "" || len(value) > maxBytes || !utf8.ValidString(value) || strings.ContainsRune(value, '\x00') {
		return fmt.Errorf("%s 格式无效", label)
	}
	return nil
}
