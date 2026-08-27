# v1 Relay / Write 历史说明

PeerContext `0.1.1` 曾包含独立 Relay、Bearer credential、Agent ACL、双边 write 审批和 detached worktree。LAN-first `0.2.0` 已将这些产品方向和实现移出当前范围。

旧 v1 用户数据不会迁移或删除；当前程序只使用独立的 `peerctx/v2` 状态。需要回看旧验证证据时，以 Git 历史中的 `0.1.1` 文档和 [试点记录](../PILOT_RESULTS.md) 为准。
