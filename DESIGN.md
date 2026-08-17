# LongHub Manager 设计

## 目标与非目标

目标是提供一个免费的、可移植的 Windows 本地 OpenClaw 管理入口。非目标是执行 Cloud Skill、保存 Cloud token、安装第三方插件，或用订阅状态限制本地 OpenClaw。

## 架构

```text
Windows UI/CLI
      |
      v
Manager HTTP API  ---->  OpenClaw discovery / Gateway / backup / diagnostics
      |
      +---->  Manager release manifest (product_surface=longhub-manager)
```

Cloud API、Cloud Plugin 和 Cloud CLI 不在 Manager 进程依赖图中。这样 Manager 的免费更新、离线本地管理和 Cloud Skill 的收费发布可以分别回滚。

源码也采用相同边界：本仓库只包含 Manager，使用 Apache-2.0；收费 Cloud API、Cloud Plugin、Cloud CLI 和 Skill 实现保留在其他仓库和发布面，不属于本仓库许可证或签名项目。Go module 路径固定为 `github.com/yypyyd/longhub-manager`，避免 Foundation 审核把公开 Manager 与商业 Cloud 单体仓库视为同一项目。

## 路由边界

本地路由只处理 OpenClaw 生命周期、Gateway、备份、诊断和 Manager 更新。所有遗留 `/api/v1/cloud*`、`/api/v1/cloud-skill*` 路由通过统一 handler 返回：

```json
{"code":"CLOUD_SKILL_MOVED_TO_PLUGIN"}
```

响应状态为 `410`，且在鉴权、读取本地凭据或执行任何旧协议前返回。旧设备/订单的历史记录由 Cloud 服务保留，不由 Manager 删除。

## 安全决策

- Manager 不拥有 Cloud 执行 bearer，不写 Cloud pairing 或 enrollment 文件。
- 更新只接受 `longhub-manager` 产品面，文件名、大小、SHA-256 与签名 manifest 必须绑定同一文件。
- 安装 staging 只复制 Manager 文件，构建中不存在 native plugin artifact staging。
- 本地备份和诊断只处理用户明确选择的本地数据，日志不得包含 Cloud token 或旧协议正文。
- 本地 HTTP API 只绑定 IPv4 loopback，使用随机 bearer，并拒绝非回环请求和非允许来源。
- Foundation Authenticode 与 LongHub Ed25519 更新签名是两套信任边界：前者证明 Windows 二进制来源，后者绑定更新 manifest 与具体安装包字节，不能互相替代。
- 正式签名请求只能来自公开 GitHub Actions 的固定 commit。版本资源生成器以 `go.mod`/`go.sum` 固定版本与校验和；安装包和主程序都必须包含一致的产品名、版本和版权元数据。
- 每次 Foundation 签名请求均需维护者人工批准。签名后重新验证 Authenticode、发布者、可信时间戳、大小、SHA-256、安装、卸载和更新流程。

## 签名方案选择

免费 Manager 使用 SignPath Foundation 为符合条件的开源项目提供的托管 OV 级 Authenticode。证书私钥保存在 Foundation 的签名基础设施中，不进入开发机、GitHub 仓库、Actions secret 或生产服务器。Windows 显示的发布者是 `SignPath Foundation`，不是 LongHub；需要 LongHub 自有发布者主体时必须迁移到单独购买并完成身份验证的商业证书。

没有使用自签名证书，因为它不能建立 Windows 公共信任；也没有把 Cloud Plugin/CLI 的 Ed25519 key 用作 Authenticode，因为 Windows 不认可该信任根。GitHub Actions 先生成明确标注的 unsigned candidate，Foundation 审核和项目配置完成后才接入正式签名步骤。

项目主页、下载页和 [Code signing policy](CODE_SIGNING_POLICY.md) 公开构建来源、维护者角色、人工审批和隐私边界。仓库与 SignPath 账户必须启用多因素认证。

## 迁移

旧 Manager 仍可启动并管理本地 OpenClaw；用户需要 Cloud Skill 时安装独立 CLI，运行 `longhub-cloud pair`，再让 CLI 安装独立插件。旧 Cloud 路由不保留长期双协议。

## 已知限制

SignPath Foundation 有权基于项目声誉、来源控制和条款审核接受或拒绝申请；公开仓库和 Apache-2.0 许可证不保证获批。当前 `0.1.2` 只是供审核的 unsigned candidate，正式 Authenticode、Foundation 项目配置和签名后 Windows E2E 尚未完成，因此不能宣称它是生产签名安装包，也不能激活生产 rollout。Cloud Plugin/CLI 已完成的 Ed25519、Portal 和 OpenClaw E2E 属于独立产品面，不能替代 Manager 门禁。

## 变更历史

### 2026-08-17 - Manager/Cloud 拆分

Manager 升级为 `0.1.1`，移除 Cloud pairing、execution credential、Bridge、enrollment 和插件 artifact，旧 Cloud 入口统一迁移错误。Manager release 固定 `product_surface=longhub-manager`，生产 `0.1.0` 候选继续暂停。

### 2026-08-17 - 独立开源与 SignPath 候选链

将 Manager 源码复制到独立的 `yypyyd/longhub-manager` 项目，采用 Apache-2.0 并加入公开签名政策、隐私说明和贡献审核规则。新增固定依赖的 Windows 版本资源生成和 GitHub Actions unsigned candidate 构建，为 SignPath Foundation 来源验证和后续 Authenticode 接入做准备；收费 Cloud 产品不进入该仓库或证书范围。
