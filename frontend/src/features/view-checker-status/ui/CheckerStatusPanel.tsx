import { useMemo } from "react";
import { useTranslation } from "react-i18next";
import { useScoreboardStore } from "@/entities/scoreboard/model/store";
import { checkerStatus } from "../model/checkerStatus";

export function CheckerStatusPanel() {
  const { t } = useTranslation("admin");
  const tasks = useScoreboardStore((state) => {
    return state.tasks
  });
  const teams = useScoreboardStore((state) => {
    return state.teams
  });
  const teamTasks = useScoreboardStore((state) => {
    return state.teamTasks
  });
  const round = useScoreboardStore((state) => {
    return state.runtimeRound ?? state.round
  });
  const running = useScoreboardStore((state) => {
    return state.phase === "running"
  });
  const summaries = useMemo(() => {
    return (tasks ?? []).map((task) => {
      const cells = (teamTasks ?? []).filter((cell) => {
        return cell.taskId === task.id
      });
      return {
        id: task.id,
        name: task.name,
        checks: cells.reduce((total, cell) => {
          return total + cell.checks
        }, 0),
        reported: cells.filter((cell) => {
          return cell.status > 0
        }).length,
        failed: cells.filter((cell) => {
          return cell.status === 110
        }).length,
      };
    })
  }, [tasks, teamTasks]);

  return (
    <section className="checker-status" aria-label={t("checkerStatus.title")}>
      <div className="checker-status__heading">
        <h2>{t("checkerStatus.title")}</h2>
        <small>{t("checkerStatus.basis")}</small>
      </div>
      <div className="checker-status__grid">
        {summaries.map((summary) => {
          const status = checkerStatus(
            round,
            summary.reported,
            summary.failed,
            running && (teams?.length ?? 0) > 0,
          );
          return (
            <div className={`checker-status__card checker-status__card--${status}`} key={summary.id}>
              <strong title={summary.name}>{summary.name}</strong>
              <span>{t(`checkerStatus.${status}`)}</span>
              <small>
                {t("checkerStatus.reports", {
                  reported: summary.reported,
                  total: teams?.length ?? 0
                })}
                {summary.failed > 0 && ` - ${t("checkerStatus.failedCount", {
                  count: summary.failed
                })}`}
              </small>
            </div>
          );
        })}
        {summaries.length === 0 && <p>{t("checkerStatus.noServices")}</p>}
      </div>
    </section>
  );
}
