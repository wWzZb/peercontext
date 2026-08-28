# peerctx CLI 命令参考

本文面向直接使用 PeerContext 的用户，介绍当前 `peerctx 0.2.0-alpha.1` 提供的命令。完整使用流程见 [第一次局域网协作](./QUICKSTART.md)。

## 输出与帮助

普通调用默认输出适合人直接阅读的文本；Project、Member 和 Agent 列表会按列对齐。`ask` 成功时直接输出远端回答，不额外编码或改写字节。

Skill 或脚本应在 `peerctx` 后加全局 `--json`，例如 `peerctx --json agent list`。JSON 模式仍只输出一个稳定 envelope：成功写 stdout，失败写 stderr；`ask` 的回答位于 `data.response.answer`，按标准 JSON Base64 编码。管道输出没有颜色、动画或额外提示。

以下帮助命令只显示说明，不启动服务、不访问网络，也不读取 stdin：

```text
peerctx --help
peerctx help
peerctx project --help
peerctx project create --help
```

参数写错时会显示简短原因、对应调用方式和查看帮助的下一步。

## Project

```text
peerctx project create --name NAME [--member NAME]
peerctx project join INVITATION [--member NAME]
peerctx project list
peerctx project use PROJECT_ID
peerctx project invite create
peerctx project member list
peerctx project member remove MEMBER_ID
```

`create` 会自动启动后台服务并生成一个默认 10 分钟过期、只能使用一次的邀请。`join` 只需要完整邀请，不需要 Relay URL。

## Agent

```text
peerctx agent register REPOSITORY [--name NAME] [--summary TEXT]
                       [--tags CSV] [--capabilities CSV]
peerctx agent list
peerctx agent get AGENT
peerctx agent remove AGENT
```

只有明确执行 `agent register` 的本地 Git 仓库会被共享。默认名称为 `<成员名>/<仓库目录名>`，注册后由后台服务自动保持在线。

## Read

```text
peerctx ask AGENT [--timeout 5m] [--request-id ID]
```

请求正文从 stdin 读取。例如：

```shell
printf '%s\n' '订单查询接口需要哪些参数？' | peerctx ask Bob/backend
```

PeerContext 不解释、不扫描也不改写正文；它会把原始字节送入提供端的只读隔离 Codex Runtime。

## 后台服务与其他命令

```text
peerctx service start
peerctx service stop
peerctx service restart
peerctx service status
peerctx skills list
peerctx skills read peer-context [--file PATH]
peerctx version
```

正常流程会自动管理后台服务，`service` 命令主要用于诊断。

`service status` 会汇总安装与运行状态、LAN 监听、mDNS 发现、托管 Project 数量和本机 Agent 数量。

## 稳定错误与自救

人类模式会显示错误码、原因和可执行的下一步；JSON 模式通过 `error.code`、进程退出码和 `error.retryable` 提供同样的信息。常见错误不会再合并成模糊的“不可用”：

| 错误码 | 含义 | 建议 |
|---|---|---|
| `invite_expired` | 邀请已过期 | 请 Owner 创建新邀请 |
| `invite_consumed` | 邀请已被成功使用 | 请 Owner 创建新邀请 |
| `project_host_offline` | Project 创建者电脑离线或不可达 | 确认创建者电脑在线、未休眠且仍在同一 LAN |
| `agent_offline` | Agent 或其电脑离线 | 检查 Agent 所在电脑和后台服务 |
| `lan_discovery_unavailable` | 原地址失效且局域网发现不可用 | 检查两台 Mac 是否同网以及网络是否允许 mDNS |
| `invalid_invitation` | 邀请不完整、格式错误或被修改 | 重新复制完整邀请，或请求新邀请 |
| `host_identity_mismatch` / `signature_invalid` | 身份或签名无法验证 | 停止操作，并向 Owner 核对邀请和 Project |
| `request_replayed` | 签名 nonce 已使用 | 重新执行命令产生新请求，不复用旧报文 |
| `clock_skew` | 两台 Mac 的时间偏差过大 | 开启自动日期与时间后重试 |

诊断不会建议 Relay、公网地址、证书或静态 IP，也不会把邀请、私钥、问题、回答或本地仓库路径写进错误建议。
