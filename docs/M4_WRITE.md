# M4 Write 与 detached worktree

## 双边确认

请求端第一次执行 `peerctx task AGENT --mode write` 时只解析本地 stdin、计算字节数/hash 并读取 Agent Manifest，不提交 write 请求。CLI 以退出码 `10` 返回确认信封。再次显式传入 token 时，Agent、模式、字节数、SHA-256 和有效期必须全部匹配；否则在 POST 正文前返回 `write_confirmation_mismatch`。

确认后的正文在 Relay 活跃内存中等待 Agent Owner。SQLite 只保存 `pending_approval`、身份、模式、时间、大小和 SHA-256。批准前 Provider WebSocket 不收到正文。Owner 可 `request approve REQUEST_ID --commit COMMIT` 或 `request deny REQUEST_ID`；批准窗口默认 10 分钟，运行默认 15 分钟。

批准、拒绝、过期、取消、ACL 撤销、credential 撤销和双方断线都是互斥终态。批准与拒绝并发时只能有一个成功；任何终态都会释放 Relay 暂存的正文。

## worktree

提供端批准后，`agent serve` 在本地执行：

1. 用 Git 验证显式 commit；
2. `git worktree add --detach` 创建请求独占目录；
3. 验证 worktree HEAD 等于 commit 且没有符号分支；
4. 使用 write Permission Profile 启动固定版本的 `isolated_runtime`；
5. 保留 worktree，不执行 commit、merge 或 push。

Permission Profile 默认拒绝整个文件系统，只允许 worktree 写、worktree `.git` 与主 Git common dir 读、临时目录写，并关闭命令网络与宿主 shell 环境继承。响应只穿过安全的 worktree ID/base commit 元数据；仓库、worktree 和 Git common dir 的绝对路径只存在提供端 0600 本地状态。

`peerctx worktree list` 仅在提供端本机显示保留路径。`peerctx worktree remove WORKTREE_ID` 明确强制删除该 worktree 及其中未提交修改，返回 `recoverable:false`。
