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
