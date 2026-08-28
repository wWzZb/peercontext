# PeerContext Windows 原生适配计划

## 文档状态

- 状态：**Planned — 尚未实现，不代表当前已经支持 Windows**
- 目标平台：Windows 11 x64（`windows/amd64`）
- 运行方式：原生 Windows + 原生 Codex CLI，不以 WSL 作为正式支持路径
- 产品范围：继续使用 LAN protocol v2、`isolated_runtime` 和 read-only
- 协作方式：仓库侧完成实现和自动化；真实 Windows 设备负责运行门禁并回传脱敏证据

当前 PRD 仍把 Windows 列在 `0.2.0` MVP 范围外。在本计划的 Runtime、安全和真实双机门禁全部通过前，README、安装指南和 CLI 不得宣称 Windows 已受支持。若决定把 Windows 纳入正式版本，先根据验证结果更新 PRD、风险说明和发布门禁。

## 目标与非目标

首个 Windows 版本需要完整支持现有公开流程：

```text
project create → project join → agent register → agent list → ask
service start|stop|restart|status
```

Windows、macOS 之间必须可以互相创建 Project、加入、共享 Agent 和发起 read，协议字段、签名和 CLI 契约保持一致。

本轮不增加：

- Relay、公网、跨子网或自动 VPN 回退；
- write、审批、worktree、commit、push 或 PR；
- WSL、Windows 10、Windows ARM64 或 Windows Server 的正式支持；
- Windows 专属协议或 Windows 专属 CLI 命令；
- 为了通过 Windows 测试而放宽仓库外读取、仓库写入或网络隔离。

## 已确定的平台方案

| 能力 | macOS | Windows 11 x64 |
|---|---|---|
| 用户级后台服务 | LaunchAgent | 当前用户的计划任务，登录时启动并在失败后重启 |
| CLI 与后台服务通信 | 权限为 `0600` 的 Unix socket | 仅当前用户 SID 可访问的 Named Pipe |
| Project 私钥 | macOS Keychain | Windows Credential Manager |
| 状态目录保护 | POSIX 权限 | Windows DACL，仅当前用户和必要的系统主体可访问 |
| Codex 隔离 | 已验证的 macOS permission profile | Windows 原生 `elevated` sandbox，通过真实 Spike 后才启用 |
| 进程超时清理 | Context 取消 | Context 取消 + Windows Job Object，确保清理整个子进程树 |
| 局域网发现 | HashiCorp mDNS | 先验证现有实现；失败后再评估 Windows DNS-SD 适配 |

OpenAI 官方文档把 Windows 11 作为推荐基线，并说明 `elevated` sandbox 强于 `unelevated` sandbox。PeerContext 第一版 Windows Runtime 只以 `elevated` 作为候选发布基线；`unelevated` 可以用于定位兼容性问题，但不能在没有独立安全证据时静默回退。

## 分阶段执行

### W0：Windows 环境验证

负责人：Windows 设备开发者。

在 Windows 11 x64 的 PowerShell 中克隆仓库并运行：

```powershell
git clone https://github.com/wWzZb/peercontext.git
cd peercontext
go build ./cmd/peerctx
go test ./...
codex --version
codex login status
```

环境要求：

- Windows 11 已更新；
- Go 版本满足 `go.mod`；
- Git、Node.js 20 和 Codex CLI 已安装；
- Codex 已登录；
- Windows 原生 `elevated` sandbox 已成功完成初始化；
- 测试仓库不能包含真实密钥或生产数据。

不得回传：`auth.json`、`.sandbox-secrets`、完整环境变量、Project/Invite 私钥、真实邀请、公司内部源码、请求正文或回答正文。用户名、机器名、本地路径和 IP 地址应脱敏。

通过条件：仓库能够编译和运行测试，Codex 已登录，`elevated` sandbox 没有阻塞性的企业策略错误。确认基础环境后，再处理 Windows 后台服务、本机通信和 Codex 隔离。

### W1：建立平台边界

负责人：仓库实现。

改造内容：

