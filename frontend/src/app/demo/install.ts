import { validateTeam, validateTask } from "@/shared/lib/adminValidation";
import { refreshGameQueries } from "@/shared/lib/queryClient";
import { AxiosError, AxiosHeaders } from "axios";
import type { AxiosAdapter, InternalAxiosRequestConfig } from "axios";
import type { Team } from "@/entities/team/model/types";
import type { Task } from "@/entities/task/model/types";
import type { TeamTaskLogEntry } from "@/entities/team-task/model/types";
import type { TeamTaskStateRaw } from "@/entities/team-scoreboard/api";
import { useScoreboardStore } from "@/entities/scoreboard/model/store";
import type { GameRuntimeStatus, RawTeamTask } from "@/entities/scoreboard/model/store";
import { useLiveScoreboardStore } from "@/entities/live-scoreboard/model/store";
import { api, http } from "@/shared/lib/axios";
import {
  createDemoTeams,
  createDemoTasks,
  createDemoTeamTasks,
  createDemoHistory,
  DEMO_INITIAL_ROUND,
  DEMO_ROUND_TIME,
} from "./data";

let teams: Team[] = [];
let tasks: Task[] = [];
let raw: RawTeamTask[] = [];
let history: TeamTaskStateRaw[] = [];
let round = DEMO_INITIAL_ROUND;
let roundStart = 0;
let nextHistoryId = 1;
let loggedIn = false;
let phase: "waiting" | "running" | "paused" | "finished" = "running";
let practice = false;
let generation = 0;
let scheduledStart = new Date(Date.now() - 3600_000).toISOString();
let pausedAt: number | null = null;
let pausedSeconds = 0;

function getRuntime(): GameRuntimeStatus {
  return {
    phase,
    practice,
    generation,
    can_start: phase === "waiting",
    can_start_practice: phase === "waiting" && new Date(scheduledStart).getTime() > Date.now(),
    reset_pending: false,
    scheduled_start: scheduledStart,
    paused_at: pausedAt,
    paused_seconds: pausedSeconds,
    round_time: DEMO_ROUND_TIME,
    round: phase === "waiting" ? 0 : round + 1,
    round_start: phase === "waiting" ? null : roundStart + DEMO_ROUND_TIME,
    results_pending: false,
  };
}

function publishState() {
  useScoreboardStore.getState().handleInitScoreboardMessage({
    teams,
    tasks,
    state: { round, round_start: roundStart, team_tasks: raw },
    runtime: getRuntime(),
  });
  useScoreboardStore.getState().setRoundTime(DEMO_ROUND_TIME);
  useScoreboardStore.getState().setConnected(true);
  refreshGameQueries();
}

function rememberSession(value: boolean) {
  loggedIn = value;
  try {
    if (value) sessionStorage.setItem("forcad-demo-auth", "on");
    else sessionStorage.removeItem("forcad-demo-auth");
  } catch {
    // In-memory demo login still works without storage.
  }
}

export function resetDemoData() {
  practice = false;
  generation += 1;
  scheduledStart = new Date(Date.now() - 3600_000).toISOString();
  phase = "running";
  pausedAt = null;
  pausedSeconds = 0;
  teams = createDemoTeams();
  tasks = createDemoTasks();
  raw = createDemoTeamTasks(teams, tasks);
  round = DEMO_INITIAL_ROUND;
  const now = Math.floor(Date.now() / 1000);
  roundStart = now - DEMO_ROUND_TIME - 35;
  history = createDemoHistory(raw, round, now);
  nextHistoryId = history.length + 1;
  publishState();

  const scoreboard = useScoreboardStore.getState();
  const seededEvents = [
    [2, 1, 1, 185],
    [3, 5, 2, 122.5],
    [1, 4, 3, 210],
    [5, 7, 4, 96.75],
    [4, 2, 1, 154],
    [6, 8, 2, 88.25],
  ].map(([attackerId, victimId, taskId, delta], index) => ({
    id: index + 1,
    ts: Date.now() - (index * 43 + 12) * 1000,
    attackerId,
    victimId,
    taskId,
    attackerName: scoreboard.teams!.find((team) => team.id === attackerId)!
      .name,
    victimName: scoreboard.teams!.find((team) => team.id === victimId)!.name,
    taskName: tasks.find((task) => task.id === taskId)!.name,
    delta,
  }));
  useLiveScoreboardStore.setState({ events: seededEvents, error: null });
}

