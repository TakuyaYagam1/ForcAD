import { create } from "zustand";
import { devtools } from "zustand/middleware";
import type { TeamTask } from "@/entities/team-task/model/types";
import type { GameRuntimeStatus as ApiGameRuntimeStatus } from "@/shared/api/generated/schemas/gameRuntimeStatus.zod";
import type { GameState } from "@/shared/api/generated/schemas/client/gameState.zod";
import type { ParticipantTask } from "@/shared/api/generated/schemas/client/participantTask.zod";
import type { ParticipantTeam } from "@/shared/api/generated/schemas/client/participantTeam.zod";
import type { ScoreboardTeamTask } from "@/shared/api/generated/schemas/client/scoreboardTeamTask.zod";
import { finiteNumber } from "@/shared/lib/numbers";
import type { GamePhase } from "@/shared/lib/gameStatus";
import { compareUuid } from "@/shared/lib/uuid";
import {
  getActiveTeamTasks,
  mapRawTeamTask,
  recalculateTeamScores,
} from "./transforms";

export type RawTeamTask = ScoreboardTeamTask;
export type GameStatePayload = GameState;
export type GameRuntimeStatus = Omit<ApiGameRuntimeStatus, "phase"> & {
  phase: GamePhase;
};
export type ScoreboardTeam = ParticipantTeam & {
  score?: number;
  token?: string;
  taskStats?: unknown;
};
export interface InitScoreboardPayload {
  state: GameStatePayload | null;
  teams: ScoreboardTeam[];
  tasks: ParticipantTask[];
  config?: { round_time: number };
  runtime?: GameRuntimeStatus;
}
interface ScoreboardState {
  round: number;
  roundStart: number | null;
  roundTime: number | null;
  roundProgress: number | null;
  teams: ScoreboardTeam[] | null;
  tasks: ParticipantTask[] | null;
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
      const activeTasks = [...tasks].sort((a, b) => compareUuid(a.id, b.id));
      const teamTasks = getActiveTeamTasks(
        state?.team_tasks ?? [],
        activeTeams,
        activeTasks,
      );
      set({
        teams: recalculateTeamScores(activeTeams, teamTasks),
        tasks: activeTasks,
        teamTasks,
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
      const teamTasks =
        teams && tasks
          ? getActiveTeamTasks(payload.team_tasks ?? [], teams, tasks)
          : (payload.team_tasks ?? []).map(mapRawTeamTask);
      set({
        round: finiteNumber(payload.round),
        roundStart: payload.round_start ?? null,
        teamTasks,
        ...(teams ? { teams: recalculateTeamScores(teams, teamTasks) } : {}),
        error: null,
      });
    },
  })),
);
