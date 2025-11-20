import { create } from "zustand";
import { devtools } from "zustand/middleware";
import type { Team } from "@/entities/team/model/types";
import type { Task } from "@/entities/task/model/types";
import type { TeamTask } from "@/entities/team-task/model/types";

// если хочешь — перенеси сортировку из моделей Task.comp / Team.comp сюда
const sortTasks = (tasks: Task[]) =>
  [...tasks].sort((a, b) => (a.id ?? 0) - (b.id ?? 0));
// const sortTeams = (teams: Team[]) =>
//   [...teams].sort((a, b) => (a.id ?? 0) - (b.id ?? 0));

export interface GameStatePayload {
  round: number;
  round_start: number | null;
  team_tasks: TeamTask[];
}

export interface InitScoreboardPayload {
  state: GameStatePayload;
  teams: Team[];
  tasks: Task[];
}

interface ScoreboardState {
  round: number;
  roundStart: number | null;
  roundTime: number | null; // если будешь потом использовать
  roundProgress: number | null; // как в Vue
  teams: Team[] | null;
  tasks: Task[] | null;
  teamTasks: TeamTask[] | null;
  error: string | null;

  // экшены
  setError(error: string | null): void;
  setRoundTime: (roundTime: number | null) => void;

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
    setRoundTime: (roundTime: number | null) => set({ roundTime }),

    setError: (error) => set({ error }),

    handleInitScoreboardMessage: ({ state, teams, tasks }) => {
      // 1) tasks
      const sortedTasks = sortTasks(tasks);
      // 2) game state
      const teamTasks = state.team_tasks;
      const round = state.round;
      const roundStart = state.round_start;

      // 3) собираем команды (аналог updateTeams в Vue)
      const mappedTeams: Team[] = teams
        .map((team) => ({
          ...team,
          // здесь можно сразу пересчитать score и прикрутить teamTasks
          // как в модели Team в Vue
        }))
        .sort((a, b) => (a.id ?? 0) - (b.id ?? 0)); // вместо Team.comp

      set({
        tasks: sortedTasks,
        teamTasks,
        round,
        roundStart,
        teams: mappedTeams,
        error: null,
      });
    },

    handleUpdateScoreboardMessage: (payload) => {
      const { round, round_start, team_tasks } = payload;
      const { tasks, teams } = get();

      if (!tasks || !teams) {
        // если нет init'а — можно либо игнорить, либо сохранить частично
        set({
          round,
          roundStart: round_start,
          teamTasks: team_tasks,
        });
        return;
      }

      const mappedTeams: Team[] = teams
        .map((team) => ({
          ...team,
          // тут же как выше — пересчёт score, стейта по task'ам и т.д.
        }))
        .sort((a, b) => (a.id ?? 0) - (b.id ?? 0));

      set({
        round,
        roundStart: round_start,
        teamTasks: team_tasks,
        teams: mappedTeams,
        error: null,
      });
    },
  }))
);
