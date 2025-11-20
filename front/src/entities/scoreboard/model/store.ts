import { create } from "zustand";
import { devtools } from "zustand/middleware";
import type { Team } from "@/entities/team/model/types";
import type { Task } from "@/entities/task/model/types";
import type { TeamTask } from "@/entities/team-task/model/types";

const sortTasks = (tasks: Task[]) =>
    [...tasks].sort((a, b) => (a.id ?? 0) - (b.id ?? 0));

/**
 * Сырые данные team_task, которые приходят от бэка
 * (и в init_scoreboard, и в update_scoreboard).
 */
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

/**
 * Состояние игры, как его шлёт бэк.
 */
export interface GameStatePayload {
    round: number;
    round_start: number | null;
    team_tasks: RawTeamTask[];
}

/**
 * init_scoreboard payload из сокета.
 */
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

/**
 * Нормализация сырых данных team_task от бэка в наш TeamTask.
 * Здесь же считаем SLA, приводим stolen/lost к boolean и чиним message.
 */
function mapRawTeamTask(raw: RawTeamTask): TeamTask {
    const sla =
        raw.checks > 0
            ? (100.0 * (raw.checks_passed ?? 0)) / raw.checks
            : 0;

    return {
        // просто детерминированный id на основе team_id и task_id
        id: raw.team_id * 1000 + raw.task_id,
        teamId: raw.team_id,
        taskId: raw.task_id,
        status: raw.status,
        stolen: raw.stolen,
        lost: raw.lost,
        sla,
        score: raw.score,
        message:
            raw.message === "" && raw.status === 101 ? "OK" : raw.message,
    };
}

/**
 * Пересчёт суммарного score для каждой команды по всем её таскам.
 */
function recalcTeamScores(teams: Team[], teamTasks: TeamTask[]): Team[] {
    const sums = new Map<number, number>();

    for (const tt of teamTasks) {
        sums.set(tt.teamId, (sums.get(tt.teamId) ?? 0) + tt.score);
    }

    return teams
        .map((team) => {
            const score =
                team.id != null ? sums.get(team.id) ?? 0 : 0;
            return {
                ...team,
                score,
            };
        })
        .sort((a, b) => {
            const sa = a.score ?? 0;
            const sb = b.score ?? 0;

            if (sb !== sa) {
                // по очкам — по убыванию
                return sb - sa;
            }

            // при равных очках — по id (по возрастанию)
            return (a.id ?? 0) - (b.id ?? 0);
        });
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

        /**
         * Полная инициализация табло.
         * Приходит state + списки команд и тасков.
         */
        handleInitScoreboardMessage: ({ state, teams, tasks }) => {
            const sortedTasks = sortTasks(tasks);

            // нормализуем team_tasks
            const teamTasks = state.team_tasks.map(mapRawTeamTask);

            // пересчитываем score по командам
            const mappedTeams = recalcTeamScores(teams, teamTasks);

            set({
                tasks: sortedTasks,
                teamTasks,
                round: state.round,
                roundStart: state.round_start,
                teams: mappedTeams,
                error: null,
            });
        },

        /**
         * Частичное обновление табло (каждый раунд).
         */
        handleUpdateScoreboardMessage: (payload) => {
            const { round, round_start, team_tasks } = payload;

            // нормализуем team_tasks
            const teamTasks = team_tasks.map(mapRawTeamTask);

            const { tasks, teams } = get();

            // если по какой-то причине init ещё не был — просто сохраняем, не трогая teams/tasks
            if (!tasks || !teams) {
                set({
                    round,
                    roundStart: round_start,
                    teamTasks,
                });
                return;
            }

            const mappedTeams = recalcTeamScores(teams, teamTasks);

            set({
                round,
                roundStart: round_start,
                teamTasks,
                teams: mappedTeams,
                error: null,
            });
        },
    }))
);