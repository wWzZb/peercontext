# PeerContext

PeerContext 是一个给开发者用的局域网协作工具：你创建 Project，把完整邀请发给同一局域网里的同事，同事加入后就能共享自己明确注册的本地仓库 Agent。没有公网服务器，也不用配置 Relay URL、端口、证书或静态 IP。

当前 `0.2.0` 是未发布的 LAN-first v2 开发版本，只支持 Apple Silicon Mac 和 read。

## 五分钟开始协作

两台 Mac 都需要安装 PeerContext、安装并登录 Codex CLI，并连接同一个局域网。

创建者执行：

```shell
peerctx project create --name backend-team
```

把 JSON 中 `data.invitation` 的完整内容发给同事。同事执行：

```shell
peerctx project join 'peerctx2_...'
peerctx agent register /absolute/path/to/repository
```

Agent 注册后自动上线，不需要保留终端。创建者查看并询问：

```shell
peerctx agent list
printf '%s\n' '请告诉我订单查询接口的必填参数。' | peerctx ask MEMBER/REPOSITORY
```

首次启动后台服务时，macOS 可能显示一次入站网络权限提示，这是正常激活步骤。

完整过程见 [Quickstart](./docs/QUICKSTART.md)。

## 它怎么工作

```text
创建者 Mac（Project 宿主）                 同事 Mac

peerctx CLI                               peerctx CLI
    │ Unix socket                             │ Unix socket
    ▼                                         ▼
用户级后台服务  ◄────── 已签名的局域网连接 ────► 用户级后台服务
    │                                             │
Project SQLite                            本地 Agent + 隔离 Codex
```

- 创建者电脑托管 Project；创建者离线或休眠时 Project 不可用。
- 邀请先使用其中的当前地址，地址变化后通过 `_peerctx._tcp` mDNS 重新发现，并用宿主公钥排除伪造广播。
- 网络内容当前是明文，因此同一局域网的被动观察者可能看到问题和回答；所有 HTTP、响应和 WebSocket 消息都使用 Ed25519 签名，防止篡改、重放和凭证盗用。
- 每个成员的 Project 私钥只存 macOS Keychain。网络上没有可复用的 Bearer token。
- 宿主数据库只保存身份、Agent 和请求元数据，不保存问题、回答、私钥或仓库路径。
- 请求正文逐字节进入 `isolated_runtime` 的 Codex stdin；CLI 不读取仓库、不解释请求，也不改写提示词。

## 安装

`0.2.0` 尚未发布到 npm，也没有 GitHub Release。本轮请从源码安装：

```shell
git clone https://github.com/wWzZb/peercontext.git
cd peercontext
go install ./cmd/peerctx
peerctx version
```

需要 Go 1.26、Git、Codex CLI 和已完成的 Codex 登录。详细说明见 [INSTALL.md](./INSTALL.md) 和 [源码开发指南](./docs/DEVELOPMENT.md)。

## Codex Skill

人直接使用 CLI 不需要 Skill。只有希望请求端 Codex 主动选择 Agent、组织最小问题并发送 read 时，才安装 `peer-context` Skill；它必须通过 `$peer-context` 显式触发，不会参与后台服务或入站 Codex。

开发版本的 Skill 源文件位于 [`.agents/skills/peer-context`](./.agents/skills/peer-context)。

## 当前范围

包含：Apple Silicon Mac、同一直接局域网、Project 创建与邀请、成员、Project-wide read、Agent 自动上线、mDNS 恢复、签名明文传输、LaunchAgent 自动管理。

不包含：公网或跨子网连接、独立 Relay、write、审批、worktree、宿主迁移、离线队列、Linux、Windows、Intel Mac。

## 文档

- [产品需求文档 2.0 Draft](./docs/PRD.md)
- [第一次局域网协作](./docs/QUICKSTART.md)
- [安装指南](./INSTALL.md)
- [CLI 公共契约](./docs/CLI_CONTRACT.md)
- [LAN Read 链路](./docs/M3_READ.md)
- [源码开发指南](./docs/DEVELOPMENT.md)
- [试点记录](./docs/PILOT_RESULTS.md)
- [Runtime Spike 结果](./spikes/codex-runtime/RESULT.md)

## License

[MIT](./LICENSE)
