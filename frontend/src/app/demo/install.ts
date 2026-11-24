import { validateTeam, validateTask } from "@/shared/lib/adminValidation";
import { refreshGameQueries } from "@/shared/lib/queryClient";
import { randomHex, randomUuid } from "@/shared/lib/random";
import { AxiosError, AxiosHeaders } from "axios";
import type { AxiosAdapter, InternalAxiosRequestConfig } from "axios";
import type { Team } from "@/entities/team/model/types";
import type { Task } from "@/entities/task/model/types";
import type { TeamTaskLogEntry } from "@/entities/team-task/model/types";
import {
  TEAM_HISTORY_PAGE_SIZE,
  type TeamTaskStateRaw,
} from "@/entities/team-scoreboard/api";
import { useScoreboardStore } from "@/entities/scoreboard/model/store";
import type { GameRuntimeStatus, RawTeamTask } from "@/entities/scoreboard/model/store";
import { useLiveScoreboardStore } from "@/entities/live-scoreboard/model/store";
import {
  AdminBulkDeactivateRequest,
  AdminGameSettingsUpdate,
  GetAdminCheckerStatsParams,
  type AdminCheckerStats,
} from "@/shared/api/generated/schemas";
import { api, http } from "@/shared/lib/axios";
import { STATUS_OK } from "@/shared/config/statuses";
import {
  createDemoTeams,
  createDemoTasks,
  createDemoTeamTasks,
  createDemoHistory,
  DEMO_DEFAULT_SCORE,
  DEMO_INITIAL_CAPTURE_EVENTS,
  type DemoCaptureSeed,
  demoUuid,
  DEMO_INITIAL_ROUND,
  DEMO_ROUND_TIME,
} from "./data";

const ONE_HOUR_MS = 60 * 60 * 1000;
const ONE_DAY_MS = 24 * ONE_HOUR_MS;
const DEMO_SESSION_STORAGE_KEY = "forcad-demo-auth";
const DEMO_REQUEST_DELAY_MS = 100;
const DEMO_ADMIN_LOG_LIMIT = 10;
const DEMO_ADMIN_USERNAME = "demo";
const DEMO_ADMIN_PASSWORD = "demo";
const DEMO_CAPTURE_DELTA = 185;
const DEMO_PROMOTION_MARGIN = 275;
const MIN_DEMO_CAPTURE_PASS_RATE = 0.01;
const GAME_ACTION_PATH =
  /^\/admin\/game\/(start|start_practice|start_final|pause|resume|finish)$/;
const UUID_PATH = "[0-9a-f]{8}-[0-9a-f]{4}-[1-8][0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}";
const UUID_PATTERN = new RegExp(`^${UUID_PATH}$`, "i");
const ADMIN_ENTITY_PATH = new RegExp(`^/admin/(teams|tasks)(?:/(${UUID_PATH}))?$`, "i");
const ADMIN_BULK_PATH = /^\/admin\/(teams|tasks)\/(activate|deactivate)$/;
const CLIENT_TEAM_HISTORY_PATH = new RegExp(`^/client/teams/(${UUID_PATH})$`, "i");

const initialDemoGameSettings = (): Omit<AdminGameSettingsUpdate, "generation" | "start_time"> => {
  return {
    rounds: 300,
    flag_lifetime: 3,
    game_hardness: 2,
    inflation: false,
    volga_attacks_mode: false,
    round_time: DEMO_ROUND_TIME,
    extra_checks: 0,
    mode: "classic",
    timezone: "UTC",
  }
};

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
let demoCsrfToken = "";
let demoGameSettings = initialDemoGameSettings();

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
    round_time: demoGameSettings.round_time,
    round: phase === "waiting" ? 0 : round + 1,
    round_start: phase === "waiting" ? null : roundStart + demoGameSettings.round_time,
    round_waiting: false,
    results_pending: false,
  };
}

function getDemoConfig() {
  return {
    id: 1,
    flag_lifetime: demoGameSettings.flag_lifetime,
    game_hardness: demoGameSettings.game_hardness,
    inflation: demoGameSettings.inflation,
    volga_attacks_mode: demoGameSettings.volga_attacks_mode,
    round_time: demoGameSettings.round_time,
    rounds: demoGameSettings.rounds,
    mode: demoGameSettings.mode,
    timezone: demoGameSettings.timezone,
    start_time: scheduledStart,
    real_round: round,
    game_running: phase === "running",
  };
}

