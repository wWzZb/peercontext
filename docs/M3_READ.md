# M3 Read 垂直链路

## 生命周期

1. 请求端 `peerctx ask AGENT --mode read` 从 stdin 读取原始字节，计算 SHA-256，构造 v1 Request。
2. Relay 校验 credential、Project/Member 绑定、Agent、read ACL、大小、过期时间和 request ID。
3. Agent 离线时立即失败，不排队；在线时正文仅在 HTTP/WS 内存中转。
4. 提供端 `agent serve` 把 Base64 解码后的原始字节直接交给 Codex stdin。
5. Codex JSONL 的最后一条 `item.completed/agent_message` 作为原始回答返回。
6. Relay 把 request 状态更新为 succeeded/failed/canceled/expired，不保存回答。

Provider WebSocket 使用统一 `ProviderMessage`，类型为 `ready/ping/pong/request/response/failure/cancel`。基础设施失败使用 `failure`，不能放进成功 `answer`。

## isolated_runtime

Runtime 不使用 Codex 版本白名单。`doctor` 和 `agent serve` 每次启动都会在一次性目录中探测必需 exec 参数、干净配置、`auth.json` 桥接，以及 read/write Permission Profile 的真实文件边界。能力不满足、可执行文件不可用或认证桥接失败时，`agent serve` 拒绝上线；不会按宽泛版本范围猜测兼容，也不会回退完整宿主环境。

每次请求创建一次性 `home/`、`codex-home/`、`tmp/` 和 `runtime/`：

- 只符号链接宿主 `auth.json`；
- 使用 `codex exec --strict-config --ephemeral --ignore-rules --skip-git-repo-check --json -`；
- Permission Profile 为 root deny、minimal read、tmp write、workspace root read、命令网络关闭；
- apps、browser、computer use、hooks、multi-agent 和 plugins 关闭；
- Codex 进程环境使用白名单；模型 shell 使用 `shell_environment_policy.inherit = "none"`，只注入干净 HOME/TMP、PATH、Git 隔离变量和 `PEERCTX_INBOUND_REQUEST=1`，不继承代理 credential；
- 运行结束删除整个临时目录，不存在完整宿主环境回退。

CLI 不读取仓库文件。它只把本地登记的路径交给 Runtime；项目 `AGENTS.md` 由 Codex 自己按正常规则读取。

## 取消、断线与重放

- read 默认 5 分钟；更短的请求过期时间优先。
- `request cancel`、请求端断线、ACL 中途撤销和 Provider 断线都会尽力取消 Codex context。
- Relay 重启时，遗留 `running` 元数据会改为 `failed`，正文无法恢复。
- 活跃的相同 request ID/相同绑定重放等待同一个结果；完成后的重放只返回已保存状态。
- 不同绑定重用同一 request ID 返回 `request_replay_mismatch`。

## M4 接口

M4 已复用 Provider WS、body-free request metadata、取消通道和 Runtime 错误信封，并新增双边确认、pending approval 和 detached worktree。write 仍使用同一个 `isolated_runtime` Adapter，但必须提供经过本地校验的 detached worktree 与主 Git metadata 边界；它不会退回主 checkout 或完整宿主环境。详见 [M4 Write 链路](./M4_WRITE.md)。
