# Manager Agent

`manageragent` 是 LongHub Manager 内置管家 Agent 的执行内核。它独立于 OpenClaw Gateway 运行，因此 Gateway 无法启动时仍可通过 Manager 检查环境、解释问题，并请求执行受控修复。

## 为什么存在

普通 OpenClaw Agent 依赖 Gateway；安装、配置或 Gateway 本身损坏时，它不能修复自己的运行基础。该模块把模型对话与 Manager 已审核的本地工具连接起来，同时保留人工确认、输出限制和配置回滚边界。

## 核心职责

- 保存模型地址和模型 ID，并将 API Key 与普通配置分离。
- 调用 OpenAI Responses 或 Chat Completions 兼容接口并处理工具调用。
- 使用已保存凭据测试真实模型与工具协议，向本地页面返回脱敏后的供应商错误。
- 管理短期内存会话、工具轮次和结果大小限制；预算耗尽时基于已有结果安全收尾。
- 发出模型轮次、工具开始/完成、审批等待和答案生成等真实生命周期事件。
- 自动执行只读工具；写工具暂停并返回一次性确认请求。
- 拒绝未知工具、无效参数、模型重定向和超大响应。

该模块不负责定义 OpenClaw 命令、不运行任意 Shell、不直接修改 OpenClaw 配置，也不持久化聊天记录。允许的工具及其实现位于 `internal/httpapi`，配置备份和回滚由 `internal/configbackup` 负责。

## 依赖关系

- Go 标准库：HTTP、JSON、加密随机数和同步原语。
- Windows：API Key 存入 Windows Credential Manager。
- 非 Windows 构建：API Key 使用权限为 `0600` 的独立文件存储。
- 调用方：`cmd/longhub-manager` 创建配置和模型客户端，`internal/httpapi` 暴露本机鉴权 API 并注入工具白名单。

## 快速使用

```go
store, err := manageragent.NewConfigStore(configPath, secretStore)
if err != nil {
    return err
}
engine, err := manageragent.NewEngine(
    store,
    manageragent.NewModelClient(nil),
    reviewedToolExecutor,
)
if err != nil {
    return err
}
response, err := engine.Turn(ctx, "", "检查 Gateway 状态")
```

需要实时界面反馈时使用 `TurnWithEvents` / `ResolveApprovalWithEvents`。事件只包含
工具名称、审核过的操作摘要和成功状态，不包含模型内部 reasoning、工具原始结果或凭据。
HTTP 层通过鉴权 SSE 接口推送这些事件，并把已验证的最终回答分片送入对话气泡。

当 `response.Approval` 非空时，调用方必须把操作说明展示给用户；只有用户明确同意后才能调用 `ResolveApproval`。

## 数据边界

Manager 控制面和工具执行均在本机。使用管家 Agent 时，用户消息、系统提示、工具定义以及完成回答所需的脱敏工具结果会发送到用户配置的模型服务。API Key 不会发给页面，只作为该模型请求的 Bearer 凭据。

## 验证

```powershell
go test ./internal/manageragent
go test -race ./internal/manageragent
```

测试覆盖配置/密钥事务、Responses/Chat 协议选择与工具续转、HTTPS 限制、
重定向阻断、错误脱敏、读写工具审批、拒绝路径、会话上限、工具轮次和 UTF-8 安全截断。
