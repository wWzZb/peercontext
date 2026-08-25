# Codex Runtime Spike

这个 Spike 是 PeerContext 正式开发前的硬门禁。它只回答一个问题：

> 在干净的 `HOME` 和 `CODEX_HOME` 中，仅复用宿主已有的 Codex 认证，能否稳定执行 `codex exec`，同时隔离个人配置、Skills、MCP、历史状态和其他仓库？

## 运行

```bash
go run ./spikes/codex-runtime --report ./spikes/codex-runtime/result.json
```

程序会执行 3 次互不复用目录的冷启动。每次都会：

1. 创建干净的 `HOME`、`CODEX_HOME` 和临时目录；
2. 只把宿主 `auth.json` 以受控符号链接映射到干净的 `CODEX_HOME`，允许 Codex 正常刷新认证；
3. 验证 `codex login status`；
4. 检查模型输入中没有已知个人 Skill；
5. 检查没有继承个人 MCP；
6. 用 `codex exec --ephemeral` 读取工作区内探针，并尝试读取工作区外探针；
7. 检查没有挂载宿主历史，也没有生成 `sessions`、`archived_sessions` 或 `history.jsonl` 会话历史。

Codex 可能在本轮隔离目录中新建 `state_*.sqlite` 运行状态库；只要它不是从宿主挂载或复制而来，且没有产生可复用会话历史，这不属于个人历史泄漏。

结果只有两种：

- `isolated_runtime`：3 次全部通过，可以采用隔离运行时；
- `full_host_runtime`：任意一项失败，MVP 直接使用提供端完整现有 Codex 环境，不做部分隔离。

结果文件不写入认证内容或其他密钥。
