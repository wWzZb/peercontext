# PeerContext MVP 产品需求文档

## 文档信息

| 项目 | 内容 |
|---|---|
| 状态 | **Final — 可进入开发** |
| 版本 | 1.0.0 |
| 日期 | 2026-08-25 |
| 作者 | wuzhibo / Codex |
| 仓库 | `/Users/wuzhibo/peercontext` |
| Go module | `github.com/wWzZb/peercontext` |
| npm package | `peerctx` |
| CLI | `peerctx` |
| Skill | `peer-context` |
| License | MIT |
| Runtime 决策 | **`isolated_runtime`，已由 3 次冷启动 Spike 通过并锁定** |

### 变更记录

| 版本 | 日期 | 说明 |
|---|---|---|
| 1.0.0 | 2026-08-25 | 合并最终产品边界、Runtime Spike 结论、技术方案和验收 Case |

## 目录

1. [执行摘要](#1-执行摘要)
2. [问题定义](#2-问题定义)
3. [目标用户与任务](#3-目标用户与任务)
4. [战略背景与产品原则](#4-战略背景与产品原则)
5. [方案总览](#5-方案总览)
6. [成功指标](#6-成功指标)
7. [用户故事与详细需求](#7-用户故事与详细需求)
8. [不在 MVP 范围内](#8-不在-mvp-范围内)
9. [依赖、风险与缓解](#9-依赖风险与缓解)
10. [开放问题](#10-开放问题)

---

## 1. 执行摘要

我们要为使用 Codex、但代码分散在多个开发者私有仓库中的团队开发 PeerContext：当 Codex A 缺少由另一私有仓库掌握的事实时，它可以通过 `peerctx` 请求该仓库所在机器的 Codex B 在本地回答，或执行经双方批准的修改。A 不获得 B 的仓库、凭证或环境权限；Relay 只路由、不保存正文；CLI 只做基础设施；可选 Skill 只教请求端 Codex 何时和如何协作。MVP 以 10 个真实跨仓库 Case 中至少 8 个在一次请求或一次澄清内解决为价值目标，同时以零正文持久化、零越权读取和零未批准写入为安全底线。

### 最终 Runtime 决策

开发前 Spike 已在 `codex-cli 0.149.0-alpha.4.3` 上完成 3 次独立冷启动，认证复用、真实模型调用、个人 Skills/MCP/历史和其他仓库隔离全部通过。因此 MVP **只实现隔离 Runtime**：每个入站请求使用干净 `HOME`、`CODEX_HOME` 和临时目录，只映射宿主认证。

完整证据见 [Runtime Spike 结果](../spikes/codex-runtime/RESULT.md)。如果未来版本或其他认证后端不满足同一门禁，`doctor` 必须拒绝提供服务，不能静默退回完整宿主环境。

---

## 2. 问题定义

### 谁遇到这个问题

- 代码按服务、端或权限边界分散在不同私有仓库的开发者；
- 正在使用 Codex 完成功能、排查故障或修改代码，但本地只拥有部分仓库的人；
- 仓库所有者，希望提供“由自己的 Codex 代查、代答、受控代改”，而不是直接分发仓库权限。

🔶 **假设：** 首批试点是 2–10 人研发协作组，成员彼此认识并能建立 Project 信任关系。MVP 不为陌生人的开放市场设计。

### 具体问题

Codex A 在处理本地任务时，经常需要另一个私有仓库中的 API 契约、业务规则、配置、SDK 用法、迁移约束或故障原因。现有选择都不理想：

- 猜测缺失信息，容易产出错误实现；
- 让用户手工复制代码或回答，协作慢且上下文损失大；
- 给 A 或开发者 A 直接开 B 仓库权限，扩大权限面；
- 建索引、RAG 或共享知识库，会复制私有代码并引入过期知识。

### 为什么痛

- **对请求方：** 任务中断，需要跨人、跨工具追问，或者承担猜错的返工成本；
- **对提供方：** 每次都要人工定位和解释，或被迫给出过宽权限；
- **对团队：** 私有边界和开发效率只能二选一，跨仓库故障与联调尤其明显。

### 证据

- 本产品定义来自当前关于 API 契约、共享模型、认证规则、部署配置、内部 SDK、组件约束、跨仓库故障、迁移兼容、测试 fixture 和双边修改的 10 类 Case；
- Runtime Spike 已证明“只复用认证的干净 Codex”在当前环境可运行；
- 🔶 **假设：** 尚无真实团队试点数据，价值指标需要在 MVP 完成后用 10 个 Case 验证。

---

## 3. 目标用户与任务

### 主要 Persona：请求方开发者 A

- 正在本地仓库中使用 Codex；
- 知道信息属于另一个团队或仓库，但没有直接权限；
- 希望得到可信答案或发起受控修改，而不是复制整个仓库。

核心任务：

> 当我的本地任务依赖另一个私有仓库的事实时，我希望让那个仓库自己的 Codex 去查并返回结果，以便我继续当前工作，同时不申请仓库权限。

### 次要 Persona：提供方开发者 B

- 拥有目标仓库与本机 Codex 环境；
- 愿意为指定 Project 成员开放某个 Agent；
- 希望按 Agent 和 `read/write` 模式控制访问，写入时逐次批准。

核心任务：

> 当可信协作者需要我的仓库信息时，我希望由我的本机 Codex 在受限环境中处理，并由我控制谁能读、何时能写，而不交出仓库凭证。

### 运维 Persona：Project Owner

- 创建自托管 Project；
- 邀请成员、管理 Owner、轮换和撤销 credential；
- 维护 Relay 与审计元数据。

### Skill 的使用者

`peer-context` Skill 只服务请求端 Codex A。它帮助 Codex 判断是否真的缺少远端事实、选择 Agent、整理一次高质量请求、处理基础设施错误和避免无效重试。提供端 Codex B 不主动调用它。

---

## 4. 战略背景与产品原则

### 业务目标

1. 验证“Codex 请求另一个私有仓库的 Codex”是否能减少跨仓库阻塞；
2. 在不共享仓库权限、不复制代码知识库的前提下完成协作；
3. 建立足够小、可自托管、可审计的基础设施层。

### 差异化

PeerContext 不是代码搜索、RAG、知识库、Agent Workflow 或仓库共享工具。它把事实留在仓库所有者机器上，按请求临时调用当地 Codex，返回的只是本次回答或经批准产生的工作树修改。

### 两层架构原则

**PeerContext 分为两个相互独立的层次。**

| 层次 | 负责 | 明确不负责 |
|---|---|---|
| `peerctx` CLI | 身份、Project、Agent 发现、授权、传输、审批、Codex 进程和执行环境隔离 | 不理解请求语义、不选择 Agent、不读取仓库内容、不规划任务、不修改提示词 |
| `peer-context` Skill | 指导请求端 Codex 判断何时协作、选择 Agent、组织请求和处理返回结果 | 不参与 Relay、不参与提供端执行、不依赖 Go 内部接口 |

必须始终满足：

- CLI 在未安装 Skill 时可以独立完成所有功能；
- Skill 只使用公开 CLI 命令和 JSON 契约；
- Relay 不加载 Skill，提供端 Codex 不主动调用 Skill；
- CLI 可以把仓库路径交给 Git/Codex，但不能打开、索引或解释代码、`AGENTS.md`、diff 或请求正文；
- 请求模式 `read/write` 由调用方明确声明，CLI 不根据文字含义推断。

### 为什么是现在

Codex 已提供非交互执行、临时会话和 Permission Profile，可以把“远端请求路由”与“本地代码推理”分开。当前实现依赖的官方行为参考：

- [Codex non-interactive mode](https://learn.chatgpt.com/docs/non-interactive-mode)
- [Codex Permission Profiles](https://learn.chatgpt.com/docs/permissions)
- [Codex Skills](https://learn.chatgpt.com/docs/build-skills)

---

## 5. 方案总览

### 5.1 系统关系

```text
请求端机器 A                                      提供端机器 B

Codex A                                            Codex B
  │ 可选读取 peer-context Skill                      ▲
  │ 判断何时问、问谁、怎么问                         │ 原始 stdin
  ▼                                                 │
peerctx CLI ──主动连接──► 自托管 Relay ◄──主动连接── peerctx agent serve
  │                         │                       │
  │                         └─只存身份/状态/审计元数据 │
  │                                                 ▼
  └──────────────回答或审批状态────────────── 隔离 Runtime + 授权仓库/worktree
```

双方客户端都主动向 Relay 建立 HTTP + WebSocket/WSS 连接，不要求 B 暴露入站端口。Relay 不运行 Codex，也不保存请求或回答正文。

### 5.2 组件

#### `peerctx` CLI

一个 Go 二进制，同时提供：

- Project/Member 身份与 credential 管理；
- Agent 注册、发现、在线服务和 ACL；
- 请求发送、接收、状态、取消和审批；
- Codex 隔离 Runtime；
- Git detached worktree 生命周期；
- 嵌入式 Skill 文本的 `skills list/read` 公共接口；
- `doctor` 和结构化错误。

#### Relay

自托管 Go 服务，SQLite 只保存：

- Project、Member、Owner、Agent、ACL；
- credential 哈希、邀请状态；
- request ID、双方身份、模式、状态、时间、正文大小和内容哈希；
- 不包含请求正文、回答正文或代码 diff。

正文仅在连接和内存中转；B 离线时立即失败，不排队。进程崩溃或双方断开时未完成正文丢失，这是 MVP 的预期行为。

#### `peer-context` Skill

仓库路径为 `.agents/skills/peer-context`，至少包含：

```text
.agents/skills/peer-context/
├── SKILL.md
├── agents/openai.yaml
└── references/
    ├── cli-contract.md
    ├── request-patterns.md
    └── error-handling.md
```

`agents/openai.yaml` 必须配置：

```yaml
policy:
  allow_implicit_invocation: false
```

它只能由用户或请求端 Codex 显式选择。用户可通过通用 Skill 工具安装，例如：

```bash
npx skills add /Users/wuzhibo/peercontext --skill peer-context --agent codex -g
```

CLI 不提供专用安装命令，只通过 `skills list/read` 暴露编入二进制、与 CLI 同版本的 Skill 内容。

### 5.3 隔离 Runtime

每个入站请求创建一次性目录：

```text
request-root/
├── home/           # 干净 HOME
├── codex-home/     # 干净 CODEX_HOME、最小 config、仅认证映射
├── tmp/            # 可写临时目录
└── runtime/        # 请求运行元数据，不含正文
```

规则：

- 只映射宿主现有 `auth.json`，允许 Codex 正常刷新认证；
- 不复制宿主 config、Skills、MCP、插件、hooks、sessions、历史数据库或其他个人目录；
- 只保留运行所需的最小环境变量；
- 使用 `codex exec --strict-config --ephemeral --json -`；
- 原始请求字节直接送到 stdin，不添加前后缀、不扫描、不脱敏、不重写；
- 项目内 `AGENTS.md` 由 Codex 正常读取，CLI 不读取；
- 只解析 Codex JSONL 中的状态和最终 Agent 消息，回答原文不改写；
- 运行结束清理临时 `HOME/CODEX_HOME`；写模式的 worktree 除外，需用户显式删除。

Permission Profile：

| 模式 | 文件权限 | 命令网络 | 写入位置 |
|---|---|---|---|
| `read` | 默认拒绝；授权仓库只读；最小运行目录只读；临时目录可写 | 关闭 | 不允许写授权仓库 |
| `write` | 默认拒绝；detached worktree 可写；主 Git 元数据只读；临时目录可写 | 关闭 | 仅 detached worktree |

仓库内指向外部路径的符号链接仍受“默认拒绝整个文件系统”约束。CLI 自己的 Relay 连接不在 Codex 子进程的网络沙箱内。

### 5.4 身份与授权

- 不设全局账号；身份只存在于 Project 内；
- `project create` 生成首个 Owner credential；
- Owner 创建一次性邀请，加入后一个设备对应一个 Project Member；
- Member 名称在 Project 内唯一；支持多个 Owner，但不能移除最后一个 Owner；
- token 优先存系统钥匙串；只有用户显式选择时才以 `0600` 文件保存；
- Relay 只保存高熵 token 的哈希，不保存明文；
- token 可轮换、撤销；一次性邀请消费后不可重放；
- Agent 由本机 Member 注册，公开 Manifest 不包含本地仓库绝对路径。

Agent Manifest：

```json
{
  "schema_version": 1,
  "name": "backend-bob",
  "summary": "订单与支付后端仓库",
  "tags": ["backend", "orders", "payments"],
  "capabilities": ["API contract", "business rules", "approved code changes"],
  "modes": ["read", "write"]
}
```

CLI 只校验结构并发布；它不解释这些文字。Skill 或人根据 Manifest 选择 Agent。

### 5.5 请求生命周期

#### Read

1. A 通过 `peerctx ask <agent> --mode read`，正文从 stdin 输入；
2. Relay 校验 Project、Member、Agent 和 ACL；
3. B 在线且 A 在 Agent read 白名单中，B 的 CLI 自动接收；
4. B 创建隔离 Runtime，在授权仓库中启动 Codex；
5. 最终 Agent 消息原样返回 A；
6. Relay 只写审计元数据与正文哈希。

#### Write

1. A 通过 `peerctx task <agent> --mode write` 提交正文；
2. CLI 不发送请求，先以退出码 `10` 返回确认信封；
3. A 的用户明确确认后，CLI 发送绑定 Agent、模式、正文哈希和过期时间的请求；
4. B 的用户通过 `request approve` 逐次批准，或 `deny` 拒绝；
5. B 的 CLI 创建 detached worktree，在隔离 Runtime 的 `workspace-write` 权限中执行；
6. 不 commit、不 merge、不 push；响应返回回答和 worktree 元数据；
7. worktree 保留，直到 B 显式 `worktree remove`。

Skill 必须把退出码 `10` 当作“需要询问用户”，不能静默追加确认参数。

#### 递归门禁

入站 Codex 子进程设置请求上下文标记，且 Permission Profile 禁止命令网络。该进程中再次执行 `peerctx ask/task` 必须返回 `recursive_request_blocked`，不能形成链式请求。CLI 不扫描正文中的 `$peer-context`；调用方显式写入时仍属于显式 Skill 触发，但递归请求照样被阻止。

### 5.6 CLI 命令

```text
peerctx relay serve

peerctx project create|list|use|join
peerctx project invite create
peerctx project member list|promote|remove
peerctx credential status|rotate|revoke

peerctx agent register|serve|list|get
peerctx agent access grant|revoke

peerctx ask <agent> --mode read
peerctx task <agent> --mode write

peerctx request pending|get|approve|deny|cancel
peerctx worktree list|remove

peerctx skills list|read
peerctx doctor
peerctx version
```

Agent 调用默认 JSON；运维命令可选 `--format text`。

成功输出：

```json
{
  "ok": true,
  "data": {},
  "meta": {
    "request_id": "req_...",
    "version": "1.0.0"
  }
}
```

失败输出写 stderr，并使用非零退出码：

```json
{
  "ok": false,
  "error": {
    "type": "authorization",
    "subtype": "agent_acl",
    "code": "agent_access_denied",
    "message": "The member cannot use this agent in read mode.",
    "hint": "Ask the agent owner to grant read access.",
    "retryable": false
  }
}
```

基础设施失败、审批状态和 Codex 回答必须分开，网络错误绝不能伪装成远端业务答案。

### 5.7 协议和数据边界

- 所有对象带 `schema_version`；MVP 协议版本为 `v1`；
- request ID 全局唯一；Relay 对重复 ID 做幂等校验和重放拒绝；
- 正文按原始字节哈希，hash 用于确认绑定、审计和去重；
- Relay 日志中禁止出现正文、回答、Authorization、邀请 token 和本地仓库路径；
- B 离线返回 `agent_offline`，不建立离线队列；
- 断线时未完成请求失败，调用方可以使用新 request ID 重试；
- 取消只保证“尽力而为”：Relay 通知 B，B 终止 Codex 子进程并清理临时 Runtime；
- TLS 是非本机部署的硬要求；MVP 不做端到端加密，Relay 进程可在转发时看到内存中的正文。

🔶 **实现默认值：** 请求正文最大 256 KiB、回答最大 2 MiB、read 超时 5 分钟、write 批准前 10 分钟过期、运行超时 15 分钟。首轮试点后根据真实数据调整，但开发先按此实现。

---

## 6. 成功指标

### 主要指标

- 10 个试点 Case 中至少 8 个在一次请求或一次澄清内得到可用于继续开发的答案或修改结果。

### 次要指标

- 已授权且双方在线的 read 请求端到端成功率 ≥ 95%；
- 成功 read 请求的 P50 完成时间 ≤ 120 秒；
- 新用户在文档指导下 15 分钟内完成 Project 创建、第二成员加入、Agent 注册和首个 read 请求；
- 未安装 Skill 时，人工 CLI 路径仍能完成全部核心流程。

🔶 **假设：** 以上时延和上手目标没有现有基线，属于 MVP 试点目标而非已有承诺。

### 安全护栏

- Relay 数据库与日志中的正文 canary 命中数为 0；
- 未授权 read 成功数为 0；
- 未经 A 确认或 B 批准的 write 执行数为 0；
- write 修改主 checkout、分支、提交或远端的次数为 0；
- 隔离 Runtime 中出现宿主个人 Skill、MCP、历史或其他仓库 canary 的次数为 0；
- 提供端入站请求隐式加载 `peer-context` Skill 的次数为 0。

---

## 7. 用户故事与详细需求

### Epic 假设

如果开发者能让目标私有仓库自己的 Codex 按明确权限回答或修改，而不共享仓库访问权，那么跨仓库依赖的阻塞和猜测会下降，并且仓库所有者仍能保持控制。

### P0 用户故事

#### US-01：Project 建立

作为 Owner，我可以创建 Project、生成首个 credential 和一次性邀请，让另一台设备加入同一信任域。

验收：

- Project 无全局账号依赖；
- 邀请只能消费一次且可过期；
- Member 名称唯一；
- 不能移除最后一个 Owner；
- credential 可轮换和撤销，Relay 只存哈希。

#### US-02：Agent 注册与发现

作为 B，我可以用本地仓库路径注册 Agent，并发布人写的 Manifest；作为 A，我可以列出在线 Agent 及其 `read/write` 能力。

验收：

- 本地路径不发布到 Relay 的公开 Manifest；
- CLI 只做 schema 校验，不解释能力描述；
- offline 状态可见，离线请求立即失败；
- Agent ACL 可分别授予或撤销 read/write。

#### US-03：无 Skill 的只读请求

作为 A，即使没安装 Skill，我也能把 stdin 原文发送给已授权 Agent，并收到 B 的 Codex 原始最终回答。

验收：

- stdin 进入 Codex 前后字节一致；
- CLI、Relay 不理解或包装正文；
- B 使用干净 Runtime；
- 仓库可读、其他路径不可读、仓库不可写；
- Relay 数据库和日志无正文；
- 基础设施错误使用结构化错误而不是答案字段。

#### US-04：双边批准的修改

作为 A，我可以请求 B 的 Codex 修改目标仓库；作为 A/B 用户，我们分别确认和批准；结果只留在 detached worktree。

验收：

- A 首次调用退出码 10，未向 Relay 发送正文；
- 确认信封绑定 Agent、模式、正文哈希和有效期；
- B 每个 write 单独批准；
- worktree 从明确 commit 创建且 detached；
- 主 checkout、分支、commit、remote 均不变化；
- CLI 不打开或解释 diff；
- 用户可列出和显式删除 worktree。

#### US-05：可选 Skill

作为请求端 Codex A，我在显式启用 `peer-context` 后，可以先检查本地信息，再发现合适 Agent，组织最小必要请求并正确处理回答或错误。

验收：

- `allow_implicit_invocation: false`；
- Skill 只调用公开 CLI；
- CLI 在 Skill 缺失时全部测试仍通过；
- Relay 和提供端没有 Skill 依赖；
- Skill 遇到 write 确认必须询问用户；
- Skill 不把超时、拒绝、离线等错误当成业务结论；
- Skill 避免对同一问题无限重试，最多一次针对性澄清。

#### US-06：运维与诊断

作为 B 或 Owner，我能通过 `doctor` 发现认证桥接、Codex 版本、Permission Profile、钥匙串、Relay TLS 和仓库/worktree 前置条件问题。

验收：

- `doctor` 不打印 token；
- 未通过隔离门禁的环境拒绝 `agent serve`；
- 不静默改用完整宿主 Runtime；
- 诊断错误给出稳定 code、hint 和 retryable。

### 关键边界 Case

| Case | 预期行为 |
|---|---|
| B 离线 | 立即 `agent_offline`，不排队、不保存正文 |
| ACL 在请求中途撤销 | 未开始则拒绝；已运行则尽力取消并记录状态 |
| 相同 request ID 重放 | 相同哈希返回已有状态；不同哈希拒绝 |
| 邀请二次使用 | 拒绝并记录审计元数据 |
| A 确认信封与正文不匹配 | 拒绝发送 |
| B 批准已过期 write | 拒绝执行 |
| Agent 仓库路径失效 | `agent_repository_unavailable` |
| 仓库存在外链 symlink | Permission Profile 阻止读取目标 |
| Codex 输出缺少最终消息 | `codex_protocol_error`，不返回半截答案 |
| 输出超过限制 | 终止并返回 `response_too_large` |
| Relay/客户端断线 | 本次请求失败；正文不落盘 |
| 入站 Codex 再调用 ask/task | `recursive_request_blocked` |
| 请求显式包含 `$peer-context` | 不扫描、不删除；Skill 可显式加载，但递归请求仍阻止 |
| write Codex 尝试 commit/push | Git 元数据只读且命令网络关闭，操作失败 |
| 并发 write 指向同一 Agent | 每个请求独立 worktree，审批和生命周期隔离 |

### 10 个试点 Case

1. **API 契约：** 前端询问后端接口字段、空值和错误码；
2. **共享模型：** 服务 A 询问服务 B 的状态枚举和转换规则；
3. **认证规则：** 客户端询问服务端登录、刷新和权限边界；
4. **部署配置：** 应用询问基础设施仓库的环境变量和部署约束；
5. **内部 SDK：** 消费方询问 SDK 的正确调用方式和兼容版本；
6. **组件约束：** 业务页面询问私有组件库的限制和推荐组合；
7. **跨仓库故障：** 一侧提供症状，请另一侧检查本地实现和测试定位原因；
8. **迁移兼容：** 新实现询问旧仓库数据/协议迁移要求；
9. **测试 fixture：** 消费方询问提供方已有 fixture、边界值和构造规则；
10. **跨仓库修改：** A 明确请求 B 修改，经双边批准后只产生 detached worktree 变更。

每个 Case 记录：请求次数、是否澄清、完成时间、结论是否可用、是否触发安全护栏。成功阈值为至少 8/10。

### 质量与测试要求

- 单元测试覆盖协议、ACL、状态机、token、确认信封、路径和输出契约；
- Relay 集成测试验证正文 canary 不进入 SQLite 或日志；
- 端到端测试覆盖两个客户端、断线、超时、取消、重放和 token 撤销；
- worktree 测试验证主 checkout、分支、提交和远端不变化；
- Skill 测试验证显式触发、公共 CLI 依赖和未安装时 CLI 可用；
- 除 Runtime Spike 和最终一次真实 Codex smoke test 外，执行测试使用 fake Codex adapter；
- npm 包装器测试平台识别、下载/校验或内置二进制调用；
- 构建目标覆盖 macOS、Linux、Windows 的 amd64/arm64；Runtime 隔离至少在实际支持的平台各做一次门禁。

---

## 8. 不在 MVP 范围内

- 全局账号、公共 Agent 市场或跨 Project 身份；
- OpenAI 或项目方托管的 Relay；
- 离线队列、正文持久化和请求恢复；
- 端到端加密；
- CLI 语义理解、Agent 自动选择、任务规划或 Prompt 优化；
- 代码索引、RAG、Embedding、Memory 或知识同步；
- 自动脱敏或正文策略扫描；
- `full/unrestricted` 远程权限模式；
- 自动 commit、merge、push 或创建 PR；
- 正式 A2A 协议兼容；
- 多级 Agent 编排或递归 PeerContext 请求；
- 本轮创建远端 Git 仓库、发布 npm 包或发布 Release。

### 后续可考虑

- 经独立 Spike 验证的 Keyring 等认证后端；
- Relay 端到端加密与托管形态；
- 更成熟的审批 UI 和审计导出；
- 在不破坏 CLI/Skill 分层的前提下适配其他本地 Agent Runtime。

---

## 9. 依赖、风险与缓解

### 依赖

| 依赖 | 用途 | 要求 |
|---|---|---|
| Codex CLI | 提供端推理与执行 | 支持 `exec --ephemeral --json -` 和 Permission Profile |
| Git | detached worktree | 提供稳定的 worktree 命令 |
| SQLite | Relay 元数据 | 禁止写入正文列 |
| 系统钥匙串 | 默认 token 保存 | 失败时只允许用户显式选择 0600 文件 |
| TLS | Relay 传输 | 非 localhost 部署必须启用 |
| Go | CLI/Relay | 单二进制和跨平台构建 |
| npm wrapper | `peerctx` | 与 Go 二进制和 Skill 同 SemVer |

### 风险

| 风险 | 影响 | 缓解 |
|---|---|---|
| Codex CLI 参数或配置变化 | Runtime 无法启动或隔离失效 | `doctor` + 版本兼容表 + CI Spike；失败即拒绝服务 |
| auth refresh 修改宿主认证 | 认证状态竞争或损坏 | 只映射 auth 文件；原子写入兼容测试；不打印或复制 token |
| Relay 可见内存正文 | 自托管 Relay 被攻破时泄漏 | TLS、最小日志、短生命周期、无正文落盘；E2E 作为后续 |
| Prompt 诱导读取其他路径 | 私有资源泄漏 | 默认拒绝整个文件系统，workspace 白名单，网络关闭，canary 测试 |
| Skill 在提供端被触发 | 递归或职责混淆 | `allow_implicit_invocation:false` + 干净 Runtime + 递归门禁 |
| write 影响主仓库 | 未授权改动 | detached worktree、Git 元数据只读、不 commit/merge/push、双边审批 |
| CLI 误读正文或代码 | 边界漂移 | 原始字节测试、接口分层、代码 review 检查禁止语义组件 |
| body 被日志中间件记录 | 隐私泄漏 | 自定义脱敏日志、canary 集成测试、禁止通用 body logging |
| Windows/Linux 行为与 macOS 不同 | 跨平台承诺失败 | 发布前逐平台门禁；未通过的平台不标记 Runtime 可用 |
| Agent 描述过时 | Skill 选错 Agent | Manifest 人工维护、显示更新时间、允许一次澄清 |

### 重要残余风险

- MVP 没有端到端加密，Relay 内存可看到正文；
- Codex 与模型本身可能产生错误答案，PeerContext 只保证来源和执行边界，不保证事实必然正确；
- File auth 是当前实测后端。其他认证后端不能根据本 PRD 自动推定可用；
- Permission Profile 属于 Codex 能力，升级 Codex 后必须重新跑门禁。

---

## 10. 开放问题

没有阻塞正式开发的产品问题。以下参数先按本 PRD 默认值实现，再用试点数据复核：

1. 🔵 **正文和回答大小上限是否合适？** Owner：试点负责人；时间：完成前 20 个请求后；当前值 256 KiB / 2 MiB。
2. 🔵 **read/write 超时是否合适？** Owner：试点负责人；时间：完成 10 个 Case 后；当前值 5 / 15 分钟。
3. 🔵 **哪些 Linux/Windows 组合能达到同一 Runtime 门禁？** Owner：工程；时间：标记对应平台支持前。
4. 🔵 **Keyring 认证是否能在不复制个人环境的情况下稳定桥接？** Owner：工程；时间：MVP 后；不阻塞当前 File auth 支持。

---

## 开发顺序与发布门禁

### M0 — 已完成：Runtime Spike 与 PRD

- 3 次冷启动全部通过；
- 最终 Runtime 锁定 `isolated_runtime`；
- PRD 1.0.0 冻结。

### M1 — 仓库骨架和公共契约

- Go CLI/Relay 目录；
- 统一 JSON 输出、错误和退出码；
- v1 协议模型、版本同步；
- fake Codex adapter；
- CI 基础构建和测试。

### M2 — Project、身份、Agent 和 Relay

- Project/Member/Owner/invite/credential；
- Relay HTTP + WS/WSS、SQLite；
- Agent register/list/get/serve、在线状态和 ACL；
- 确认正文不落盘。

### M3 — Read 垂直链路

- stdin 原文传输；
- B 隔离 Runtime；
- Permission Profile read；
- Codex JSONL 解析和原始答案返回；
- offline、timeout、cancel、replay。

### M4 — Write 与 worktree

- A 退出码 10 确认信封；
- B pending/approve/deny；
- detached worktree 和 workspace-write；
- worktree list/remove；
- 双边审批与 Git 不变量测试。

### M5 — Skill、npm 和验收

- 显式触发的 `peer-context` Skill；
- Skill 文本嵌入与 `skills list/read`；
- `peerctx` npm 包装器；
- 版本同步 CI；
- 10 个试点 Case、最终真实 Codex smoke test和跨平台构建。

发布 MVP 前，所有安全护栏必须为 0 违规，且 10 个 Case 至少 8 个通过。

---

## PRD 自评

### 最强部分

CLI、Skill、Relay 和提供端 Codex 的职责边界，以及 read/write 与 Runtime 的安全验收条件已经明确，可以直接转成测试。

### 最弱部分

用户价值指标目前来自设计 Case，缺少真实团队基线；跨平台 Runtime 也只有 macOS arm64 实测。

### 需要验证的主要假设

- 10 个 Case 能代表最常见的跨仓库协作；
- 256 KiB 请求和 2 MiB 回答足够 MVP；
- 自托管 Relay、无离线队列对首批团队可接受；
- Manifest 足以让人或 Skill 找到正确 Agent。

### 推荐下一步

从 M1 开始做最薄的 read 垂直链路，先让两个本地客户端经 Relay 完成一次 fake Codex 请求，再逐步替换为已验证的隔离 Runtime；每个里程碑都先锁定公共 JSON 契约和安全测试。
