"use strict";

const path = require("node:path");
const { resolveBinary } = require("../lib/peerctx.js");

require("./check-version-sync.js");

for (const [platform, arch] of [["darwin", "arm64"], ["darwin", "x64"], ["linux", "arm64"], ["linux", "x64"], ["win32", "arm64"], ["win32", "x64"]]) {
  resolveBinary({ root: path.join(__dirname, ".."), platform, arch, env: {} });
}
