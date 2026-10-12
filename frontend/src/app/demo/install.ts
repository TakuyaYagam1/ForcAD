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
  DEMO_DEFAULT_SCORE,
  demoUuid,
  DEMO_INITIAL_ROUND,
  DEMO_ROUND_TIME,
} from "./data";

const ONE_HOUR_MS = 60 * 60 * 1000;
const ONE_DAY_MS = 24 * ONE_HOUR_MS;
const DEMO_SESSION_STORAGE_KEY = "forcad-demo-auth";
const DEMO_REQUEST_DELAY_MS = 100;
const DEMO_ADMIN_USERNAME = "demo";
const DEMO_ADMIN_PASSWORD = "demo";
const DEMO_CAPTURE_DELTA = 185;
const DEMO_PROMOTION_MARGIN = 275;
const MIN_DEMO_CAPTURE_PASS_RATE = 0.01;
const GAME_ACTION_PATH =
  /^\/admin\/game\/(start|start_practice|start_final|pause|resume|finish)$/;
const UUID_PATH = "[0-9a-f]{8}-[0-9a-f]{4}-[1-8][0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}";
const ADMIN_ENTITY_PATH = new RegExp(`^/admin/(teams|tasks)(?:/(${UUID_PATH}))?$`, "i");
const CLIENT_TEAM_HISTORY_PATH = new RegExp(`^/client/teams/(${UUID_PATH})$`, "i");

interface DemoCaptureSeed {
  attackerId: string;
  victimId: string;
  taskId: string;
  delta: number;
}

const INITIAL_CAPTURE_EVENTS: DemoCaptureSeed[] = [
  {
    attackerId: demoUuid(2),
    victimId: demoUuid(1),
    taskId: demoUuid(1001),
    delta: 185,
  },
  { attackerId: demoUuid(3), victimId: demoUuid(5), taskId: demoUuid(1002), delta: 122.5 },
  { attackerId: demoUuid(1), victimId: demoUuid(4), taskId: demoUuid(1003), delta: 210 },
  { attackerId: demoUuid(5), victimId: demoUuid(7), taskId: demoUuid(1004), delta: 96.75 },
  { attackerId: demoUuid(4), victimId: demoUuid(2), taskId: demoUuid(1001), delta: 154 },
  { attackerId: demoUuid(6), victimId: demoUuid(8), taskId: demoUuid(1002), delta: 88.25 },
];

let teams: Team[] = [];
let tasks: Task[] = [];
let rawTeamTasks: RawTeamTask[] = [];
let teamTaskHistory: TeamTaskStateRaw[] = [];
const demoAdminLogIds = new Map<string, string>();
let round = DEMO_INITIAL_ROUND;
let roundStart = 0;
let isLoggedIn = false;
let phase: "waiting" | "running" | "paused" | "finished" = "running";
let practice = false;
let generation = 0;
let scheduledStart = new Date(Date.now() - ONE_HOUR_MS).toISOString();
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
    round_waiting: false,
    results_pending: false,
  };
}

function getDemoConfig() {
  return {
    id: 1,
    flag_lifetime: 3,
    game_hardness: 2,
    inflation: false,
    volga_attacks_mode: false,
    round_time: DEMO_ROUND_TIME,
    rounds: null,
    mode: "demo",
    timezone: "UTC",
    start_time: scheduledStart,
    real_round: round,
    game_running: phase === "running",
  };
}

function getParticipantTeams() {
  return teams
    .filter((team) => team.active)
    .map(({ id, name, ip, highlighted, active, logo_path }) => ({
      id: id!,
      name,
      ip,
      highlighted,
      active,
      logo_path,
    }));
}

function getParticipantTasks() {
  return tasks
    .filter((task) => task.active)
    .map(({ id, name }) => ({ id: id!, name }));
}

