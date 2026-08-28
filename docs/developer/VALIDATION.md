# PeerContext 验证状态

本文区分“自动化已经证明的事实”和“发布前仍需由真实用户完成的验证”。测试通过不等于 LAN-first 产品体验已经验证。

## LAN v2 已有证据

自动化集成测试使用两套独立配置目录模拟两台 Mac，已经覆盖：

- `project create` 自动启动宿主；
- `project join` 只使用完整邀请；
- Agent 注册后自动上线；
- 第一次 `ask` 成功；
- 请求正文逐字节进入 fake Runtime；
- 请求、回答和仓库路径不进入 Host SQLite；
- 邀请过期与单次消费、签名篡改、nonce 重放和时钟偏差；
- 成员移除、Project-wide read、v1 状态隔离和非直接局域网拒绝；
- Host 地址变化后的 mDNS 重发现与伪造广播拒绝。

这些测试证明实现链路和安全检查可运行，但两套配置目录仍在同一台机器上，不能替代真实网络、macOS 防火墙和双人首次使用。

## Runtime 证据

2026-08-26 在 Apple Silicon Mac 上，使用 `codex-cli 0.150.0-alpha.8` 完成 3 次独立冷启动 Spike。认证复用、个人 Skills、MCP、历史、干净 HOME、真实 `codex exec` 和仓库外隔离共 24 项检查全部通过。完整环境、边界和残余风险见 [Runtime Spike 结果](../../spikes/codex-runtime/RESULT.md)。

正式 `IsolatedAdapter` 的真实 smoke 也验证了：授权仓库可读、仓库没有被修改、仓库外 canary 不可读、最终回答不泄漏禁止 canary。真实 smoke 需要联网并可能产生模型费用，因此默认不在日常测试和 CI 中执行。

2026-08-28 在 `0.2.0-alpha.1` 发布前，使用当前 `codex-cli 0.150.0-alpha.12.2` 再次执行真实 smoke，结果为 PASS（15.98 秒）。

运行方式：

```shell
PEERCTX_REAL_CODEX_SMOKE=1 go test -v -count=1 -timeout 4m ./internal/codex -run '^TestRealIsolatedRuntimeSmoke$'
```

## 发布前待验证

`0.2.0` 发布前仍需在真实 Apple Silicon Mac 上完成：

1. LaunchAgent 安装、重启和重新登录后的恢复；
2. macOS 首次入站网络权限提示；
3. 两台真实 Mac 的 create → join → register → ask；
4. Host IP 变化后的 mDNS 重发现；
5. 公司网络禁用 mDNS、Host 离线、Agent 离线、防火墙阻断和局域网隔离；
6. 10 组双人首次激活，至少 8 组在 5 分钟内完成，且不手工配置 URL、端口、证书、静态 IP 或常驻终端。

试点需要记录各阶段耗时、失败原因、是否需要帮助、首个回答是否可用，以及所有安全护栏结果。只有满足 [PRD](../product/PRD.md) 的发布门禁后，才能把状态改为已验证。

## v1 历史证据

`0.1.1` 曾使用 fake Codex 完成 10 类跨仓库 fixture，并在当时的 Relay/write 方向下通过相关安全测试。这只能证明旧协议流程能运行，不能证明 LAN v2 的激活体验或用户价值，也不能用作 `0.2.0` 的发布依据。需要复查时从 Git 历史或 `v0.1.1` tag 获取原始实现和测试，不在当前文档中继续展开已删除的产品方向。
