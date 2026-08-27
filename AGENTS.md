# PeerContext development rules

开始改代码前，先完整阅读：

1. `docs/PRD.md`
2. `spikes/codex-runtime/RESULT.md`

不可改变的边界：

- `peerctx` CLI 与 `peer-context` Skill 是两个独立层次。
- CLI 只负责基础设施，不理解请求语义，不读取仓库内容，不规划任务，不改写提示词。
- Skill 只调用公开 CLI；未安装 Skill 时 CLI 必须独立可用。
- Skill 必须仅显式触发，不参与后台服务、局域网协议或提供端入站 Codex。
- Runtime 模式由 Spike 锁定为 `isolated_runtime`，v2 仅支持 read。
- 入站请求正文必须逐字节送入 Codex stdin；数据库和日志不持久化请求、回答、私钥或仓库路径。
- Project 只允许同一直接局域网访问；传输虽为明文，但 HTTP 请求、响应和 WebSocket 消息必须验证 Ed25519 签名、时间和 nonce。
- 不得重新引入独立 Relay、Bearer credential、write、审批、worktree、公网或跨子网回退。

实现以 PRD 2.0 的验收条件为准。发现 PRD 与现有 Codex 接口不一致时，先记录证据和影响，不要自行放宽安全边界。
