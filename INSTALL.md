# PeerContext CLI 安装指南

以下步骤面向 AI Agent。

## 环境要求

- macOS Apple 芯片（当前试用支持）
- Node.js 18+（npm/npx）
- Git 和 Codex CLI

## 第 1 步 安装

```shell
# 安装 CLI
npm install -g peerctx@0.1.1

# 安装 peer-context Skill
npx -y skills add https://wwzzb.github.io/peercontext --skill peer-context --agent codex -g -y
```

## 第 2 步 验证

```shell
peerctx version
peerctx skills list
```

安装完成后，在 Codex 中通过 `$peer-context` 显式使用。
