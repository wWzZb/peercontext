"use strict";

const assert = require("node:assert/strict");
const fs = require("node:fs");
const path = require("node:path");
const test = require("node:test");

const root = path.join(__dirname, "..");
const packageJSON = require(path.join(root, "package.json"));
const guide = fs.readFileSync(path.join(root, "INSTALL.md"), "utf8");
const readme = fs.readFileSync(path.join(root, "README.md"), "utf8");

test("source installation guide stays aligned with unpublished LAN v2", () => {
  assert.ok(packageJSON.files.includes("INSTALL.md"), "npm package must include the AI installation guide");
  assert.match(guide, /go install \.\/cmd\/peerctx/);
  assert.match(guide, /未发布/);
  assert.doesNotMatch(guide, /npm install -g/);
  assert.match(guide, /peerctx version/);
  assert.match(guide, /peerctx service status/);
  assert.match(readme, /\[INSTALL\.md\]\(\.\/INSTALL\.md\)/);
  assert.doesNotMatch(guide, /peerctx project (?:create|join)/);
  assert.doesNotMatch(guide, /peerctx (?:relay|agent) serve/);
  assert.match(guide, /`\$peer-context` 显式触发/);
  assert.ok(guide.split("\n").length <= 60, "installation guide should stay concise");
  assert.equal(fs.existsSync(path.join(root, "docs", "ONBOARDING.md")), false);
  assert.equal(fs.existsSync(path.join(root, "docs", "SKILL_HOSTING.md")), false);
});

test("AI installation guide does not contain a credential-shaped example", () => {
  assert.doesNotMatch(guide, /Authorization:\s*Bearer/i);
  assert.doesNotMatch(guide, /invite_[A-Za-z0-9_-]{16,}/);
});
