# Codex Runtime Spike 结果

## 结论

**PASS — PeerContext MVP 采用隔离 Runtime。**

在 `codex-cli 0.149.0-alpha.4.3`、macOS arm64 上，连续 3 次冷启动均成功：

- 使用全新的 `HOME` 和 `CODEX_HOME`；
- 只通过符号链接复用宿主 `auth.json`；
- `codex login status` 能识别现有 ChatGPT 登录；
- 真实 `codex exec --ephemeral --json -` 能调用模型并返回随机探针；
- 已知个人 Skills 未进入模型上下文；
- 宿主个人 MCP 未加载；
- 宿主历史未挂载，也未生成可复用的会话历史；
- 授权工作区可读，工作区外随机探针不可读。

因此正式实现锁定为：每个入站请求使用干净运行目录，只复用提供端已有认证，不加载其个人配置、Skills、MCP、插件、hooks 或历史。

## 运行信息

| 项目 | 结果 |
|---|---|
| 日期 | 2026-08-25 |
| 平台 | darwin/arm64 |
| Codex | `codex-cli 0.149.0-alpha.4.3` |
| 认证桥接 | `host_auth_json_symlink` |
| 冷启动次数 | 3 |
| 总结论 | `isolated_runtime` |

## 三轮结果

| 断言 | Run 1 | Run 2 | Run 3 |
|---|---:|---:|---:|
| 只映射认证 | PASS | PASS | PASS |
| 登录状态可复用 | PASS | PASS | PASS |
| 个人 Skills 隔离 | PASS | PASS | PASS |
| 个人 MCP 隔离 | PASS | PASS | PASS |
| 真实 `codex exec` | PASS | PASS | PASS |
| 其他仓库隔离 | PASS | PASS | PASS |
| 个人历史隔离 | PASS | PASS | PASS |
| 干净 `HOME` | PASS | PASS | PASS |

每轮完整脱敏结果见 [result.json](./result.json)。

## 关于 `state_5.sqlite`

Codex 会在每轮全新的隔离 `CODEX_HOME` 中创建自己的 `state_5.sqlite`。这不是宿主个人历史泄漏：该文件不是从宿主复制或挂载而来，运行时也没有生成 `sessions`、`archived_sessions` 或 `history.jsonl`。

本门禁验证的是“宿主个人历史是否进入入站运行时、入站请求是否写回宿主会话历史”，而不是禁止 Codex 在临时目录中创建自身运行状态。整个临时目录会在请求结束后删除。

## 适用范围与残余风险

- 当前实测的是本机已有 `auth.json` 的认证桥接；其他认证后端未被本次结果覆盖。
- 宿主认证可能被 Codex 正常刷新，这是唯一允许触达的宿主状态。
- Permission Profile 和 Codex CLI 都可能随版本变化。正式 CLI 的 `doctor` 必须验证版本、认证桥接和权限配置；不满足时拒绝启动，不静默切换运行模式。
- macOS arm64 已实测；Linux 和 Windows 需要在 CI/发布前补平台测试。

## 重跑

```bash
go run ./spikes/codex-runtime --report ./spikes/codex-runtime/result.json
```
