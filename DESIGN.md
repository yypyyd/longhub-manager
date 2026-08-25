# LongHub Manager 设计

## 目标与非目标

目标是提供一个免费的、可移植的 Windows 本地 OpenClaw 管理入口。非目标是执行
Cloud Skill、保存 Cloud token、安装第三方插件，或用订阅状态限制本地 OpenClaw。

## 架构

```text
Windows EXE
      |
      +----> WebView2 native window (embedded Manager workspace)
      |
      v
Manager HTTP API  ---->  OpenClaw discovery / Gateway / backup / diagnostics
      |
      +---->  Manager Agent ----> user-configured model endpoint
      |
      +---->  Manager release manifest (product_surface=longhub-manager)
```

Windows 入口使用系统 Microsoft Edge WebView2 将带 token 的本地页面加载到应用窗口；
发布程序通过 manifest 和启动时初始化声明 Per-Monitor V2 DPI 感知，避免 Windows
在 125%/150% 等显示缩放下对整个 WebView 做位图拉伸。
托盘菜单、重复启动唤醒和窗口聚焦都复用该窗口，不再调用 `ShellExecuteW` 或默认浏览器。
WebView2 缓存放在 LongHub 专用配置目录，关闭窗口后保留进程和托盘服务，退出菜单才会结束
Manager。唯一的外部浏览器入口是用户主动点击“打开 OpenClaw”后执行的官方
`openclaw dashboard`；浏览器行为和认证 URL 均由 OpenClaw 所有。

Cloud API、Cloud Plugin 和 Cloud CLI 不在 Manager 进程依赖图中。这样 Manager 的免费
更新、离线本地管理和 Cloud Skill 的收费发布可以分别回滚。

源码也采用相同边界：本仓库只包含 Manager，使用 Apache-2.0；收费 Cloud API、Cloud
Plugin、Cloud CLI 和 Skill 实现保留在其他仓库和发布面，不属于本仓库许可证或签名项目。
Go module 路径固定为 `github.com/yypyyd/longhub-manager`。

## 路由边界

本地路由只处理 OpenClaw 生命周期、Gateway、备份、诊断和 Manager 更新。所有遗留 `/api/v1/cloud*`、`/api/v1/cloud-skill*` 路由通过统一 handler 返回：

```json
{"code":"CLOUD_SKILL_MOVED_TO_PLUGIN"}
```

响应状态为 `410`，且在鉴权、读取本地凭据或执行任何旧协议前返回。旧设备/订单的历史记录由 Cloud 服务保留，不由 Manager 删除。

本地 inventory 路由只允许代码中固定的机器可读 OpenClaw 命令，响应在返回页面或进入管家
Agent 前递归清除密钥字段、凭据格式和用户主目录。所有命令输出上限为 512 KiB；超过上限
会终止为错误，不能把无限 CLI 输出保存在内存。

`/api/v1/manage` 只接受严格的类型化 JSON，可设置默认模型、切换已知插件、管理 Agent 消息
Cron 任务和重建记忆索引。模型/插件配置修改复用恢复点、官方验证和失败回滚事务；Cron 只允许
消息任务，不暴露 OpenClaw 的 `--command`、环境变量、工作目录或任意附加参数。所有动作都要求
页面或管家 Agent 显式确认并由服务端再次验证 ID、长度、调度类型和 JSON 字段。这些能力不再
作为独立的日常管理页面展示；OpenClaw 正常管理入口是其官方 Control UI。

Manager 首页只展示安装、Gateway 和恢复相关摘要。点击“打开 OpenClaw 控制台”时，服务端仅
执行固定的 `openclaw dashboard`，由 OpenClaw 自己解析端口、认证和浏览器跳转；Manager 不
拼接、不返回也不记录可能包含短期认证 Token 的控制台 URL。

管家 Agent 独立于 Gateway，由 Manager 启动时注入固定工具。只读工具可自动调用；安装、
Gateway 生命周期、修复和配置恢复等写工具会返回一次性审批状态，必须由用户在页面确认。
模型地址只能是 HTTPS 或本机回环 HTTP，API Key 在 Windows Credential Manager 中保存，
不会写入普通 JSON 配置或返回 WebView。Agent 对话和回答所需的脱敏工具结果会发送到用户
选择的模型服务，因此该服务是显式的外部数据边界，不属于 LongHub Cloud。
保存配置不等于模型可用；页面会用同一凭据、模型和工具定义做无副作用连接测试。
供应商失败信息经 API Key、凭据样式和用户目录脱敏并限长后才返回 WebView。
模型客户端同时支持 Responses 和 Chat Completions；自动模式对 `/codex/v1` 类地址选择
Responses，也允许用户在页面显式覆盖。

修复流程使用配置事务锁：记录配置原先存在/缺失状态，存在时先创建完整性快照，再运行固定
`openclaw doctor --fix --non-interactive`，随后用临时候选文件调用 OpenClaw 官方配置校验。
修复或校验失败时使用脱离请求取消的短期上下文恢复快照；原来没有配置时则删除修复生成的
普通文件，恢复到原先缺失状态。

## 安全决策

- Manager 不拥有 Cloud 执行 bearer，不写 Cloud pairing 或 enrollment 文件。
- 更新只接受 `longhub-manager` 产品面，文件名、大小、SHA-256 与签名 manifest 必须绑定同一文件。
- 安装 staging 只复制 Manager 文件，构建中不存在 native plugin artifact staging。
- 本地备份和诊断只处理用户明确选择的本地数据，日志不得包含 Cloud token 或旧协议正文。
- 本地 HTTP API 只绑定 IPv4 loopback，使用随机 bearer，并拒绝非回环请求和非允许来源。
- HTTP 响应使用 CSP、禁止嵌入、禁用 MIME 嗅探并限制请求头、读取和写入超时。
- 管家 Agent 拒绝模型重定向、未知工具和非对象参数；限制会话、消息、工具轮次、模型响应与
  工具结果大小。写工具逐项确认，用户拒绝时不执行。
