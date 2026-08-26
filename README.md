# PeerContext

PeerContext 让一个本地 Codex 向另一个开发者机器上的 Codex 请求私有仓库上下文，或发起经双方批准的修改，而不共享仓库权限。

项目分为两个独立层次：

- `peerctx` CLI：身份、发现、授权、传输、审批和隔离执行；
- `peer-context` Skill：请求端 Codex 的可选使用说明，只通过公开 CLI 工作。

CLI 不理解请求内容、不读取代码、不规划任务，也不修改提示词。Skill 未安装时 CLI 必须完整可用；Skill 不参与 Relay 或提供端 Codex 的运行。

当前实现已覆盖 M1–M5：Project/身份/Agent/Relay、read 请求、双边批准的 write、detached worktree、显式 `peer-context` Skill、`doctor`、npm 薄包装和跨平台构建自动化。CLI 在未安装 Skill 时仍可完成全部核心流程。

read/write 正文经 HTTP + WebSocket/WSS 在内存中转，提供端使用固定门禁版本的 `isolated_runtime`。Relay 只保存大小、SHA-256、身份、状态和时间；仓库路径只保存在 Agent 所在机器的本地配置中。write 需要请求端确认和提供端逐次批准，只在明确 commit 的 detached worktree 中执行，不自动 commit、merge 或 push。

## 交给 Codex 安装

仓库提供了一份面向 AI Agent 的自包含安装手册：[PeerContext CLI 安装指南](./INSTALL.md)。可以把该 Markdown 文件直接交给同事的 Codex，并发送：

```text
请完整阅读附件中的 PeerContext CLI 安装指南，然后按顺序执行。需要全局安装时先让我确认；只安装 CLI 和 Skill，不要创建或加入 Project，不要启动 Relay 或 Agent。完成后只报告版本、Skill 状态和仍需人工处理的步骤。
```

CLI 通过 npm 安装，显式触发的 `peer-context` Skill 通过 `npx skills add` 从独立 HTTPS 地址安装。安装不会创建或加入 Project；实际用法由安装后的 Skill 自己说明。

- [最终 PRD](./docs/PRD.md)
- [Runtime Spike 结果](./spikes/codex-runtime/RESULT.md)
- [CLI JSON 与退出码契约](./docs/CLI_CONTRACT.md)
- [M3 Read 链路](./docs/M3_READ.md)
- [M4 Write 链路](./docs/M4_WRITE.md)
- [M5 自动化试点记录](./docs/PILOT_RESULTS.md)
