import { groupHistoryByRound } from "@/entities/team-scoreboard/model/history";
import { useMemo, useState } from "react";
import { Link, useParams } from "react-router-dom";
import { useQuery } from "@tanstack/react-query";
import { ArrowLeft, AlertCircle } from "lucide-react";
import { useScoreboardStore } from "@/entities/scoreboard/model/store";
import { fetchTeamStates } from "@/entities/team-scoreboard/api";
import { TeamAvatar } from "@/shared/ui/brand/TeamAvatar";
import { BrandIcon } from "@/shared/ui/brand/BrandIcon";
import { GearMechanism } from "@/shared/ui/brand/GearMechanism";
import { ClockworkDial } from "@/shared/ui/brand/ClockworkDial";
import { RankBadge } from "@/shared/ui/brand/RankBadge";
import { ScoreValue } from "@/shared/ui/brand/ScoreValue";
import { formatScore } from "@/shared/lib/formatScore";
import { StatCard } from "@/shared/ui/brand/StatCard";
import { ServiceCell } from "@/shared/ui/brand/ServiceCell";
import { RecentEvents } from "@/shared/ui/brand/RecentEvents";
import { HeroArt } from "@/shared/ui/brand/TournamentHero";
import type { Task } from "@/entities/task/model/types";

const EMPTY_TASKS: Task[] = [];

export function TeamScoreboardView() {
  const [expandedHistory, setExpandedHistory] = useState(false);
  const { teamId: rawTeamId } = useParams();
  const teamId = Number(rawTeamId);
  const invalid = !Number.isInteger(teamId) || teamId <= 0;

  const teams = useScoreboardStore((state) => state.teams);
  const tasks = useScoreboardStore((state) => state.tasks) ?? EMPTY_TASKS;
  const current = useScoreboardStore((state) => state.teamTasks) ?? [];

  const team = teams?.find((item) => item.id === teamId);
  const values = current.filter((item) => item.teamId === teamId);
  const place = teams?.findIndex((item) => item.id === teamId);

  const sla = values.length
    ? values.reduce((sum, value) => sum + value.sla, 0) / values.length
    : 0;

  const stolen = values.reduce((sum, value) => sum + value.stolen, 0);
  const lost = values.reduce((sum, value) => sum + value.lost, 0);

  const historyQuery = useQuery({
    queryKey: ["team-history", teamId],
    queryFn: () => fetchTeamStates(teamId),
    enabled: !invalid,
    refetchInterval: 10000,
  });

  const history = useMemo(
    () =>
      groupHistoryByRound(
        historyQuery.data ?? [],
        (tasks ?? []).map((task) => task.id!).filter(Number.isFinite),
      ),
    [historyQuery.data, tasks],
  );

  if (invalid) {
    return (
      <div className="notice">
        <AlertCircle size={16} />
        Некорректный идентификатор команды.
      </div>
    );
  }

  if (!team) {
    return (
      <div className="empty-state mt-8">
        <BrandIcon name="team" />

        <h2>
          {teams === null ? "Ожидаем данные команды" : "Команда не найдена"}
        </h2>

        <p>
          {teams === null
            ? "Профиль появится после подключения к серверу."
            : "Проверь ссылку или выбери команду в рейтинге."}
        </p>

        <Link to="/" className="text-link mt-4">
          <ArrowLeft size={14} />К рейтингу
        </Link>
      </div>
    );
  }

  return (
    <div className="team-page">
      <div className="team-hero">
        <Link to="/" className="back-link">
          <ArrowLeft size={18} />К рейтингу
        </Link>

        <section className="team-header">
          <div className="team-header-identity">
            <TeamAvatar name={team.name} src={team.logo_path} size="large" />

            <div>
              <h1>{team.name}</h1>

              <div className="team-header-meta">
                <span>{team.ip}</span>
                {!team.active && <span>Неактивна</span>}
              </div>
            </div>
          </div>

          <div
            className="team-current-rank"
            aria-label={`Место в рейтинге: ${(place ?? -1) + 1}`}
          >
            {place !== undefined && place >= 0 ? (
              <RankBadge rank={place + 1} />
            ) : (
              "—"
            )}

            <span>Место</span>
          </div>

          <HeroArt />
        </section>
      </div>

      <div className="stats-grid">
        <StatCard
          icon="podium"
          label="Место"
          value={place !== undefined && place >= 0 ? place + 1 : "—"}
        />

        <StatCard
          icon="cup"
          label="Очки"
          value={<ScoreValue value={team.score ?? 0} />}
        />

        <StatCard
          icon="shield"
          label="Доступность"
          value={
            <>
              {formatScore(sla)}% <small>SLA</small>
            </>
          }
        />

        <StatCard
          icon="flag"
          label="Захвачено"
          value={stolen}
          detail={
            <>
              Потеряно <b>{lost}</b>
            </>
          }
        />
      </div>

      <div className="team-content-grid">
        <div className="team-content-main">
          <section className="team-panel services-panel">
            <div className="section-heading">
              <h2>Состояние сервисов</h2>
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
              <h2>История раундов</h2>

              {history.length > 5 && (
                <button
                  type="button"
                  className="history-expand text-link"
                  aria-expanded={expandedHistory}
                  onClick={() => setExpandedHistory(!expandedHistory)}
                >
                  {expandedHistory
                    ? "Свернуть историю"
                    : `Показать всю историю (${history.length})`}
                </button>
              )}
            </div>

            {historyQuery.isError && (
              <div className="notice" role="alert">
                <AlertCircle size={15} />
                Не удалось загрузить историю.
                <button
                  type="button"
                  className="text-link"
                  onClick={() => void historyQuery.refetch()}
                >
                  Повторить
                </button>
              </div>
            )}

            <div className="table-frame">
              <div
                className="table-scroll"
                role="region"
                aria-label="История раундов команды"
                tabIndex={0}
              >
                <table className="scoreboard-table history-table">
                  <caption className="sr-only">
                    История результатов команды по раундам
                  </caption>

                  <thead>
                    <tr>
                      <th scope="col">Раунд</th>

                      <th
                        scope="col"
                        className="total-column"
                        title="Исторические очки с учётом SLA"
                      >
                        Очки
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
                        const roundLabel = row.incomplete
                          ? `${row.round} · неполные данные`
                          : String(row.round);

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
                                  ? "Частичный итог: есть данные не всех сервисов"
                                  : "Очки с учётом SLA"
                              }
                            >
                              {formatScore(row.score)}
                            </td>

                            {tasks.map((task) => (
                              <td key={task.id} className="history-value-cell">
                                <ServiceCell
                                  name={`${task.name} · раунд ${roundLabel}`}
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
                            ? "Загружаем историю…"
                            : "История раундов пока недоступна."}
                        </td>
                      </tr>
                    )}
                  </tbody>
                </table>
              </div>
            </div>

            <p className="sr-only">
              Суммарные очки в истории рассчитаны с учётом SLA.
            </p>
          </section>
        </div>

        <aside className="team-content-aside" aria-label="События команды">
          <RecentEvents teamId={teamId} />

          <div className="team-emblem" aria-hidden="true">
            <ClockworkDial />
            <img src="/brand/mechanism.webp" alt="" />
            <BrandIcon name="shield" />
            <GearMechanism />
            <i className="sparkle sparkle--one" />
            <i className="sparkle sparkle--two" />
          </div>
        </aside>
      </div>
    </div>
  );
}
