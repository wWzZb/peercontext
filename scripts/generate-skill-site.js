"use strict";

const fs = require("node:fs");
const path = require("node:path");

const root = path.join(__dirname, "..");
const sourceRoot = path.join(root, ".agents", "skills", "peer-context");
const siteRoot = path.join(root, "skill-site");

function listFiles(directory, prefix = "") {
  return fs.readdirSync(directory, { withFileTypes: true })
    .flatMap((entry) => {
      const relative = prefix ? `${prefix}/${entry.name}` : entry.name;
      const absolute = path.join(directory, entry.name);
      return entry.isDirectory() ? listFiles(absolute, relative) : [relative];
    })
    .sort();
}

const files = listFiles(sourceRoot);
const skillText = fs.readFileSync(path.join(sourceRoot, "SKILL.md"), "utf8");
const descriptionMatch = skillText.match(/^description:\s*(.+)$/m);
if (!descriptionMatch) {
  throw new Error("peer-context SKILL.md is missing its description");
}

const index = `${JSON.stringify({
  skills: [{
    name: "peer-context",
    description: descriptionMatch[1].trim().replace(/^['"]|['"]$/g, ""),
    files
  }]
}, null, 2)}\n`;

const expected = new Map([
  [".nojekyll", Buffer.from("")],
  [".well-known/skills/index.json", Buffer.from(index)]
]);
for (const file of files) {
  expected.set(`.well-known/skills/peer-context/${file}`, fs.readFileSync(path.join(sourceRoot, ...file.split("/"))));
}

function writeSite() {
  for (const [relative, content] of expected) {
    const target = path.join(siteRoot, ...relative.split("/"));
    fs.mkdirSync(path.dirname(target), { recursive: true });
    fs.writeFileSync(target, content);
  }
}

function checkSite() {
  const actualFiles = fs.existsSync(siteRoot) ? listFiles(siteRoot) : [];
  const expectedFiles = [...expected.keys()].sort();
  if (JSON.stringify(actualFiles) !== JSON.stringify(expectedFiles)) {
    throw new Error("hosted Skill file list is stale; run node scripts/generate-skill-site.js --write");
  }
  for (const [relative, content] of expected) {
    const actual = fs.readFileSync(path.join(siteRoot, ...relative.split("/")));
    if (!actual.equals(content)) {
      throw new Error(`${relative} is stale; run node scripts/generate-skill-site.js --write`);
    }
  }
}

if (process.argv.includes("--write")) {
  writeSite();
} else if (process.argv.includes("--check")) {
  checkSite();
} else {
  throw new Error("use --write or --check");
}
