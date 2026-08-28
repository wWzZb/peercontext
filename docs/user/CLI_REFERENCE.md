# peerctx CLI 命令参考

本文面向直接使用 PeerContext 的用户，介绍当前 `peerctx 0.2.0-alpha.1` 提供的命令。完整使用流程见 [第一次局域网协作](./QUICKSTART.md)。

## 当前输出方式

当前所有命令都输出一个 JSON 对象：成功写入 stdout，失败写入 stderr。创建 Project 后，从 `data.invitation` 复制完整邀请；`ask` 的回答位于 `data.response.answer`，内容按标准 JSON Base64 编码。

更适合人阅读的默认输出、`--json` 和完整 `--help` 仍在规划中，尚未实现。进展见 [开发路线图](../developer/ROADMAP.md)。

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
