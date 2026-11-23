import { useMemo, useState } from "react";
import { useTranslation } from "react-i18next";
import { useScoreboardStore } from "@/entities/scoreboard/model/store";
import {
  getGetAdminCheckerStatsQueryKey,
  useGetAdminCheckerStats,
} from "@/shared/api/generated/endpoints";
import { compareUuid } from "@/shared/lib/uuid";
import "./CheckerStatusPanel.css";

function CheckerResults({ teamId, round }: {
  teamId: string;
  round: number;
}) {
  const { t } = useTranslation("admin");
  const generation = useScoreboardStore((state) => {
    return state.generation
  });
  const phase = useScoreboardStore((state) => {
    return state.phase
  });
  const pending = useScoreboardStore((state) => {
    return state.resultsPending
  });
  const params = {
    team_id: teamId,
    round,
  };
  const query = useGetAdminCheckerStats(params, {
    query: {
      queryKey: [...getGetAdminCheckerStatsQueryKey(params), generation, phase, pending],
      refetchInterval: phase === "running" || pending ? 5000 : false,
      retry: false,
    },
  });
  if (query.isError) {
    return <p role="alert">{t("checkerStatus.error")}</p>;
  }
  if (!query.data) {
    return <p role="status">{t("checkerStatus.loading")}</p>;
  }
  if (!query.data.tasks.length) {
    return <p role="status">{t("checkerStatus.noServices")}</p>;
  }
  return (
    <>
      <p className="checker-results__basis">
        {t(round === 0 ? "checkerStatus.configuredBasis" : "checkerStatus.basis")}
      </p>
      <div className="checker-results__grid">
        {query.data.tasks.map((task) => {
          return (
            <article className="checker-status__card" aria-label={task.name} key={task.task_id}>
              <strong title={task.name}>{task.name}</strong>
              <dl className="checker-results__counts">
                {(["check", "put", "get"] as const).map((action) => {
                  const count = task[action];
                  return (
                    <div key={action}>
                      <dt>{action.toUpperCase()}</dt>
                      <dd>
                        {count.maximum > 0
                          ? `${count.reported}/${count.maximum}`
                          : count.reported > 0 ? count.reported : t("checkerStatus.unplanned")}
                        {count.failed > 0 && <small className="checker-results__failure">
                          {t("checkerStatus.failedCount", {
                            count: count.failed
                          })}
                        </small>}
                        {count.no_result > 0 && <small>
                          {t("checkerStatus.noResult", {
                            count: count.no_result
                          })}
                        </small>}
                      </dd>
                    </div>
                  );
                })}
              </dl>
            </article>
          );
        })}
      </div>
    </>
  );
}

export function CheckerStatusPanel() {
  const { t } = useTranslation("admin");
  const players = useScoreboardStore((state) => {
    return state.teams
  });
  const npcs = useScoreboardStore((state) => {
    return state.npcTeams
  });
  const round = useScoreboardStore((state) => {
    return state.runtimeRound
  });
  const error = useScoreboardStore((state) => {
    return state.error
  });
  const [selection, setSelection] = useState("");
  const teams = useMemo(() => {
    return [...(players ?? []), ...npcs].sort((a, b) => {
      return compareUuid(a.id, b.id)
    });
  }, [players, npcs]);
  const team = teams.find((candidate) => {
    return candidate.id === selection
  }) ?? teams[0];
  let contents;
  if (error && (players === null || round === null)) {
    contents = <p role="alert">{t("checkerStatus.error")}</p>;
  } else if (players !== null && !team) {
    contents = <p role="status">{t("checkerStatus.noTeams")}</p>;
  } else if (team && round !== null) {
    contents = <CheckerResults teamId={team.id} round={round} />;
  } else {
    contents = <p role="status">{t("checkerStatus.loading")}</p>;
  }

  return (
    <section className="checker-status" aria-label={t("checkerStatus.title")}>
      <div className="checker-status__heading">
        <h2>{t("checkerStatus.title")}</h2>
        {team && (
          <label className="checker-results__team">
            {t("checkerStatus.team")}
            <select value={team.id} onChange={(event) => {
              setSelection(event.target.value);
            }}>
              {teams.map((candidate) => {
                return <option key={candidate.id} value={candidate.id}>{candidate.name} ({candidate.ip})</option>;
              })}
            </select>
          </label>
        )}
        {round !== null && <small>{round === 0
          ? t("checkerStatus.noRound")
          : t("checkerStatus.round", {
            round
          })}</small>}
      </div>
      {contents}
    </section>
  );
}
