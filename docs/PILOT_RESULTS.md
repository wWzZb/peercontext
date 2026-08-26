# M5 试点验收记录

自动化预试点覆盖 PRD 的 10 类跨仓库 Case：API 契约、共享模型、认证规则、部署配置、内部 SDK、组件约束、跨仓库故障、迁移兼容、测试 fixture 和双边批准的跨仓库修改。

运行：

```bash
go test ./internal/relayclient -run TestTenCrossRepositoryPilotFixturesCloseInOneRequestWithSafetyGuards
```

结果：10/10 fixture 在一次请求、零澄清内完成；read/write ACL、生效中的双边 write 审批、detached worktree 元数据和 Relay 正文 canary 同时通过。测试使用 fake Codex，符合 PRD “除最终一次 smoke 外自动化测试不调用真实模型”的约束。

这份结果证明协议和产品流程能承载 10 类 Case，不冒充真实团队价值数据。真实团队仍需按相同字段记录请求次数、澄清次数、完成时间、结论是否可用和安全护栏；达到至少 8/10 后，才能确认 PRD 的价值假设。

## 最终真实 Runtime smoke

2026-08-26 在已门禁的 macOS arm64、`codex-cli 0.149.0-alpha.4.3` 上执行：

```bash
PEERCTX_REAL_CODEX_SMOKE=1 go test -v -count=1 -timeout 4m ./internal/codex -run '^TestRealIsolatedRuntimeSmoke$'
```

一次真实模型调用在 25.29 秒完成：授权 workspace canary 可读，workspace 文件未被修改，workspace 外 canary 被 Permission Profile 阻止，最终回答未泄漏禁止 canary。日常和 CI 测试继续默认跳过这条付费/联网 smoke，只使用 fake adapter。