function getDemoScoreboard() {
  const participantTeams = getParticipantTeams();
  const participantTasks = getParticipantTasks();
  const teamIds = new Set(participantTeams.map((team) => team.id));
  const taskIds = new Set(participantTasks.map((task) => task.id));

  return {
    state: {
      round_start: roundStart,
      round,
      team_tasks: rawTeamTasks.filter(
        (cell) => teamIds.has(cell.team_id) && taskIds.has(cell.task_id),
      ),
    },
    teams: participantTeams,
    tasks: participantTasks,
    config: getDemoConfig(),
    runtime: getRuntime(),
  };
}

function publishState() {
  const scoreboard = useScoreboardStore.getState();
  scoreboard.handleInitScoreboardMessage({
    teams: getParticipantTeams(),
    tasks: getParticipantTasks(),
    state: { round, round_start: roundStart, team_tasks: rawTeamTasks },
    runtime: getRuntime(),
  });
  scoreboard.setRoundTime(DEMO_ROUND_TIME);
  scoreboard.setConnected(true);
  refreshGameQueries();
}

function rememberSession(value: boolean) {
  isLoggedIn = value;
  try {
    if (value) sessionStorage.setItem(DEMO_SESSION_STORAGE_KEY, "on");
    else sessionStorage.removeItem(DEMO_SESSION_STORAGE_KEY);
  } catch {
    // In-memory demo login still works without storage.
  }
}

export function resetDemoData() {
  practice = false;
  generation += 1;
  scheduledStart = new Date(Date.now() - ONE_HOUR_MS).toISOString();
  phase = "running";
  pausedAt = null;
  pausedSeconds = 0;
  teams = createDemoTeams();
  tasks = createDemoTasks();
  rawTeamTasks = createDemoTeamTasks(teams, tasks);
  round = DEMO_INITIAL_ROUND;
  const now = Math.floor(Date.now() / 1000);
  roundStart = now - DEMO_ROUND_TIME - 35;
  teamTaskHistory = createDemoHistory(rawTeamTasks, round, now);
  demoAdminLogIds.clear();
  publishState();

  const scoreboard = useScoreboardStore.getState();
  const seededEvents = INITIAL_CAPTURE_EVENTS.map(
    ({ attackerId, victimId, taskId, delta }, index) => ({
      id: demoUuid(300_000 + index),
      ts: Date.now() - (index * 43 + 12) * 1000,
      attackerId,
      victimId,
      taskId,
      attackerName: scoreboard.teams!.find((team) => team.id === attackerId)!
        .name,
      victimName: scoreboard.teams!.find((team) => team.id === victimId)!.name,
      taskName: tasks.find((task) => task.id === taskId)!.name,
      delta,
    }),
  );
  useLiveScoreboardStore.setState({ events: seededEvents, error: null });
}

export function prepareDemoGame() {
  phase = "waiting";
  practice = false;
  generation += 1;
  scheduledStart = new Date(Date.now() + ONE_DAY_MS).toISOString();
  pausedAt = null;
  pausedSeconds = 0;
  round = 0;
  roundStart = 0;
  teamTaskHistory = [];
  demoAdminLogIds.clear();
  rawTeamTasks = rawTeamTasks.map((cell) => ({
    ...cell,
    score: DEMO_DEFAULT_SCORE,
    stolen: 0,
    lost: 0,
    checks: 0,
    checks_passed: 0,
    status: -1,
  }));
  publishState();
}

function recordSnapshot() {
  const now = Math.floor(Date.now() / 1000);
  teamTaskHistory.unshift(
    ...rawTeamTasks.map((teamTask) => ({
      ...teamTask,
      round,
      timestamp: `${now}-${tasks.findIndex((task) => task.id === teamTask.task_id) + 1}`,
    })),
  );
}

