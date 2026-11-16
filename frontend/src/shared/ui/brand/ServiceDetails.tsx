import { useInfiniteQuery } from "@tanstack/react-query";
import { AlertCircle, ArrowDown, RefreshCw } from "lucide-react";
import { useTranslation } from "react-i18next";
import { getClientCheckerLogs } from "@/shared/api/generated/endpoints/client/client";
import { STATUS_META_BY_CODE, STATUS_OK } from "@/shared/config/statuses";
import { getLocale } from "@/shared/i18n";
import { formatScore } from "@/shared/lib/formatScore";
import type { ServiceValue } from "./ServiceCell";
import "./ServiceDetails.css";

const PAGE_SIZE = 20;

function StatusLabel({ status }: {
  status?: number
}) {
  const { t } = useTranslation("common");
  const meta = status === undefined ? undefined : STATUS_META_BY_CODE[status];
  return (
    <span className="service-log-status" style={meta ? {
      backgroundColor: meta.color,
      color: meta.foreground
    } : undefined}>
      {meta?.label ?? (status === undefined || status === -1 ? t("service.noDataShort") : String(status))}
    </span>
  );
}

export function ServiceDetails({
  teamId, taskId, value, snapshotRound,
}: {
  teamId?: string;
  taskId?: string;
  value?: ServiceValue;
  snapshotRound?: number;
}) {
  const { t } = useTranslation("common");
  const locale = getLocale();
  const logsQuery = useInfiniteQuery({
    queryKey: ["public-checker-logs", teamId, taskId],
    initialPageParam: undefined as string | undefined,
    queryFn: ({ pageParam, signal }) => {
      return getClientCheckerLogs(teamId!, taskId!, {
        limit: PAGE_SIZE,
        ...(pageParam ? {
          before_id: pageParam
        } : {}),
      }, signal)
    },
    getNextPageParam: (page) => {
      return page.has_more ? page.next_before_id : undefined
    },
    enabled: Boolean(teamId && taskId),
    staleTime: 0,
    refetchInterval: snapshotRound === undefined ? 10_000 : false,
    retry: 1,
  });
  const logs = logsQuery.data?.pages.flatMap((page) => {
    return page.logs
  }) ?? [];
  const latest = logs[0];
  const initialLoading = logsQuery.isPending && Boolean(teamId && taskId);
  const latestStatus = latest?.status ?? (snapshotRound === undefined ? value?.status : undefined);
  const meta = latestStatus === undefined ? undefined : STATUS_META_BY_CODE[latestStatus];
  const latestMessage = latest?.public_message || (latest?.status === STATUS_OK ? "OK" : "") ||
    (snapshotRound === undefined ? value?.message : "") || t("service.noCheckerMessage");
  const dateTime = (value: string) => {
    const date = new Date(value);
    return Number.isNaN(date.getTime()) ? value : new Intl.DateTimeFormat(locale, {
      day: "2-digit",
      month: "short",
      year: "numeric",
      hour: "2-digit",
      minute: "2-digit",
      second: "2-digit",
    }).format(date);
  };
  const number = new Intl.NumberFormat(locale).format;
  return (
    <div className="service-inspector">
      <div className="service-inspector-metrics" aria-label={snapshotRound === undefined ? t("service.currentSnapshot") : t("service.roundSnapshot", {
        round: snapshotRound
      })}>
        <div><span>{t("service.score")}</span><strong>{value ? formatScore(value.score) : "—"}</strong></div>
        <div><span>SLA</span><strong>{value && value.checks > 0 ? `${formatScore(value.sla)}%` : "—"}</strong></div>
        <div><span>{t("service.capturedFlags")}</span><strong>{value ? `+${number(value.stolen)}` : "—"}</strong></div>
        <div><span>{t("service.lostFlags")}</span><strong>{value ? `−${number(value.lost)}` : "—"}</strong></div>
      </div>
      {snapshotRound !== undefined && (
        <p className="service-inspector-snapshot">{t("service.snapshotNote", {
          round: snapshotRound
        })}</p>
      )}
      <section className="service-inspector-latest" aria-labelledby="service-latest-heading">
        <div className="service-inspector-section-heading">
          <h3 id="service-latest-heading">{t("service.lastCheck")}</h3>
          <StatusLabel status={latestStatus} />
        </div>
        <dl className={`service-inspector-check-facts${latest ? " service-inspector-content-ready" : ""}`} aria-busy={initialLoading}>
          <div><dt>{t("service.completedAt")}</dt><dd>{latest ? <time dateTime={latest.finished_at}>{dateTime(latest.finished_at)}</time> : initialLoading ? <span className="service-inspector-skeleton" aria-hidden="true" /> : "—"}</dd></div>
          <div><dt>{t("service.duration")}</dt><dd>{latest ? <>{number(latest.duration_ms)} {t("service.milliseconds")}</> : initialLoading ? <span className="service-inspector-skeleton" aria-hidden="true" /> : "—"}</dd></div>
          <div><dt>{t("service.round")}</dt><dd>{latest ? number(latest.round) : initialLoading ? <span className="service-inspector-skeleton" aria-hidden="true" /> : "—"}</dd></div>
          <div><dt>{t("service.action")}</dt><dd>{latest ? <>{latest.action} / {latest.stage}</> : initialLoading ? <span className="service-inspector-skeleton" aria-hidden="true" /> : "—"}</dd></div>
        </dl>
        <div className="service-inspector-check-note">
          {latest?.timed_out ? <p className="service-inspector-timeout"><AlertCircle size={14} />{t("service.timedOut")}</p> : !latest && !initialLoading ? <p className="service-inspector-hint">{t("service.checkTimeUnavailable")}</p> : null}
        </div>
        <div className="service-inspector-response">
          <span>{t("service.checkerResponse")}</span>
          <pre className={latest ? "service-inspector-content-ready" : undefined} style={meta ? {
            borderLeftColor: meta.color
          } : undefined}>{initialLoading && !latest && !(snapshotRound === undefined && value?.message) ? <span className="service-inspector-skeleton service-inspector-response-skeleton" aria-hidden="true" /> : latestMessage}</pre>
        </div>
      </section>
      <section className="service-inspector-history" aria-labelledby="service-history-heading" aria-busy={initialLoading}>
        <div className="service-inspector-section-heading">
          <h3 id="service-history-heading">{t("service.checkHistory")}</h3>
          {teamId && taskId && <button className="service-inspector-button" type="button" disabled={logsQuery.isFetching} onClick={() => {
            return void logsQuery.refetch()
          }}><RefreshCw size={13} aria-hidden="true" />{t("service.refresh")}</button>}
        </div>
        <p className="service-inspector-hint">{t("service.historyDescription")}</p>
        {!teamId || !taskId ? <p className="service-inspector-state">{t("service.logsUnavailable")}</p> : (
          <>
            {initialLoading && <>
              <p className="service-inspector-loading-status" role="status">{t("service.loadingChecks")}</p>
              <div className="service-inspector-log-list" aria-hidden="true">
                {[0, 1, 2].map((row) => {
                  return <div className="service-inspector-log-placeholder" key={row}>
                    <span className="service-inspector-skeleton" />
                    <span className="service-inspector-skeleton" />
                    <span className="service-inspector-skeleton" />
                  </div>
                })}
              </div>
            </>}
            {logsQuery.isError && <div className="service-inspector-error" role="alert"><AlertCircle size={16} /><span>{t("service.logsError")}</span><button type="button" className="service-inspector-button" onClick={() => {
              return void (logsQuery.isFetchNextPageError ? logsQuery.fetchNextPage() : logsQuery.refetch())
            }}>{t("service.retry")}</button></div>}
            {!logsQuery.isPending && !logsQuery.isError && logs.length === 0 && <p className="service-inspector-state">{t("service.emptyChecks")}</p>}
            {logs.length > 0 && <ol className="service-inspector-log-list">
              {logs.map((log) => {
                return (
                  <li key={log.id} className="service-inspector-content-ready">
                    <details className="service-inspector-log">
                      <summary>
                        <span className="service-inspector-log-heading"><StatusLabel status={log.status} /><time dateTime={log.finished_at}>{dateTime(log.finished_at)}</time></span>
                        <span className="service-inspector-log-meta">{t("service.round")} {number(log.round)} · {log.action} / {log.stage} · {number(log.duration_ms)} {t("service.milliseconds")}{log.timed_out ? ` · ${t("service.timedOut")}` : ""}</span>
                        <span className="service-inspector-log-preview">{log.public_message || (log.status === STATUS_OK ? "OK" : t("service.noCheckerMessage"))}</span>
                      </summary>
                      <div className="service-inspector-log-expanded">
                        <dl><div><dt>{t("service.startedAt")}</dt><dd><time dateTime={log.started_at}>{dateTime(log.started_at)}</time></dd></div><div><dt>{t("service.completedAt")}</dt><dd><time dateTime={log.finished_at}>{dateTime(log.finished_at)}</time></dd></div></dl>
                        <pre>{log.public_message || (log.status === STATUS_OK ? "OK" : t("service.noCheckerMessage"))}</pre>
                      </div>
                    </details>
                  </li>
                )
              })}
            </ol>}
            {logsQuery.hasNextPage && <button type="button" className="service-inspector-button service-inspector-load-more" disabled={logsQuery.isFetchingNextPage} onClick={() => {
              return void logsQuery.fetchNextPage()
            }}><ArrowDown size={14} aria-hidden="true" />{logsQuery.isFetchingNextPage ? t("service.loadingChecks") : t("service.loadOlder")}</button>}
          </>
        )}
      </section>
    </div>
  );
}
