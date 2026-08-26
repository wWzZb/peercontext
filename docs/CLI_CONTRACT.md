# peerctx CLI 公共契约（M1）

`peerctx` 默认只输出 JSON。成功只写 stdout，失败只写 stderr；一次调用只写一个 JSON 对象，并以换行结束。基础设施错误、审批状态和 Codex 回答使用不同字段，调用方不能把错误信封当成远端回答。

## 成功信封

```json
{
  "ok": true,
  "data": {},
  "meta": {
    "request_id": "req_...",
    "version": "0.1.0"
  }
}
```

不涉及请求的命令省略 `meta.request_id`。协议数据对象自身带 `schema_version`。

## 失败信封

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

`type` 是错误大类，`subtype` 是稳定分组，`code` 是调用方应该匹配的具体机器码。`message` 和 `hint` 给人阅读，不应被用于流程判断。

## 退出码

| 退出码 | 含义 |
|---:|---|
| 0 | 成功 |
| 1 | 未分类内部错误或输出失败 |
| 2 | 命令或参数用法错误 |
| 3 | 本地配置错误 |
| 4 | 身份认证失败 |
| 5 | 授权或 ACL 拒绝 |
| 6 | Relay 连接或传输失败 |
| 7 | 对象不存在 |
| 8 | 冲突、重复或重放拒绝 |
| 9 | Agent 或依赖当前不可用 |
| 10 | write 请求需要请求端用户确认；请求尚未发送 |
| 11 | 提供端用户拒绝审批 |
| 12 | 超时或过期 |
| 13 | 请求已取消 |
| 14 | v1 或 Codex JSONL 协议错误 |
| 15 | 隔离 Runtime、Codex 或 Git worktree 执行错误 |

退出码 `10` 是必须停下来询问用户的状态，不可自动确认或重试。

## 协议和 Runtime

- 协议版本：`v1`，对象 `schema_version` 为 `1`；
- Runtime 只有 `isolated_runtime`；门禁失败时拒绝服务，不存在完整宿主环境回退；
- 请求模式只能由调用方明确指定为 `read` 或 `write`；CLI 不从正文推断；
- 请求正文最大 256 KiB，回答最大 2 MiB；hash 直接基于原始字节计算；
- v1 JSON 中的 `body` 和 `answer` 是标准 Base64 字符串。接收端必须先解码再送入 Codex stdin 或返回调用方，不能把 Base64 文本当成请求或回答内容。该编码只属于传输层，不算提示词改写。

## M2 命令

```text
peerctx relay serve [--listen HOST:PORT] [--database PATH]
                    [--tls-cert PATH --tls-key PATH] [--log-file PATH]

peerctx project create --name NAME --owner NAME [--relay URL]
                       [--credential-file PATH]
peerctx project join --invite-token TOKEN --member NAME [--relay URL]
                     [--credential-file PATH]
peerctx project list
peerctx project use PROJECT_ID
peerctx project invite create [--expires-in 10m]
peerctx project member list
peerctx project member promote MEMBER_ID
peerctx project member remove MEMBER_ID

peerctx credential status
peerctx credential rotate
peerctx credential revoke [--credential CREDENTIAL_ID]

peerctx agent register --repository PATH --name NAME --summary TEXT
                       [--tags CSV] [--capabilities CSV]
                       [--modes read,write]
peerctx agent list
peerctx agent get AGENT
peerctx agent serve AGENT
peerctx agent access grant|revoke AGENT --member MEMBER_ID
                                      [--modes read,write]
```

credential 默认进入系统钥匙串。只有显式传入 `--credential-file` 时才写入 `0600` 文件；Project/Agent 的本地配置同样使用 `0600`。`project invite create` 会且只会在创建成功的 JSON 中返回一次邀请 token。

`agent register --repository` 只把绝对路径写入提供端本机配置。Relay 收到的 Agent Manifest 没有仓库路径字段。CLI 不打开该目录、不读取代码或 `AGENTS.md`，也不解释 Manifest 文本。

