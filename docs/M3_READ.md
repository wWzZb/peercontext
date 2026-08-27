# LAN v2 Read 链路

## 生命周期

1. `peerctx ask AGENT` 从 stdin 读取原始字节并计算 SHA-256。
2. 本机 CLI 通过 Unix socket 把请求交给用户级后台服务。
3. 后台服务使用当前 Project 成员私钥签名请求，并直连 Project 宿主；地址失效时通过 mDNS 重发现并验证宿主公钥。
4. 宿主验证直接局域网来源、成员签名、时间和 nonce，再检查 Agent 在线状态。所有 Project 成员都可以 read 明确注册的 Agent。
5. 宿主通过已签名 WebSocket 帧把请求交给 Agent 所在电脑。
6. 提供端后台服务将请求正文逐字节送入 `isolated_runtime` 的 Codex stdin。
7. Codex 最终 Agent message 作为回答，经提供端和宿主分别签名后返回请求端。

正文和回答只在活跃内存及网络中经过。SQLite 只记录请求 ID、成员、Agent、状态、字节数、hash 和时间，不保存正文或回答。

## Agent 生命周期

`agent register` 把仓库绝对路径只保存到提供端本机 v2 状态，并把不含路径的 Manifest 注册到 Project。后台服务立即建立 Agent 长连接，断线后自动重连；无需 `agent serve` 或常驻终端。

宿主离线、Agent 离线或请求端断线时不排队。Project 创建者离线后整个 Project 不可用。

## isolated_runtime

每次请求创建一次性 home、Codex home 和 tmp：

- 只桥接已验证的 Codex `auth.json`；
- 使用 `codex exec --strict-config --ephemeral --ignore-rules --skip-git-repo-check --json -`；
- root deny、workspace read、tmp write、网络关闭；
- apps、browser、computer use、hooks、multi-agent 和 plugins 关闭；
- 运行结束删除临时目录。

没有 write Permission Profile、detached worktree 或完整宿主环境回退。
