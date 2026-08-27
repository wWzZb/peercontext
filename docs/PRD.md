# PeerContext LAN-first MVP 产品需求文档

## 文档信息

| 项目 | 内容 |
|---|---|
| 状态 | **2.0 Draft — 产品方向重置，尚未发布** |
| 版本 | 2.0.0 |
| 日期 | 2026-08-27 |
| 产品版本目标 | `peerctx 0.2.0` / protocol `v2` |
| 首发平台 | macOS Apple Silicon |
| Runtime | `isolated_runtime` |

### 变更记录

| 版本 | 日期 | 说明 |
|---|---|---|
| 1.0.0 | 2026-08-25 | 以独立自托管 Relay 为前置条件的初版 MVP |
| 2.0.0 Draft | 2026-08-27 | 重置为 LAN-first：创建 Project、发送邀请、同事加入即可使用；移除公网 Relay 与 write |

> `0.1.1` 是旧方向的实现，不能作为本 PRD 的验收基线。v2 不迁移 v1 Project 或 credential，也不会删除它们。

## 1. 执行摘要

PeerContext v2 是给同一局域网内、使用 Codex 且代码分散在不同私有仓库中的开发者使用的协作工具。开发者创建 Project 后，PeerContext 自动在其 Mac 上托管 Project 并返回一段完整邀请；同事粘贴邀请即可加入，显式共享本地仓库为 Agent，Project 成员随后可以发起只读查询。用户不需要部署服务器、填写 Relay URL、配置端口或证书，也不需要维护常驻终端。

MVP 的首要目标不是证明路由基础设施能运行，而是验证两个第一次使用的开发者能否在已经安装 PeerContext、登录 Codex 且处于同一局域网的前提下，5 分钟内完成创建、加入、共享 Agent 和第一次成功 read。仓库权限不扩散；请求正文逐字节进入提供方的隔离 Codex Runtime；Project Host 不持久化请求正文或回答。

## 2. 问题定义

### 谁遇到这个问题

- 2–10 人研发协作组中的开发者，彼此认识并处于同一公司或家庭局域网；
- 正在本地使用 Codex，但需要另一位开发者私有仓库中的 API、业务规则、配置或故障事实；
- 愿意共享“由本机 Codex 代查”的能力，但不愿分发仓库或凭证。

### 真正的问题

跨仓库事实缺失会中断当前任务。手工复制代码、找人解释、猜测实现或直接授予仓库权限都会带来效率或权限问题。

PeerContext `0.1.1` 虽然验证了隔离 Runtime 和跨仓库链路，却要求用户先部署 Relay、准备可访问 URL、配置 TLS、分别启动 `relay serve` 与 `agent serve`。这把基础设施运维变成了产品入口，偏离了首批 2–10 人熟人协作组的真实场景。

### 为什么痛

- 请求方只是想问另一个仓库，却必须先理解 Relay、TLS、WSS 和在线进程；
- 提供方共享一个仓库前还要维护 Agent 常驻终端和逐人 ACL；
- 即使双方就在同一局域网，首次协作仍可能花费超过解决问题本身的时间；
- 文档中的“15 分钟完成部署”掩盖了产品没有做到即用。

### 证据

- `0.1.1` Quickstart 明确要求一个双方可访问的 Relay URL 和 TLS；
- 2026-08-27 的产品复盘中，目标用户明确否定了这一前置条件，并要求“创建 Project、发送邀请、同事加入”；
- Runtime Spike 已证明 macOS arm64 上隔离 Codex read 可用，网络与入门体验成为当前最大风险；
- 🔶 **假设：** 同一局域网、熟人协作和明文内容传输可以代表首批试点环境，仍需真实双人试点验证。

## 3. 目标用户与任务

### 主要 Persona：创建 Project 的开发者

- 不负责部署团队基础设施；
- 希望用一条命令创建临时或长期协作空间；
- 能通过现有聊天工具把一段邀请发给同事；
- 接受自己的电脑在线时 Project 才可用。

