"use strict";

const assert = require("node:assert/strict");
const fs = require("node:fs");
const path = require("node:path");
const test = require("node:test");

const root = path.join(__dirname, "..");
const packageJSON = require(path.join(root, "package.json"));
const guide = fs.readFileSync(path.join(root, "INSTALL.md"), "utf8");
const readme = fs.readFileSync(path.join(root, "README.md"), "utf8");

test("AI installation guide stays short and aligned with the packaged CLI and Skill", () => {
  assert.ok(packageJSON.files.includes("INSTALL.md"), "npm package must include the AI installation guide");
  assert.match(guide, new RegExp(`npm install -g @wwzzb/peerctx@${packageJSON.version.replaceAll(".", "\\.")}`));
  assert.match(guide, /skills add https:\/\/wwzzb\.github\.io\/peercontext --skill peer-context --agent codex -g -y/);
  assert.match(guide, /peerctx version/);
  assert.match(guide, /peerctx skills list/);
  assert.match(readme, /\[PeerContext CLI 安装指南\]\(\.\/INSTALL\.md\)/);
  assert.doesNotMatch(guide, /peerctx project (?:create|join)/);
  assert.doesNotMatch(guide, /peerctx (?:relay|agent) serve/);
  assert.match(guide, /通过 `\$peer-context` 显式使用/);
  assert.ok(guide.split("\n").length <= 32, "installation guide should stay concise");
  assert.equal(fs.existsSync(path.join(root, "docs", "ONBOARDING.md")), false);
  assert.equal(fs.existsSync(path.join(root, "docs", "SKILL_HOSTING.md")), false);
});

test("AI installation guide does not contain a credential-shaped example", () => {
  assert.doesNotMatch(guide, /Authorization:\s*Bearer/i);
  assert.doesNotMatch(guide, /invite_[A-Za-z0-9_-]{16,}/);
});
