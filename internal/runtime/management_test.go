package runtime

import (
	"context"
	"testing"
)

func TestReviewedManagementCommandsUseTypedArguments(t *testing.T) {
	runner := &fakeRunner{path: "openclaw"}
	adapter := NewNativeAdapter(runner)
	tests := []struct {
		name string
		run  func() error
		args []string
	}{
		{name: "default model", run: func() error { return adapter.SetDefaultModel(context.Background(), "openai/gpt-5") }, args: []string{"models", "set", "openai/gpt-5"}},
		{name: "enable plugin", run: func() error { return adapter.SetPluginEnabled(context.Background(), "memory-core", true) }, args: []string{"plugins", "enable", "memory-core"}},
		{name: "disable cron", run: func() error { return adapter.SetCronEnabled(context.Background(), "job-123", false) }, args: []string{"cron", "edit", "job-123", "--disable"}},
		{name: "remove cron", run: func() error { return adapter.RemoveCron(context.Background(), "job-123") }, args: []string{"cron", "rm", "job-123", "--json"}},
		{name: "reindex memory", run: func() error { return adapter.ReindexMemory(context.Background(), "main", true) }, args: []string{"memory", "index", "--agent=main", "--force"}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if err := test.run(); err != nil {
				t.Fatal(err)
			}
			if !slicesEqual(runner.lastArgs, test.args) {
				t.Fatalf("unexpected args: %v", runner.lastArgs)
			}
		})
	}
}

func TestAddCronMessageUsesOnlyReviewedAgentPayload(t *testing.T) {
	runner := &fakeRunner{path: "openclaw"}
	adapter := NewNativeAdapter(runner)
	err := adapter.AddCronMessage(context.Background(), CronMessageRequest{
		Name: "daily-check", Message: "检查 Gateway 状态", ScheduleType: "cron",
		Schedule: "0 8 * * *", AgentID: "main", Disabled: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	want := []string{
		"cron", "add", "--name=daily-check", "--message=检查 Gateway 状态",
		"--session=isolated", "--cron=0 8 * * *", "--json", "--agent=main", "--disabled",
	}
	if !slicesEqual(runner.lastArgs, want) {
		t.Fatalf("unexpected cron args: %v", runner.lastArgs)
	}
}

func TestReviewedManagementRejectsCommandShapedIDs(t *testing.T) {
	runner := &fakeRunner{path: "openclaw"}
	adapter := NewNativeAdapter(runner)
	for _, invalid := range []string{"", "--help", "model id", "model\x00id", "../../model"} {
		if err := adapter.SetDefaultModel(context.Background(), invalid); err == nil {
			t.Fatalf("invalid model id accepted: %q", invalid)
		}
	}
	if runner.lastName != "" {
		t.Fatal("invalid management input reached the command runner")
	}
}
