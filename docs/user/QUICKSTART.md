# 第一次局域网协作

目标：两位同事在同一个局域网里，从零完成一次 read，不配置任何 URL、端口、证书或常驻终端。

## 开始之前

两台 Apple Silicon Mac 都需要按 [README 安装说明](../../README.md#安装) 完整安装 `peerctx` CLI 和 `peer-context` Skill、安装并登录 Codex CLI，并连接同一个公司或家庭局域网。创建者电脑需要保持登录、在线且不休眠。

以下流程由双方在新的 Codex 对话中显式使用 `$peer-context` 完成。Skill 会调用公开 CLI、解析 JSON 并展示结果；需要直接操作 CLI 时见 [命令参考](./CLI_REFERENCE.md)。

## 1. 创建 Project

创建者发送：

```text
$peer-context 创建一个名为 backend-team 的 PeerContext Project，把完整邀请和下一步给我。
```

成员名默认取全局 Git 用户名，没有时取 macOS 用户名。需要指定名称时在消息中告诉 Skill。

Skill 会自动启动后台服务并返回一个以 `peerctx2_` 开头的完整邀请。原样发给同事。邀请只能使用一次，默认 10 分钟过期。

## 2. 加入 Project

同事发送：

```text
$peer-context 加入这个 PeerContext 邀请：peerctx2_...
```

不需要 Relay URL。邀请先尝试当前直连地址；如果创建者 IP 已变化，PeerContext 会在局域网重新发现宿主并核对宿主公钥。

## 3. 注册本地 Agent

同事进入一个明确愿意共享查询的仓库，然后发送：

```text
$peer-context 分析当前仓库并给出 Agent Manifest 候选，等我确认后再注册。
```

Skill 会展示本地路径、公开的 `name`、`summary`、`tags`、`capabilities`、Project 全员可只读查询的影响，以及局域网正文未加密的限制。确认这些信息后，再明确回复同意注册。

默认名称是 `<成员名>/<目录名>`，例如 `Bob/backend`；可以在确认前要求修改。

注册成功时 Agent 已经自动上线。后台服务由 macOS LaunchAgent 托管，不需要保持终端打开。

Project 内所有成员都可以 read 明确注册的 Agent，包括之后新加入的成员。仓库不会因为加入 Project 自动共享。

## 4. 发出第一次 read

创建者发送：

```text
$peer-context 查看当前 Project 中的 Agent，并向最匹配的 Agent 询问订单查询接口的必填参数。请区分已确认事实和不确定内容。
```

Skill 会选择公开 Manifest 最匹配的 Agent、发送最少必要上下文，并解析成功结果中的回答。基础设施错误不会被当成仓库事实。

## 常见问题

### macOS 网络权限提示

首次接收入站连接时允许 `peerctx` 接收本地网络连接。无需手动修改防火墙设置。

### 找不到 Project 宿主

确认创建者电脑在线、未休眠，并且两台电脑仍在同一个直接局域网。公司网络禁用 mDNS 时，邀请里的当前 IP 仍可使用；如果 IP 已变化，PeerContext 会明确提示 mDNS 可能被禁用，此时重新创建邀请通常可恢复当前直连地址。

### Agent offline

确认 Agent 所在电脑在线，并运行 `peerctx service status`。正常情况不需要手动 `agent serve`；可用 `peerctx service restart` 做诊断。

### 0.1.1 数据不兼容

v1 Project 和 credential 不迁移，也不会删除。重新创建或加入 LAN v2 Project。
