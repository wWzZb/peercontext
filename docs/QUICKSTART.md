# 第一次局域网协作

目标：两位同事在同一个局域网里，从零完成一次 read，不配置任何 URL、端口、证书或常驻终端。

## 开始之前

两台 Apple Silicon Mac 都需要安装 `peerctx 0.2.0` 源码版本、安装并登录 Codex CLI，并连接同一个公司或家庭局域网。创建者电脑需要保持登录、在线且不休眠。

## 1. 创建 Project

创建者执行：

```shell
peerctx project create --name backend-team
```

成员名默认取全局 Git 用户名，没有时取 macOS 用户名；可用 `--member NAME` 覆盖。

命令会自动启动后台服务并返回一个以 `peerctx2_` 开头的完整邀请。复制 JSON 中的 `data.invitation`，原样发给同事。邀请只能使用一次，默认 10 分钟过期。

## 2. 加入 Project

同事执行：

```shell
peerctx project join 'peerctx2_...'
```

不需要 Relay URL。邀请先尝试当前直连地址；如果创建者 IP 已变化，PeerContext 会在局域网重新发现宿主并核对宿主公钥。

## 3. 注册本地 Agent

同事把一个明确愿意共享查询的仓库注册为 Agent：

```shell
peerctx agent register /absolute/path/to/backend
```

默认名称是 `<成员名>/<目录名>`，例如 `Bob/backend`。也可提供说明：

```shell
peerctx agent register /absolute/path/to/backend \
  --name Bob/backend \
  --summary 'Backend API contracts and business rules' \
  --tags backend,api
```

注册成功时 Agent 已经自动上线。后台服务由 macOS LaunchAgent 托管，不需要保持终端打开。

Project 内所有成员都可以 read 明确注册的 Agent，包括之后新加入的成员。仓库不会因为加入 Project 自动共享。

## 4. 发出第一次 read

创建者执行：

```shell
peerctx agent list
printf '%s\n' \
  '请检查当前仓库并告诉我订单查询接口的必填参数。请区分已确认事实和不确定内容。' \
  | peerctx ask Bob/backend
```

成功 JSON 的 `data.response.answer` 是标准 Base64 编码的原始回答字节。CLI 的 JSON 结构保持机器可读；调用程序负责按 JSON Base64 规则解码。

## 常见问题

### macOS 网络权限提示

首次接收入站连接时允许 `peerctx` 接收本地网络连接。无需手动修改防火墙设置。

### 找不到 Project 宿主

确认创建者电脑在线、未休眠，并且两台电脑仍在同一个直接局域网。公司网络禁用 mDNS 时，邀请里的当前 IP 仍可使用；如果 IP 已变化，PeerContext 会明确提示 mDNS 可能被禁用，此时重新创建邀请通常可恢复当前直连地址。

### Agent offline

确认 Agent 所在电脑在线，并运行 `peerctx service status`。正常情况不需要手动 `agent serve`；可用 `peerctx service restart` 做诊断。

### 0.1.1 数据不兼容

v1 Project 和 credential 不迁移，也不会删除。重新创建或加入 LAN v2 Project。
