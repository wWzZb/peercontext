# PeerContext

简体中文 | [English](./README.en.md)

PeerContext 是一个给开发者用的局域网协作工具：你创建 Project，把完整邀请发给同一局域网里的同事，同事加入后就能共享自己明确注册的本地仓库 Agent。没有公网服务器，也不用配置 Relay URL、端口、证书或静态 IP。

PeerContext 当前支持 Apple Silicon Mac、同一直接局域网和只读查询。

## 五分钟开始协作

两台 Mac 都需要完整安装 PeerContext（`peerctx` CLI 和 `peer-context` Skill）、安装并登录 Codex CLI，并连接同一个局域网。

推荐双方打开新的 Codex 对话并显式使用 `$peer-context`。创建者发送：

```text
$peer-context 创建一个名为 backend-team 的 PeerContext Project，把完整邀请和下一步给我。
```

把返回的完整邀请发给同事。同事发送：

```text
$peer-context 加入这个 PeerContext 邀请：peerctx2_...
```

同事进入准备共享的仓库，先让 Skill 展示公开 Manifest 和共享范围，确认后再注册：

```text
$peer-context 分析当前仓库并给出 Agent Manifest 候选，等我确认后再注册。
```

Agent 注册后自动上线，不需要保留终端。创建者发送：

```text
$peer-context 查看当前 Project 中的 Agent，并向最匹配的 Agent 询问订单查询接口的必填参数。
```

首次启动后台服务时，macOS 可能显示一次入站网络权限提示，这是正常激活步骤。

完整过程见 [Quickstart](./docs/user/QUICKSTART.md)。需要直接使用 CLI 时见 [命令参考](./docs/user/CLI_REFERENCE.md)。

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

完整安装包括 `peerctx` CLI 和 `peer-context` Skill，两者都需要安装。开始前请准备：

- Apple Silicon Mac；
- Go 1.26；
- Git；
- Node.js 18 或更高版本（包含 `npx`）；
- 已安装并登录的 Codex CLI。

### 自己安装

先安装 CLI：

```shell
git clone https://github.com/wWzZb/peercontext.git
cd peercontext
go install ./cmd/peerctx
```

如果终端找不到 `peerctx`，请把 `$(go env GOPATH)/bin` 加入 PATH。

再安装 Skill：

```shell
npx skills add https://wwzzb.github.io/peercontext/ \
  --skill peer-context \
  --agent codex \
  --global
```

`npx` 会临时运行安装器，不需要提前全局安装 `skills`。最后验证 CLI：

```shell
peerctx version
peerctx service status
```

尚未使用 PeerContext 时，`service status` 显示服务尚未运行是正常的。打开一个新的 Codex 对话并显式使用 Skill：

```text
$peer-context 帮我查看当前 PeerContext 状态
```

Skill 可以让 Codex 创建或加入 Project、生成邀请、分析并注册当前仓库、管理成员与后台服务，以及选择 Agent 发起 read。它只在你显式输入 `$peer-context` 时启用。

### 交给 Codex 安装

也可以把 [Codex 安装任务](./INSTALL.md) 直接交给 Codex，并发送：

```text
请完整阅读附件中的 PeerContext 安装任务并按顺序执行。执行 go install、全局安装 Skill 或修改 PATH 前先让我确认。只安装并验证 peerctx CLI 和 peer-context Skill，不要创建或加入 Project，也不要注册仓库。完成后只报告版本、Skill 安装状态和仍需人工处理的步骤。
```

`INSTALL.md` 是给 Codex 执行的自包含任务，不是另一份给人阅读的安装说明。参与开发请看 [源码开发指南](./docs/developer/DEVELOPMENT.md)。Skill 源文件位于 [`.agents/skills/peer-context`](./.agents/skills/peer-context)。

## 当前范围

包含：Apple Silicon Mac、同一直接局域网、Project 创建与邀请、成员、Project-wide read、Agent 自动上线、mDNS 恢复、签名明文传输、LaunchAgent 自动管理。

不包含：公网或跨子网连接、独立 Relay、write、审批、worktree、宿主迁移、离线队列、Linux、Windows、Intel Mac。

## 文档

完整入口见 [文档导航](./docs/README.md)。

给用户：

- 本页的 [安装说明](#安装)
- [第一次局域网协作](./docs/user/QUICKSTART.md)
- [CLI 命令参考](./docs/user/CLI_REFERENCE.md)

给开发者与产品维护者：

- [源码开发指南](./docs/developer/DEVELOPMENT.md)
- [开发路线图](./docs/developer/ROADMAP.md)
- [验证状态](./docs/developer/VALIDATION.md)
- [产品需求文档](./docs/product/PRD.md)
- [Runtime Spike 结果](./spikes/codex-runtime/RESULT.md)

## License

[MIT](./LICENSE)
