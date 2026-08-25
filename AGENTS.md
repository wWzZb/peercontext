# PeerContext development rules

开始改代码前，先完整阅读：

1. `docs/PRD.md`
2. `spikes/codex-runtime/RESULT.md`

不可改变的边界：

- `peerctx` CLI 与 `peer-context` Skill 是两个独立层次。
- CLI 只负责基础设施，不理解请求语义，不读取仓库内容，不规划任务，不改写提示词。
- Skill 只调用公开 CLI；未安装 Skill 时 CLI 必须独立可用。
- Skill 必须仅显式触发，不参与 Relay，也不参与提供端入站 Codex 的运行。
- Runtime 模式已由 Spike 锁定为 `isolated_runtime`。
- 入站请求正文必须逐字节送入 Codex stdin；Relay 不持久化正文。
- 写请求必须经过请求端确认和提供端逐次批准，只在 detached worktree 中执行，不自动 commit、merge 或 push。

实现以 PRD 的验收条件为准。发现 PRD 与现有 Codex 接口不一致时，先记录证据和影响，不要自行放宽安全边界。
