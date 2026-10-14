import { useScoreboardStore } from "@/entities/scoreboard/model/store";
import type { Task } from "@/entities/task/model/types";
import { fetchTeamStates } from "@/entities/team-scoreboard/api";
import { groupHistoryByRound } from "@/entities/team-scoreboard/model/history";
import { formatScore } from "@/shared/lib/formatScore";
import { getCombinedSlaPercent } from "@/shared/lib/sla";
import { useViewportAnimations } from "@/shared/lib/useViewportAnimations";
import { normalizeUuid } from "@/shared/lib/uuid";
import { BrandIcon } from "@/shared/ui/brand/BrandIcon";
import { HeroArt } from "@/shared/ui/brand/PageHero";
import { RankBadge } from "@/shared/ui/brand/RankBadge";
import { RecentEvents } from "@/shared/ui/brand/RecentEvents";
import { ScoreValue } from "@/shared/ui/brand/ScoreValue";
import { ServiceCell } from "@/shared/ui/brand/ServiceCell";
import { StatCard } from "@/shared/ui/brand/StatCard";
import { TeamAvatar } from "@/shared/ui/brand/TeamAvatar";
import { useQuery } from "@tanstack/react-query";
import { AlertCircle, ArrowLeft } from "lucide-react";
import { useMemo, useRef, useState } from "react";
import { Link, useParams } from "react-router-dom";
import { useTranslation } from "react-i18next";
import { getLocale } from "@/shared/i18n";

const EMPTY_TASKS: Task[] = [];