核心任务：

> 我和同事已经在同一个局域网。我希望创建 Project、把邀请发给他，然后直接开始跨仓库查询，不配置服务器或网络。

### 次要 Persona：加入并共享仓库的同事

- 粘贴邀请加入 Project；
- 明确选择一个本地 Git 仓库注册为 Agent；
- 希望 Agent 自动保持在线，同时 Codex 只能读取该仓库。

核心任务：

> 我希望加入同事的 Project，选择一个仓库共享 read 能力，然后回到正常开发，不维护后台终端。

### 请求端 Codex

只有在用户显式启用 `peer-context` Skill 后，才发现 Agent、组织最小必要问题并调用公开 CLI。Skill 不创建 Project、不加入 Project、不管理后台服务，也不参与提供端入站 Runtime。

## 4. 战略背景与产品原则

### 业务目标

1. 验证“另一个私有仓库自己的 Codex 代查事实”能否减少跨仓库阻塞；
2. 把首次价值时间从配置基础设施缩短到 5 分钟内；
3. 在不共享仓库权限、不复制知识库的情况下保持明确安全边界。

### 产品原则

- **Project-first：** 用户创建的是 Project，不是 Relay 部署；
- **局域网默认：** MVP 只解决同一局域网，不把公网部署作为高级选项偷偷带回；
- **安全细节自动化：** 自动生成身份、签名和后台服务，不要求证书或长期 token；
- **显式共享仓库：** 加入 Project 不读取任何仓库，只有 `agent register` 才共享指定路径；
- **Project 即 read 信任域：** 已共享 Agent 默认可被全部 Project 成员读取；
- **CLI/Skill 分层：** CLI 只做基础设施，Skill 只在请求端解释何时和如何协作；
- **没有写能力：** v2 MVP 不提供 write、worktree、commit、merge、push 或 PR。

## 5. 方案总览

### 5.1 核心用户流程

```text
创建者 Mac                                      同事 Mac

peerctx project create --name X
  │ 自动启动用户级后台服务
  │ 自动托管 Project
  └─ 返回 peerctx2_... 完整邀请 ───────────────► peerctx project join INVITE
                                                   │
                                                   ├─ 生成 Project 设备密钥
                                                   ├─ 加入并保存 Project
                                                   └─ peerctx agent register /repo
                                                              │ 自动上线

peerctx ask colleague/backend ◄──────── Project Host ─────────┘
          │
          └─ 回答来自同事 Mac 上的 isolated Codex Runtime
```

### 5.2 用户级后台服务

同一个 `peerctx` 二进制通过 macOS LaunchAgent 在用户登录后运行一个后台服务。它：

- 托管本机创建的多个 Project；
- 保存 Project、Member、Invite、Agent 和无正文审计元数据；
- 监听局域网连接并发布一个不含 Project 信息的 `_peerctx._tcp` 服务；
- 维持本机 Agent 到对应 Project Host 的签名 WebSocket；
- 通过权限为 `0600` 的 Unix socket 接受本机 CLI 控制；
- 即使本机只加入他人 Project，也可维持统一监听与广播；只有本机实际托管的 Project 才能通过 Host 公钥身份探测。

创建者离线、退出登录、电脑休眠或网络隔离时，Project 立即不可用；不排队，不接力，不选主。

### 5.3 邀请与加入

完整邀请格式为 `peerctx2_` 加 Base64URL 编码的 v2 JSON，至少包含 Project ID、名称、协议版本、当前局域网端点、Project Host 公钥、单次邀请 ID、邀请签名私钥和过期时间。

Host 只保存邀请公钥。加入方生成独立 Ed25519 Project 密钥，使用邀请私钥签名加入请求；签名绑定邀请、成员名、新成员公钥、nonce 和时间。邀请默认 10 分钟过期，只能成功消费一次。

