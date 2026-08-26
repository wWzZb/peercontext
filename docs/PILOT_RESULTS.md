# M5 试点验收记录

自动化预试点覆盖 PRD 的 10 类跨仓库 Case：API 契约、共享模型、认证规则、部署配置、内部 SDK、组件约束、跨仓库故障、迁移兼容、测试 fixture 和双边批准的跨仓库修改。

运行：

```bash
go test ./internal/relayclient -run TestTenCrossRepositoryPilotFixturesCloseInOneRequestWithSafetyGuards
```

结果：10/10 fixture 在一次请求、零澄清内完成；read/write ACL、生效中的双边 write 审批、detached worktree 元数据和 Relay 正文 canary 同时通过。测试使用 fake Codex，符合 PRD “除最终一次 smoke 外自动化测试不调用真实模型”的约束。

这份结果证明协议和产品流程能承载 10 类 Case，不冒充真实团队价值数据。真实团队仍需按相同字段记录请求次数、澄清次数、完成时间、结论是否可用和安全护栏；达到至少 8/10 后，才能确认 PRD 的价值假设。

## 初始真实 Runtime smoke

2026-08-26 在已门禁的 macOS arm64、`codex-cli 0.149.0-alpha.4.3` 上执行：

```bash
PEERCTX_REAL_CODEX_SMOKE=1 go test -v -count=1 -timeout 4m ./internal/codex -run '^TestRealIsolatedRuntimeSmoke$'
```

一次真实模型调用在 25.29 秒完成：授权 workspace canary 可读，workspace 文件未被修改，workspace 外 canary 被 Permission Profile 阻止，最终回答未泄漏禁止 canary。日常和 CI 测试继续默认跳过这条付费/联网 smoke，只使用 fake adapter。

## 当前 Codex 版本复验

2026-08-26 在同一 macOS arm64 门禁环境中，使用当前 `codex-cli 0.150.0-alpha.8` 重新执行 3 次独立冷启动 Spike。认证复用、个人 Skills、MCP、历史、干净 HOME、真实 `codex exec` 和仓库外隔离共 24 项检查全部通过，结果见 [Runtime Spike 结果](../spikes/codex-runtime/RESULT.md)。

随后使用正式 `IsolatedAdapter` 重跑上述真实 smoke，在 12.14 秒内通过：授权 workspace canary 可读，read workspace 未被修改，workspace 外 canary 被阻止，最终回答未泄漏禁止 canary。

为避免 Codex 高频更新导致持续维护版本白名单，Runtime 后续改为启动时能力门禁，并在当前 `0.150.0-alpha.8` 上再次实跑通过。门禁不比较版本字符串，而是实际检查必需 exec 参数、认证桥接、read 模式不可写且不可越界，以及 write 模式只能写 workspace、Git 元数据只读、仓库外不可读写。能力门禁落地后的最终正式 smoke 在 18.64 秒内通过；完整 3 轮 Spike 继续保留为平台和发布证据。

`peerctx 0.1.1` 发布候选在相同环境中再次执行正式 smoke，并于 16.38 秒内通过。
