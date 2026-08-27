# PeerContext 0.2.0 安装指南

## 当前状态

`0.2.0` 仍是未发布的 LAN-first v2 开发版本。本轮不要从 npm 或 GitHub Release 安装，使用源码安装。

支持环境：

- Apple Silicon Mac；
- Go 1.26；
- Git；
- 已安装并登录的 Codex CLI；
- 与协作者位于同一个直接连接的局域网。

## 交给 Codex 安装

需要执行 `go install` 或修改全局 PATH 时，Codex 应先取得用户许可。安装阶段只安装 CLI，不创建或加入 Project。

## 源码安装

```shell
git clone https://github.com/wWzZb/peercontext.git
cd peercontext
go install ./cmd/peerctx
```

如果终端找不到 `peerctx`，把 Go 的二进制目录加入 PATH：

```shell
export PATH="$(go env GOPATH)/bin:$PATH"
```

验证：

```shell
peerctx version
peerctx service status
```

`service status` 在尚未使用前可以返回 `running:false`。第一次 `project create`、`project join` 或 `agent register` 会自动安装并启动 macOS LaunchAgent，配置 `RunAtLoad` 和 `KeepAlive`。终端关闭后服务继续运行。

## Codex Skill（可选）

人直接调用 CLI 不需要 Skill。开发版本可从仓库复制 `.agents/skills/peer-context` 到个人 Skills 目录。安装后仍必须用 `$peer-context` 显式触发。

## 下一步

按 [第一次局域网协作](./docs/QUICKSTART.md) 完成 `create → join → register → ask`。