export function simulateDemoCapture(promote = false) {
  if (phase !== "running") return;
  const rankedTeams = useScoreboardStore.getState().teams ?? [];
  const activeTeams = rankedTeams.filter((team) => team.active);
  const activeTaskIds = new Set(
    tasks.filter((task) => task.active).map((task) => task.id),
  );
  if (activeTeams.length < 2 || !activeTaskIds.size) return;
  const attacker = activeTeams[1];
  const victim = activeTeams[0];
  const attackerCell = rawTeamTasks.find(
    (teamTask) =>
      teamTask.team_id === attacker.id &&
      activeTaskIds.has(teamTask.task_id) &&
      teamTask.checks_passed > 0,
  );
  const victimCell = rawTeamTasks.find(
    (teamTask) =>
      teamTask.team_id === victim.id &&
      teamTask.task_id === attackerCell?.task_id,
  );
  if (!attackerCell || !victimCell) return;
  const delta = promote
    ? Math.max(
        DEMO_CAPTURE_DELTA,
        ((victim.score ?? 0) - (attacker.score ?? 0) + DEMO_PROMOTION_MARGIN) /
          Math.max(
            MIN_DEMO_CAPTURE_PASS_RATE,
            attackerCell.checks_passed / Math.max(1, attackerCell.checks),
          ),
      )
    : DEMO_CAPTURE_DELTA;
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
    team_tasks: rawTeamTasks,
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
  const teamIds = new Set(teams.map((team) => team.id));
  const taskIds = new Set(tasks.map((task) => task.id));
  const existingCells = new Set(
    rawTeamTasks.map(({ team_id, task_id }) => `${team_id}:${task_id}`),
  );

  for (const team of teams) {
    for (const task of tasks) {
      const cellKey = `${team.id}:${task.id}`;
      if (existingCells.has(cellKey)) continue;
      rawTeamTasks.push({
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
      existingCells.add(cellKey);
    }
  }
  rawTeamTasks = rawTeamTasks.filter(
    (teamTask) =>
      teamIds.has(teamTask.team_id) && taskIds.has(teamTask.task_id),
  );
}

function demoLogs(teamId: string, taskId: string): TeamTaskLogEntry[] {
  return teamTaskHistory
    .filter((entry) => entry.team_id === teamId && entry.task_id === taskId)
    .slice(0, 15)
    .map((entry) => ({
      ...entry,
      id: getDemoAdminLogId(entry),
      team_id: teamId,
      task_id: taskId,
      ts: Number(entry.timestamp.slice(0, entry.timestamp.indexOf("-"))),
      public_message: entry.message || "OK",
      private_message: "Demo entry: check completed",
      command: `python demo/checker.py check ${teams.find((team) => team.id === teamId)?.ip ?? "10.10.14.8"}`,
    }));
}

function getDemoAdminLogId(entry: TeamTaskStateRaw): string {
  const key = `${entry.team_id}:${entry.task_id}:${entry.timestamp}`;
  const existingId = demoAdminLogIds.get(key);
  if (existingId) return existingId;

  const id = crypto.randomUUID();
  demoAdminLogIds.set(key, id);
  return id;
}

type DemoResponse = ReturnType<typeof makeResponse>;

function handleClientRequest(
  config: InternalAxiosRequestConfig,
  requestPath: string,
): DemoResponse | undefined {
  if (requestPath === "/client/status") return makeResponse(config, getRuntime());
  if (requestPath === "/client/config")
    return makeResponse(config, getDemoConfig());
  if (requestPath === "/client/scoreboard")
    return makeResponse(config, getDemoScoreboard());
  if (requestPath === "/client/teams")
    return makeResponse(config, getParticipantTeams());
  if (requestPath === "/client/tasks")
    return makeResponse(config, getParticipantTasks());

  const teamHistoryMatch = requestPath.match(CLIENT_TEAM_HISTORY_PATH);
  if (!teamHistoryMatch) return;

  const teamId = teamHistoryMatch[1]!.toLowerCase();
  return makeResponse(
    config,
    teamTaskHistory.filter((entry) => entry.team_id === teamId),
  );
}

function handleAdminSessionRequest(
  config: InternalAxiosRequestConfig,
  requestPath: string,
  method: string,
  body: Record<string, unknown>,
): DemoResponse | undefined {
  if (requestPath === "/admin/login") {
    if (method !== "POST") fail(config, 405, "Sign-in requires POST");
    if (
      body.username !== DEMO_ADMIN_USERNAME ||
      body.password !== DEMO_ADMIN_PASSWORD
    ) {
      fail(config, 403, "Use demo as both the username and password");
    }
    rememberSession(true);
    if (!isLoggedIn) fail(config, 403, "No active demo session");
    return makeResponse(config, { status: "ok", username: DEMO_ADMIN_USERNAME });
  }
  if (requestPath === "/admin/status") {
    if (method !== "GET") fail(config, 405, "Session checks require GET");
    if (!isLoggedIn) fail(config, 403, "No active demo session");
    return makeResponse(config, {
      username: DEMO_ADMIN_USERNAME,
      status: "ok",
    });
  }
  if (requestPath === "/admin/logout") {
    if (method !== "POST") fail(config, 405, "Sign-out requires POST");
    rememberSession(false);
    return makeResponse(config, { status: "ok" });
  }
}

function requireAdminSession(
  config: InternalAxiosRequestConfig,
  requestPath: string,
): void {
  if (requestPath.startsWith("/admin/") && !isLoggedIn) {
    fail(config, 403, "No active demo session");
  }
}

function handleGameActionRequest(
  config: InternalAxiosRequestConfig,
  requestPath: string,
  method: string,
  body: Record<string, unknown>,
): DemoResponse | undefined {
  const gameAction = requestPath.match(GAME_ACTION_PATH)?.[1];
  if (!gameAction) return;

  if (method !== "POST") fail(config, 405, "Game controls require POST");
  if (
    (gameAction === "finish" || gameAction === "start_final") &&
    body.confirm !== true
  ) {
    fail(config, 400, "Game action confirmation is required");
  }
  if (body.generation !== undefined && body.generation !== generation) {
    fail(config, 409, "Game session has changed");
  }
  if (phase === "finished" && gameAction !== "finish") {
    fail(config, 409, "Game has already finished");
  }

  const startsGame =
    gameAction === "start" ||
    gameAction === "start_practice" ||
    gameAction === "start_final";
  if (startsGame) {
    if (phase !== "waiting") {
      fail(config, 409, "Game cannot be started in its current state");
    }
    const scheduledStartIsFuture =
      new Date(scheduledStart).getTime() > Date.now();
    if (gameAction === "start_practice" && !scheduledStartIsFuture) {
      fail(config, 409, "The scheduled start has arrived; rehearsal cannot start");
    }
    practice = gameAction !== "start_final" && scheduledStartIsFuture;
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

function handleAdminEntityRequest(
  config: InternalAxiosRequestConfig,
  requestPath: string,
  method: string,
  body: Record<string, unknown>,
): DemoResponse | undefined {
  const entityMatch = requestPath.match(ADMIN_ENTITY_PATH);
  if (!entityMatch) return;

  const isTeamCollection = entityMatch[1] === "teams";
  const collection = isTeamCollection ? teams : tasks;
  const recordId = entityMatch[2]?.toLowerCase() ?? null;
  const existingRecord = collection.find((record) => record.id === recordId);

  if (method === "GET") {
    if (recordId === null) return makeResponse(config, collection);
    if (!existingRecord) fail(config, 404, "Record not found");
    return makeResponse(config, existingRecord);
  }
  if (method === "DELETE") {
    if (!existingRecord) fail(config, 404, "Record not found");
    existingRecord.active = false;
    addMissingCells();
    publishState();
    return makeResponse(config, { status: "ok" });
  }
  if (method !== "POST" && method !== "PUT") {
    fail(config, 405, "Action is not supported in demo mode");
  }
  if (method === "PUT" && !existingRecord) {
    fail(config, 404, "Record not found");
  }

  const nextId = existingRecord?.id ?? crypto.randomUUID();
  let savedRecord: Team | Task;

  if (isTeamCollection) {
    const savedTeam = {
      ...createDemoTeams()[0],
      ...(existingRecord ?? {}),
      ...body,
      id: nextId,
    } as Team;
    if (method === "POST") {
      savedTeam.token = [...crypto.getRandomValues(new Uint8Array(8))]
        .map((byte) => byte.toString(16).padStart(2, "0"))
        .join("");
    }
    const validationError = validateTeam(savedTeam, method === "POST");
    if (validationError) fail(config, 400, validationError);
    const tokenAlreadyExists = teams.some(
      (team) => team.id !== nextId && team.token === savedTeam.token,
    );
    if (tokenAlreadyExists) {
      fail(config, 409, "Token is already used by another team");
    }
    savedTeam.name = savedTeam.name.trim();
    savedTeam.ip = savedTeam.ip.trim();
    teams = [...teams.filter((team) => team.id !== nextId), savedTeam];
    savedRecord = savedTeam;
  } else {
    const savedTask = {
      ...createDemoTasks()[0],
      ...(existingRecord ?? {}),
      ...body,
      id: nextId,
    } as Task;
    const validationError = validateTask(savedTask);
    if (validationError) fail(config, 400, validationError);
    tasks = [...tasks.filter((task) => task.id !== nextId), savedTask];
    savedRecord = savedTask;
  }

  addMissingCells();
  recordSnapshot();
  publishState();
  return makeResponse(config, savedRecord, method === "POST" ? 201 : 200);
}

function handleAdminTeamTaskLogRequest(
  config: InternalAxiosRequestConfig,
  requestPath: string,
): DemoResponse | undefined {
  if (requestPath !== "/admin/teamtasks") return;

  const params = config.params as
    | { team_id?: string; task_id?: string }
    | undefined;
  return makeResponse(
    config,
    params?.team_id && params.task_id
      ? demoLogs(params.team_id.toLowerCase(), params.task_id.toLowerCase())
      : [],
  );
}

const demoAdapter: AxiosAdapter = async (config) => {
  // Resolve requests entirely in the browser; no backend connection is made.
  await new Promise<void>((resolve) =>
    window.setTimeout(resolve, DEMO_REQUEST_DELAY_MS),
  );
  if (config.signal?.aborted) {
    throw new AxiosError("Cancelled", AxiosError.ERR_CANCELED, config);
  }

  const requestBase = config.baseURL ?? window.location.origin;
  const requestUrl = new URL(config.url ?? "/", requestBase + "/");
  const requestPath = requestUrl.pathname.replace(/^\/api/, "").replace(/\/$/, "");
  const method = config.method?.toUpperCase() ?? "GET";
  const body = readBody(config);

  const clientResponse = handleClientRequest(config, requestPath);
  if (clientResponse) return clientResponse;

  const sessionResponse = handleAdminSessionRequest(
    config,
    requestPath,
    method,
    body,
  );
  if (sessionResponse) return sessionResponse;

  requireAdminSession(config, requestPath);

  const gameResponse = handleGameActionRequest(
    config,
    requestPath,
    method,
    body,
  );
  if (gameResponse) return gameResponse;

  const entityResponse = handleAdminEntityRequest(
    config,
    requestPath,
    method,
    body,
  );
  if (entityResponse) return entityResponse;

  const logResponse = handleAdminTeamTaskLogRequest(config, requestPath);
  if (logResponse) return logResponse;

  fail(config, 404, "Request is not supported in demo mode");
};

export function installDemoData() {
  try {
    isLoggedIn = sessionStorage.getItem(DEMO_SESSION_STORAGE_KEY) === "on";
  } catch {
    isLoggedIn = false;
  }
  api.defaults.adapter = demoAdapter;
  http.defaults.adapter = demoAdapter;
  resetDemoData();
}