Relay 非 loopback 监听必须同时提供证书和私钥。Relay 日志只包含 HTTP 方法、状态码和耗时，不记录 URL 值、header、请求/响应正文或本地路径。

## M3 Read 命令

```text
peerctx ask AGENT --mode read [--timeout 5m] [--request-id REQUEST_ID]
peerctx request get REQUEST_ID
peerctx request cancel REQUEST_ID
```

`ask` 必须显式声明 `--mode read`，正文只从 stdin 读取。CLI 最多读取 256 KiB 加一个用于判断超限的字节，不按文本解码，不扫描语义，也不添加提示词。成功数据使用：

```json
{
  "schema_version": 1,
  "response": {
    "schema_version": 1,
    "request_id": "req_...",
    "status": "succeeded",
    "answer": "标准 Base64"
  },
  "replayed": false
}
```

已完成 request ID 的相同绑定重放只返回 `metadata` 和 `replayed:true`，因为 Relay 不保存回答。相同 ID 绑定到不同正文哈希、Project、Member、Agent 或模式时返回 `request_replay_mismatch`。

稳定错误包括 `agent_offline`、`agent_access_denied`、`request_timeout`、`request_canceled`、`request_replay_mismatch`、`response_too_large`、`codex_protocol_error`、`agent_repository_unavailable` 和 `isolated_runtime_unavailable`。入站 Runtime 中的 `peerctx ask/task` 在读取 stdin 前返回 `recursive_request_blocked`。

## M4 Write 与 worktree

```text
peerctx task AGENT --mode write [--approval-timeout 10m]
             [--run-timeout 15m] [--request-id REQUEST_ID]
peerctx task AGENT --mode write --confirm CONFIRMATION_TOKEN

peerctx request pending
peerctx request approve REQUEST_ID --commit COMMIT
peerctx request deny REQUEST_ID
peerctx request get|cancel REQUEST_ID

peerctx worktree list
peerctx worktree remove WORKTREE_ID
```

`task` 首次调用读取 stdin 并解析 Agent，但不向 `/v1/requests` 发送正文。它向 stderr 写 `write_confirmation_required`，退出 `10`，并在 `error.details` 返回绑定 Agent ID、`write` 模式、原始字节数、SHA-256 和有效期的确认对象及 token。只有再次显式提供 `--confirm` 且 stdin 逐字节匹配时才发送。

Relay 将 write 正文只保留在活跃进程内存，状态为 `pending_approval`。Agent Owner 使用 `request approve --commit` 逐次批准；commit 必须显式给出。批准前正文不会进入 Provider Runtime；拒绝返回 `write_request_denied`/退出 `11`，过期返回 `write_approval_expired`/退出 `12`。

提供端从明确 commit 创建独立 detached worktree。write Permission Profile 只允许该 worktree 写、`.git` 与主 Git common dir 只读、命令网络关闭；CLI 不读取 diff，不自动 commit、merge 或 push。响应只返回 worktree ID、Agent、request ID 和 base commit，不返回提供端路径。提供端本地 `worktree list` 可见路径；`worktree remove` 是显式且不可恢复的删除。

## M5 Skill、诊断和 npm

```text
peerctx skills list
peerctx skills read peer-context [--file PATH]
peerctx doctor
```

`skills list/read` 暴露编入二进制、与 `peerctx` 同版本的 `.agents/skills/peer-context` 文本，不负责安装。Skill 的 `allow_implicit_invocation` 为 `false`，只服务请求端并且只调用本页公开命令。

`doctor` 以无 token、无本地路径的结构化 checks 验证已门禁平台、固定 Codex 版本、`auth.json` 桥接、Permission Profile、credential 存储、Relay TLS/health、Git 仓库和保留 worktree。任一阻塞检查失败时返回非零和 `error.details`；`agent serve` 同样拒绝未门禁平台，不存在完整宿主环境回退。

`peerctx` npm 包只是同版本 Go 二进制的平台包装。内置二进制必须通过 SHA-256 manifest 校验；Node 层不解析命令、stdin、正文或回答。