### 5.4 连接与身份安全

MVP 按产品决策使用局域网明文 HTTP/WS，不配置 TLS。所有控制请求、响应和 WS 消息必须签名：

- 每个 Project Member 使用独立 Ed25519 密钥；私钥只存 macOS Keychain；
- 签名绑定协议、Project、成员、方法/消息类型、路径、原始 body hash、nonce 和时间；
- Host 校验成员、公钥、时间窗口和近期 nonce，拒绝篡改和重放；
- Host 响应与 Host/Agent WS 帧同样签名；
- 网络上不存在可复用的长期 Bearer token；
- mDNS 结果不受信任，客户端必须用已保存的 Host 公钥验证端点身份。

Host 只接受来自当前直接连接局域网网段的远端地址。邀请中的地址优先；失效后客户端浏览 `_peerctx._tcp` 并逐个验证 Host。mDNS 被禁用且地址已变化时返回明确诊断，不退回公网服务。

⚠️ **明确残余风险：** 签名只保护身份、完整性和防重放，不提供内容保密。同一局域网中能观察流量的设备可能看到请求和回答。邀请必须通过双方认可的渠道发送。

### 5.5 Agent 与 read

- `peerctx agent register REPOSITORY` 只记录绝对路径，不读取或解释仓库；
- Agent 默认名称为 `<member>/<repository-basename>`，summary/tags/capabilities 可选；
- Agent 注册后由后台服务自动上线并断线重连；
- 所有 Project 成员默认可以 read 已共享 Agent，不设逐人 ACL；
- Member 被移除或 Agent 被删除时，新请求立即拒绝；
- stdin 原始字节不增加前后缀、不扫描、不脱敏，直接进入提供方 isolated Runtime；
- 最终 Codex Agent message 原样返回，请求正文与回答不落盘。

### 5.6 CLI 公共契约

```text
peerctx project create --name NAME [--member NAME]
peerctx project join INVITATION [--member NAME]
peerctx project list|use
peerctx project invite create
peerctx project member list|remove

peerctx agent register REPOSITORY [--name NAME] [--summary TEXT]
                       [--tags CSV] [--capabilities CSV]
peerctx agent list|get|remove

peerctx ask AGENT [--timeout 5m] [--request-id ID]
peerctx service start|stop|restart|status
peerctx skills list|read
peerctx version
```

默认仍输出单个 JSON envelope。移除 `relay`、`credential`、`agent serve/access`、`task`、write 审批、worktree、`--relay`、`--credential-file`、`--invite-token` 和 `--mode`。成员显示名由 `--member` 覆盖；否则优先使用 Git `user.name`，再使用 macOS 用户名。

## 6. 成功指标

### 主要指标

- 在双方已安装 PeerContext、登录 Codex 且处于同一局域网的前提下，至少 8/10 组第一次使用的两人组合在 5 分钟内完成 Project 创建、邀请加入、Agent 注册和首个成功 read；
- 过程中手动填写 Relay URL、端口、证书、静态 IP 或保持终端运行的次数为 0；允许一次 macOS 原生网络权限确认。

### 次要指标

- Agent 注册后 10 秒内显示 online；
- Host IP 变化且 mDNS 可用时 15 秒内恢复连接；
- 已加入、Agent 在线的 read 成功率 ≥ 95%；
- 成功 read 的 P50 完成时间 ≤ 120 秒；
- 10 类 read 试点 Case 至少 8 类在一次请求或一次针对性澄清内得到可用结果。

🔶 **假设：** 以上目标尚无真实 LAN v2 基线，旧版 fake Codex 10/10 不能作为证明。

### 安全护栏

- 未授权或已移除成员成功 read 数为 0；
- 长期私钥或可重放身份 token 出现在网络、数据库或日志中的次数为 0；
- 请求正文或回答进入 Project 数据库或日志的次数为 0；
- Codex 读取授权仓库之外 canary 的次数为 0；
- read Runtime 修改授权仓库的次数为 0；
- 非直接局域网来源被 Host 接受的次数为 0；
- v2 执行任何 write 的次数为 0。

