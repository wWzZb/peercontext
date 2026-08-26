#!/usr/bin/env node

const { run } = require("../lib/peerctx.js");

const result = run(process.argv.slice(2));
if (result.error) {
  process.stderr.write(JSON.stringify({
    ok: false,
    error: {
      type: "configuration",
      subtype: "npm_wrapper",
      code: result.error.code || "peerctx_binary_unavailable",
      message: "The platform peerctx binary is unavailable or failed verification.",
      hint: "Install a package containing the matching verified binary, or explicitly set PEERCTX_BINARY.",
      retryable: false
    }
  }) + "\n");
  process.exitCode = 3;
} else if (result.signal) {
  process.exitCode = 1;
} else {
  process.exitCode = result.status == null ? 1 : result.status;
}
