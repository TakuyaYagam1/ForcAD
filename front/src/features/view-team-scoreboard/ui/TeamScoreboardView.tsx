import { useMemo } from "react";
import { useScoreboardStore } from "@/entities/scoreboard/model/store";
import {
  STATUS_META_BY_CODE,
  STATUS_COLOR_BY_CODE,
} from "@/shared/config/statuses";
import { cn } from "@/lib/utils";

interface TeamScoreboardViewProps {
  teamId: number;
}

export function TeamScoreboardView({ teamId }: TeamScoreboardViewProps) {
  const teams = useScoreboardStore((s) => s.teams) ?? [];
  const tasks = useScoreboardStore((s) => s.tasks) ?? [];
  const teamTasks = useScoreboardStore((s) => s.teamTasks) ?? [];

  const team = teams.find((t) => t.id === teamId);
  const place = useMemo(() => {
    if (!team) return null;
    const sorted = [...teams].sort((a, b) => (a.id ?? 0) - (b.id ?? 0));
    const idx = sorted.findIndex((t) => t.id === team.id);
    return idx === -1 ? null : idx + 1;
  }, [teams, team]);

  const rows = useMemo(
    () =>
      tasks.map((task) => {
        const tt = teamTasks.find(
          (x) => x.teamId === teamId && x.taskId === task.id
        );
        const meta = tt?.status ? STATUS_META_BY_CODE[tt.status] : undefined;
        const bg = tt?.status ? STATUS_COLOR_BY_CODE[tt.status] : undefined;
        return { task, tt, meta, bg };
      }),
    [tasks, teamTasks, teamId]
  );

  if (!team) {
    return (
      <div className="rounded-xl border border-red-500/40 bg-red-500/10 px-4 py-3 text-sm text-red-100">
        Команда с id {teamId} не найдена в табло.
      </div>
    );
  }

  return (
    <div className="space-y-4">
      {/* Шапка команды */}
      <div className="rounded-2xl border border-slate-800/80 bg-slate-950/80 px-4 py-4 shadow-lg shadow-indigo-900/40 backdrop-blur">
        <div className="flex flex-col gap-2 md:flex-row md:items-end md:justify-between">
          <div className="space-y-1">
            <div className="text-[11px] uppercase tracking-[0.35em] text-slate-500">
              Team
            </div>
            <div className="text-2xl font-semibold text-slate-50">
              {team.name}
            </div>
            <div className="flex flex-wrap gap-3 text-xs text-slate-400">
              {place && (
                <span>
                  place:{" "}
                  <span className="font-mono text-slate-100">#{place}</span>
                </span>
              )}
              <span>
                score:{" "}
                <span className="font-mono text-emerald-300">{team.score}</span>
              </span>
            </div>
          </div>

          <div className="flex flex-col items-start gap-1 text-xs text-slate-400 md:items-end">
            <div>
              tasks:{" "}
              <span className="font-mono text-slate-100">{tasks.length}</span>
            </div>
            <div>
              active tasks:{" "}
              <span className="font-mono text-emerald-300">
                {rows.filter((r) => r.tt?.status === 101).length}
              </span>
            </div>
          </div>
        </div>
      </div>

      {/* Таблица задач команды */}
      <div className="overflow-hidden rounded-2xl border border-slate-800/80 bg-slate-950/80 shadow-xl shadow-indigo-900/40 backdrop-blur">
        <div className="max-h-[70vh] overflow-auto">
          <table className="min-w-full border-collapse text-sm text-slate-100">
            <thead className="sticky top-0 z-10 bg-slate-950/90 backdrop-blur">
              <tr className="border-b border-slate-800/80">
                <th className="px-3 py-2 text-left text-xs uppercase tracking-[0.18em] text-slate-400">
                  Task
                </th>
                <th className="px-3 py-2 text-left text-xs uppercase tracking-[0.18em] text-slate-400">
                  Status
                </th>
                <th className="px-3 py-2 text-right text-xs uppercase tracking-[0.18em] text-slate-400">
                  Score
                </th>
                <th className="px-3 py-2 text-center text-xs uppercase tracking-[0.18em] text-slate-400">
                  Flags
                </th>
                <th className="px-3 py-2 text-center text-xs uppercase tracking-[0.18em] text-slate-400">
                  SLA
                </th>
              </tr>
            </thead>

            <tbody>
              {rows.map(({ task, tt, meta, bg }) => (
                <tr
                  key={task.id}
                  className={cn(
                    "border-b border-slate-800/60 transition-colors hover:bg-slate-900/80",
                    bg && "bg-opacity-80"
                  )}
                  style={bg ? { backgroundColor: bg } : undefined}
                >
                  {/* Task name */}
                  <td className="px-3 py-2 text-sm">
                    <div className="flex flex-col">
                      <span className="font-medium text-slate-50">
                        {task.name}
                      </span>
                    </div>
                  </td>

                  {/* Status */}
                  <td className="px-3 py-2 text-xs">
                    {meta ? (
                      <span className="inline-flex items-center rounded-full border border-slate-700/70 bg-slate-900/60 px-2 py-0.5 text-[10px] uppercase tracking-[0.16em] text-slate-100">
                        {meta.label}
                      </span>
                    ) : (
                      <span className="text-slate-400">—</span>
                    )}
                  </td>

                  {/* Score */}
                  <td className="px-3 py-2 text-right text-sm">
                    <span className="font-mono text-slate-50">
                      {tt?.score ?? 0}
                    </span>
                  </td>

                  {/* Flags */}
                  <td className="px-3 py-2 text-center text-xs">
                    {tt ? (
                      <div className="flex items-center justify-center gap-2">
                        <span
                          className={cn(
                            "inline-flex items-center rounded-full px-2 py-0.5 text-[10px] font-mono",
                            tt.stolen
                              ? "bg-emerald-500/20 text-emerald-200"
                              : "bg-slate-800/70 text-slate-400"
                          )}
                        >
                          ST
                        </span>
                        <span
                          className={cn(
                            "inline-flex items-center rounded-full px-2 py-0.5 text-[10px] font-mono",
                            tt.lost
                              ? "bg-red-500/20 text-red-200"
                              : "bg-slate-800/70 text-slate-400"
                          )}
                        >
                          LS
                        </span>
                      </div>
                    ) : (
                      <span className="text-slate-400">—</span>
                    )}
                  </td>

                  {/* SLA */}
                  <td className="px-3 py-2 text-center text-xs">
                    {tt ? (
                      <span className="font-mono text-slate-100">
                        {Math.round(tt.sla ?? 0)}%
                      </span>
                    ) : (
                      <span className="text-slate-400">—</span>
                    )}
                  </td>
                </tr>
              ))}

              {rows.length === 0 && (
                <tr>
                  <td
                    colSpan={5}
                    className="px-3 py-4 text-center text-xs text-slate-400"
                  >
                    Для этой команды пока нет задач в табло.
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
