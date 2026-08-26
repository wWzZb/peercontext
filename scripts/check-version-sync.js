"use strict";

const fs = require("node:fs");
const path = require("node:path");

const packageJSON = require("../package.json");
const versionSource = fs.readFileSync(path.join(__dirname, "..", "internal", "version", "version.go"), "utf8");
const match = versionSource.match(/const Current = "([^"]+)"/);
if (!match || match[1] !== packageJSON.version) {
  throw new Error("package.json and Go binary versions differ");
}
