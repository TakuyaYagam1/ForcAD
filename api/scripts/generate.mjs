import { spawn } from "node:child_process";
import { open, lstat, rm } from "node:fs/promises";
import path from "node:path";
import { fileURLToPath } from "node:url";

const scriptDirectory = path.dirname(fileURLToPath(import.meta.url));
const apiDirectory = path.resolve(scriptDirectory, "..");
const repositoryDirectory = path.resolve(apiDirectory, "..");

function commandFor(mode, apiRoot, backendRoot, frontendRoot) {
  const python =
    process.env.FORCAD_PYTHON ??
    path.join(
      apiRoot,
      ".venv",
      process.platform === "win32" ? "Scripts/python.exe" : "bin/python",
    );
  const bundle = path.join(apiRoot, "openapi.yml");
  const commands = [
    {
      command: python,
      args: ["scripts/merge-schemas.py"],
      cwd: apiRoot,
    },
    {
      command: process.execPath,
      args: [
        path.join(apiRoot, "node_modules", "@redocly", "cli", "bin", "cli.js"),
        "bundle",
        "index.yml",
        "--output",
        "openapi.yml",
      ],
      cwd: apiRoot,
    },
  ];

  if (mode === "backend" || mode === "all") {
    commands.push({
      command: "go",
      args: [
        "tool",
        "ogen",
        "--config",
        "codegen/ogen.yml",
        "--target",
        "internal/adapter/primary/http/api",
        "--package",
        "api",
        "--clean",
        bundle,
      ],
      cwd: backendRoot,
    });
  }

  if (mode === "frontend" || mode === "all") {
    commands.push({
      command: process.execPath,
      args: [
        path.join(frontendRoot, "node_modules", "orval", "dist", "bin", "orval.mjs"),
        "--config",
        "./codegen/orval.config.ts",
      ],
      cwd: frontendRoot,
    });
  }

  return commands;
}

function signalChildTree(child, signal) {
  if (process.platform !== "win32" && child.pid) {
    try {
      process.kill(-child.pid, signal);
      return;
    } catch (error) {
      if (error.code === "ESRCH") return;
    }
  }
  if (child.exitCode === null && child.signalCode === null) child.kill(signal);
}

function processGroupExists(pid) {
  try {
    process.kill(-pid, 0);
    return true;
  } catch (error) {
    return error.code !== "ESRCH";
  }
}

function wait(milliseconds) {
  return new Promise((resolve) => setTimeout(resolve, milliseconds));
}

async function waitForProcessGroupExit(pid) {
  if (process.platform === "win32" || !pid) return;

  const gracefulDeadline = Date.now() + 2000;
  while (processGroupExists(pid) && Date.now() < gracefulDeadline) {
    await wait(25);
  }
  if (!processGroupExists(pid)) return;

  try {
    process.kill(-pid, "SIGKILL");
  } catch (error) {
    if (error.code === "ESRCH") return;
    throw error;
  }
  const forcedDeadline = Date.now() + 1000;
  while (processGroupExists(pid) && Date.now() < forcedDeadline) {
    await wait(25);
  }
}

function runCommand({ command, args, cwd, signal }) {
  return new Promise((resolve, reject) => {
    const child = spawn(command, args, {
      cwd,
      stdio: "inherit",
      detached: process.platform !== "win32",
    });
    let settled = false;
    let forceKillTimer;

    const finish = (callback, value) => {
      if (settled) return;
      settled = true;
      clearTimeout(forceKillTimer);
      signal?.removeEventListener("abort", abortChild);
      callback(value);
    };

    const abortChild = () => {
      signalChildTree(child, signal.reason === "SIGINT" ? "SIGINT" : "SIGTERM");
      forceKillTimer = setTimeout(() => signalChildTree(child, "SIGKILL"), 2000);
    };

    if (signal?.aborted) abortChild();
    else signal?.addEventListener("abort", abortChild, { once: true });

    child.once("error", (error) => finish(reject, error));
    child.once("close", async (code, childSignal) => {
      if (signal?.aborted) await waitForProcessGroupExit(child.pid);
      if (signal?.aborted) {
        finish(reject, new Error("API generation was interrupted"));
        return;
      }
      if (code === 0) {
        finish(resolve);
        return;
      }
      const result = childSignal ? `signal ${childSignal}` : `exit code ${code}`;
      finish(reject, new Error(`${command} ${args.join(" ")} failed with ${result}`));
    });
  });
}

async function fileExists(filename) {
  try {
    await lstat(filename);
    return true;
  } catch (error) {
    if (error.code === "ENOENT") return false;
    throw error;
  }
}

export async function runGeneration({
  mode,
  apiRoot = apiDirectory,
  backendRoot = path.join(repositoryDirectory, "backend"),
  frontendRoot = path.join(repositoryDirectory, "frontend"),
  execute = runCommand,
  signal,
}) {
  if (!new Set(["all", "backend", "frontend"]).has(mode)) {
    throw new Error(`unsupported generation mode: ${mode}`);
  }

  const bundle = path.join(apiRoot, "openapi.yml");
  const mergedSchemas = path.join(apiRoot, "components", "schemas.yml");
  const lockPath = path.join(apiRoot, ".openapi.lock");
  let lock;
  let ownsTemporaryFiles = false;

  try {
    try {
      lock = await open(lockPath, "wx", 0o600);
    } catch (error) {
      if (error.code === "EEXIST") {
        throw new Error("API generation is already running (api/.openapi.lock exists)");
      }
      throw error;
    }

    const existingFiles = [];
    if (await fileExists(mergedSchemas)) existingFiles.push("api/components/schemas.yml");
    if (await fileExists(bundle)) existingFiles.push("api/openapi.yml");
    if (existingFiles.length > 0) {
      throw new Error(`refusing to overwrite pre-existing ${existingFiles.join(" and ")}`);
    }
    ownsTemporaryFiles = true;

    for (const command of commandFor(mode, apiRoot, backendRoot, frontendRoot)) {
      if (signal?.aborted) throw signal.reason ?? new Error("API generation aborted");
      await execute({ ...command, signal });
    }
  } finally {
    try {
      if (ownsTemporaryFiles) {
        await rm(mergedSchemas, { force: true });
        await rm(bundle, { force: true });
      }
    } finally {
      if (lock) {
        await lock.close();
        await rm(lockPath, { force: true });
      }
    }
  }
}

async function main(args) {
  if (args.length !== 1 || !new Set(["all", "backend", "frontend"]).has(args[0])) {
    throw new Error("usage: node api/scripts/generate.mjs all|backend|frontend");
  }

  const controller = new AbortController();
  let receivedSignal;
  const abort = (signal) => {
    receivedSignal = signal;
    controller.abort(signal);
  };
  const onInterrupt = () => abort("SIGINT");
  const onTerminate = () => abort("SIGTERM");
  process.once("SIGINT", onInterrupt);
  process.once("SIGTERM", onTerminate);

  try {
    await runGeneration({ mode: args[0], signal: controller.signal });
    if (receivedSignal) process.exitCode = receivedSignal === "SIGINT" ? 130 : 143;
  } catch (error) {
    if (receivedSignal) {
      process.exitCode = receivedSignal === "SIGINT" ? 130 : 143;
      return;
    }
    throw error;
  } finally {
    process.removeListener("SIGINT", onInterrupt);
    process.removeListener("SIGTERM", onTerminate);
  }
}

if (process.argv[1] && path.resolve(process.argv[1]) === fileURLToPath(import.meta.url)) {
  main(process.argv.slice(2)).catch((error) => {
    console.error(error.message);
    process.exitCode = 1;
  });
}
