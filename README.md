# LongHub Manager

LongHub Manager is a free, open-source Windows desktop application for
installing, operating and recovering the user's existing native OpenClaw
installation. It provides a native WebView2 workspace, an independent Manager
Agent, local Gateway lifecycle controls, configuration backup and restore,
diagnostics and signed Manager updates without requiring a LongHub account or
subscription. OpenClaw's own Control UI remains the home for models, Agents,
channels, sessions, automation, memory, usage, Skills and plugins.

LongHub Manager does not contain Cloud Skill implementations, Cloud Plugin or
CLI packages, payment or entitlement logic, cloud execution credentials,
device pairing, a Manager Bridge, or bundled OpenClaw binaries. LongHub Cloud
services are separate products and are not part of this repository or its
Apache-2.0 license.

## Code signing policy

Free code signing provided by [SignPath.io](https://about.signpath.io),
certificate by [SignPath Foundation](https://signpath.org).

See the complete [code signing policy](CODE_SIGNING_POLICY.md),
[privacy policy](PRIVACY.md), [contribution rules](CONTRIBUTING.md) and
[Apache License 2.0](LICENSE).

## Local features

- Use a focused local workspace for OpenClaw status, installation, Gateway
  service control, recovery and Manager maintenance. Open the authenticated
  OpenClaw Control UI through OpenClaw's own fixed `dashboard` command.
- Use the independent Manager Agent with a user-selected OpenAI
  Responses/Chat-Completions-compatible
  model, fixed local tools and explicit approval for every write operation;
  the Agent remains fully available whether the Gateway is running or not.
- Test the saved Manager Agent model, credential and tool-call protocol before
  use, and show bounded redacted provider errors instead of treating stored
  fields as proof that the model is available.
- Discover the system-native Node.js and OpenClaw installation.
- Run a fail-closed install preflight and install the pinned upstream OpenClaw
  package only after explicit confirmation.
- Start, stop, restart and inspect the local OpenClaw Gateway.
- Manage the current user's Gateway startup task.
- Give the Manager Agent structured, redacted access to models, Agents,
  channels, Cron, memory, usage, sessions, Skills, plugins, security and
  diagnostics through fixed OpenClaw CLI actions.
- Let the Manager Agent apply typed model, plugin, Cron and memory operations
  only after explicit approval. Manager does not duplicate these features as
  standalone pages and never accepts Shell or arbitrary CLI flags.
- Run a reviewed `openclaw doctor --fix` repair only after confirmation and a
  successful recovery checkpoint; validate the resulting config and roll back
  automatically when repair or validation fails.
- Bound every native command response and every model/tool response before it
  enters the Manager API or Agent context.
- Create and restore local OpenClaw configuration backups.
- Check, download and apply signed LongHub Manager updates on explicit request.
- Return a fixed HTTP `410 CLOUD_SKILL_MOVED_TO_PLUGIN` response from removed
  legacy Cloud Skill routes without reading credentials or changing state.

## Build and test

Prerequisites for a Windows installer build are Go 1.24 or newer and NSIS 3.

```powershell
go test ./...
go vet ./...
go build ./cmd/longhub-manager

./scripts/build-windows-release.ps1 `
  -Version 0.2.1 `
  -CloudApiBaseUrl https://154-9-26-158.sslip.io `
  -AllowUnsigned
```

`-AllowUnsigned` is only for isolated testing and the candidate submitted to
the SignPath review pipeline. Public releases must pass the repository's
Authenticode, publisher, timestamp, SHA-256, installation, uninstallation and
update gates. Signing certificates, private keys and credentials must never be
stored in the repository.

Silent NSIS installs (`/S`) do not launch Manager automatically; interactive
installs launch the native window after successful installation.

The installer stages only `LongHubManager.exe` and non-secret
`release-config.json`. It must not contain an `artifacts` directory, Cloud Skill
package, plugin archive or execution credential.

The Windows build uses the system Microsoft Edge WebView2 Runtime for the
embedded Manager window, so Manager startup does not launch the user's default
browser. The explicit **Open OpenClaw** action delegates to `openclaw
dashboard`, which opens OpenClaw's own Control UI using the browser behavior
owned by OpenClaw. The Manager WebView profile is stored under the current
user’s LongHub configuration directory and is isolated from ordinary browser
profiles.

The release executable declares per-monitor-v2 DPI awareness and initializes
the same mode before creating its tray or WebView windows. Windows therefore
renders the WebView at the monitor's native scale instead of bitmap-stretching
the complete Manager window on displays configured above 100%.

The Manager control plane and native tools run locally. When the optional
Manager Agent is used, its conversation, tool definitions and the redacted
tool results needed for an answer are sent to the model endpoint configured by
the user. The API key is stored separately from ordinary configuration and is
never returned to the WebView.

## 中文说明

LongHub Manager 是免费的开源 Windows 桌面应用，用于发现和管理用户自己机器上的
原生 OpenClaw。它提供本地 Gateway 生命周期控制、配置备份恢复、诊断和 Manager
更新；本地模型、Provider、Channels、Agent、插件、MCP、工作区和第三方 Skill 不
受 LongHub 账号或 Cloud Skill 订阅限制。

当前工作台聚焦首页、完整管家 Agent、Gateway 服务管理、OpenClaw 安装、诊断修复、配置
备份和 Manager 自身设置。模型、Agent、消息渠道、会话、记忆、定时任务、用量、Skills、
插件和 OpenClaw 安全配置继续由 OpenClaw 官方 Control UI 管理，Manager 首页通过 OpenClaw
自己的 `dashboard` 命令安全打开它，不自行拼接可能含认证信息的地址。Manager Agent 仍可
通过固定、脱敏工具检查这些状态，并在逐次确认后执行类型化操作。执行修复前必须创建恢复点；
修复后配置未通过验证时自动回滚。

Manager 控制面和本机工具只在回环地址运行。使用可选管家 Agent 时，对话、工具定义和回答
所需的脱敏工具结果会发送到用户自己配置的模型服务；API Key 与普通配置分离保存，且不会
返回 WebView。

收费 Cloud Skill、Cloud API、Cloud Plugin 和 `longhub-cloud` CLI 是独立产品，
不包含在本仓库、Manager 进程或安装包内，也不使用本仓库的 Apache-2.0 许可证。

安装脚本不会创建或暂存 `artifacts`，也不会读取旧的
`NativePluginArtifactDirectory` 或 `INCLUDE_NATIVE_PLUGIN_ARTIFACT`。正式版本必须
通过 SignPath Authenticode 和 Windows 安装验收后才能开放下载。

## Repository layout

```text
cmd/longhub-manager/   Manager entry point and embedded public assets
internal/httpapi/      Loopback-only local HTTP API and migration responses
internal/runtime/      Native OpenClaw and Gateway lifecycle integration
internal/configbackup/ Local configuration backup and restore
internal/manageragent/ Independent Manager Agent, model client and credentials
internal/managerupdate Signed update verification and recovery
installer/             Manager-only NSIS installer
scripts/               Windows build scripts
```
