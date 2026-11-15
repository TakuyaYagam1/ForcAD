import { useViewportAnimations } from "@/shared/lib/useViewportAnimations";
import { snapshotScoreboard } from "../model/scoreboardUpdates";
import { useScoreboardUpdates } from "../model/useScoreboardUpdates";
import { useScoreboardStore } from "@/entities/scoreboard/model/store";
import { getCompletedScoreRound } from "../model/roundDelta";
import { PreviousRoundDelta } from "./PreviousRoundDelta";
import { RankBadge } from "@/shared/ui/brand/RankBadge";
import { ScoreValue } from "@/shared/ui/brand/ScoreValue";
import { ServiceCell } from "@/shared/ui/brand/ServiceCell";
import { TeamAvatar } from "@/shared/ui/brand/TeamAvatar";
import { useTranslation } from "react-i18next";
import { useMemo, useRef } from "react";
import { useLocation, useNavigate } from "react-router-dom";
import "./ScoreboardTable.css";

export interface ScoreboardTeam {
  id: string;
  name: string;
  score: number;
  highlighted?: boolean;
  logo_path?: string;
  ip: string;
}

export interface ScoreboardTask {
  id: string;
  name: string;
}

export interface ScoreboardTeamTask {
  teamId: string;
  taskId: string;
  status: number;
  score: number;
  checks: number;
  checksPassed: number;
  sla: number;
  stolen: number;
  lost: number;
  message: string;
}

interface ScoreboardTableProps {
  teams: ScoreboardTeam[];
  tasks: ScoreboardTask[];
  teamTasks: ScoreboardTeamTask[];
  onTeamClick?: (teamId: string) => void;
  onTaskClick?: (taskId: string) => void;
  onCellClick?: (teamId: string, taskId: string) => void;
}

export function ScoreboardTable({
  teams,
  tasks,
  teamTasks,
  onTeamClick,
  onTaskClick,
  onCellClick,
}: ScoreboardTableProps) {
  const { t } = useTranslation("public");
  const navigate = useNavigate();
  const { search } = useLocation();
  const snapshotRound = useScoreboardStore((state) => {
    return state.round;
  });
  const runtimeRound = useScoreboardStore((state) => {
    return state.runtimeRound;
  });
  const generation = useScoreboardStore((state) => {
    return state.generation;
  });
  const resetPending = useScoreboardStore((state) => {
    return state.resetPending;
  });
  const completedRound = resetPending ? null : getCompletedScoreRound(snapshotRound, runtimeRound);
  const taskIds = useMemo(() => {
    return tasks.map((task) => {
      return task.id;
    });
  }, [tasks]);
  const frameRef = useRef<HTMLDivElement>(null);
  const previewLarge =
    !onCellClick &&
    ["127.0.0.1", "localhost"].includes(window.location.hostname) &&
    new URLSearchParams(search).get("preview") === "large";

  const ranks = new Map(teams.map((team, index) => {
    return [team.id, index + 1]
  }));
  useViewportAnimations(frameRef, teams.length > 0);
  useScoreboardUpdates(frameRef, snapshotScoreboard(teams, tasks, teamTasks));

  const cells = useMemo(
    () => {
      return new Map(
        teamTasks.map((value) => {
          return [`${value.teamId}:${value.taskId}`, value]
        }),
      )
    },
    [teamTasks],
  );

  if (!teams.length) {
    return (
      <div className="empty-state">
        <h2>{t("scoreboard.table.noTeamsTitle")}</h2>
        <p>{t("scoreboard.table.noTeamsDescription")}</p>
      </div>
    );
  }

  return (
    <div
      ref={frameRef}
      data-viewport-animation="running"
      className="table-frame"
    >
      <div
        className="table-scroll"
        role="region"
        aria-label={t("scoreboard.table.regionLabel")}
        tabIndex={0}
      >
        <table className="scoreboard-table">
          <caption className="sr-only">
            {t("scoreboard.table.caption")}
          </caption>
          <colgroup>
            <col style={{
              width: "60px"
            }} />
            <col style={{
              width: "220px"
            }} />
            <col style={{
              width: "120px"
            }} />
            <col style={{
              width: "120px"
            }} />
            {tasks.map((task) => {
              return (
                <col
                  key={task.id}
                  style={{
                    width: `calc((100% - 520px) / ${tasks.length})`,
                  }}
                />
              )
            })}
          </colgroup>

          <thead>
            <tr>
              <th scope="col" className="place-column">
                {t("scoreboard.table.place")}
              </th>
              <th scope="col" className="team-column">
                {t("scoreboard.table.team")}
              </th>
              <th scope="col" className="delta-column">
                {t("scoreboard.delta.column")}
              </th>
              <th scope="col" className="total-column">
                {t("scoreboard.table.score")}
              </th>

              {tasks.map((task) => {
                return (
                  <th scope="col" key={task.id}>
                    {onTaskClick ? (
                      <button type="button" onClick={() => {
                        return onTaskClick(task.id)
                      }}>
                        {task.name}
                      </button>
                    ) : (
                      task.name
                    )}
                  </th>
                )
              })}
            </tr>
          </thead>

          <tbody>
            {teams.map((team) => {
              const rank = ranks.get(team.id) ?? 0;
              return (
                <tr
                  key={team.id}
                  data-team-id={team.id}
                  className={`scoreboard-row ${rank === 1 ? "is-leader" : ""} ${team.highlighted ? "is-highlighted" : ""}`}
                >
                  <td className="place-column">
                    <RankBadge rank={rank} />
                  </td>

                  <td className="team-column">
                    <button
                      type="button"
                      className="team-identity"
                      onClick={() => {
                        return onTeamClick
                          ? onTeamClick(team.id)
                          : navigate(`/team/${team.id}`)
                      }
                      }
                    >
                      <TeamAvatar name={team.name} src={team.logo_path} />
                      <span>
                        <span className="team-name">{team.name}</span>
                        <span className="team-ip">{team.ip}</span>
                      </span>
                    </button>
                  </td>

                  <td className="delta-column">
                    <PreviousRoundDelta scope={completedRound === null ? null : {
                      generation,
                      round: completedRound,
                      teamId: team.id,
                      taskIds,
                    }} />
                  </td>

                  <td className="total-column">
                    <ScoreValue value={team.score} />
                  </td>

                  {tasks.map((task, taskIndex) => {
                    const value = cells.get(`${team.id}:${task.id}`);
                    return (
                      <td key={task.id} className="service-column">
                        <span className="service-cell-name">{task.name}</span>
                        <ServiceCell
                          teamId={team.id}
                          taskId={task.id}
                          teamName={team.name}
                          value={
                            previewLarge && rank === 1 && taskIndex === 0 && value
                              ? {
                                ...value,
                                score: 12345.67,
                                stolen: 1234,
                                lost: 1234
                              }
                              : value
                          }
                          name={`${team.name} · ${task.name}`}
                          onClick={
                            onCellClick
                              ? () => {
                                return onCellClick(team.id, task.id)
                              }
                              : undefined
                          }
                        />
                      </td>
                    );
                  })}
                </tr>
              );
            })}
          </tbody>
        </table>
      </div>
    </div>
  );
}
