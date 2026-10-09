import { create } from "zustand";
import { devtools } from "zustand/middleware";
import type { Team } from "@/entities/team/model/types";
import type { Task } from "@/entities/task/model/types";
import type { TeamTask } from "@/entities/team-task/model/types";
import { finiteNumber } from "@/shared/lib/numbers";
import { getSlaPercent } from "@/shared/lib/sla";
import type { GamePhase } from "@/shared/lib/gameStatus";
export interface RawTeamTask {
  task_id: number;
  team_id: number;
  status: number;
  stolen: number;
  lost: number;
  score: number;
  checks: number;
  checks_passed: number;
  message: string;
}
export interface GameStatePayload {
  round: number;
  round_start: number | null;
  team_tasks: RawTeamTask[];
}
export interface GameRuntimeStatus {
  phase: GamePhase;
  practice?: boolean;
  can_start?: boolean;
  can_start_practice?: boolean;
  reset_pending?: boolean;
  generation?: number;
  scheduled_start?: string;
  results_pending?: boolean;
  round_waiting?: boolean;
  paused_at: number | null;
  paused_seconds: number;
  round_time?: number;
  round?: number;
  round_start?: number | null;
}
export interface InitScoreboardPayload {
  state: GameStatePayload | null;
  teams: Team[];
  tasks: Task[];
  config?: { round_time?: number };
  runtime?: GameRuntimeStatus;
}
interface ScoreboardState {
  round: number;
  roundStart: number | null;
  roundTime: number | null;
  roundProgress: number | null;
  teams: Team[] | null;
  tasks: Task[] | null;
  teamTasks: TeamTask[] | null;
  error: string | null;
  connected: boolean;
  phase: GameRuntimeStatus["phase"];
  roundWaiting: boolean;
  resultsPending: boolean;
  practice: boolean;
  canStart: boolean;
  canStartPractice: boolean;
  resetPending: boolean;
  generation: number;
  scheduledStart: string | null;
  pausedAt: number | null;
  pausedSeconds: number;
  runtimeRound: number | null;
  currentRoundStart: number | null;
  setError(error: string | null): void;
  setConnected(connected: boolean): void;
  setRoundTime(roundTime: number | null): void;
  setRuntime(runtime: GameRuntimeStatus): void;
  invalidateRuntime(): void;
  handleInitScoreboardMessage(payload: InitScoreboardPayload): void;
  handleUpdateScoreboardMessage(payload: GameStatePayload): void;
}
function mapRawTeamTask(raw: RawTeamTask, index: number): TeamTask {
  const checks = Math.max(0, Math.trunc(finiteNumber(raw.checks)));
  const passed = Math.min(
    checks,
    Math.max(0, Math.trunc(finiteNumber(raw.checks_passed))),
  );
  const status = finiteNumber(raw.status, -1);
  return {
    id: index,
    teamId: finiteNumber(raw.team_id),
    taskId: finiteNumber(raw.task_id),
    status,
    stolen: finiteNumber(raw.stolen),
    lost: finiteNumber(raw.lost),
    checks,
    checksPassed: passed,
    sla: getSlaPercent(checks, passed) ?? 0,
    score: finiteNumber(raw.score),
    message: raw.message ? String(raw.message) : status === 101 ? "OK" : "",
  };
}
function activeCells(
  raw: RawTeamTask[],
  teams: Team[],
  tasks: Task[],
): TeamTask[] {
  const teamIds = new Set(teams.map((t) => t.id));
  const taskIds = new Set(tasks.map((t) => t.id));
  return raw
    .map(mapRawTeamTask)
    .filter((tt) => teamIds.has(tt.teamId) && taskIds.has(tt.taskId));
}
function recalcTeamScores(teams: Team[], cells: TeamTask[]): Team[] {
  const totals = new Map<number, number>();
  for (const cell of cells)
    totals.set(
      cell.teamId,
      (totals.get(cell.teamId) ?? 0) + (cell.score * cell.sla) / 100,
    );
  return teams
    .map((team) => ({ ...team, score: totals.get(team.id ?? -1) ?? 0 }))
    .sort((a, b) => b.score - a.score || (a.id ?? 0) - (b.id ?? 0));
}
export const useScoreboardStore = create<ScoreboardState>()(
  devtools((set, get) => ({
    round: 0,
    roundStart: null,
    roundTime: null,
    roundProgress: null,
    teams: null,
    tasks: null,
    teamTasks: null,
    error: null,
    connected: false,
    phase: "unknown",
    roundWaiting: false,
    resultsPending: false,
    practice: false,
    canStart: false,
    canStartPractice: false,
    resetPending: false,
    generation: 0,
    scheduledStart: null,
    pausedAt: null,
    pausedSeconds: 0,
    runtimeRound: null,
    currentRoundStart: null,
    setError: (error) => set({ error }),
    setConnected: (connected) => set({ connected }),
    setRoundTime: (roundTime) => set({ roundTime }),
    invalidateRuntime: () =>
      set({
        phase: "unknown",
        roundWaiting: false,
        resultsPending: false,
        canStart: false,
        canStartPractice: false,
        pausedAt: null,
        pausedSeconds: 0,
        runtimeRound: null,
        currentRoundStart: null,
      }),
    setRuntime: (runtime) =>
      set({
        phase: runtime.phase,
        roundWaiting: runtime.round_waiting === true,
        resultsPending: runtime.results_pending === true,
        practice: runtime.practice === true,
        canStart: runtime.can_start === true,
        canStartPractice: runtime.can_start_practice === true,
        resetPending: runtime.reset_pending === true,
        generation: runtime.generation ?? 0,
        scheduledStart: runtime.scheduled_start ?? null,
        pausedAt: runtime.paused_at,
        pausedSeconds: finiteNumber(runtime.paused_seconds),
        runtimeRound:
          runtime.round == null ? null : finiteNumber(runtime.round),
        currentRoundStart: runtime.round_start
          ? finiteNumber(runtime.round_start)
          : null,
        ...(runtime.round_time && runtime.round_time > 0
          ? { roundTime: runtime.round_time }
          : {}),
      }),
    handleInitScoreboardMessage: ({ state, teams, tasks, config, runtime }) => {
      const activeTeams = teams.filter((t) => t.active !== false);
      const activeTasks = tasks
        .filter((t) => t.active !== false)
        .sort((a, b) => (a.id ?? 0) - (b.id ?? 0));
      const cells = activeCells(
        state?.team_tasks ?? [],
        activeTeams,
        activeTasks,
      );
      set({
        teams: recalcTeamScores(activeTeams, cells),
        tasks: activeTasks,
        teamTasks: cells,
        round: finiteNumber(state?.round),
        roundStart: state?.round_start ?? null,
        error:
          state === null
            ? "Состояние игры пока недоступно. Ожидаем обновления сервера."
            : null,
        ...(config?.round_time && config.round_time > 0
          ? { roundTime: config.round_time }
          : {}),
      });
      if (runtime) get().setRuntime(runtime);
    },
    handleUpdateScoreboardMessage: (payload) => {
      const { teams, tasks } = get();
      const cells =
        teams && tasks
          ? activeCells(payload.team_tasks ?? [], teams, tasks)
          : (payload.team_tasks ?? []).map(mapRawTeamTask);
      set({
        round: finiteNumber(payload.round),
        roundStart: payload.round_start ?? null,
        teamTasks: cells,
        ...(teams ? { teams: recalcTeamScores(teams, cells) } : {}),
        error: null,
      });
    },
  })),
);
