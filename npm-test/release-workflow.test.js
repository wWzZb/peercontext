"use strict";

const assert = require("node:assert/strict");
const fs = require("node:fs");
const path = require("node:path");
const test = require("node:test");

const root = path.join(__dirname, "..");
const workflow = fs.readFileSync(path.join(root, ".github", "workflows", "release.yml"), "utf8");

test("release workflow publishes the single supported package with trusted publishing", () => {
  assert.match(workflow, /workflow_dispatch:/);
  assert.match(workflow, /id-token: write/);
  assert.match(workflow, /node-version: 24/);
  assert.match(workflow, /package-manager-cache: false/);
  assert.match(workflow, /GOOS=darwin GOARCH=arm64 CGO_ENABLED=0/);
  assert.doesNotMatch(workflow, /GOOS=(?:linux|windows)|GOARCH=amd64|win32-/);
  assert.match(workflow, /tarball="\$\(realpath "\$\{tarball\}"\)"/);
  assert.match(workflow, /publish_tarball="\$\(realpath "\$\{publish_tarball\}"\)"/);
  assert.match(workflow, /npm publish "\$\{PUBLISH_TARBALL\}" --access public --tag "\$\{NPM_TAG\}"/);
  assert.doesNotMatch(workflow, /NPM_TOKEN|NODE_AUTH_TOKEN/);
});

test("release workflow can safely resume an existing GitHub release", () => {
  assert.match(workflow, /gh release view "\$\{RELEASE_TAG\}"/);
  assert.match(workflow, /gh release download "\$\{RELEASE_TAG\}"/);
  assert.match(workflow, /sha256sum --check SHA256SUMS/);
  assert.match(workflow, /npm view "peerctx@\$\{RELEASE_VERSION\}" version/);
  assert.match(workflow, /cmp "\$\{PUBLISH_TARBALL\}"/);
});