## 7. 用户故事与验收

### Epic 假设

如果 PeerContext 把局域网托管、身份和 Agent 在线状态自动化，开发者只需创建 Project、分享邀请并选择仓库，那么首次协作能在 5 分钟内完成，同时仍不需要共享仓库权限。

### US-01：一条命令创建 Project

- 自动安装并启动用户级后台服务；
- Project 与创建者 Member 原子创建；
- 创建者成为首个 Owner；
- 返回完整、单次、带过期时间的邀请；
- 失败时不留下半创建 Project 或泄漏邀请私钥。

### US-02：粘贴邀请加入

- 先尝试邀请端点，失败后自动发现；
- 验证 Host 公钥后才保存 Project；
- 邀请过期、消费、篡改分别返回稳定错误；
- v1 token/URL 不被 v2 接受；
- 成员由独立公钥和 Member ID 标识，显示名可重复。

### US-03：共享 Agent 后自动在线

- CLI 不读取仓库内容；
- 路径必须是本机 Git worktree；
- 注册后后台服务自动建立 WS；
- 终端关闭或用户重新登录后 Agent 自动恢复；
- 删除 Agent 后立即停止共享。

### US-04：Project 内 read

- 不需要逐人 ACL 或 `--mode read`；
- Member 身份、签名、nonce、Agent 所属关系全部通过后才执行；
- Agent 离线立即失败，不排队；
- stdin 和最终回答字节不被 CLI/Host 改写；
- 只读隔离 Runtime不得写仓库或读取其他路径。

### US-05：后台服务与诊断

- `service status` 显示安装、进程、局域网监听、发现和 Agent 连接状态；
- `service start|stop|restart` 可重复执行；
- 错误不得包含私钥、邀请私钥、正文或本地仓库路径。

### US-06：显式 Skill

- `peer-context` 仍为显式触发；
- 只调用 `agent list|get` 和 `ask` 的公开 v2 CLI；
- 不启动服务、不创建/加入 Project；
- 不提及 Relay、write、审批或 worktree；
- 基础设施错误不得伪装为业务答案。

### 关键边界 Case

| Case | 预期行为 |
|---|---|
| Host 离线 | `project_host_offline`，不排队 |
| Agent 离线 | `agent_offline`，不启动请求 |
| 邀请二次使用 | `invite_consumed` |
| 邀请已过期 | `invite_expired` |
| 邀请或 Host 公钥被篡改 | `host_identity_mismatch` 或 `invalid_invitation` |
| HTTP 签名被修改 | `signature_invalid` |
| nonce 重放 | `request_replayed` |
| 时钟偏差超限 | `clock_skew` |
| mDNS 被禁用 | 当前地址可用则继续；否则 `lan_discovery_unavailable` |
| Host 地址变化 | 自动发现并更新本地端点 |
| 请求来自非直连网段 | `lan_peer_required` |
| 成员被移除 | 后续请求拒绝；该成员拥有的 Agent 从 Project 移除 |
| Agent 仓库失效 | `agent_repository_unavailable` |
| Codex 缺少最终消息 | `codex_protocol_error`，不返回半截答案 |
| 请求或回答超限 | 终止并返回稳定大小错误 |
| 入站 Codex 再调用 PeerContext | `recursive_request_blocked` |

## 8. 不在 MVP 范围内

- 公网、跨子网、VPN、托管或独立 Relay；
- Project Host 迁移、接力、高可用或离线队列；
- TLS、正文端到端加密或流量保密；
- write、worktree、commit、merge、push、PR；
- Linux、Windows、Intel Mac；
- v1 Project、credential 或 Agent 迁移；
- 全局账号、跨 Project 身份或多设备合并；
- 自动仓库描述、索引、RAG、Embedding、Memory；
- CLI 语义理解、Agent 自动选择或 Prompt 改写；
- Agent 递归请求与多级编排。

