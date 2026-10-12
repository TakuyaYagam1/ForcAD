import { readFile, stat } from "node:fs/promises";
import { expect, type Browser, type BrowserContext, type Page, type WebSocketRoute } from "@playwright/test";

export interface AdminCredentials {
  username: string;
  password: string;
}

interface ScoreboardEvent {
  type?: unknown;
  payload?: {
    data?: {
      runtime?: { phase?: unknown };
    };
  };
}

export interface RuntimeStatus {
  phase: string;
  practice: boolean;
  reset_pending: boolean;
}

export interface PublicStreamObservation {
  phases: string[];
  connections: number;
  suppressInits: (suppress: boolean) => void;
  sendUpdate: (data: {
    round_start: number;
    round: number;
    team_tasks: Array<{
      team_id: string;
      task_id: string;
      status: number;
      stolen: number;
      lost: number;
      score: number;
      checks: number;
      checks_passed: number;
      message: string | null;
    }>;
  }) => void;
  disconnect: () => Promise<void>;
  reconnect: () => void;
}

export function createE2EContext(browser: Browser): Promise<BrowserContext> {
  return browser.newContext({
    baseURL: process.env.FORCAD_E2E_BASE_URL || "http://nginx:8080",
  });
}

export async function readAdminCredentials(): Promise<AdminCredentials> {
  const envFile = process.env.FORCAD_E2E_ADMIN_ENV_FILE;
  if (!envFile) {
    throw new Error("Set FORCAD_E2E_ADMIN_ENV_FILE to the generated admin env file.");
  }

  let contents: string;
  try {
    const file = await stat(envFile);
    if (!file.isFile() || (file.mode & 0o077) !== 0) {
      throw new Error("Admin env file permissions are not private.");
    }
    contents = await readFile(envFile, "utf8");
  } catch {
    throw new Error("Could not read a private generated admin env file.");
  }

  const values = new Map<string, string>();
  for (const line of contents.split(/\r?\n/)) {
    if (!line || line.startsWith("#")) continue;
    const separator = line.indexOf("=");
    if (separator < 0) continue;
    values.set(line.slice(0, separator), line.slice(separator + 1));
  }

  const username = values.get("ADMIN_USERNAME");
  const password = values.get("ADMIN_PASSWORD");
  if (!username || !password) {
    throw new Error("The generated admin env file must define both admin credentials.");
  }
  return { username, password };
}

export async function loginAsAdmin(
  page: Page,
  credentials: AdminCredentials,
): Promise<void> {
  await page.goto("/admin/scoreboard");
  await expect(page).toHaveURL(/\/admin\/login$/);
  await page.getByLabel("Логин", { exact: true }).fill(credentials.username);
  await page.getByLabel("Пароль", { exact: true }).fill(credentials.password);
  await page.getByRole("button", { name: "Войти", exact: true }).click();
  await expect(page).toHaveURL(/\/admin\/scoreboard$/);
  await expect(
    page.getByRole("heading", { name: "Управление соревнованием" }),
  ).toBeVisible();
}

export async function observePublicStream(page: Page): Promise<PublicStreamObservation> {
  let disconnected = false;
  let suppressInit = false;
  let active: WebSocketRoute[] = [];
  let clientSocket: WebSocketRoute | null = null;
  const observation: PublicStreamObservation = {
    phases: [],
    connections: 0,
    suppressInits(suppress) {
      suppressInit = suppress;
    },
    sendUpdate(data) {
      if (!clientSocket) throw new Error("The public WebSocket is not connected.");
      clientSocket.send(JSON.stringify({
        type: "update_scoreboard",
        payload: { data },
        timestamp: new Date().toISOString(),
      }));
    },
    async disconnect() {
      disconnected = true;
      await Promise.all(active.map((socket) => socket.close({ code: 4001, reason: "Connection interrupted" })));
      active = [];
      clientSocket = null;
    },
    reconnect() {
      disconnected = false;
    },
  };
  await page.routeWebSocket("**/api/events/ws", async (socket) => {
    if (disconnected) {
      await socket.close({ code: 4001, reason: "Connection interrupted" });
      return;
    }
    // Forward the actual server's messages unchanged. Only the transport is
    // interrupted: Chromium's offline mode alone leaves existing sockets open.
    const server = socket.connectToServer();
    active = [socket, server];
    clientSocket = socket;
    observation.connections += 1;
    server.onMessage((payload) => {
      const frame = typeof payload === "string" ? payload : payload.toString("utf8");
      let event: ScoreboardEvent;
      try {
        event = JSON.parse(frame) as ScoreboardEvent;
      } catch {
        socket.send(payload);
        return;
      }
      if (event.type !== "init_scoreboard") {
        socket.send(payload);
        return;
      }
      if (suppressInit) {
        return;
      }
      socket.send(payload);
      const phase = event.payload?.data?.runtime?.phase;
      if (typeof phase === "string") observation.phases.push(phase);
    });
  });
  return observation;
}

export async function expectNewPhase(
  observation: PublicStreamObservation,
  phase: string,
  previousCount: number,
): Promise<void> {
  await expect.poll(
    () => observation.phases.filter((observed) => observed === phase).length,
    {
      message: `Expected a new public WebSocket event for phase ${phase}`,
      timeout: 60_000,
    },
  ).toBeGreaterThan(previousCount);
}

export async function currentRuntime(page: Page): Promise<RuntimeStatus> {
  return page.evaluate(async () => {
    const response = await fetch("/api/client/status/", { cache: "no-store" });
    if (!response.ok) throw new Error("Could not read game runtime status.");
    return (await response.json()) as RuntimeStatus;
  });
}

export async function resetPractice(page: Page): Promise<void> {
  await page.context().setOffline(false);
  await page.goto("/admin/scoreboard");
  await expect(
    page.getByRole("heading", { name: "Управление соревнованием" }),
  ).toBeVisible();

  const runtime = await currentRuntime(page);
  if (runtime.practice && ["running", "paused"].includes(runtime.phase)) {
    await page.getByRole("button", {
      name: "Завершить тестовую игру",
      exact: true,
    }).click();
    await page.getByRole("dialog").getByRole("button", {
      name: "Завершить тестовую игру",
      exact: true,
    }).click();
  }

  await expect.poll(async () => {
    const status = await currentRuntime(page);
    return {
      phase: status.phase,
      practice: status.practice,
      reset_pending: status.reset_pending,
    };
  }, {
    message: "Expected practice cleanup to return the game to waiting",
    timeout: 90_000,
  }).toEqual({ phase: "waiting", practice: false, reset_pending: false });
}
