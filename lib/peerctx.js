"use strict";

const crypto = require("node:crypto");
const fs = require("node:fs");
const path = require("node:path");
const { spawnSync } = require("node:child_process");

const supported = new Set([
  "darwin-arm64"
]);

function platformKey(platform = process.platform, arch = process.arch) {
  const key = `${platform}-${arch}`;
  if (!supported.has(key)) {
    const error = new Error(`unsupported platform ${key}`);
    error.code = "unsupported_platform";
    throw error;
  }
  return key;
}

function binaryFilename() {
  return "peerctx";
}

function sha256(file) {
  return crypto.createHash("sha256").update(fs.readFileSync(file)).digest("hex");
}

function resolveBinary(options = {}) {
  const env = options.env || process.env;
  if (env.PEERCTX_BINARY) {
    const explicit = path.resolve(env.PEERCTX_BINARY);
    if (!fs.statSync(explicit).isFile()) {
      const error = new Error("PEERCTX_BINARY is not a file");
      error.code = "peerctx_binary_unavailable";
      throw error;
    }
    return explicit;
  }
  const root = options.root || path.resolve(__dirname, "..");
  const platform = options.platform || process.platform;
  const arch = options.arch || process.arch;
  const relative = path.posix.join("vendor", platformKey(platform, arch), binaryFilename(platform));
  const binary = path.join(root, ...relative.split("/"));
  let checksums;
  try {
    checksums = JSON.parse(fs.readFileSync(path.join(root, "checksums.json"), "utf8"));
  } catch (cause) {
    const error = new Error("binary checksum manifest is unavailable", { cause });
    error.code = "binary_checksum_unavailable";
    throw error;
  }
  if (!fs.existsSync(binary) || typeof checksums[relative] !== "string") {
    const error = new Error(`verified binary is unavailable for ${platform}-${arch}`);
    error.code = "peerctx_binary_unavailable";
    throw error;
  }
  const actual = sha256(binary);
  if (actual !== checksums[relative].toLowerCase()) {
    const error = new Error("peerctx binary checksum mismatch");
    error.code = "binary_checksum_mismatch";
    throw error;
  }
  return binary;
}

function run(args, options = {}) {
  try {
    const binary = resolveBinary(options);
    return spawnSync(binary, args, {
      cwd: options.cwd || process.cwd(),
      env: options.env || process.env,
      stdio: options.stdio || "inherit",
      windowsHide: true
    });
  } catch (error) {
    return { error };
  }
}

module.exports = { binaryFilename, platformKey, resolveBinary, run, sha256 };