## 9. 依赖、风险与缓解

| 风险/依赖 | 影响 | 缓解 |
|---|---|---|
| [macOS LaunchAgent](https://developer.apple.com/library/archive/documentation/MacOSX/Conceptual/BPSystemStartup/Chapters/CreatingLaunchdJobs.html) | 服务无法自动恢复 | `RunAtLoad` + `KeepAlive`，提供幂等 service 命令 |
| [HashiCorp mDNS](https://github.com/hashicorp/mdns) 在公司网络被禁用 | IP 变化后不能重发现 | 邀请先带直连端点；返回明确诊断，不伪装公网支持 |
| macOS 防火墙提示 | 首次连接需要一次确认 | 允许原生提示；不要求手工修改设置 |
| 创建者休眠或离线 | 整个 Project 不可用 | CLI 明确显示 Host 依赖；不做错误离线承诺 |
| 明文 LAN 流量 | 请求/回答可能被观察 | 文档明确披露；所有消息签名；只发送最小上下文 |
| mDNS 冒充 | 客户端连接错误主机 | Project Host 公钥固定，发现结果必须验证签名 |
| 设备私钥泄漏 | 成员被冒充 | Keychain 保存；Owner 可移除成员并重新邀请 |
| Codex CLI 变化 | Runtime 不可用或隔离失效 | 启动能力门禁和真实 Runtime smoke；失败拒绝 Agent 上线 |
| 旧 v1 状态存在 | 新旧语义混淆 | 使用独立 v2 状态目录，保留但不读取 v1 |

## 10. 开放问题

没有阻塞实现的产品决策。以下问题由试点验证，不允许在实现中自行扩张范围：

1. 🔵 **公司网络中 mDNS 的真实可用率？** 完成 10 组双人激活后统计；
2. 🔵 **5 分钟目标是否足够激进？** 记录 create、join、register、ask 各阶段耗时；
3. 🔵 **明文内容风险是否能被首批团队接受？** 试点前明确告知并收集反馈；
4. 🔵 **Host 单点在线是否成为主要阻塞？** MVP 后根据离线次数决定是否研究迁移。

## 开发顺序与发布门禁

1. 协议 v2、邀请签名、Project 设备密钥和独立状态目录；
2. 内嵌 Project Host、签名 HTTP/WS 和 Project-wide read；
3. LaunchAgent、Unix socket、自动 Agent 生命周期和 mDNS；
4. v2 CLI、Skill 与全部文档；
5. 删除 v1 Relay/write/worktree 公共面；
6. 自动化安全测试、macOS arm64 真实 smoke 和 10 组双人激活。

只有所有安全护栏为 0 违规、8/10 激活在 5 分钟内完成，并且 read 试点至少 8/10 可用后，才能把本 PRD 状态从 Draft 改为 Final 并发布 `0.2.0`。

## PRD 自评

### 最强部分

首次价值路径、局域网边界、read-only 范围和安全残余风险均已明确，能直接转换为验收测试。

### 最弱部分

局域网发现与真实双人激活尚无数据；明文内容风险只有产品决策，没有试点接受度证据。

### 最高风险假设

- 2–10 人熟人团队愿意让创建者电脑作为 Project 单点 Host；
- 完整邀请直连加 mDNS 足以覆盖常见公司局域网；
- 明文内容可见但身份不可伪造的安全取舍可以接受；
- 自动后台服务不会比手动终端引入更多安装和升级问题。

### 推荐下一步

自动化双配置目录的 create → join → register → fake read、LaunchAgent 定义和 mDNS 验证发现已经完成。下一步在两台真实 Apple Silicon Mac 上执行登录恢复、网络权限提示、IP 变化和 10 组双人首次激活试点；真实结果必须单独记录，不能用自动化 fixture 替代。