export function prepareDemoGame() {
  phase = "waiting";
  practice = false;
  generation += 1;
  scheduledStart = new Date(Date.now() + 86400_000).toISOString();
  pausedAt = null;
  pausedSeconds = 0;
  round = 0;
  roundStart = 0;
  history = [];
  raw = raw.map((cell) => ({
    ...cell, score: 2500, stolen: 0, lost: 0, checks: 0, checks_passed: 0, status: -1,
  }));
  publishState();
}

function recordSnapshot() {
  const now = Math.floor(Date.now() / 1000);
  history.unshift(
    ...raw.map((item) => ({
      ...item,
      id: nextHistoryId++,
      round,
      timestamp: `${now}-${item.task_id}`,
    })),
  );
}

export function simulateDemoCapture(promote = false) {
  if (phase !== "running") return;
  const leaders = useScoreboardStore.getState().teams ?? [];
  const active = leaders.filter((team) => team.active);
  const taskIds = new Set(tasks.filter((task) => task.active).map((task) => task.id));
  if (active.length < 2 || !taskIds.size) return;
  const attacker = active[1];
  const victim = active[0];
  const attackerCell = raw.find((item) => item.team_id === attacker.id && taskIds.has(item.task_id) && item.checks_passed > 0);
  const victimCell = raw.find(
    (item) =>
      item.team_id === victim.id && item.task_id === attackerCell?.task_id,
  );
  if (!attackerCell || !victimCell) return;
  const delta = promote
    ? Math.max(
        185,
        ((victim.score ?? 0) - (attacker.score ?? 0) + 275) /
          Math.max(
            0.01,
            attackerCell.checks_passed / Math.max(1, attackerCell.checks),
          ),
      )
    : 185;
  attackerCell.score += delta;
  attackerCell.stolen += 1;
  victimCell.score = Math.max(0, victimCell.score - delta);
  victimCell.lost += 1;
  round += 1;
  pausedSeconds = 0;
  refreshGameQueries();
  roundStart = Math.floor(Date.now() / 1000) - DEMO_ROUND_TIME;
  recordSnapshot();
  useScoreboardStore.getState().setRuntime(getRuntime());
  useScoreboardStore.getState().handleUpdateScoreboardMessage({
    round,
    round_start: roundStart,
    team_tasks: raw,
  });
  useLiveScoreboardStore.getState().pushNotification({
    generation,
    attacker_id: attacker.id!,
    victim_id: victim.id!,
    task_id: attackerCell.task_id,
    attacker_delta: delta,
  });
}

function makeResponse(
  config: InternalAxiosRequestConfig,
  data: unknown,
  status = 200,
) {
  return {
    data: structuredClone(data),
    status,
    statusText: status < 400 ? "OK" : "Demo request failed",
    headers: new AxiosHeaders({ "content-type": "application/json" }),
    config,
  };
}

function fail(
  config: InternalAxiosRequestConfig,
  status: number,
  message: string,
): never {
  throw new AxiosError(
    message,
    AxiosError.ERR_BAD_REQUEST,
    config,
    undefined,
    makeResponse(config, { error: message }, status),
  );
}

function readBody(config: InternalAxiosRequestConfig): Record<string, unknown> {
  const data: unknown = config.data;
  if (typeof data === "string") {
    try {
      const parsed: unknown = JSON.parse(data);
      if (parsed && typeof parsed === "object" && !Array.isArray(parsed)) {
        return parsed as Record<string, unknown>;
      }
    } catch {
      fail(config, 400, "Invalid demo request data");
    }
  }
  if (data && typeof data === "object" && !Array.isArray(data)) {
    return data as Record<string, unknown>;
  }
  return {};
}

