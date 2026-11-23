#!/usr/bin/env node
// Runs the portmole program that npm installed for this platform.
"use strict";
const { spawnSync } = require("child_process");

const pkg = `@portmole/${process.platform}-${process.arch}`;
let program;
try {
  program = require.resolve(`${pkg}/bin/portmole${process.platform === "win32" ? ".exe" : ""}`);
} catch {
  console.error(
    `portmole: no program for ${process.platform} ${process.arch}.\n` +
      `The package ${pkg} is missing. npm skips it when run with --omit=optional,\n` +
      `and it exists only for Windows, macOS and Linux on x64 or arm64.\n` +
      `You can download the program from https://github.com/portmole/portmole/releases instead.`
  );
  process.exit(1);
}

// A package packed without the executable bit would fail with EACCES.
if (process.platform !== "win32") {
  try {
    require("fs").chmodSync(program, 0o755);
  } catch {}
}

const args = process.argv.slice(2);

// `portmole update` for a global install is done here and not by the
// program: Windows does not let a running program be replaced.
if (args.length === 1 && args[0] === "update") {
  const path = require("path");
  const check = spawnSync(program, ["update", "--check"], { stdio: "inherit" });
  if (check.status !== 10) process.exit(check.status === null ? 1 : check.status);
  // shell: npm is npm.cmd on Windows.
  const root = spawnSync("npm", ["root", "-g"], { encoding: "utf8", shell: true });
  const here = path.resolve(__dirname, "..", "..");
  if (root.status !== 0 || path.resolve(root.stdout.trim()).toLowerCase() !== here.toLowerCase()) {
    console.log(
      "This copy was not installed with `npm install -g portmole`, so it is left alone.\n" +
        "Started through npx, portmole is the newest version every time."
    );
    process.exit(0);
  }
  console.log("Installing it with npm...");
  const install = spawnSync("npm", ["install", "-g", "portmole@latest"], { stdio: "inherit", shell: true });
  if (install.status !== 0) {
    console.error(
      "\nportmole: npm could not install the new version. An agent session or a terminal that\n" +
        "has portmole running keeps the program open; close those, then run `portmole update` again."
    );
    process.exit(1);
  }
  console.log("Updated. Agents pick up the new version in their next session.");
  process.exit(0);
}

const result = spawnSync(program, args, { stdio: "inherit" });
if (result.error) {
  console.error(`portmole: could not start ${program}: ${result.error.message}`);
  process.exit(1);
}
process.exit(result.status === null ? 1 : result.status);
