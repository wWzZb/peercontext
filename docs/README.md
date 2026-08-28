# PeerContext 文档

文档按读者分开。第一次使用 PeerContext，不需要阅读产品设计和内部架构。

## 给用户

建议按下面的顺序阅读：

1. [README 安装说明](../README.md#安装)
2. [第一次局域网协作](./user/QUICKSTART.md)
3. [CLI 命令参考](./user/CLI_REFERENCE.md)

## 给开发者与 AI coding

- [源码开发指南](./developer/DEVELOPMENT.md)：环境、调试、测试和发布门禁。
- [验证状态](./developer/VALIDATION.md)：已有证据和发布前仍需完成的真实验证。
- [开发路线图](./developer/ROADMAP.md)：尚未实现、以后可能改进的功能。
- [Windows 原生适配计划](./developer/WINDOWS_PORT_PLAN.md)：Windows 11 x64 的分阶段实现、真实设备协作和安全门禁。
- [Runtime Spike 结果](../spikes/codex-runtime/RESULT.md)：`isolated_runtime` 的真实验证证据。

当前命令和实现结构以代码、测试及用户 CLI Reference 为准，不再维护一份容易过期的重复说明。`ROADMAP.md` 只描述未来计划，开发时不能把其中的内容当作已经存在的能力。

## 给产品设计

- [LAN-first v2 PRD](./product/PRD.md)：用户问题、产品范围、成功指标和验收标准。
