"use strict";

const crypto = require("node:crypto");
const fs = require("node:fs");
const path = require("node:path");

const root = path.join(__dirname, "..");
const vendor = path.join(root, "vendor");
const checksums = {};

function visit(directory) {
  for (const entry of fs.readdirSync(directory, { withFileTypes: true })) {
    const full = path.join(directory, entry.name);
    if (entry.isDirectory()) {
      visit(full);
    } else if (entry.isFile()) {
      const relative = path.relative(root, full).split(path.sep).join("/");
      checksums[relative] = crypto.createHash("sha256").update(fs.readFileSync(full)).digest("hex");
    }
  }
}

visit(vendor);
fs.writeFileSync(path.join(root, "checksums.json"), JSON.stringify(checksums, null, 2) + "\n");