1. 把 `LaunchAgent` 抽象为通用的用户级 `ServiceManager`；
2. 使用 `*_darwin.go` 和 `*_windows.go` 隔离后台服务实现；
3. 把本地 IPC 的监听、拨号和地址生成抽成平台接口；
4. 把安全目录创建、文件权限和原子替换抽成平台接口；
5. 把 Codex 的认证桥接、最小环境和进程清理抽成平台接口；
6. 移除公共 CLI 对 `LaunchAgent` 具体类型的直接依赖，但不改变 CLI 命令和 JSON 契约。

通过条件：

- macOS 的现有测试全部通过，LaunchAgent 行为没有倒退；
- `GOOS=windows GOARCH=amd64 CGO_ENABLED=0 go build ./cmd/peerctx` 成功；
- Windows 产品路径不依赖 `PEERCTX_ALLOW_UNSUPPORTED`；
- 协议 v2、签名格式和数据库结构没有无依据的变化。

### W2：Windows 后台服务、本地 IPC 与状态安全

负责人：仓库实现 + Windows 设备验证。

后台服务：

- 通过当前用户的计划任务执行 `peerctx.exe _service-run`；
- `service start` 注册、启用并立即运行计划任务；
- `service stop` 停止进程并阻止计划任务立即拉起；
- `service restart` 完成一次可观察的停止和重新启动；
- 用户重新登录后自动恢复；
- 可执行文件路径含空格或中文时仍能正确启动；
- 不要求保存 Windows 用户密码，也不提升为系统级服务。

本地 IPC：

- 使用每用户唯一的 Named Pipe；
- Pipe DACL 只允许当前用户 SID；
- 继续复用当前 HTTP control handler，不开放本地 TCP 控制端口；
- 第二个 Windows 用户连接时必须失败；
- 僵尸 Pipe、后台进程崩溃和重复 `start` 必须能恢复。

状态与凭据：

- Project 私钥写入 Windows Credential Manager；
- 状态、SQLite 和日志目录使用显式 DACL；
- 状态文件使用支持覆盖且抗中断的 Windows 原子替换；
- 私钥、邀请私钥、请求、回答和仓库路径不得进入日志或 Host SQLite；
- `USERNAME` 只作为 Git `user.name` 缺失时的成员显示名回退。

通过条件：`service start|status|restart|stop`、重新登录恢复、跨用户拒绝和多次状态保存均在真实 Windows 上通过。

### W3：Windows `isolated_runtime` Spike

负责人：仓库实现 Spike；Windows 设备运行真实门禁。

在正式 Adapter 解锁 Windows 前，先扩展 Runtime Spike 并生成独立的 Windows 结果。Spike 不得直接假设 macOS 的 `auth.json` 符号链接方案在 Windows 可用。

需要验证和决定：

- Windows 上实际使用的 Codex 认证存储与安全桥接方式；
- 全新的临时 `HOME`、`USERPROFILE`、`CODEX_HOME`、`TEMP` 和 `TMP`；
- Windows 启动 Codex 所需的最小 `PATH`、`SystemRoot`、`ComSpec` 等环境；
- permission profile 能否在 `elevated` sandbox 下只读授权仓库并禁用网络；
- Windows Job Object 是否能在超时、取消和解析失败时清理全部子进程；
- 临时目录、认证桥接和运行状态是否在请求结束后可靠清除。

每次冷启动都必须验证：

1. `codex login status` 能识别宿主登录；
2. 真实 `codex exec --ephemeral --json -` 返回最终 Agent message；
3. 请求 stdin 字节没有被 PeerContext 增删或换行转换；
4. 授权仓库可读；
5. 授权仓库不可写；
6. 仓库外随机 canary 不可读；
7. 网络不可用；
8. 个人 Skill、MCP、插件、hooks、规则和历史未加载；
9. 入站 Runtime 不能再次执行 `peerctx ask`；
10. 请求结束后没有残留进程和临时凭据。

