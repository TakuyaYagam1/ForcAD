import assert from "node:assert/strict";
import { mkdir, mkdtemp, readFile, rm, writeFile } from "node:fs/promises";
import os from "node:os";
import path from "node:path";
import test from "node:test";
import { runGeneration } from "./generate.mjs";

async function temporaryApi(t) {
  const root = await mkdtemp(path.join(os.tmpdir(), "forcad-api-"));
  t.after(() => rm(root, { recursive: true, force: true }));
  return root;
}

async function createBundle({ apiRoot }) {
  await writeFile(path.join(apiRoot, "openapi.yml"), "openapi: 3.2.0\n");
}

async function createMergedSchemas({ apiRoot }) {
  const components = path.join(apiRoot, "components");
  await mkdir(components, { recursive: true });
  await writeFile(path.join(components, "schemas.yml"), "Example: {}\n");
}

test("generation removes its temporary bundle and lock after success", async (t) => {
  const apiRoot = await temporaryApi(t);
  const commands = [];

  await runGeneration({
    mode: "backend",
    apiRoot,
    execute: async (command) => {
      commands.push(command);
      if (command.args.includes("merge-schemas.py")) await createMergedSchemas({ apiRoot });
      if (command.args.includes("bundle")) await createBundle({ apiRoot });
    },
  });

  assert.equal(commands.length, 4);
  assert.match(commands[0].command, /[\\/]\.venv[\\/]bin[\\/]python$/);
  assert.equal(commands[1].command, process.execPath);
  assert.equal(commands[2].command, "go");
  assert.equal(commands[3].command, process.execPath);
  assert.match(commands[3].args[0], /[\\/]patch-ogen-uuid\.mjs$/);
  await assert.rejects(readFile(path.join(apiRoot, "openapi.yml")), { code: "ENOENT" });
  await assert.rejects(readFile(path.join(apiRoot, "components", "schemas.yml")), { code: "ENOENT" });
  await assert.rejects(readFile(path.join(apiRoot, ".openapi.lock")), { code: "ENOENT" });
});

test("generation removes its temporary bundle after a generator fails", async (t) => {
  const apiRoot = await temporaryApi(t);

  await assert.rejects(
    runGeneration({
      mode: "backend",
      apiRoot,
      execute: async (command) => {
        if (command.args.includes("merge-schemas.py")) await createMergedSchemas({ apiRoot });
        if (command.args.includes("bundle")) await createBundle({ apiRoot });
        if (command.command === "go") throw new Error("generator failed");
      },
    }),
    /generator failed/,
  );

  await assert.rejects(readFile(path.join(apiRoot, "openapi.yml")), { code: "ENOENT" });
  await assert.rejects(readFile(path.join(apiRoot, "components", "schemas.yml")), { code: "ENOENT" });
  await assert.rejects(readFile(path.join(apiRoot, ".openapi.lock")), { code: "ENOENT" });
});

test("generation refuses and preserves pre-existing temporary files", async (t) => {
  const apiRoot = await temporaryApi(t);
  const bundle = path.join(apiRoot, "openapi.yml");
  const mergedSchemas = path.join(apiRoot, "components", "schemas.yml");
  await mkdir(path.dirname(mergedSchemas), { recursive: true });
  await writeFile(bundle, "owned by another process\n");
  await writeFile(mergedSchemas, "owned schema data\n");

  await assert.rejects(
    runGeneration({ mode: "backend", apiRoot, execute: async () => assert.fail("must not run") }),
    /refusing to overwrite pre-existing.*schemas.yml.*openapi.yml/,
  );

  assert.equal(await readFile(bundle, "utf8"), "owned by another process\n");
  assert.equal(await readFile(mergedSchemas, "utf8"), "owned schema data\n");
  await assert.rejects(readFile(path.join(apiRoot, ".openapi.lock")), { code: "ENOENT" });
});

test("the exclusive lock rejects a concurrent generation", async (t) => {
  const apiRoot = await temporaryApi(t);
  let started;
  const isStarted = new Promise((resolve) => {
    started = resolve;
  });
  let release;
  const waitForRelease = new Promise((resolve) => {
    release = resolve;
  });

  const first = runGeneration({
    mode: "backend",
    apiRoot,
    execute: async (command) => {
      if (command.args.includes("merge-schemas.py")) await createMergedSchemas({ apiRoot });
      if (command.args.includes("bundle")) {
        await createBundle({ apiRoot });
        started();
        await waitForRelease;
      }
    },
  });
  await isStarted;

  await assert.rejects(
    runGeneration({ mode: "frontend", apiRoot, execute: async () => assert.fail("must not run") }),
    /generation is already running/,
  );

  release();
  await first;
  await assert.rejects(readFile(path.join(apiRoot, "openapi.yml")), { code: "ENOENT" });
  await assert.rejects(readFile(path.join(apiRoot, "components", "schemas.yml")), { code: "ENOENT" });
  await assert.rejects(readFile(path.join(apiRoot, ".openapi.lock")), { code: "ENOENT" });
});

test("generation removes its temporary bundle after an abort", async (t) => {
  const apiRoot = await temporaryApi(t);
  const controller = new AbortController();
  let started;
  const isStarted = new Promise((resolve) => {
    started = resolve;
  });

  const generation = runGeneration({
    mode: "backend",
    apiRoot,
    signal: controller.signal,
    execute: async (command) => {
      if (command.args.includes("merge-schemas.py")) await createMergedSchemas({ apiRoot });
      if (command.args.includes("bundle")) await createBundle({ apiRoot });
      if (command.args.includes("bundle")) started();
      else return;
      await new Promise((resolve, reject) => {
        command.signal.addEventListener(
          "abort",
          () => reject(new Error("generation aborted")),
          { once: true },
        );
      });
    },
  });
  await isStarted;
  controller.abort("SIGINT");

  await assert.rejects(generation, /generation aborted/);
  await assert.rejects(readFile(path.join(apiRoot, "openapi.yml")), { code: "ENOENT" });
  await assert.rejects(readFile(path.join(apiRoot, "components", "schemas.yml")), { code: "ENOENT" });
  await assert.rejects(readFile(path.join(apiRoot, ".openapi.lock")), { code: "ENOENT" });
});