function getParticipantTeams() {
  return teams
    .filter((team) => {
      return team.active
    })
    .map(({ id, name, ip, kind, highlighted, active, logo_path }) => {
      return {
        id: id!,
        name,
        ip,
        kind,
        highlighted,
        active,
        logo_path,
      }
    });
}

function getParticipantTasks() {
  return tasks
    .filter((task) => {
      return task.active
    })
    .map(({ id, name }) => {
      return {
        id: id!,
        name
      }
    });
}

function demoCheckerMessage(status: number) {
  switch (status) {
    case STATUS_OK:
      return "OK";
    case 102:
      return "Stored data is missing or corrupted";
    case 103:
      return "The service returned an invalid response";
    case 104:
      return "Service unavailable: connection refused";
    case 110:
      return "The checker could not complete the check";
    default:
      return "";
  }
}

function withDemoCheckerMessage<T extends RawTeamTask>(cell: T) {
  return {
    ...cell,
    message: demoCheckerMessage(cell.status),
  };
}

function getDemoScoreboard() {
  const participantTeams = getParticipantTeams();
  const participantTasks = getParticipantTasks();
  const teamIds = new Set(participantTeams.map((team) => {
    return team.id
  }));
  const taskIds = new Set(participantTasks.map((task) => {
    return task.id
  }));

  return {
    state: {
      round_start: roundStart,
      round,
      team_tasks: rawTeamTasks.filter(
        (cell) => {
          return teamIds.has(cell.team_id) && taskIds.has(cell.task_id)
        },
      ).map(withDemoCheckerMessage),
    },
    teams: participantTeams,
    tasks: participantTasks,
    config: getDemoConfig(),
    runtime: getRuntime(),
  };
}

function publishState() {
  const scoreboard = useScoreboardStore.getState();
  scoreboard.handleInitScoreboardMessage(getDemoScoreboard());
  scoreboard.setRoundTime(demoGameSettings.round_time);
  scoreboard.setConnected(true);
  refreshGameQueries();
}

function rememberSession(value: boolean) {
  isLoggedIn = value;
  demoCsrfToken = value ? randomHex(32) : "";
  try {
    if (value) {
      sessionStorage.setItem(DEMO_SESSION_STORAGE_KEY, "on");
    } else {
      sessionStorage.removeItem(DEMO_SESSION_STORAGE_KEY);
    }
  } catch {
    // In-memory demo login still works without storage.
  }
}

export function resetDemoData() {
  practice = false;
  generation += 1;
  scheduledStart = new Date(Date.now() - ONE_HOUR_MS).toISOString();
  demoGameSettings = initialDemoGameSettings();
  phase = "running";
  pausedAt = null;
  pausedSeconds = 0;
  teams = createDemoTeams();
  tasks = createDemoTasks();
  rawTeamTasks = createDemoTeamTasks(teams, tasks);
  round = DEMO_INITIAL_ROUND;
  const now = Math.floor(Date.now() / 1000);
  roundStart = now - demoGameSettings.round_time - 35;
  teamTaskHistory = createDemoHistory(rawTeamTasks, round, now);
  demoAdminLogIds.clear();
  publishState();

  const scoreboard = useScoreboardStore.getState();
  const captureCounts = new Map<number, number>();
  for (const event of DEMO_INITIAL_CAPTURE_EVENTS) {
    captureCounts.set(
      event.roundOffset,
      (captureCounts.get(event.roundOffset) ?? 0) + 1,
    );
  }
  const capturePositions = new Map<number, number>();
  const seededEvents = DEMO_INITIAL_CAPTURE_EVENTS.map(
    ({ attackerId, victimId, taskId, delta, roundOffset }, index) => {
      const capturePosition = capturePositions.get(roundOffset) ?? 0;
      capturePositions.set(roundOffset, capturePosition + 1);
      const captureCount = captureCounts.get(roundOffset)!;
      const roundEnd =
        roundStart + demoGameSettings.round_time * (1 - roundOffset);
      const secondsBeforeRoundEnd =
        ((capturePosition + 1) / (captureCount + 1)) * demoGameSettings.round_time;
      return {
        id: demoUuid(300_000 + index),
        ts: Math.floor(roundEnd - secondsBeforeRoundEnd) * 1000,
        attackerId,
        victimId,
        taskId,
        attackerName: scoreboard.teams!.find((team) => {
          return team.id === attackerId
        })!
          .name,
        victimName: scoreboard.teams!.find((team) => {
          return team.id === victimId
        })!.name,
        taskName: tasks.find((task) => {
          return task.id === taskId
        })!.name,
        delta,
      }
    },
  );
  useLiveScoreboardStore.setState({
    events: seededEvents,
    error: null
  });
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
  rawTeamTasks = rawTeamTasks.map((cell) => {
    return {
      ...cell,
      score: DEMO_DEFAULT_SCORE,
      stolen: 0,
      lost: 0,
      checks: 0,
      checks_passed: 0,
      status: -1,
    }
  });
  publishState();
}

