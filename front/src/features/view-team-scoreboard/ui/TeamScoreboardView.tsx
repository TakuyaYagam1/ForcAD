import { useMemo } from "react";
import { useParams } from "react-router-dom";
import { useQuery } from "@tanstack/react-query";

import { useScoreboardStore } from "@/entities/scoreboard/model/store";
import { fetchTeamStates } from "@/entities/team-scoreboard/api";
import {
    STATUS_COLOR_BY_CODE,
    STATUS_META_BY_CODE,
} from "@/shared/config/statuses";

export function TeamScoreboardView() {
    const { teamId: rawTeamId } = useParams();
    const teamId = Number(rawTeamId);
    const invalidTeamId = Number.isNaN(teamId);

    const teams = useScoreboardStore((s) => s.teams) ?? [];
    const tasks = useScoreboardStore((s) => s.tasks) ?? [];

    // команда (если id валидный)
    const team =
        !invalidTeamId && teams.length > 0
            ? teams.find((t) => t.id === teamId) ?? null
            : null;

    // история состояний по команде
    const historyQuery = useQuery({
        queryKey: ["team-history", teamId],
        queryFn: () => fetchTeamStates(teamId),
        enabled: !invalidTeamId,
    });

    // собираем "срезы" по раундам (как было)
    const { rows, rowCount } = useMemo(() => {
        const states = historyQuery.data ?? [];
        if (!states.length) {
            return { rows: [], rowCount: 0 };
        }

        // сгруппировать по taskId (как this.by_task в Vue)
        const byTask: Record<number, typeof states> = {};
        for (const st of states) {
            const key = st.taskId;
            if (!byTask[key]) byTask[key] = [];
            byTask[key].push(st);
        }

        const taskIds = Object.keys(byTask)
            .map(Number)
            .sort((a, b) => a - b);
        const columns = taskIds.map((id) => byTask[id]);
        const minLen = Math.min(...columns.map((c) => c.length));
        const result: {
            tasks: typeof states;
            score: number;
        }[] = [];

        for (let i = 0; i < minLen; i += 1) {
            const slice = columns.map((c) => c[i]);
            const totalScore = slice.reduce(
                (acc, { score, sla }) => acc + (score * sla) / 100.0,
                0
            );
            result.push({ tasks: slice, score: totalScore });
        }

        return { rows: result, rowCount: minLen };
    }, [historyQuery.data]);

    // место команды — сортировка по score (чем больше, тем выше)
    const place = useMemo(() => {
        if (!team) return null;
        if (!teams.length) return null;

        const sorted = [...teams].sort(
            (a, b) => (b.score ?? 0) - (a.score ?? 0)
        );
        const idx = sorted.findIndex((t) => t.id === team.id);
        return idx === -1 ? null : idx + 1;
    }, [teams, team]);

    // --- Дальше только условный рендер, хуки выше, порядок фиксированный ---

    if (invalidTeamId) {
        return (
            <div className="rounded-xl border border-red-500/40 bg-red-500/10 px-4 py-3 text-sm text-red-100">
                Некорректный идентификатор команды: {rawTeamId}
            </div>
        );
    }

    if (!team) {
        return (
            <div className="rounded-xl border border-red-500/40 bg-red-500/10 px-4 py-3 text-sm text-red-100">
                Команда с id {teamId} пока не найдена в табло (возможно, табло ещё не
                инициализировано).
            </div>
        );
    }

    return (
        <div className="space-y-4">
            {/* шапка команды */}
            <div className="rounded-2xl border border-slate-800/80 bg-slate-950/80 px-4 py-4 shadow-lg shadow-indigo-900/40 backdrop-blur">
                <div className="flex flex-col gap-2 md:flex-row md:items-end md:justify-between">
                    <div className="space-y-1">
                        <div className="text-[11px] uppercase tracking-[0.35em] text-slate-500">
                            Team
                        </div>
                        <div className="text-2xl font-semibold text-slate-50">
                            {team.name}
                        </div>
                        {/* IP под названием команды */}
                        <div className="text-sm text-slate-100">{team.ip}</div>

                        <div className="flex flex-wrap gap-3 text-xs text-slate-400">
                            {place && (
                                <span>
                  place:{" "}
                                    <span className="font-mono text-slate-100">#{place}</span>
                </span>
                            )}
                            <span>
                score:{" "}
                                <span className="font-mono text-emerald-300">
                  {(team.score ?? 0).toFixed(2)}
                </span>
              </span>
                            <span>
                snapshots:{" "}
                                <span className="font-mono text-slate-100">{rowCount}</span>
              </span>
                        </div>
                    </div>
                </div>
            </div>

            {/* таблица состояний по раундам */}
            <div className="overflow-hidden rounded-2xl border border-slate-800/80 bg-slate-950/80 shadow-xl shadow-indigo-900/40 backdrop-blur">
                <div className="max-h-[70vh] overflow-auto">
                    <table className="min-w-full border-collapse text-sm text-slate-100">
                        <thead className="sticky top-0 z-10 bg-slate-950/90 backdrop-blur">
                        <tr className="border-b border-slate-800/80">
                            <th className="px-3 py-2 text-left text-xs uppercase tracking-[0.18em] text-slate-400">
                                Snapshot
                            </th>
                            <th className="px-3 py-2 text-right text-xs uppercase tracking-[0.18em] text-slate-400">
                                Total score
                            </th>
                            {tasks.map((t) => (
                                <th
                                    key={t.id}
                                    className="px-3 py-2 text-center text-xs uppercase tracking-[0.18em] text-slate-400"
                                >
                                    {t.name}
                                </th>
                            ))}
                        </tr>
                        </thead>
                        <tbody>
                        {rows.map((row, rowIdx) => (
                            <tr
                                key={rowIdx}
                                className="border-b border-slate-800/60 hover:bg-slate-900/80"
                            >
                                <td className="px-3 py-2 text-xs text-slate-400">
                                    #{rowIdx + 1}
                                </td>
                                <td className="px-3 py-2 text-right text-sm">
                    <span className="font-mono text-emerald-300">
                      {row.score.toFixed(2)}
                    </span>
                                </td>
                                {row.tasks.map((tt, colIdx) => {
                                    const meta = STATUS_META_BY_CODE[tt.status];
                                    const bg = STATUS_COLOR_BY_CODE[tt.status];
                                    return (
                                        <td
                                            key={colIdx}
                                            className="px-3 py-2 text-xs align-top"
                                            style={{ backgroundColor: bg }}
                                        >
                                            <div className="flex flex-col gap-1">
                                                <div className="flex items-center justify-between gap-1">
                            <span className="font-mono text-slate-50">
                              {tt.score.toFixed(2)}
                            </span>
                                                    {meta && (
                                                        <span className="rounded-full bg-slate-900/60 px-2 py-0.5 text-[10px] uppercase tracking-[0.16em] text-slate-100">
                                {meta.label}
                              </span>
                                                    )}
                                                </div>
                                                <div className="text-[10px] text-slate-100">
                                                    SLA: {tt.sla.toFixed(2)}%
                                                </div>
                                                <div className="text-[10px] text-slate-100">
                                                    Flags: +{tt.stolen}/-{tt.lost}
                                                </div>
                                                <div className="text-[10px] text-slate-200">
                                                    {tt.message}
                                                </div>
                                            </div>
                                        </td>
                                    );
                                })}
                            </tr>
                        ))}

                        {!rows.length && (
                            <tr>
                                <td
                                    colSpan={2 + tasks.length}
                                    className="px-3 py-4 text-center text-xs text-slate-400"
                                >
                                    История для этой команды пока недоступна.
                                </td>
                            </tr>
                        )}
                        </tbody>
                    </table>
                </div>
            </div>
        </div>
    );
}
