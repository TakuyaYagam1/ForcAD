import { useScoreboardStore } from "@/entities/scoreboard/model/store";
import type { ParticipantTeamTaskHistory } from "@/shared/api/generated/schemas/client/participantTeamTaskHistory.zod";
import { serviceHistoryQueryOptions } from "@/entities/team-scoreboard/api";
import { STATUS_META_BY_CODE, STATUS_OK } from "@/shared/config/statuses";
import { formatScore } from "@/shared/lib/formatScore";
import { useInfiniteQuery } from "@tanstack/react-query";
import { useMemo, useState } from "react";
import { useTranslation } from "react-i18next";

interface ServiceCheckerSummaryProps {
  teamId?: string;
  taskId?: string;
  snapshotRound?: number;
}

const roundMetrics = [
  ["round_fp_gained", "+FP", "service.gainedFp"],
  ["round_flags_captured", "+FLAGS", "service.capturedFlags"],
  ["round_opponent_fp", "-FP", "service.opponentFp"],
  ["round_flags_lost", "-FLAG", "service.lostFlags"],
] as const;

function compareTimestamps(first: string, second: string): number {
  const firstSeparator = first.indexOf("-");
  const secondSeparator = second.indexOf("-");
  const secondsDifference =
    (Number(first.slice(0, firstSeparator)) || 0) -
    (Number(second.slice(0, secondSeparator)) || 0);
  if (secondsDifference !== 0) {
    return secondsDifference
  }
  return (Number(first.slice(firstSeparator + 1)) || 0) -
    (Number(second.slice(secondSeparator + 1)) || 0);
}

function latestSnapshots(
  pages: ParticipantTeamTaskHistory[][],
  teamId: string,
  taskId: string,
  maxRound: number,
): ParticipantTeamTaskHistory[] {
  const latestByRound = new Map<number, ParticipantTeamTaskHistory>();
  for (const page of pages) {
    for (const snapshot of page) {
      if (
        snapshot.team_id !== teamId ||
        snapshot.task_id !== taskId ||
        snapshot.round <= 0 ||
        snapshot.round > maxRound
      ) {
        continue
      }
      const current = latestByRound.get(snapshot.round);
      if (!current || compareTimestamps(snapshot.timestamp, current.timestamp) > 0) {
        latestByRound.set(snapshot.round, snapshot);
      }
    }
  }
  return [...latestByRound.values()].sort((first, second) => {
    return second.round - first.round
  });
}

export function ServiceCheckerSummary({
  teamId,
  taskId,
  snapshotRound,
}: ServiceCheckerSummaryProps) {
  const { t } = useTranslation("common");
  const publishedRound = useScoreboardStore((state) => {
    return state.round
  });
  const runtimeRound = useScoreboardStore((state) => {
    return state.runtimeRound
  });
  const generation = useScoreboardStore((state) => {
    return state.generation
  });
  const latestRound = snapshotRound ?? publishedRound;
  // A newly published round must not move the reader back to the first page.
  const [scrollAnchor, setScrollAnchor] = useState<number | null>(null);
  const maxRound = Math.min(scrollAnchor ?? latestRound, latestRound);
  const historyQuery = useInfiniteQuery(serviceHistoryQueryOptions(teamId, taskId, maxRound, generation));
  const pages = historyQuery.data?.pages;
  const rounds = useMemo(() => {
    if (!pages || !teamId || !taskId || maxRound <= 0) {
      return []
    }
    return latestSnapshots(pages, teamId, taskId, maxRound)
  }, [maxRound, pages, taskId, teamId]);
  const retry = historyQuery.isFetchNextPageError ? historyQuery.fetchNextPage : historyQuery.refetch;
  const readError = historyQuery.isError && (
    <div className="checker-round-state checker-round-state--error" role="alert">
      <span>{t("service.checkerHistoryError")}</span>
      <button type="button" className="text-link" onClick={() => {
        void retry()
      }}>
        {t("service.retry")}
      </button>
    </div>
  );

  return (
    <section className="checker-round-summary" aria-label={t("service.checkerHistory")}>
      <span className="eyebrow">{t("service.checkerHistory")}</span>

      {!teamId || !taskId ? (
        <p className="checker-round-state" role="status">
          {t("service.checkerHistoryUnavailable")}
        </p>
      ) : maxRound <= 0 ? (
        <p className="checker-round-state" role="status">
          {t((runtimeRound ?? 0) > 0 ? "service.noCheckerHistory" : "service.roundNotStarted")}
        </p>
      ) : !pages ? (
        readError || <p className="checker-round-state" role="status">{t("service.loadingCheckerHistory")}</p>
      ) : rounds.length === 0 ? (
        <p className="checker-round-state" role="status">
          {t("service.noCheckerHistory")}
        </p>
      ) : (
        <div
          className="checker-round-list"
          role="region"
          tabIndex={0}
          aria-label={t("service.checkerHistory")}
          onScroll={({ currentTarget }) => {
            const { scrollTop, scrollHeight, clientHeight } = currentTarget;
            if (snapshotRound === undefined) {
              setScrollAnchor(scrollTop > 0 ? maxRound : null);
            }
            if (
              scrollTop > 0 && scrollHeight - scrollTop - clientHeight < clientHeight / 2 &&
              historyQuery.hasNextPage && !historyQuery.isFetchingNextPage && !historyQuery.isFetchNextPageError
            ) {
              void historyQuery.fetchNextPage({
                cancelRefetch: false,
              });
            }
          }}
        >
          <table aria-label={t("service.checkerHistory")}>
            <thead>
              <tr>
                <th scope="col">{t("service.roundColumn")}</th>
                <th scope="col">{t("service.statusColumn")}</th>
                <th scope="col">{t("service.messageColumn")}</th>
                {roundMetrics.map(([field, label, description]) => {
                  return (
                    <th key={field} scope="col" className="checker-round-metric" title={t(description)}>
                      {label}
                    </th>
                  )
                })}
              </tr>
            </thead>
            <tbody>
              {rounds.map((snapshot) => {
                const statusMeta = STATUS_META_BY_CODE[snapshot.status];
                const statusLabel = snapshot.status === -1
                  ? t("service.noStatusData")
                  : `${snapshot.status} ${statusMeta?.label ?? t("service.unknownStatus")}`;
                const message = snapshot.message?.trim()
                  ? snapshot.message
                  : snapshot.status === STATUS_OK ? "OK" : "—";

                return (
                  <tr
                    className={`checker-round-entry${statusMeta ? " checker-round-entry--colored" : ""}`}
                    key={snapshot.round}
                    style={statusMeta ? {
                      backgroundColor: statusMeta.color,
                    } : undefined}
                  >
                    <th scope="row">{t("service.round", {
                      round: snapshot.round,
                    })}</th>
                    <td className="checker-round-entry-status">{statusLabel}</td>
                    <td className="checker-round-entry-message" dir="auto" title={message}>{message}</td>
                    {roundMetrics.map(([field, label]) => {
                      const value = snapshot[field];
                      return (
                        <td key={field} className="checker-round-metric" data-label={label}>
                          {value == null ? "—" : formatScore(value)}
                        </td>
                      )
                    })}
                  </tr>
                )
              })}
            </tbody>
          </table>
          {historyQuery.isFetchingNextPage && (
            <p className="checker-round-state" role="status">{t("service.loadingCheckerHistory")}</p>
          )}
          {historyQuery.isFetchNextPageError && readError}
        </div>
      )}
    </section>
  );
}
