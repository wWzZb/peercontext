# PeerContext

PeerContext 让一个本地 Codex 向另一个开发者机器上的 Codex 请求私有仓库上下文，或发起经双方批准的修改，而不共享仓库权限。

项目分为两个独立层次：

- `peerctx` CLI：身份、发现、授权、传输、审批和隔离执行；
- `peer-context` Skill：请求端 Codex 的可选使用说明，只通过公开 CLI 工作。

CLI 不理解请求内容、不读取代码、不规划任务，也不修改提示词。Skill 未安装时 CLI 必须完整可用；Skill 不参与 Relay 或提供端 Codex 的运行。

当前状态：Runtime Spike 已通过，最终 PRD 已冻结，准备进入 MVP 开发。

- [最终 PRD](./docs/PRD.md)
- [Runtime Spike 结果](./spikes/codex-runtime/RESULT.md)
