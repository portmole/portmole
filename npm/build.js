// Writes the npm packages for a release:
//
//   node npm/build.js <version> <binaries dir> <out dir>
//
// <binaries dir> holds the six programs as the release workflow names them,
// portmole-<goos>-<goarch>[.exe]. <out dir> gets one folder per package, each
// ready for `npm publish`: the platform packages, then "portmole" itself,
// which lists them as optional dependencies so npm installs the one that
// matches the machine.
"use strict";
const fs = require("fs");
const path = require("path");

const [version, binDir, outDir] = process.argv.slice(2);
if (!/^\d+\.\d+\.\d+(-[0-9A-Za-z.-]+)?$/.test(version || "") || !binDir || !outDir) {
  console.error("usage: node npm/build.js <version> <binaries dir> <out dir>");
  process.exit(2);
}

// Node's names for a platform, and Go's names for the same one.
const targets = [
  { os: "win32", cpu: "x64", goos: "windows", goarch: "amd64" },
  { os: "win32", cpu: "arm64", goos: "windows", goarch: "arm64" },
  { os: "darwin", cpu: "x64", goos: "darwin", goarch: "amd64" },
  { os: "darwin", cpu: "arm64", goos: "darwin", goarch: "arm64" },
  { os: "linux", cpu: "x64", goos: "linux", goarch: "amd64" },
  { os: "linux", cpu: "arm64", goos: "linux", goarch: "arm64" },
];

const main = JSON.parse(fs.readFileSync(path.join(__dirname, "portmole", "package.json"), "utf8"));
const write = (file, value) => fs.writeFileSync(file, JSON.stringify(value, null, 2) + "\n");

fs.rmSync(outDir, { recursive: true, force: true });
const optional = {};
for (const t of targets) {
  const ext = t.goos === "windows" ? ".exe" : "";
  const source = path.join(binDir, `portmole-${t.goos}-${t.goarch}${ext}`);
  if (!fs.existsSync(source)) {
    console.error(`missing ${source}`);
    process.exit(1);
  }
  const name = `@portmole/${t.os}-${t.cpu}`;
  const dir = path.join(outDir, `${t.os}-${t.cpu}`);
  fs.mkdirSync(path.join(dir, "bin"), { recursive: true });
  const program = path.join(dir, "bin", "portmole" + ext);
  fs.copyFileSync(source, program);
  fs.chmodSync(program, 0o755);
  write(path.join(dir, "package.json"), {
    name,
    version,
    description: `The portmole program for ${t.os} ${t.cpu}. Install "portmole" instead.`,
    license: main.license,
    homepage: main.homepage,
    repository: main.repository,
    os: [t.os],
    cpu: [t.cpu],
    files: ["bin"],
  });
  optional[name] = version;
}

const dir = path.join(outDir, "portmole");
fs.cpSync(path.join(__dirname, "portmole"), dir, { recursive: true });
write(path.join(dir, "package.json"), { ...main, version, optionalDependencies: optional });
console.log(`wrote ${targets.length + 1} packages for ${version} to ${outDir}`);
