# PeerContext 安装任务（供 Codex 执行）

你正在为用户安装 PeerContext。本文件只供 Codex 执行；不要把它改写成面向用户的安装说明。

## 目标和边界

- 完整安装必须同时包含 `peerctx` CLI 和 `peer-context` Skill，不能省略 Skill。
- 只完成安装和验证，不创建或加入 Project，不注册仓库，也不发起 read。
- 执行 `go install`、全局安装 Skill 或修改持久化 PATH 前，向用户展示准备执行的命令并取得明确许可。
- 不自动安装缺失的 Go、Git、Node.js 或 Codex CLI；缺少依赖时停止并报告。

## 1. 检查环境

运行只读检查：

```shell
uname -m
go version
git --version
node --version
codex --version
codex login status
```

仅在设备为 Apple Silicon Mac、Go 版本为 1.26、Node.js 为 18 或更高版本、Git 和 Codex CLI 均可用且 Codex 已登录时继续。

## 2. 安装 CLI

如果当前工作区已经是 PeerContext 仓库，直接使用它；否则在用户同意的位置执行：

```shell
git clone https://github.com/wWzZb/peercontext.git
cd peercontext
```

取得用户许可后安装：

```shell
go install ./cmd/peerctx
```

如果终端找不到 `peerctx`，先运行 `go env GOPATH` 确认二进制目录。修改持久化 PATH 前再次取得用户许可。

## 3. 安装 Skill

Skill 是完整安装的必选部分。取得用户许可后运行：

```shell
npx skills add https://wwzzb.github.io/peercontext/ --skill peer-context --agent codex --global
```

`npx` 会临时运行安装器，不需要预先全局安装 `skills`。不要用仓库中的开发源文件替代这条发布源安装命令。

## 4. 验证并报告

```shell
peerctx version
peerctx service status
```

尚未使用 PeerContext 时，`service status` 返回 `running:false` 是正常的。不要为了改变该状态而创建 Project 或启动业务流程。

完成后只向用户报告：CLI 版本、服务状态、Skill 是否安装成功，以及仍需用户处理的步骤。提醒用户新开一个 Codex 对话，并通过 `$peer-context` 显式使用 Skill。
