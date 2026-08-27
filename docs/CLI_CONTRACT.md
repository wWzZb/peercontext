# peerctx CLI 公共契约（LAN v2）

`peerctx` 默认只输出 JSON。成功只写 stdout，失败只写 stderr；一次调用只写一个 JSON 对象并以换行结束。

## 信封

成功：

```json
{"ok":true,"data":{"schema_version":2},"meta":{"version":"0.2.0"}}
```

失败：

```json
{"ok":false,"error":{"type":"peerctx","subtype":"lan_v2","code":"lan_connection_failed","message":"...","hint":"...","retryable":true}}
```

`message` 和 `hint` 给人阅读，调用方匹配 `error.code`。现有退出码数值保持不变；v2 常用值为：`0` 成功、`2` 用法错误、`3` 本地配置、`5` 授权拒绝、`6` 局域网连接、`7` 不存在、`8` 冲突或重放、`9` Agent 不可用、`12` 超时、`14` 协议错误、`15` Codex Runtime 错误。

## 公开命令

```text
peerctx project create --name NAME [--member NAME]
peerctx project join INVITATION [--member NAME]
peerctx project list
peerctx project use PROJECT_ID
peerctx project invite create
peerctx project member list
peerctx project member remove MEMBER_ID

peerctx agent register REPOSITORY [--name NAME] [--summary TEXT]
                       [--tags CSV] [--capabilities CSV]
peerctx agent list
peerctx agent get AGENT
peerctx agent remove AGENT

peerctx ask AGENT [--timeout 5m] [--request-id ID]

peerctx service start|stop|restart|status
peerctx skills list
peerctx skills read peer-context [--file PATH]
peerctx version
```

没有 `relay serve`、`--relay`、credential、`agent serve/access`、`task`、write 审批、request 或 worktree 命令。

## 字节和大小

`ask` 只从 stdin 读取正文，最多 256 KiB。CLI 不按文本解释、不扫描语义、不添加提示词。成功结果继续使用 `data.response.answer`；Go JSON 会把 `body` 和 `answer` 字节编码为标准 Base64。接收端解码后，原始请求字节逐字节进入 Codex stdin。回答最大 2 MiB。

## 本地后台服务

除 `service stop/status` 等诊断命令外，正常命令会自动确保用户级 LaunchAgent 已安装并运行。CLI 通过权限为 `0600` 的 Unix socket 控制它。服务监听端口由系统自动分配，不是用户配置项。

## 协议安全

- 协议 `v2`，对象 `schema_version:2`；v1 状态保留但忽略。
- 每个成员每个 Project 使用独立 Ed25519 密钥，私钥只存 macOS Keychain。
- HTTP 请求、响应、WebSocket 握手和消息都签名，签名绑定 Project、成员、类型、原始 payload hash、nonce 和时间。
- 宿主拒绝过期时间、重用 nonce、未知成员、伪造响应和非直接连接网段。
- 传输当前不加密；签名不提供内容保密。
