import { fetchTaskAdmin } from "@/entities/task/api/admin";
import { fetchTeamTaskLog } from "@/entities/team-task/api";
import { fetchTeamAdmin } from "@/entities/team/api/admin";
import { STATUS_META_BY_CODE } from "@/shared/config/statuses";
import { getLocale } from "@/shared/i18n/index";
import { formatScore } from "@/shared/lib/formatScore";
import { normalizeUuid } from "@/shared/lib/uuid";
import { BrandIcon } from "@/shared/ui/brand/BrandIcon";
import { AdminShell } from "@/widgets/admin-shell/ui/AdminShell";
import { useQuery } from "@tanstack/react-query";
import { ArrowLeft } from "lucide-react";
import { Link, useParams } from "react-router-dom";
import { useTranslation } from "react-i18next";

export function AdminTeamTaskLogPage() {
  const { t } = useTranslation("admin");
  const { teamId: rawTeamId, taskId: rawTaskId } = useParams();
  const teamId = normalizeUuid(rawTeamId);
  const taskId = normalizeUuid(rawTaskId);
  const validIds = teamId !== undefined && taskId !== undefined;
  const logQuery = useQuery({
    queryKey: ["teamtask-log", teamId, taskId],
    queryFn: ({ signal }) => fetchTeamTaskLog(teamId!, taskId!, signal),
    enabled: validIds,
    refetchInterval: 5000,
  });
  const teamQuery = useQuery({
    queryKey: ["team", teamId],
    queryFn: ({ signal }) => fetchTeamAdmin(teamId!, signal),
    enabled: validIds,
  });
  const taskQuery = useQuery({
    queryKey: ["task", taskId],
    queryFn: ({ signal }) => fetchTaskAdmin(taskId!, signal),
    enabled: validIds,
  });
  const numberFormat = new Intl.NumberFormat(getLocale());
  return (
    <AdminShell>
      <Link to="/admin/scoreboard" className="back-link">
        <ArrowLeft size={14} />{t("teamTaskLog.back")}
      </Link>
      <div className="admin-page">
        <div className="admin-heading">
          <div>
            <p className="eyebrow">{t("common.organizerPanel")}</p>
            <h1>{t("teamTaskLog.title")}</h1>
            <p>
              {teamQuery.data?.name ?? (teamId
                ? t("team.nameWithId", { id: teamId })
                : t("team.invalidId"))}
              <span className="footer-divider">/</span>
              {taskQuery.data?.name ?? (taskId
                ? t("task.nameWithId", { id: taskId })
                : t("task.invalidId"))}
            </p>
          </div>
          <BrandIcon name="terminal" />
        </div>
        {logQuery.isError && (
          <div className="notice" role="alert">
            {t("teamTaskLog.loadError")}
            <button
              className="text-link"
              type="button"
              onClick={() => void logQuery.refetch()}
            >
              {t("common.retry")}
            </button>
          </div>
        )}
        <div className="table-frame">
          <div
            className="table-scroll"
            role="region"
            aria-label={t("teamTaskLog.regionLabel")}
            tabIndex={0}
          >
            <table className="scoreboard-table">
              <thead>
                <tr>
                  {[
                    t("teamTaskLog.round"),
                    t("teamTaskLog.status"),
                    t("teamTaskLog.score"),
                    t("teamTaskLog.captured"),
                    t("teamTaskLog.lost"),
                    t("teamTaskLog.checks"),
                    t("teamTaskLog.publicMessage"),
                    t("teamTaskLog.privateMessage"),
                    t("teamTaskLog.command"),
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
                      <td className="history-round">{numberFormat.format(entry.round)}</td>
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
                      <td className="text-[#95bca5]">+{numberFormat.format(entry.stolen)}</td>
                      <td className="text-[#c88b82]">-{numberFormat.format(entry.lost)}</td>
                      <td>
                        {numberFormat.format(entry.checks_passed)}/{numberFormat.format(entry.checks)}
                      </td>
                      <td>
                        <pre className="log-message">
                          {entry.public_message || "-"}
                        </pre>
                      </td>
                      <td>
                        <pre className="log-message">
                          {entry.private_message || "-"}
                        </pre>
                      </td>
                      <td>
                        <pre className="log-message">
                          {entry.command || "-"}
                        </pre>
                      </td>
                    </tr>
                  );
                })}
                {!validIds && (
                  <tr>
                    <td colSpan={9} className="quiet-empty text-center" role="alert">
                      {t("teamTaskLog.invalidIds")}
                    </td>
                  </tr>
                )}
                {validIds && !logQuery.data?.length && (
                  <tr>
                    <td colSpan={9} className="quiet-empty text-center">
                      {logQuery.isPending
                        ? t("teamTaskLog.loadingEntries")
                        : t("teamTaskLog.empty")}
                    </td>
                  </tr>
                )}
              </tbody>
            </table>
          </div>
        </div>
      </div>
    </AdminShell>
  );
}
