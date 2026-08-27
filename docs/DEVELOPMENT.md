# PeerContext 源码开发指南

## 开发边界

开始修改前完整阅读 [PRD](./PRD.md) 和 [Runtime Spike 结果](../spikes/codex-runtime/RESULT.md)。

- CLI 与 `peer-context` Skill 是独立层；CLI 不理解请求语义，不读仓库，不改写提示词。
- Skill 只显式触发，只调用公开 CLI，不参与后台服务或入站 Codex。
- v2 只支持 read，Runtime 固定为 `isolated_runtime`。
- 请求正文逐字节进入 Codex stdin；数据库与日志不保存请求、回答、私钥或仓库路径。
- 网络仅允许直接局域网，传输明文但所有请求、响应和帧都必须签名。

## 环境

产品门禁平台是 Apple Silicon Mac。开发需要 Go 1.26、Git；npm 包装与 Skill 站点检查需要 Node.js 20。真实 Runtime 验证还需要已登录的 Codex CLI。

```shell
git clone https://github.com/wWzZb/peercontext.git
cd peercontext
go mod download
go test ./...
go install ./cmd/peerctx
```

`0.2.0` 未发布，不使用 npm 或 GitHub Release 安装。

## 目录

```text
cmd/peerctx/                  CLI 入口
internal/cli/                 v2 公共命令与 JSON 输出
internal/service/             后台服务、Unix socket、LaunchAgent
internal/lanhost/             签名 LAN HTTP/WS、Project Store、Agent broker
internal/discovery/           _peerctx._tcp mDNS 广播与验证发现
internal/v2state/             独立 v2 本机状态与 Keychain
internal/codex/               read-only isolated_runtime
internal/skillbundle/         嵌入式 Skill
pkg/protocol/v2/              v2 协议对象、邀请与签名
.agents/skills/peer-context/  Skill 源文件
```

v1 Relay、credential、write 和 worktree 实现已从当前代码删除。旧用户状态保留在 v1 目录，不迁移也不删除。

## 运行与调试

```shell
go run ./cmd/peerctx version
go run ./cmd/peerctx _service-run   # 内部入口，仅调试
peerctx service status
peerctx service restart
```

正常产品流程不要直接运行内部入口。`project create/join` 和 `agent register` 会自动安装带 `RunAtLoad`、`KeepAlive` 的 LaunchAgent。

两个独立配置目录可以模拟两台 Mac；集成测试已经这样覆盖完整激活链路。测试专用环境变量 `PEERCTX_TEST_KEY_DIR`、`PEERCTX_DISABLE_MDNS` 和 `PEERCTX_ALLOW_UNSUPPORTED` 不属于公开产品配置。

## 测试

```shell
test -z "$(gofmt -l .)"
go test ./...
go test -race ./...
go vet ./...
GOOS=darwin GOARCH=arm64 CGO_ENABLED=0 go build ./cmd/peerctx
npm test
git diff --check
```

重点测试：

- `pkg/protocol/v2`：邀请、Ed25519、篡改、时钟偏差；
- `internal/lanhost`：nonce 重放、直接局域网限制、成员与 Agent；
- `internal/service`：双配置 create → join → register → ask、字节不变与不落盘；
- `internal/v2state`：v1 隔离和私钥存储。

## Skill

修改 `.agents/skills/peer-context` 后运行：

```shell
go generate ./internal/skillbundle
node scripts/generate-skill-site.js --write
go test ./internal/skillbundle
npm test
```

## 发布门禁

本轮不发布 npm 或 GitHub Release。正式发布前还需要在真实 Apple Silicon Mac 上完成 LaunchAgent 安装/重启/登录恢复、真实隔离 Codex smoke，以及 10 组双人首次激活试点中至少 8 组在 5 分钟内完成且无手工网络配置。
