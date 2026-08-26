# M2 Relay 接口

M2 Relay 暴露 `/v1` HTTP API 和 Agent WebSocket。HTTP 使用 `Authorization: Bearer <project credential>`；创建 Project 和消费邀请是仅有的两个无 credential 入口。

主要路由：

```text
POST   /v1/projects
POST   /v1/project/join
POST   /v1/project/invites
GET    /v1/project/members
POST   /v1/project/members/{member}/promote
DELETE /v1/project/members/{member}

GET    /v1/credential/status
POST   /v1/credential/rotate
DELETE /v1/credential
DELETE /v1/credentials/{credential}

POST   /v1/agents
GET    /v1/agents
GET    /v1/agents/{agent}
POST   /v1/agents/{agent}/access
GET    /v1/agents/{agent}/serve   # WebSocket / WSS

POST   /v1/requests
GET    /v1/requests/pending
GET    /v1/requests/{request}
DELETE /v1/requests/{request}
POST   /v1/requests/{request}/approve
POST   /v1/requests/{request}/deny
```

SQLite 只保存 Project、Member、Owner 标记、credential/invite 哈希、Agent Manifest、ACL 和请求审计元数据。`request_metadata` 只有 request ID、双方身份、模式、状态、时间、正文大小和 SHA-256，没有正文或回答列。

Agent 在线状态来自当前进程内活跃的 WebSocket 连接，不作为可恢复状态写入 SQLite。连接断开后立即显示离线；M2 不建立正文队列。M3 会在同一连接上增加 read 请求的内存转发，但不会改变这条持久化边界。

M3/M4 已在这条连接上增加 `request`、`response`、`failure` 和 `cancel` 消息；write 的明确 base commit 只在批准后的内存消息中出现。详细契约见 [M3 Read 链路](./M3_READ.md) 和 [M4 Write 链路](./M4_WRITE.md)。
