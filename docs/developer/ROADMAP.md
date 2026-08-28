# PeerContext 开发路线图

本文记录尚未实现、未来可能改进的功能，供维护者和 AI coding 规划工作。它不是当前能力说明，也不是发布承诺；只有完成实现、测试和用户文档更新后，条目才能标记为 `Implemented`。

状态定义：`Planned` 表示方向已确认但尚未开始，`In Progress` 表示正在实现，`Implemented` 表示已经通过测试并进入当前文档。

## 当前优先级

| 优先级 | 项目 | 状态 | 目标 |
|---|---|---|---|
| P0 | 人类可读输出与 `--json` | Planned | 人直接使用时清楚，Skill 和脚本仍有稳定结构化输出 |
| P0 | 完整帮助入口 | Planned | 用户不查文档也能发现命令和参数 |
| P1 | 列表表格与统一格式选项 | Planned | Project、Member 和 Agent 列表更容易浏览 |
| P1 | 错误诊断改进 | Planned | 用人话区分宿主离线、Agent 离线、防火墙和发现失败 |
| P2 | Shell completion | Planned | 补全命令、子命令和静态参数，不补全邀请或敏感信息 |
| Future | SSH-like 主机直连研究 | Planned | 在不提供产品公网服务的前提下，由目标设备监听并接受已授权成员直连 |
| Future | Write 协作研究 | Planned | 在 read 模式验证价值后，单独评估安全、可审阅的远程修改流程 |

## CLI-UX-01：人类可读输出与 `--json`

### 当前问题

`peerctx` 当前所有成功和失败都只返回 JSON。它便于 Skill 与脚本调用，但在“创建 Project、复制邀请、让同事加入”的人工流程中显得生硬；回答还需要用户自行从 JSON Base64 中解码。

### 计划方向

- 普通调用默认输出简洁的人类可读文本；
- 所有公开命令支持全局 `--json`，继续返回稳定的单对象 JSON envelope；
- `peer-context` Skill 和自动化脚本统一显式传入 `--json`，不依赖终端是否为 TTY；
- stdout 只放结果，stderr 只放错误，退出码语义保持稳定；
- `ask` 的 stdin 字节和远端回答字节不能因为展示模式发生变化；
- 邀请仍只在明确创建邀请的结果中出现，不写入日志或额外诊断。

### 验收条件

1. `peerctx project create --name Demo` 直接显示 Project 已创建、完整邀请、过期时间和下一步；
2. 同一命令加 `--json` 后仍返回一个可稳定解析的 JSON 对象；
3. `peerctx ask AGENT` 默认直接显示回答，加 `--json` 后保留结构化结果；
4. 所有公开命令都有 human/JSON 双模式测试；
5. 内嵌 Skill、Skill 站点和示例全部显式使用 `--json`；
6. 非交互管道不会出现颜色、进度动画或混入结果的提示文字。

`--format pretty|json|table|ndjson|csv` 可在真实使用证明有需要后继续设计；P0 先只保证清楚的默认输出和稳定的 `--json`。

## CLI-UX-02：完整帮助入口

### 当前问题

当前 `peerctx --help` 会被当作未知命令并返回 JSON 错误，用户无法在终端中逐层发现正确用法。

### 计划方向

- 支持 `peerctx --help` 和 `peerctx help`；
- 支持 `peerctx project --help`、`peerctx agent --help` 等命令组帮助；
- 支持 `peerctx project create --help` 等具体命令帮助；
- 帮助内容使用人类可读文本，不启动后台服务、不访问网络、不读取 stdin；
- 参数错误给出简短原因、对应用法和下一步，不打印整页无关内容。

### 验收条件

```text
peerctx --help
peerctx project --help
peerctx project create --help
```

三条命令都以退出码 `0` 返回对应层级的帮助，不返回 `unknown_command`，也不创建或修改任何 PeerContext 状态。

## CLI-UX-03：列表与诊断体验

- `project list`、`project member list` 和 `agent list` 在默认模式下使用对齐、可扫读的列表；
- `--json` 始终保留完整字段，不因人类展示取舍而丢失数据；
- `service status` 汇总后台服务、Host、发现和 Agent 状态；
- 常见网络错误给出可执行的检查建议，但不建议用户配置 Relay、公网地址、证书或静态 IP。

## NET-FUTURE-01：SSH-like 用户主机直连研究

