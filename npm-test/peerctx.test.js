"use strict";

const assert = require("node:assert/strict");
const crypto = require("node:crypto");
const fs = require("node:fs");
const os = require("node:os");
const path = require("node:path");
const test = require("node:test");

const { binaryFilename, platformKey, resolveBinary, run } = require("../lib/peerctx.js");

test("maps every declared platform and rejects unknown targets", () => {
  for (const key of ["darwin-arm64", "darwin-x64", "linux-arm64", "linux-x64", "win32-arm64", "win32-x64"]) {
    const [platform, arch] = key.split("-");
    assert.equal(platformKey(platform, arch), key);
  }
  assert.equal(binaryFilename("win32"), "peerctx.exe");
  assert.equal(binaryFilename("linux"), "peerctx");
  assert.throws(() => platformKey("freebsd", "x64"), { code: "unsupported_platform" });
});

test("resolves a bundled binary only after SHA-256 verification", (t) => {
  const root = fs.mkdtempSync(path.join(os.tmpdir(), "peerctx-npm-test-"));
  t.after(() => fs.rmSync(root, { recursive: true, force: true }));
  const relative = "vendor/linux-x64/peerctx";
  const binary = path.join(root, ...relative.split("/"));
  fs.mkdirSync(path.dirname(binary), { recursive: true });
  fs.writeFileSync(binary, "verified peerctx binary");
  const digest = crypto.createHash("sha256").update(fs.readFileSync(binary)).digest("hex");
  fs.writeFileSync(path.join(root, "checksums.json"), JSON.stringify({ [relative]: digest }));
  assert.equal(resolveBinary({ root, platform: "linux", arch: "x64", env: {} }), binary);
  fs.appendFileSync(binary, " tampered");
  assert.throws(() => resolveBinary({ root, platform: "linux", arch: "x64", env: {} }), { code: "binary_checksum_mismatch" });
});

test("explicit binary override invokes the Go CLI without parsing its arguments", (t) => {
  if (process.platform === "win32") {
    t.skip("POSIX fixture executable");
    return;
  }
  const root = fs.mkdtempSync(path.join(os.tmpdir(), "peerctx-npm-run-"));
  t.after(() => fs.rmSync(root, { recursive: true, force: true }));
  const binary = path.join(root, "fake-peerctx");
  fs.writeFileSync(binary, "#!/bin/sh\n[ \"$1\" = 'version' ] || exit 99\nexit 17\n", { mode: 0o700 });
  const result = run(["version"], { env: { ...process.env, PEERCTX_BINARY: binary }, stdio: "pipe" });
  assert.equal(result.status, 17);
});
