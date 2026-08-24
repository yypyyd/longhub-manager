# LongHub Manager

LongHub Manager is a free, open-source Windows desktop application for
discovering and managing the user's existing native OpenClaw installation. It
provides a native WebView2 workspace (not a browser redirect), local Gateway
lifecycle controls, configuration backup and restore, diagnostics and signed
Manager updates without requiring a LongHub account or subscription.

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

- Discover the system-native Node.js and OpenClaw installation.
- Install OpenClaw through the upstream package when the user confirms.
- Start, stop, restart and inspect the local OpenClaw Gateway.
- Manage the current user's Gateway startup task.
- Show the native Skills inventory as a searchable, filterable readiness list
  with wrapped descriptions and source metadata.
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

The installer stages only `LongHubManager.exe` and non-secret
`release-config.json`. It must not contain an `artifacts` directory, Cloud Skill
package, plugin archive or execution credential.

The Windows build uses the system Microsoft Edge WebView2 Runtime for the
embedded Manager window. It does not launch the user's default browser. The
runtime profile is stored under the current user's LongHub configuration
directory and is isolated from ordinary browser profiles.

## 中文说明

LongHub Manager 是免费的开源 Windows 桌面应用，用于发现和管理用户自己机器上的
原生 OpenClaw。它提供本地 Gateway 生命周期控制、配置备份恢复、诊断和 Manager
更新；本地模型、Provider、Channels、Agent、插件、MCP、工作区和第三方 Skill 不
受 LongHub 账号或 Cloud Skill 订阅限制。

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
internal/managerupdate Signed update verification and recovery
installer/             Manager-only NSIS installer
scripts/               Windows build scripts
```