通过条件：连续 3 次冷启动全部通过，并把脱敏的平台、Codex 版本、断言和结论写入 Spike 结果。任何一项失败都保持 `runtime_platform_ungated`，不得切换到非隔离模式，也不得静默使用 `unelevated`。

### W4：局域网、mDNS 与 Windows 防火墙

负责人：仓库实现 + 至少一台 Windows 和一台 Mac 的真实网络验证。

验证内容：

- Windows 能监听 Project Host HTTP/WS，并只接受直接连接局域网来源；
- 现有 mDNS 可以在 Windows 广播、发现和关闭；
- Host 地址变化后能重新发现，并继续验证固定 Host 公钥；
- Windows Defender 防火墙只允许专用网络上的必要入站流量；
- 首次使用不要求用户填写端口、静态 IP、Relay URL 或证书；
- WSL、Hyper-V、VPN、Tailscale、Teredo 等虚拟或隧道网卡不会被误当成产品支持的直接局域网；
- mDNS 被禁用且邀请地址失效时返回明确诊断，不回退公网或跨子网。

真实组合：

| Project Host | Agent 提供方 | 请求方 | 必须通过 |
|---|---|---|---|
| Windows | Windows | Windows | 完整 create → join → register → ask |
| Windows | macOS | Windows | Windows Host 与 Mac Agent |
| macOS | Windows | macOS | Mac Host 与 Windows Agent |
| macOS | macOS | Windows | Windows 作为请求方 |

所有组合都必须继续验证 Ed25519 签名、nonce、防重放、Host 公钥固定和请求/回答不落盘。

### W5：Windows 自动化、打包和安装体验

负责人：仓库实现 + Windows 设备安装验证。

改造内容：

- GitHub Actions 增加 `windows-latest` 原生单元和集成测试；
- 构建矩阵增加 `windows/amd64` 和 `peerctx.exe`；
- npm 包装支持 `win32-x64`、`.exe` 文件名和 SHA-256 校验；
- Windows 测试不使用 POSIX shell fixture；
- 安装文档明确 Go、Git、Node、Codex、Windows sandbox 和防火墙要求；
- 正式公开分发前评估代码签名和 SmartScreen 体验；
- `peer-context` Skill 继续只调用相同公开 CLI，不增加平台内部接口。

通过条件：干净 Windows 11 用户能够按安装文档完成安装，关闭终端和重新登录后服务仍可用，打包产物校验失败时拒绝执行。

### W6：发布门禁与文档转正

负责人：产品维护者 + 真实双人试点。

必须完成：

- Windows/Windows、Windows/macOS 和 macOS/Windows 双机真实测试；
- 登录恢复、休眠恢复、IP 变化、防火墙阻断、mDNS 禁用、Host 离线和 Agent 离线；
- 带空格、中文和较长路径的 Git worktree；
- 请求超时、Codex 异常退出和进程树清理；
- 当前用户可访问、其他本机用户不可访问 Named Pipe 和 Project 私钥；
- 安全护栏保持 0 违规。

只有 W0–W6 全部通过后，才执行：

1. 更新 PRD 的首发/支持平台和验收矩阵；
2. 更新 README、安装指南、Quickstart、CLI Reference、验证状态和 Skill 文档；
3. 删除 Windows 的 `Planned` 标记；
4. 发布包含 Windows 产物的版本。

## 协作与证据格式

真实 Windows 结果按一次运行一个目录保存，建议结构：

```text
artifacts/windows-port/<date>-<phase>/
  environment.txt
  test-summary.txt
  result.json
```

仓库只提交脱敏后的结构化结果和结论；完整诊断日志留在测试机器本地。发生失败时优先回传：命令、退出码、稳定错误码、Windows/Codex/PeerContext 版本和已经脱敏的最小 stderr。不要回传用户目录、认证文件、邀请、请求或回答。

每个阶段采用同一交接方式：

1. 仓库侧提供一个小范围变更和准确运行命令；
2. Windows 设备在干净 PowerShell 中执行；
3. 回传脱敏结果；
4. 仓库侧根据证据修复；
5. 当前阶段门禁全绿后再进入下一阶段。
