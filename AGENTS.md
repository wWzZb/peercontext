# PeerContext development rules

开始改代码前，先完整阅读：

1. `docs/product/PRD.md`
2. `spikes/codex-runtime/RESULT.md`

规划尚未实现的体验时阅读 `docs/developer/ROADMAP.md`；Roadmap 不是当前能力来源。当前命令、模块和实现链路以代码与测试为准，不维护可由代码直接推导的重复文档。

用户文档回归规则：

- `README.md` 是唯一给人看的安装说明；必须同时保留“自己安装”和“交给 Codex 安装”两个入口。前者直接写完整步骤，后者链接 `INSTALL.md` 并提供可复制给 Codex 的提示词。不要把人类安装步骤拆到其他文档。
- `INSTALL.md` 是只供 Codex 执行的自包含安装任务，不作为用户阅读入口；它只安装和验证，不创建或加入 Project、不注册仓库，也不发起 read。
- 完整产品安装必须同时安装 `peerctx` CLI 和 `peer-context` Skill，不得把 Skill 标为可选。CLI 在未安装 Skill 时仍能独立运行是架构边界，不是面向用户的安装选项。
- `docs/user/` 只说明用户当前能用什么、需要什么以及怎么操作。
- 不在用户入口描述“开发分支”“开发版本”“未发布”“本轮”或发布门禁、试点进度等内部状态；这些内容放在 `docs/developer/` 或 `docs/product/`。
- 暂时只有一种安装或使用方式时，直接给出当前可用步骤，不向用户解释内部发布过程。

不可改变的边界：

- `peerctx` CLI 与 `peer-context` Skill 是两个独立层次。
- CLI 只负责基础设施，不理解请求语义，不读取仓库内容，不规划任务，不改写提示词。
- Skill 只调用公开 CLI；未安装 Skill 时 CLI 必须独立可用。
- Skill 必须仅显式触发，不参与后台服务、局域网协议或提供端入站 Codex。
- Runtime 模式由 Spike 锁定为 `isolated_runtime`，v2 仅支持 read。
- 入站请求正文必须逐字节送入 Codex stdin；数据库和日志不持久化请求、回答、私钥或仓库路径。
- Project 只允许同一直接局域网访问；传输虽为明文，但 HTTP 请求、响应和 WebSocket 消息必须验证 Ed25519 签名、时间和 nonce。
- PeerContext 产品不得运营或默认依赖公网协调、发现、STUN 或 Relay 服务；未来若研究公网互联，只能由用户自己的设备或用户自行提供的网络完成托管与直连。
- 不得重新引入独立 Relay、Bearer credential、write、审批、worktree、公网或跨子网回退。

实现以 PRD 2.0 的验收条件为准。发现 PRD 与现有 Codex 接口不一致时，先记录证据和影响，不要自行放宽安全边界。