export function TeamScoreboardView() {
  const { t } = useTranslation("public");
  const locale = getLocale();
  const formatNumber = new Intl.NumberFormat(locale).format;
  const heroRef = useRef<HTMLDivElement>(null);
  const [expandedHistory, setExpandedHistory] = useState(false);
  const { teamId: rawTeamId } = useParams();
  const teamId = normalizeUuid(rawTeamId) ?? null;
  const invalid = teamId === null;

  const teams = useScoreboardStore((state) => state.teams);
  const tasks = useScoreboardStore((state) => state.tasks) ?? EMPTY_TASKS;
  const current = useScoreboardStore((state) => state.teamTasks) ?? [];

  const team = teams?.find((item) => item.id === teamId);
  useViewportAnimations(heroRef, team?.id);

  const values = current.filter((item) => item.teamId === teamId);
  const place = teams?.findIndex((item) => item.id === teamId);

  const sla = getCombinedSlaPercent(values);

  const stolen = values.reduce((sum, value) => sum + value.stolen, 0);
  const lost = values.reduce((sum, value) => sum + value.lost, 0);

  const historyQuery = useQuery({
    queryKey: ["team-history", teamId],
    queryFn: ({ signal }) => fetchTeamStates(teamId ?? "", signal),
    enabled: !invalid,
    refetchInterval: 10000,
  });

  const history = useMemo(
    () =>
      groupHistoryByRound(
        historyQuery.data ?? [],
        tasks.map((task) => task.id).filter((id): id is string => id !== null),
      ),
    [historyQuery.data, tasks],
  );

  if (invalid) {
    return (
      <div className="notice">
        <AlertCircle size={16} />
        {t("teamScoreboard.invalidId")}
      </div>
    );
  }

  if (!team) {
    return (
      <div className="empty-state mt-8">
        <BrandIcon name="team" />

        <h2>
          {teams === null
            ? t("teamScoreboard.waitingTitle")
            : t("teamScoreboard.notFoundTitle")}
        </h2>

        <p>
          {teams === null
            ? t("teamScoreboard.waitingDescription")
            : t("teamScoreboard.notFoundDescription")}
        </p>

        <Link to="/" className="text-link mt-4">
          <ArrowLeft size={14} />
          {t("teamScoreboard.backToScoreboard")}
        </Link>
      </div>
    );
  }

  return (
    <div className="team-page">
      <div
        ref={heroRef}
        className="team-hero"
        data-viewport-animation="running"
      >
        <Link to="/" className="back-link">
          <ArrowLeft size={18} />
          {t("teamScoreboard.backToScoreboard")}
        </Link>

        <section className="team-header">
          <div className="team-header-identity">
            <TeamAvatar name={team.name} src={team.logo_path} size="large" />

            <div>
              <h1>{team.name}</h1>

              <div className="team-header-meta">
                <span>{team.ip}</span>
                {!team.active && <span>{t("teamScoreboard.inactive")}</span>}
              </div>
            </div>
          </div>

          <div
            className="team-current-rank"
            aria-label={t("teamScoreboard.rankAriaLabel", {
              rank: formatNumber((place ?? -1) + 1),
            })}
          >
            {place !== undefined && place >= 0 ? (
              <RankBadge rank={place + 1} />
            ) : (
              "-"
            )}

            <span>{t("teamScoreboard.place")}</span>
          </div>

          <HeroArt />
        </section>
      </div>

      <div className="stats-grid">
        <StatCard
          icon="podium"
          label={t("teamScoreboard.place")}
          value={
            place !== undefined && place >= 0
              ? formatNumber(place + 1)
              : "-"
          }
        />

        <StatCard
          icon="score"
          label={t("teamScoreboard.score")}
          value={<ScoreValue value={team.score ?? 0} />}
        />

        <StatCard
          icon="check"
          label={t("teamScoreboard.availability")}
          value={
            sla === null ? (
              "-"
            ) : (
              <>
                {formatScore(sla)}% <small>SLA</small>
              </>
            )
          }
        />

        <StatCard
          icon="check"
          label={t("teamScoreboard.captured")}
          value={formatNumber(stolen)}
          detail={
            <>
              {t("teamScoreboard.lost")} <b>{formatNumber(lost)}</b>
            </>
          }
        />
      </div>

      <div className="team-content-grid">
        <div className="team-content-main">
          <section className="team-panel services-panel">
            <div className="section-heading">
              <h2>{t("teamScoreboard.servicesTitle")}</h2>
            </div>

            <div className="services-grid">
              {(tasks ?? []).map((task, index) => {
                const value = values.find((item) => item.taskId === task.id);

                return (
                  <div className="service-card" key={task.id}>
                    <BrandIcon
                      name={
                        index % 4 === 2
                          ? "check"
                          : index % 4 === 3
                            ? "team"
                            : "terminal"
                      }
                    />

                    <div className="service-card-copy">
                      <div className="service-card-header">
                        <span>{task.name}</span>
                      </div>

                      <ServiceCell name={task.name} value={value} />
                    </div>
                  </div>
                );
              })}
            </div>
          </section>

          <section className="team-panel history-panel">
            <div className="section-heading">
              <h2>{t("teamScoreboard.historyTitle")}</h2>

              {history.length > 5 && (
                <button
                  type="button"
                  className="history-expand text-link"
                  aria-expanded={expandedHistory}
                  onClick={() => setExpandedHistory(!expandedHistory)}
                >
                  {expandedHistory
                    ? t("teamScoreboard.collapseHistory")
                    : t("teamScoreboard.expandHistory", {
                        formattedCount: formatNumber(history.length),
                      })}
                </button>
              )}
            </div>

            {historyQuery.isError && (
              <div className="notice" role="alert">
                <AlertCircle size={15} />
                {t("teamScoreboard.historyLoadError")}
                <button
                  type="button"
                  className="text-link"
                  onClick={() => void historyQuery.refetch()}
                >
                  {t("teamScoreboard.retry")}
                </button>
              </div>
            )}

            <div className="table-frame">
              <div
                className="table-scroll"
                role="region"
                aria-label={t("teamScoreboard.historyRegionLabel")}
                tabIndex={0}
              >
                <table className="scoreboard-table history-table">
                  <caption className="sr-only">
                    {t("teamScoreboard.historyCaption")}
                  </caption>

                  <thead>
                    <tr>
                      <th scope="col">{t("teamScoreboard.round")}</th>

                      <th
                        scope="col"
                        className="total-column"
                        title={t("teamScoreboard.historicalScoreTitle")}
                      >
                        {t("teamScoreboard.score")}
                      </th>

                      {tasks.map((task) => (
                        <th scope="col" key={task.id}>
                          {task.name}
                        </th>
                      ))}
                    </tr>
                  </thead>

                  <tbody>
                    {(expandedHistory ? history : history.slice(0, 5)).map(
                      (row, index) => {
                        const formattedRound = formatNumber(row.round);
                        const roundLabel = row.incomplete
                          ? t("teamScoreboard.incompleteRound", {
                              round: formattedRound,
                            })
                          : formattedRound;
                        const serviceRoundLabel = t(
                          "teamScoreboard.roundForService",
                          { round: roundLabel },
                        );

                        return (
                          <tr
                            key={row.round}
                            className={index === 0 ? "history-current" : ""}
                          >
                            <td className="history-round">{roundLabel}</td>

                            <td
                              className="total-column"
                              title={
                                row.incomplete
                                  ? t("teamScoreboard.partialScoreTitle")
                                  : t("teamScoreboard.scoreWithSlaTitle")
                              }
                            >
                              {formatScore(row.score)}
                            </td>

                            {tasks.map((task) => (
                              <td key={task.id} className="history-value-cell">
                                <ServiceCell
                                  name={`${task.name}, ${serviceRoundLabel}`}
                                  value={row.tasks.find(
                                    (value) => value.taskId === task.id,
                                  )}
                                />
                              </td>
                            ))}
                          </tr>
                        );
                      },
                    )}

                    {!history.length && (
                      <tr>
                        <td
                          colSpan={2 + tasks.length}
                          className="quiet-empty text-center"
                        >
                          {historyQuery.isPending
                            ? t("teamScoreboard.loadingHistory")
                            : t("teamScoreboard.historyUnavailable")}
                        </td>
                      </tr>
                    )}
                  </tbody>
                </table>
              </div>
            </div>

            <p className="sr-only">
              {t("teamScoreboard.totalScoreNote")}
            </p>
          </section>
        </div>

        <aside
          className="team-content-aside"
          aria-label={t("teamScoreboard.eventsLabel")}
        >
          <RecentEvents teamId={teamId} />
        </aside>
      </div>
    </div>
  );
}