- Manager 控制面在本机，但用户启用管家 Agent 后，对话和脱敏工具结果会发送到用户配置的
  模型服务；产品文案和模块文档必须明确这一边界。
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

SignPath Foundation 有权基于项目声誉、来源控制和条款审核接受或拒绝申请；公开仓库和 Apache-2.0
许可证不保证获批。当前 `0.2.0` 只是通过本机安装/卸载验收的 unsigned candidate，正式
Authenticode、Foundation 项目配置和签名后 Windows E2E 尚未完成，因此不能宣称它是生产签名安装包，
也不能激活生产 rollout。Cloud Plugin/CLI 已完成的 Ed25519、Portal 和 OpenClaw E2E 属于独立产品面，
不能替代 Manager 门禁。

## 变更历史

### 2026-08-25 - 管家模型连接校验

管家模型配置从“保存即显示已配置”改为“已保存”与“连接正常”分层状态。
保存后立即用真实工具协议测试模型，连接与正常对话错误都返回限长、脱敏的供应商原因，
使用户能区分无模型通道权限、凭据错误、协议不兼容和网络问题。
后续实机对比确认 `/codex/v1/responses` 可用而误调的 `/chat/completions` 返回 403，
因此补充 Responses 工具协议、推理状态续转和显式协议选择。

### 2026-08-25 - Windows 高 DPI 清晰渲染

发布程序增加 Per-Monitor V2 DPI manifest，并在任何托盘或 WebView2 窗口创建前
初始化相同的 DPI 感知模式。这会让 WebView2 按显示器原生比例重新排版和渲染，
修复 Windows 125%/150% 缩放下整个 Manager 窗口被位图放大导致的文字与图标发虚。

### 2026-08-25 - 聚焦安装、服务与恢复边界

保留完整管家 Agent 和现有 Gateway 服务管理；移除 Manager 中重复的模型、Agent、渠道、
会话、安全、记忆、定时任务、用量、Skills 和插件独立页面。首页改为状态与维护入口，并通过
OpenClaw 固定 `dashboard` 命令进入官方 Control UI。结构化读取和类型化操作仍作为管家 Agent
的受控工具存在，不削弱 Agent 能力。

### 2026-08-25 - Manager 自更新与 OpenClaw 安装分层

“OpenClaw 安装与更新”页面只负责外部 OpenClaw 运行核心的预检、安装和维护；LongHub
Manager 自身的版本检查、下载和安装入口迁入“关于 LongHub Manager”。后端更新验证与恢复
机制保持不变，只调整产品信息架构，避免把管理工具自身和被管理对象呈现为同一产品。

### 2026-08-25 - 完整 OpenClaw 控制台与可恢复修复

Manager 工作台升级为仪表盘、独立管家 Agent、服务、模型、Agent、渠道、会话、记忆、
定时任务、用量、Skills、插件、安全和系统分区组成的完整控制台。全部状态页接通固定的
OpenClaw JSON 命令，并对响应做大小限制和脱敏。管家 Agent 使用独立模型配置、系统凭据、
内存会话、固定工具和写操作确认。修复动作升级为恢复点、固定命令、官方配置验证和失败
自动回滚组成的完整事务，不使用模拟数据填充页面。

### 2026-08-17 - Skills 结构化展示

Manager 后端将 `openclaw skills list` 的终端表格解析为稳定的状态、名称、说明和来源字段，
合并 CLI 为适配终端宽度生成的描述续行，再通过本地 API 返回结构化 JSON。Skills 页面使用
普通表格排版并提供名称、说明、来源搜索以及状态、来源筛选，不再向 WebView 返回或渲染
ASCII 表格原文。解析器覆盖中英文、emoji、ANSI 控制序列和异常输出，接口测试同时约束原始
CLI 输出不得出现在 Skills 响应中。

### 2026-08-17 - 隐藏后台 CLI 控制台

刷新总览时会调用 `openclaw`、`npm` 和 Windows 系统探测命令。Windows 子进程统一设置
`HideWindow` 与 `CREATE_NO_WINDOW`，包括 `.cmd` shim，避免探测过程创建可见的
`openclaw` 终端窗口；命令输出仍然由 Manager 在后台读取。

### 2026-08-17 - WebView2 桌面窗口

Manager 从“后台 HTTP 服务 + 默认浏览器”入口升级为 Windows 原生 WebView2 窗口，保留
loopback API、托盘和单实例唤醒；页面改为参考 ClawPanel 信息架构的本地管理工作台，
不复制其品牌或实现。旧版进程占用默认端口时，新版交互实例自动使用新的回环端口，
避免升级后再次打开旧浏览器页。

### 2026-08-17 - Manager/Cloud 拆分

Manager 升级为 `0.1.1`，移除 Cloud pairing、execution credential、Bridge、enrollment 和插件 artifact，旧 Cloud 入口统一迁移错误。Manager release 固定 `product_surface=longhub-manager`，生产 `0.1.0` 候选继续暂停。

### 2026-08-17 - 独立开源与 SignPath 候选链

将 Manager 源码复制到独立的 `yypyyd/longhub-manager` 项目，采用 Apache-2.0 并加入公开签名政策、隐私说明和贡献审核规则。新增固定依赖的 Windows 版本资源生成和 GitHub Actions unsigned candidate 构建，为 SignPath Foundation 来源验证和后续 Authenticode 接入做准备；收费 Cloud 产品不进入该仓库或证书范围。