公网互联属于 LAN-first v2 之后的独立产品方向，不是 `0.2.0` 的能力、发布门禁或局域网连接失败时的自动回退。目标体验参考 SSH：提供 Agent 的设备运行本机监听服务，访问方使用已保存的可达端点直接连接；PeerContext 不负责公网发现或 NAT 打洞。PeerContext 产品不得运营或默认依赖公网协调、发现、STUN 或 Relay 服务；Project Host、成员协调和 Codex Runtime 仍运行在用户自己的设备上。只有当前 MVP 完成真实双机试点，并确认跨网络协作是主要需求后，才进入方案设计和实现。

用户不应被要求另外填写 PeerContext 公网 URL。邀请或签名后的 Agent 信息应同时携带连接端点和固定公钥，效果类似 SSH 的 `user@host` 与 Host Key，但粘贴邀请后由 CLI 自动保存和验证。像 SSH 一样，目标设备必须已经能通过公网 IP、域名、端口映射、用户 VPN 或其他自有可路由网络被访问；否则连接明确失败。

研究至少需要回答：

- Project 创建者的 Mac 如何通过公网 IPv6、PCP、NAT-PMP、UPnP 或用户自行提供的网络获得可达端点，并在无法暴露时明确失败；
- 邀请如何携带签名的 Host 端点和公钥，使成员无需访问 PeerContext 公网服务即可加入；
- 多人 Project 中，每个 Agent 提供方如何发布签名后的直连端点，使请求方直接连接目标设备，而不让 Project Host 转发请求正文和回答；
- Host 地址变化、休眠或离线后如何恢复；无法恢复时如何明确要求重新邀请，而不伪装成高可用服务；
- 请求和回答如何建立类似 SSH 的加密、双向认证通道，不能沿用当前明文 LAN 传输；
- Host 身份、成员身份、密钥轮换、撤销、防重放和流量限制如何工作；
- 对称 NAT、企业防火墙或运营商 CGNAT 导致直连失败时，如何给出清楚诊断并停止，而不是回退到中心服务；
- 如何支持用户已有的 VPN、可路由网络或自有基础设施，但不把它们变成 PeerContext 的默认托管依赖；
- 如何继续保证请求正文逐字节进入隔离 Runtime、不持久化请求和回答，并维持 read-only 边界。

进入实现前必须另写并评审新的 PRD、威胁模型和协议版本。不得部署 PeerContext 官方公网协调或 Relay 服务，不得默认依赖第三方公共打洞服务，也不得直接复用已删除的 v1 Relay/Bearer credential 方案。在新方案通过门禁前，不得在 v2 中加入公网或跨子网回退。

## WRITE-FUTURE-01：Write 协作研究

Write 属于 read-only v2 之后的独立产品方向，不是 `0.2.0` 的能力，也不能通过隐藏参数、内部接口或兼容模式开启。只有 read 试点证明跨仓库协作有价值，并且用户确实需要远程修改时，才进入方案设计和实现。

研究至少需要回答：

- 仓库提供方如何针对具体任务、仓库和有效时间作出明确授权，且能随时撤销；
- 修改如何与用户当前工作区及未提交改动隔离，避免覆盖或污染现有工作；
- 用户如何在应用修改前查看 diff、执行命令和风险范围，并明确接受或拒绝；
- 哪些文件、命令和 Git 操作可以开放，如何禁止越界读取、凭据访问和任意外部副作用；
- 超时、中断、Codex 失败或成员被移除时，如何停止执行并安全清理临时状态；
- 如何留下足够的授权与结果审计元数据，同时仍不持久化请求正文、回答、私钥、仓库路径或源码内容；
- commit、push 和 PR 是否应继续分阶段授权，而不是包含在一次笼统的 write 权限中。

进入实现前必须另写并评审新的 PRD、威胁模型、权限模型和协议版本，并补充真实仓库的破坏性测试。不得把已删除的 v1 write、审批或 worktree 实现直接接回 v2；在新方案完成全部门禁前，当前 Runtime 和公开 CLI 必须保持 read-only。

## 变更规则

实现 Roadmap 条目时必须同时更新：

1. 用户 Quickstart 与 CLI Reference；
2. `peer-context` Skill 源文件、嵌入 bundle 和 Skill 站点；
3. CLI 单元测试与端到端测试；
4. 若改变产品验收路径，再更新 PRD。

不得借体验改进重新引入 Relay、Bearer credential、write、审批、worktree、公网或跨子网连接。SSH-like 用户主机直连和 write 只能分别按 `NET-FUTURE-01`、`WRITE-FUTURE-01` 作为独立方向推进。
