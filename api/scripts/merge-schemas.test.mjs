import assert from "node:assert/strict";
import { execFile } from "node:child_process";
import { mkdtemp, mkdir, readFile, rm, writeFile } from "node:fs/promises";
import os from "node:os";
import path from "node:path";
import { promisify } from "node:util";
import test from "node:test";
import { fileURLToPath } from "node:url";

const execFileAsync = promisify(execFile);
const scriptDirectory = path.dirname(fileURLToPath(import.meta.url));
const apiDirectory = path.resolve(scriptDirectory, "..");
const python =
  process.env.FORCAD_PYTHON ??
  path.join(apiDirectory, ".venv", process.platform === "win32" ? "Scripts/python.exe" : "bin/python");
const merger = path.join(scriptDirectory, "merge-schemas.py");
const redocly = path.join(apiDirectory, "node_modules", "@redocly", "cli", "bin", "cli.js");

async function temporaryDirectory(t) {
  const directory = await mkdtemp(path.join(os.tmpdir(), "forcad-openapi-"));
  t.after(() => rm(directory, { recursive: true, force: true }));
  return directory;
}

async function runMerger(directory) {
  const schemas = path.join(directory, "schemas");
  const output = path.join(directory, "components", "schemas.yml");
  await mkdir(path.dirname(output), { recursive: true });
  return execFileAsync(
    python,
    [merger, "--schema-dir", schemas, "--output", output],
    { cwd: apiDirectory },
  );
}

test("schema merging rejects duplicate YAML keys in one source file", async (t) => {
  const directory = await temporaryDirectory(t);
  const schemas = path.join(directory, "schemas");
  await mkdir(schemas, { recursive: true });
  await writeFile(
    path.join(schemas, "common_schemas.yml"),
    "schemas:\n  Duplicate:\n    type: string\n  Duplicate:\n    type: integer\n",
  );

  await assert.rejects(runMerger(directory), (error) => {
    assert.match(error.stderr, /duplicate key 'Duplicate'/);
    return true;
  });
});

test("schema merging rejects duplicate names across source files", async (t) => {
  const directory = await temporaryDirectory(t);
  const schemas = path.join(directory, "schemas");
  await mkdir(schemas, { recursive: true });
  await writeFile(path.join(schemas, "client_schemas.yml"), "schemas:\n  Duplicate:\n    type: string\n");
  await writeFile(path.join(schemas, "common_schemas.yml"), "schemas:\n  Duplicate:\n    type: integer\n");

  await assert.rejects(runMerger(directory), (error) => {
    assert.match(error.stderr, /duplicate schema name 'Duplicate'/);
    return true;
  });
});

test("Redocly bundle rejects a broken component reference", async (t) => {
  const directory = await temporaryDirectory(t);
  const components = path.join(directory, "components");
  await mkdir(components, { recursive: true });
  await writeFile(
    path.join(directory, "index.yml"),
    [
      "openapi: 3.2.0",
      "info:",
      "  title: Broken reference fixture",
      "  version: 1.0.0",
      "paths: {}",
      "components:",
      "  schemas:",
      "    Example:",
      "      $ref: ./components/schemas.yml#/Missing",
      "",
    ].join("\n"),
  );
  await writeFile(path.join(components, "schemas.yml"), "Existing:\n  type: string\n");

  await assert.rejects(
    execFileAsync(process.execPath, [redocly, "bundle", "index.yml", "--output", "bundle.yml"], {
      cwd: directory,
    }),
  );
});