function recordSnapshot(
  capture?: Pick<
    DemoCaptureSeed,
    "attackerId" | "victimId" | "taskId" | "delta"
  >,
) {
  const snapshotRound = round > 0 ? round : getRuntime().round;
  let previousSnapshots: Map<string, TeamTaskStateRaw> | undefined;
  if (teamTaskHistory[0]?.round === snapshotRound) {
    previousSnapshots = new Map();
    for (const entry of teamTaskHistory) {
      if (entry.round !== snapshotRound) {
        break;
      }
      previousSnapshots.set(`${entry.team_id}:${entry.task_id}`, entry);
    }
  }
  const creditedDelta = capture?.delta ?? 0;
  const now = Math.floor(Date.now() / 1000);
  const snapshots = rawTeamTasks.map((teamTask) => {
    const key = `${teamTask.team_id}:${teamTask.task_id}`;
    const previous = previousSnapshots?.get(key);
    const capturedFlag = capture !== undefined &&
      teamTask.team_id === capture.attackerId &&
      teamTask.task_id === capture.taskId;
    const lostFlag = capture !== undefined &&
      teamTask.team_id === capture.victimId &&
      teamTask.task_id === capture.taskId;
    return {
      ...teamTask,
      round: snapshotRound,
      round_flags_captured:
        (previous?.round_flags_captured ?? 0) + (capturedFlag ? 1 : 0),
      round_fp_gained:
        (previous?.round_fp_gained ?? 0) + (capturedFlag ? creditedDelta : 0),
      round_flags_lost:
        (previous?.round_flags_lost ?? 0) + (lostFlag ? 1 : 0),
      round_opponent_fp:
        (previous?.round_opponent_fp ?? 0) + (lostFlag ? creditedDelta : 0),
      timestamp: `${now}-${tasks.findIndex((task) => {
        return task.id === teamTask.task_id
      }) + 1}`,
    }
  });
  if (previousSnapshots) {
    teamTaskHistory.splice(0, previousSnapshots.size, ...snapshots);
  } else {
    teamTaskHistory.unshift(...snapshots);
  }
}

