import { Link, useParams } from "react-router-dom";
import { useQuery } from "@tanstack/react-query";
import { ArrowLeft } from "lucide-react";
import { AppShell } from "@/shared/ui/layout/AppShell";
import { fetchTeamTaskLog } from "@/entities/team-task/api";
import { fetchTeamAdmin } from "@/entities/team/api/admin";
import { fetchTaskAdmin } from "@/entities/task/api/admin";
import { STATUS_META_BY_CODE } from "@/shared/config/statuses";
import { BrandIcon } from "@/shared/ui/brand/BrandIcon";
import { formatScore } from "@/shared/lib/formatScore";

export function AdminTeamTaskLogPage() {
  const { teamId: rawTeamId, taskId: rawTaskId } = useParams();
  const teamId = Number(rawTeamId);
  const taskId = Number(rawTaskId);
  const logQuery = useQuery({
    queryKey: ["teamtask-log", teamId, taskId],
    queryFn: () => fetchTeamTaskLog(teamId, taskId),
    enabled:
      Number.isInteger(teamId) &&
      teamId > 0 &&
      Number.isInteger(taskId) &&
      taskId > 0,
    refetchInterval: 5000,
  });
  const teamQuery = useQuery({
    queryKey: ["team", teamId],
    queryFn: () => fetchTeamAdmin(teamId),
  });
  const taskQuery = useQuery({
    queryKey: ["task", taskId],
    queryFn: () => fetchTaskAdmin(taskId),
  });
  return (
    <AppShell>
      <Link to="/admin/scoreboard" className="back-link">
        <ArrowLeft size={14} />К управлению соревнованием
      </Link>
      <div className="admin-page">
        <div className="admin-heading">
          <div>
            <p className="eyebrow">Панель организатора</p>
            <h1>Лог чекера</h1>
            <p>
              {teamQuery.data?.name ?? `Команда #${teamId}`}
              <span className="footer-divider">/</span>
              {taskQuery.data?.name ?? `Сервис #${taskId}`}
            </p>
          </div>
          <BrandIcon name="terminal" />
        </div>
        {logQuery.isError && (
          <div className="notice" role="alert">
            Не удалось загрузить лог.
            <button
              className="text-link"
              type="button"
              onClick={() => void logQuery.refetch()}
            >
              Повторить
            </button>
          </div>
        )}
        <div className="table-frame">
          <div
            className="table-scroll"
            role="region"
            aria-label="Лог чекера"
            tabIndex={0}
          >
            <table className="scoreboard-table">
              <thead>
                <tr>
                  {[
                    "Раунд",
                    "Статус",
                    "Очки",
                    "Захвачено",
                    "Потеряно",
                    "Проверки",
                    "Публичное сообщение",
                    "Приватное сообщение",
                    "Команда запуска",
                  ].map((label) => (
                    <th scope="col" key={label}>
                      {label}
                    </th>
                  ))}
                </tr>
              </thead>
              <tbody>
                {logQuery.data?.map((entry) => {
                  const meta = STATUS_META_BY_CODE[entry.status];
                  return (
                    <tr key={entry.id}>
                      <td className="history-round">{entry.round}</td>
                      <td>
                        <span className="status-tag">
                          <i
                            className="status-dot"
                            style={{
                              background: meta?.color,
                              color: meta?.color,
                            }}
                          />
                          {meta?.label ?? entry.status}
                        </span>
                      </td>
                      <td className="whitespace-nowrap">
                        {formatScore(entry.score)}
                      </td>
                      <td className="text-[#95bca5]">+{entry.stolen}</td>
                      <td className="text-[#c88b82]">−{entry.lost}</td>
                      <td>
                        {entry.checks_passed}/{entry.checks}
                      </td>
                      <td>
                        <pre className="log-message">
                          {entry.public_message || "—"}
                        </pre>
                      </td>
                      <td>
                        <pre className="log-message">
                          {entry.private_message || "—"}
                        </pre>
                      </td>
                      <td>
                        <pre className="log-message">
                          {entry.command || "—"}
                        </pre>
                      </td>
                    </tr>
                  );
                })}
                {!logQuery.data?.length && (
                  <tr>
                    <td colSpan={9} className="quiet-empty text-center">
                      {logQuery.isPending
                        ? "Загружаем записи…"
                        : "В логе пока нет записей."}
                    </td>
                  </tr>
                )}
              </tbody>
            </table>
          </div>
        </div>
      </div>
    </AppShell>
  );
}
