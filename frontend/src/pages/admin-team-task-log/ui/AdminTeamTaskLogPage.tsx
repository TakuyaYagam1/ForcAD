import { fetchTaskAdmin } from "@/entities/task/api/admin";
import { fetchTeamTaskLog } from "@/entities/team-task/api";
import { fetchTeamAdmin } from "@/entities/team/api/admin";
import { STATUS_META_BY_CODE } from "@/shared/config/statuses";
import { getLocale } from "@/shared/i18n/index";
import { formatScore } from "@/shared/lib/formatScore";
import { normalizeUuid } from "@/shared/lib/uuid";
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
    queryFn: ({ signal }) => {
      return fetchTeamTaskLog(teamId!, taskId!, signal)
    },
    enabled: validIds,
    refetchInterval: 5000,
  });
  const teamQuery = useQuery({
    queryKey: ["team", teamId],
    queryFn: ({ signal }) => {
      return fetchTeamAdmin(teamId!, signal)
    },
    enabled: validIds,
  });
  const taskQuery = useQuery({
    queryKey: ["task", taskId],
    queryFn: ({ signal }) => {
      return fetchTaskAdmin(taskId!, signal)
    },
    enabled: validIds,
  });
  const numberFormat = new Intl.NumberFormat(getLocale());
  return (
    <AdminShell>
      <div className="admin-page admin-page--logs">
        <Link to="/admin/scoreboard" className="back-link admin-back-link">
          <ArrowLeft size={14} />{t("teamTaskLog.back")}
        </Link>
        <div className="admin-heading">
          <div>
            <p className="eyebrow">{t("common.organizerPanel")}</p>
            <h1>{t("teamTaskLog.title")}</h1>
            <p>
              {teamQuery.data?.name ?? (teamId
                ? t("team.nameWithId", {
                  id: teamId
                })
                : t("team.invalidId"))}
              <span className="footer-divider">/</span>
              {taskQuery.data?.name ?? (taskId
                ? t("task.nameWithId", {
                  id: taskId
                })
                : t("task.invalidId"))}
            </p>
          </div>
        </div>
        {logQuery.isError && (
          <div className="notice" role="alert">
            {t("teamTaskLog.loadError")}
            <button
              className="text-link"
              type="button"
              onClick={() => {
                return void logQuery.refetch()
              }}
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
            <table className="scoreboard-table" role="table" aria-label={t("teamTaskLog.regionLabel")}>
              <thead role="rowgroup">
                <tr role="row">
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
                  ].map((label) => {
                    return (
                      <th role="columnheader" scope="col" key={label}>
                        {label}
                      </th>
                    )
                  })}
                </tr>
              </thead>
              <tbody role="rowgroup">
                {logQuery.data?.map((entry) => {
                  const meta = STATUS_META_BY_CODE[entry.status];
                  return (
                    <tr role="row" key={entry.id}>
                      <td role="cell" data-label={t("teamTaskLog.round")} className="history-round">{numberFormat.format(entry.round)}</td>
                      <td role="cell" data-label={t("teamTaskLog.status")}>
                        <span className="status-tag" style={meta ? {
                          color: meta.foreground,
                          backgroundColor: meta.color,
                          padding: "3px 7px"
                        } : undefined}>
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
                      <td role="cell" data-label={t("teamTaskLog.score")}>
                        {formatScore(entry.score)}
                      </td>
                      <td role="cell" data-label={t("teamTaskLog.captured")} className="admin-text-success">+{numberFormat.format(entry.stolen)}</td>
                      <td role="cell" data-label={t("teamTaskLog.lost")} className="admin-text-danger">-{numberFormat.format(entry.lost)}</td>
                      <td role="cell" data-label={t("teamTaskLog.checks")}>
                        {numberFormat.format(entry.checks_passed)}/{numberFormat.format(entry.checks)}
                      </td>
                      <td role="cell" data-label={t("teamTaskLog.publicMessage")}>
                        <pre className="log-message">
                          {entry.public_message || "-"}
                        </pre>
                      </td>
                      <td role="cell" data-label={t("teamTaskLog.privateMessage")}>
                        <pre className="log-message">
                          {entry.private_message || "-"}
                        </pre>
                      </td>
                      <td role="cell" data-label={t("teamTaskLog.command")}>
                        <pre className="log-message">
                          {entry.command || "-"}
                        </pre>
                      </td>
                    </tr>
                  );
                })}
                {!validIds && (
                  <tr role="row">
                    <td role="cell" colSpan={9} className="quiet-empty text-center">
                      <span role="alert">{t("teamTaskLog.invalidIds")}</span>
                    </td>
                  </tr>
                )}
                {validIds && !logQuery.data?.length && (
                  <tr role="row">
                    <td role="cell" colSpan={9} className="quiet-empty text-center">
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