export function simulateDemoCapture(promote = false) {
  if (phase !== "running") {
    return;
  }
  const rankedTeams = useScoreboardStore.getState().teams ?? [];
  const activeTeams = rankedTeams.filter((team) => {
    return team.active
  });
  const activeTaskIds = new Set(
    tasks.filter((task) => {
      return task.active
    }).map((task) => {
      return task.id
    }),
  );
  if (activeTeams.length < 2 || !activeTaskIds.size) {
    return;
  }
  const attacker = activeTeams[1];
  const victim = activeTeams[0];
  const attackerCell = rawTeamTasks.find(
    (teamTask) => {
      return teamTask.team_id === attacker.id &&
      activeTaskIds.has(teamTask.task_id) &&
      teamTask.checks_passed > 0
    },
  );
  const victimCell = rawTeamTasks.find(
    (teamTask) => {
      return teamTask.team_id === victim.id &&
      teamTask.task_id === attackerCell?.task_id
    },
  );
  if (!attackerCell || !victimCell) {
    return;
  }
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
  roundStart = Math.floor(Date.now() / 1000) - demoGameSettings.round_time;
  const capture = {
    attackerId: attacker.id!,
    victimId: victim.id!,
    taskId: attackerCell.task_id,
    delta,
  };
  recordSnapshot(capture);
  useScoreboardStore.getState().setRuntime(getRuntime());
  useScoreboardStore.getState().handleUpdateScoreboardMessage({
    round,
    round_start: roundStart,
    team_tasks: rawTeamTasks.map(withDemoCheckerMessage),
  });
  useLiveScoreboardStore.getState().pushNotification({
    generation,
    attacker_id: capture.attackerId,
    victim_id: capture.victimId,
    task_id: capture.taskId,
    attacker_delta: capture.delta,
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
    headers: new AxiosHeaders({
      "content-type": "application/json"
    }),
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
    makeResponse(config, {
      error: message
    }, status),
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
  const teamIds = new Set(teams.map((team) => {
    return team.id
  }));
  const taskIds = new Set(tasks.map((task) => {
    return task.id
  }));
  const existingCells = new Set(
    rawTeamTasks.map(({ team_id, task_id }) => {
      return `${team_id}:${task_id}`
    }),
  );

  for (const team of teams) {
    for (const task of tasks) {
      const cellKey = `${team.id}:${task.id}`;
      if (existingCells.has(cellKey)) {
        continue;
      }
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
    (teamTask) => {
      return teamIds.has(teamTask.team_id) && taskIds.has(teamTask.task_id)
    },
  );
}

function demoLogs(teamId: string, taskId: string): TeamTaskLogEntry[] {
  const entries: TeamTaskStateRaw[] = [];
  for (const entry of teamTaskHistory) {
    if (entry.team_id !== teamId || entry.task_id !== taskId) {
      continue;
    }
    entries.push(entry);
    if (entries.length >= DEMO_ADMIN_LOG_LIMIT) {
      break;
    }
  }
  return entries.map((entry) => {
    return {
      ...entry,
      id: getDemoAdminLogId(entry),
      team_id: teamId,
      task_id: taskId,
      ts: Number(entry.timestamp.slice(0, entry.timestamp.indexOf("-"))),
      public_message: demoCheckerMessage(entry.status),
      private_message: "Demo: check completed",
      command: `python demo/checker.py check ${teams.find((team) => {
        return team.id === teamId
      })?.ip ?? "10.10.14.8"}`,
    }
  });
}

function getDemoAdminLogId(entry: TeamTaskStateRaw): string {
  const key = `${entry.team_id}:${entry.task_id}:${entry.timestamp}`;
  const existingId = demoAdminLogIds.get(key);
  if (existingId) {
    return existingId;
  }

  const id = randomUuid();
  demoAdminLogIds.set(key, id);
  return id;
}

type DemoResponse = ReturnType<typeof makeResponse>;

function handleClientRequest(
  config: InternalAxiosRequestConfig,
  requestPath: string,
): DemoResponse | undefined {
  if (requestPath === "/client/status") {
    return makeResponse(config, getRuntime());
  }
  if (requestPath === "/client/config") {
    return makeResponse(config, getDemoConfig());
  }
  if (requestPath === "/client/scoreboard") {
    return makeResponse(config, getDemoScoreboard());
  }
  if (requestPath === "/client/teams") {
    return makeResponse(config, getParticipantTeams());
  }
  if (requestPath === "/client/tasks") {
    return makeResponse(config, getParticipantTasks());
  }

  const teamHistoryMatch = requestPath.match(CLIENT_TEAM_HISTORY_PATH);
  if (!teamHistoryMatch) {
    return;
  }
  const teamId = teamHistoryMatch[1]!.toLowerCase();

  const params = config.params as
    | {
      task_id?: string;
      through_round?: number;
    }
    | undefined;
  const taskIdValue = params?.task_id;
  if (taskIdValue !== undefined && !UUID_PATTERN.test(taskIdValue)) {
    fail(config, 400, "Invalid team history task ID");
  }
  const taskId = taskIdValue?.toLowerCase();
  const throughRound = params?.through_round;
  if (
    throughRound !== undefined &&
    (
      !Number.isInteger(throughRound) ||
      throughRound <= 0 ||
      throughRound > 2_147_483_647
    )
  ) {
    fail(config, 400, "Invalid team history round");
  }

  const history: TeamTaskStateRaw[] = [];
  const latestPublishedRound = Math.min(throughRound ?? round, round);
  let includedRounds = 0;
  let previousRound: number | undefined;
  for (const entry of teamTaskHistory) {
    if (entry.round <= 0 || entry.round > latestPublishedRound) {
      continue;
    }
    const matchesTeam = entry.team_id === teamId;
    const matchesScope = matchesTeam &&
      (taskId === undefined || entry.task_id === taskId);
    if (taskId !== undefined && !matchesScope) {
      continue;
    }
    if (entry.round !== previousRound) {
      if (includedRounds >= TEAM_HISTORY_PAGE_SIZE) {
        break;
      }
      previousRound = entry.round;
      includedRounds += 1;
    }
    if (matchesScope) {
      history.push(withDemoCheckerMessage(entry));
    }
  }
  return makeResponse(config, history);
}

function handleAdminSessionRequest(
  config: InternalAxiosRequestConfig,
  requestPath: string,
  method: string,
  body: Record<string, unknown>,
): DemoResponse | undefined {
  if (requestPath === "/admin/login") {
    if (method !== "POST") {
      fail(config, 405, "Sign-in requires POST");
    }
    if (
      body.username !== DEMO_ADMIN_USERNAME ||
      body.password !== DEMO_ADMIN_PASSWORD
    ) {
      fail(config, 403, "Use demo as both the username and password");
    }
    rememberSession(true);
    if (!isLoggedIn) {
      fail(config, 403, "No active demo session");
    }
    return makeResponse(config, {
      status: "ok",
      username: DEMO_ADMIN_USERNAME,
      csrf_token: demoCsrfToken
    });
  }
  if (requestPath === "/admin/status") {
    if (method !== "GET") {
      fail(config, 405, "Session checks require GET");
    }
    if (!isLoggedIn) {
      fail(config, 403, "No active demo session");
    }
    return makeResponse(config, {
      username: DEMO_ADMIN_USERNAME,
      status: "ok",
      csrf_token: demoCsrfToken,
    });
  }
  if (requestPath === "/admin/logout") {
    if (method !== "POST") {
      fail(config, 405, "Sign-out requires POST");
    }
    rememberSession(false);
    return makeResponse(config, {
      status: "ok"
    });
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

function handleGameSettingsRequest(
  config: InternalAxiosRequestConfig,
  requestPath: string,
  method: string,
  body: Record<string, unknown>,
): DemoResponse | undefined {
  if (requestPath !== "/admin/game/config") {
    return;
  }
  if (method === "GET") {
    return makeResponse(config, {
      config: getDemoConfig(),
      runtime: getRuntime(),
      extra_checks: demoGameSettings.extra_checks,
    });
  }
  if (method !== "PUT") {
    fail(config, 405, "Game settings require PUT");
  }
  const parsed = AdminGameSettingsUpdate.safeParse(body);
  if (!parsed.success) {
    fail(config, 400, "Invalid game settings");
  }
  const next = parsed.data;
  if (next.game_hardness <= 1 || (next.mode !== "classic" && next.extra_checks > 0)) {
    fail(config, 400, "Invalid game settings");
  }
  try {
    if (next.timezone === "Local") {
      fail(config, 400, "Invalid timezone");
    }
    new Intl.DateTimeFormat("en", {
      timeZone: next.timezone
    });
  } catch {
    fail(config, 400, "Invalid timezone");
  }
  if (next.generation !== generation || phase === "finished") {
    fail(config, 409, "Game session has changed");
  }
  const changedRules =
    next.flag_lifetime !== demoGameSettings.flag_lifetime ||
    next.game_hardness !== demoGameSettings.game_hardness ||
    next.inflation !== demoGameSettings.inflation ||
    next.volga_attacks_mode !== demoGameSettings.volga_attacks_mode ||
    next.round_time !== demoGameSettings.round_time ||
    next.extra_checks !== demoGameSettings.extra_checks ||
    next.mode !== demoGameSettings.mode ||
    next.timezone !== demoGameSettings.timezone ||
    Date.parse(next.start_time) !== Date.parse(scheduledStart);
  if (phase !== "waiting" && changedRules) {
    fail(config, 409, "Only the round limit can change after game start");
  }
  if (phase !== "waiting" && next.rounds !== null && next.rounds <= round) {
    fail(config, 409, "Round limit must exceed the current round");
  }
  demoGameSettings = {
    rounds: next.rounds,
    flag_lifetime: next.flag_lifetime,
    game_hardness: next.game_hardness,
    inflation: next.inflation,
    volga_attacks_mode: next.volga_attacks_mode,
    round_time: next.round_time,
    extra_checks: next.extra_checks,
    mode: next.mode,
    timezone: next.timezone,
  };
  scheduledStart = new Date(next.start_time).toISOString();
  publishState();
  return makeResponse(config, {
    config: getDemoConfig(),
    runtime: getRuntime(),
    extra_checks: demoGameSettings.extra_checks,
  });
}

function handleBulkStateRequest(
  config: InternalAxiosRequestConfig,
  requestPath: string,
  method: string,
  body: Record<string, unknown>,
): DemoResponse | undefined {
  const match = requestPath.match(ADMIN_BULK_PATH);
  if (!match) {
    return;
  }
  const activate = match[2] === "activate";
  if (method !== "POST") {
    fail(
      config,
      405,
      activate
        ? "Batch activation requires POST"
        : "Batch deactivation requires POST",
    );
  }
  const parsed = AdminBulkDeactivateRequest.safeParse(body);
  if (!parsed.success) {
    fail(config, 400, "Batch must contain 1–100 unique IDs");
  }
  const requestedIds = new Set(parsed.data.ids);
  if (requestedIds.size !== parsed.data.ids.length) {
    fail(config, 400, "Batch must contain 1–100 unique IDs");
  }
  const collection = match[1] === "teams" ? teams : tasks;
  const collectionIds = new Set(collection.flatMap((record) => {
    return record.id === null ? [] : [record.id]
  }));
  if (parsed.data.ids.some((id) => {
    return !collectionIds.has(id)
  })) {
    fail(config, 404, "Record not found");
  }
  let changed = 0;
  for (const record of collection) {
    if (
      record.active !== activate
      && record.id !== null
      && requestedIds.has(record.id)
    ) {
      record.active = activate;
      changed += 1;
    }
  }
  if (changed > 0) {
    addMissingCells();
    publishState();
  }
  return makeResponse(
    config,
    activate
      ? {
        activated: changed,
      }
      : {
        deactivated: changed,
      },
  );
}

function handleGameActionRequest(
  config: InternalAxiosRequestConfig,
  requestPath: string,
  method: string,
  body: Record<string, unknown>,
): DemoResponse | undefined {
  const gameAction = requestPath.match(GAME_ACTION_PATH)?.[1];
  if (!gameAction) {
    return;
  }

  if (method !== "POST") {
    fail(config, 405, "Game controls require POST");
  }
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
    roundStart = Math.floor(Date.now() / 1000) - demoGameSettings.round_time;
    recordSnapshot();
  } else if (gameAction === "finish" && practice) {
    const originalStart = scheduledStart;
    prepareDemoGame();
    scheduledStart = originalStart;
  } else if (gameAction === "resume") {
    if (pausedAt !== null) {
      pausedSeconds += Date.now() / 1000 - pausedAt;
    }
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
  if (!entityMatch) {
    return;
  }

  const isTeamCollection = entityMatch[1] === "teams";
  const collection = isTeamCollection ? teams : tasks;
  const recordId = entityMatch[2]?.toLowerCase() ?? null;
  const existingRecord = collection.find((record) => {
    return record.id === recordId
  });

  if (method === "GET") {
    if (recordId === null) {
      return makeResponse(config, collection);
    }
    if (!existingRecord) {
      fail(config, 404, "Record not found");
    }
    return makeResponse(config, existingRecord);
  }
  if (method === "DELETE") {
    if (!existingRecord) {
      fail(config, 404, "Record not found");
    }
    existingRecord.active = false;
    addMissingCells();
    publishState();
    return makeResponse(config, {
      status: "ok"
    });
  }
  if (method !== "POST" && method !== "PUT") {
    fail(config, 405, "Action is not supported in demo mode");
  }
  if (method === "PUT" && !existingRecord) {
    fail(config, 404, "Record not found");
  }

  const nextId = existingRecord?.id ?? randomUuid();
  let savedRecord: Team | Task;

  if (isTeamCollection) {
    const savedTeam = {
      ...createDemoTeams()[0],
      ...(existingRecord ?? {}),
      ...body,
      id: nextId,
    } as Team;
    if (method === "POST") {
      savedTeam.token = randomHex(8);
    }
    const validationError = validateTeam(savedTeam, method === "POST");
    if (validationError) {
      fail(config, 400, validationError);
    }
    const tokenAlreadyExists = teams.some(
      (team) => {
        return team.id !== nextId && team.token === savedTeam.token
      },
    );
    if (tokenAlreadyExists) {
      fail(config, 409, "Token is already used by another team");
    }
    savedTeam.name = savedTeam.name.trim();
    savedTeam.ip = savedTeam.ip.trim();
    teams = [...teams.filter((team) => {
      return team.id !== nextId
    }), savedTeam];
    savedRecord = savedTeam;
  } else {
    const savedTask = {
      ...createDemoTasks()[0],
      ...(existingRecord ?? {}),
      ...body,
      id: nextId,
    } as Task;
    const validationError = validateTask(savedTask);
    if (validationError) {
      fail(config, 400, validationError);
    }
    tasks = [...tasks.filter((task) => {
      return task.id !== nextId
    }), savedTask];
    savedRecord = savedTask;
  }

  addMissingCells();
  recordSnapshot();
  publishState();
  return makeResponse(config, savedRecord, method === "POST" ? 201 : 200);
}

function demoCheckerStats(config: InternalAxiosRequestConfig): DemoResponse {
  const parsed = GetAdminCheckerStatsParams.safeParse(config.params);
  if (!parsed.success) {
    fail(config, 400, "Expected a team ID and a non-negative round");
  }
  const { team_id, round: requestedRound } = parsed.data;
  const active = teams.some((team) => {
    return team.active && team.id === team_id
  });
  const result: AdminCheckerStats = {
    team_id,
    round: requestedRound,
    tasks: [],
  };
  // The offline board has scoreboard snapshots, but no checker jobs or action logs.
  const count = (configured: number) => {
    return {
      maximum: requestedRound === 0 ? configured : 0,
      reported: 0,
      failed: 0,
      no_result: 0,
    };
  };
  if (active) {
    for (const task of tasks) {
      if (task.active && task.id !== null) {
        const checksEnabled = demoGameSettings.mode !== "blitz" || task.get_period >= 0;
        result.tasks.push({
          task_id: task.id,
          name: task.name,
          check: count(checksEnabled ? 1 + demoGameSettings.extra_checks : 0),
          put: count(task.puts),
          get: count(checksEnabled ? task.gets : 0),
        });
      }
    }
  }
  return makeResponse(config, result);
}

function handleAdminTeamTaskLogRequest(
  config: InternalAxiosRequestConfig,
  requestPath: string,
): DemoResponse | undefined {
  if (requestPath !== "/admin/teamtasks") {
    return;
  }

  const params = config.params as
    | {
      team_id?: string;
      task_id?: string
    }
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
  await new Promise<void>((resolve) => {
    return window.setTimeout(resolve, DEMO_REQUEST_DELAY_MS)
  },
  );
  if (config.signal?.aborted) {
    throw new AxiosError("Cancelled", AxiosError.ERR_CANCELED, config);
  }

  const requestBase = config.baseURL ?? window.location.origin;
  const requestUrl = new URL(config.url ?? "/", requestBase + "/");
  const requestPath = requestUrl.pathname.replace(/^\/api\/v1/, "").replace(/\/$/, "");
  const method = config.method?.toUpperCase() ?? "GET";
  const body = readBody(config);

  const clientResponse = handleClientRequest(config, requestPath);
  if (clientResponse) {
    return clientResponse;
  }

  const sessionResponse = handleAdminSessionRequest(
    config,
    requestPath,
    method,
    body,
  );
  if (sessionResponse) {
    return sessionResponse;
  }

  requireAdminSession(config, requestPath);

  const settingsResponse = handleGameSettingsRequest(
    config,
    requestPath,
    method,
    body,
  );
  if (settingsResponse) {
    return settingsResponse;
  }

  const bulkResponse = handleBulkStateRequest(
    config,
    requestPath,
    method,
    body,
  );
  if (bulkResponse) {
    return bulkResponse;
  }

  const gameResponse = handleGameActionRequest(
    config,
    requestPath,
    method,
    body,
  );
  if (gameResponse) {
    return gameResponse;
  }

  const entityResponse = handleAdminEntityRequest(
    config,
    requestPath,
    method,
    body,
  );
  if (entityResponse) {
    return entityResponse;
  }

  if (requestPath === "/admin/checker-stats") {
    if (method !== "GET") {
      fail(config, 405, "Checker statistics require GET");
    }
    return demoCheckerStats(config);
  }

  const logResponse = handleAdminTeamTaskLogRequest(config, requestPath);
  if (logResponse) {
    return logResponse;
  }

  fail(config, 404, "Request is not supported in demo mode");
};

export function installDemoData() {
  try {
    isLoggedIn = sessionStorage.getItem(DEMO_SESSION_STORAGE_KEY) === "on";
  } catch {
    isLoggedIn = false;
  }
  demoCsrfToken = isLoggedIn ? randomHex(32) : "";
  api.defaults.adapter = demoAdapter;
  http.defaults.adapter = demoAdapter;
  resetDemoData();
}