function addMissingCells() {
  for (const team of teams) {
    for (const task of tasks) {
      if (
        raw.some((item) => item.team_id === team.id && item.task_id === task.id)
      )
        continue;
      raw.push({
        team_id: team.id!,
        task_id: task.id!,
        score: task.default_score,
        stolen: 0,
        lost: 0,
        checks: 0,
        checks_passed: 0,
        status: -1,
        message: "",
      });
    }
  }
  raw = raw.filter(
    (item) =>
      teams.some((team) => team.id === item.team_id) &&
      tasks.some((task) => task.id === item.task_id),
  );
}

function demoLogs(teamId: number, taskId: number): TeamTaskLogEntry[] {
  return history
    .filter((item) => item.team_id === teamId && item.task_id === taskId)
    .slice(0, 15)
    .map((item, index) => ({
      ...item,
      id: item.id ?? index + 1,
      team_id: teamId,
      task_id: taskId,
      ts: Number(item.timestamp.split("-")[0]),
      public_message: item.message || "OK",
      private_message: "Demo entry: check completed",
      command: `python demo/checker.py check ${teams.find((team) => team.id === teamId)?.ip ?? "10.10.14.8"}`,
    }));
}

const demoAdapter: AxiosAdapter = async (config) => {
  // Resolve requests entirely in the browser; no backend connection is made.
  await new Promise<void>((resolve) => window.setTimeout(resolve, 100));
  if (config.signal?.aborted)
    throw new AxiosError("Cancelled", AxiosError.ERR_CANCELED, config);
  const url = new URL(
    config.url ?? "/",
    `${config.baseURL ?? window.location.origin}/`,
  );
  const path = url.pathname.replace(/^\/api/, "").replace(/\/$/, "");
  const method = config.method?.toUpperCase() ?? "GET";
  const body = readBody(config);

  if (path === "/client/status")
    return makeResponse(config, getRuntime());
  if (path === "/client/config")
    return makeResponse(config, { round_time: DEMO_ROUND_TIME });
  if (path === "/scoreboard/init") {
    return makeResponse(config, {
      teams,
      tasks,
      state: { round, round_start: roundStart, team_tasks: raw },
      runtime: getRuntime(),
    });
  }
  if (path === "/client/teams")
    return makeResponse(
      config,
      teams.filter((team) => team.active),
    );
  const teamHistory = path.match(/^\/client\/teams\/(\d+)$/);
  if (teamHistory)
    return makeResponse(
      config,
      history.filter((item) => item.team_id === Number(teamHistory[1])),
    );
  if (path === "/admin/login") {
    if (method !== "POST") fail(config, 405, "Sign-in requires POST");
    if (method === "POST") {
      if (body.username !== "demo" || body.password !== "demo")
        fail(config, 403, "Use demo as both the username and password");
      rememberSession(true);
    }
    if (!loggedIn) fail(config, 403, "No active demo session");
    return makeResponse(config, { username: "demo" });
  }
  if (path === "/admin/status") {
    if (method !== "GET")
      fail(config, 405, "Session checks require GET");
    if (!loggedIn) fail(config, 403, "No active demo session");
    return makeResponse(config, { username: "demo", status: "ok" });
  }
  if (path === "/admin/logout") {
    if (method !== "POST") fail(config, 405, "Sign-out requires POST");
    rememberSession(false);
    return makeResponse(config, { status: "ok" });
  }
  if (path.startsWith("/admin/") && !loggedIn)
    fail(config, 403, "No active demo session");

  const gameAction = path.match(/^\/admin\/game\/(start|start_practice|start_final|pause|resume|finish)$/)?.[1];
  if (gameAction) {
    if (method !== "POST") fail(config, 405, "Game controls require POST");
    if ((gameAction === "finish" || gameAction === "start_final") && body.confirm !== true)
      fail(config, 400, "Game action confirmation is required");
    if (body.generation !== undefined && body.generation !== generation)
      fail(config, 409, "Game session has changed");
    if (phase === "finished" && gameAction !== "finish")
      fail(config, 409, "Game has already finished");
    if (gameAction === "start" || gameAction === "start_practice" || gameAction === "start_final") {
      if (phase !== "waiting") fail(config, 409, "Game cannot be started in its current state");
      const early = new Date(scheduledStart).getTime() > Date.now();
      if (gameAction === "start_practice" && !early)
        fail(config, 409, "The scheduled start has arrived; rehearsal cannot start");
      practice = gameAction !== "start_final" && early;
      generation += 1;
      phase = "running";
      roundStart = Math.floor(Date.now() / 1000) - DEMO_ROUND_TIME;
    } else if (gameAction === "finish" && practice) {
      const originalStart = scheduledStart;
      prepareDemoGame();
      scheduledStart = originalStart;
    } else if (gameAction === "resume") {
      if (pausedAt !== null) pausedSeconds += Date.now() / 1000 - pausedAt;
      pausedAt = null;
      phase = "running";
    } else {
      pausedAt ??= Date.now() / 1000;
      phase = gameAction === "finish" ? "finished" : "paused";
    }
    publishState();
    return makeResponse(config, getRuntime());
  }

  const entity = path.match(/^\/admin\/(teams|tasks)(?:\/(\d+))?$/);
  if (entity) {
    const isTeam = entity[1] === "teams";
    const collection = isTeam ? teams : tasks;
    const id = entity[2] ? Number(entity[2]) : null;
    const existing = collection.find((item) => item.id === id);
    if (method === "GET") {
      if (id === null) return makeResponse(config, collection);
      if (!existing) fail(config, 404, "Record not found");
      return makeResponse(config, existing);
    }
    if (method === "DELETE") {
      if (!existing) fail(config, 404, "Record not found");
      existing.active = false;
      addMissingCells();
      publishState();
      return makeResponse(config, { status: "ok" });
    }
    if (method === "POST" || method === "PUT") {
      if (method === "PUT" && !existing) fail(config, 404, "Record not found");
      const nextId =
        existing?.id ??
        Math.max(0, ...collection.map((item) => item.id ?? 0)) + 1;
      let saved: Team | Task;
      if (isTeam) {
        saved = {
          ...createDemoTeams()[0],
          ...(existing ?? {}),
          ...body,
          id: nextId,
        } as Team;
        if (method === "POST")
          saved.token = [...crypto.getRandomValues(new Uint8Array(8))]
            .map((n) => n.toString(16).padStart(2, "0"))
            .join("");
        const error = validateTeam(saved, method === "POST");
        if (error) fail(config, 400, error);
        if (
          teams.some(
            (team) =>
              team.id !== nextId && team.token === (saved as Team).token,
          )
        )
          fail(config, 409, "Token is already used by another team");
        saved.name = saved.name.trim();
        saved.ip = saved.ip.trim();
        teams = [...teams.filter((item) => item.id !== nextId), saved];
      } else {
        saved = {
          ...createDemoTasks()[0],
          ...(existing ?? {}),
          ...body,
          id: nextId,
        } as Task;
        const error = validateTask(saved);
        if (error) fail(config, 400, error);
        tasks = [...tasks.filter((item) => item.id !== nextId), saved];
      }
      addMissingCells();
      recordSnapshot();
      publishState();
      return makeResponse(config, saved, method === "POST" ? 201 : 200);
    }
    fail(config, 405, "Action is not supported in demo mode");
  }
  if (path === "/admin/teamtasks") {
    const params = config.params as
      { team_id?: number; task_id?: number } | undefined;
    return makeResponse(
      config,
      demoLogs(Number(params?.team_id), Number(params?.task_id)),
    );
  }
  fail(config, 404, "Request is not supported in demo mode");
};

export function installDemoData() {
  try {
    loggedIn = sessionStorage.getItem("forcad-demo-auth") === "on";
  } catch {
    loggedIn = false;
  }
  api.defaults.adapter = demoAdapter;
  http.defaults.adapter = demoAdapter;
  resetDemoData();
}
