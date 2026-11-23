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

const result = spawnSync(program, process.argv.slice(2), { stdio: "inherit" });
if (result.error) {
  console.error(`portmole: could not start ${program}: ${result.error.message}`);
  process.exit(1);
}
process.exit(result.status === null ? 1 : result.status);
