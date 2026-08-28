"use strict";

const assert = require("node:assert/strict");
const fs = require("node:fs");
const path = require("node:path");
const test = require("node:test");

const root = path.join(__dirname, "..");
const packageJSON = require(path.join(root, "package.json"));
const guide = fs.readFileSync(path.join(root, "INSTALL.md"), "utf8");
const readme = fs.readFileSync(path.join(root, "README.md"), "utf8");
const docsIndex = fs.readFileSync(path.join(root, "docs", "README.md"), "utf8");
const quickstart = fs.readFileSync(path.join(root, "docs", "user", "QUICKSTART.md"), "utf8");
const cliReference = fs.readFileSync(path.join(root, "docs", "user", "CLI_REFERENCE.md"), "utf8");

test("README is the human installation guide and installs both required layers", () => {
  assert.ok(packageJSON.files.includes("INSTALL.md"), "npm package must include the Codex installation task");
  assert.match(readme, /完整安装包括 `peerctx` CLI 和 `peer-context` Skill，两者都需要安装/);
  assert.match(readme, /go install \.\/cmd\/peerctx/);
  assert.match(readme, /npx skills add https:\/\/wwzzb\.github\.io\/peercontext\//);
  assert.match(readme, /\[Codex 安装任务\]\(\.\/INSTALL\.md\)/);
  assert.match(docsIndex, /\[README 安装说明\]\(\.\.\/README\.md#安装\)/);
  for (const humanDoc of [docsIndex, quickstart, cliReference]) {
    assert.doesNotMatch(humanDoc, /INSTALL\.md/);
  }
  assert.match(quickstart, /完整安装 `peerctx` CLI 和 `peer-context` Skill/);
  assert.doesNotMatch(readme, /Skill（可选）|Skill.*可选|不需要 Skill/);
});

test("INSTALL.md is a Codex-only task with a mandatory Skill", () => {
  assert.match(guide, /^# PeerContext 安装任务（供 Codex 执行）/);
  assert.match(guide, /本文件只供 Codex 执行/);
  assert.match(guide, /完整安装必须同时包含 `peerctx` CLI 和 `peer-context` Skill/);
  assert.match(guide, /go install \.\/cmd\/peerctx/);
  assert.doesNotMatch(guide, /npm install -g/);
  assert.match(guide, /peerctx version/);
  assert.match(guide, /peerctx service status/);
  assert.doesNotMatch(guide, /peerctx project (?:create|join)/);
  assert.doesNotMatch(guide, /peerctx (?:relay|agent) serve/);
  assert.match(guide, /`\$peer-context` 显式/);
  assert.match(guide, /npx skills add https:\/\/wwzzb\.github\.io\/peercontext\/ --skill peer-context --agent codex --global/);
  assert.doesNotMatch(guide, /复制 `\.agents\/skills\/peer-context`/);
  assert.doesNotMatch(guide, /Skill（可选）|Skill.*可选/);
  assert.doesNotMatch(guide, /docs\/user|第一次局域网协作|CLI 命令参考/);
  for (const document of [readme, guide, quickstart, cliReference]) {
    assert.doesNotMatch(document, /开发分支|开发版本|未发布|尚未发布|本轮|源码版本/);
  }
  assert.ok(guide.split("\n").length <= 80, "AI installation task should stay concise");
  assert.equal(fs.existsSync(path.join(root, "docs", "ONBOARDING.md")), false);
  assert.equal(fs.existsSync(path.join(root, "docs", "SKILL_HOSTING.md")), false);
});

test("Codex installation task does not contain a credential-shaped example", () => {
  assert.doesNotMatch(guide, /Authorization:\s*Bearer/i);
  assert.doesNotMatch(guide, /invite_[A-Za-z0-9_-]{16,}/);
});
