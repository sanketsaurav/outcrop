// Bumps the plugin version everywhere it lives: manifest.json, versions.json,
// plugin/package.json. Usage: node scripts/bump-plugin-version.mjs 0.2.0
import fs from "node:fs";
import process from "node:process";

const version = process.argv[2];
if (!/^\d+\.\d+\.\d+$/.test(version ?? "")) {
	console.error("usage: node scripts/bump-plugin-version.mjs <x.y.z>");
	process.exit(1);
}

const read = (p) => JSON.parse(fs.readFileSync(p, "utf8"));
const write = (p, v) => fs.writeFileSync(p, JSON.stringify(v, null, "\t") + "\n");

const manifest = read("manifest.json");
manifest.version = version;
write("manifest.json", manifest);

const versions = read("versions.json");
versions[version] = manifest.minAppVersion;
write("versions.json", versions);

const pkg = read("plugin/package.json");
pkg.version = version;
write("plugin/package.json", pkg);

console.log(`plugin version → ${version} (minAppVersion ${manifest.minAppVersion})`);
console.log("next: git add -A && git commit, then: git tag " + version + " && git push origin main --tags");
