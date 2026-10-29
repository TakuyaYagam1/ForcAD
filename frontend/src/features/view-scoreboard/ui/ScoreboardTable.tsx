import { useViewportAnimations } from "@/shared/lib/useViewportAnimations";
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
  const frameRef = useRef<HTMLDivElement>(null);
  const previewLarge =
    !onCellClick &&
    ["127.0.0.1", "localhost"].includes(window.location.hostname) &&
    new URLSearchParams(search).get("preview") === "large";

  const ranks = new Map(teams.map((team, index) => [team.id, index + 1]));
  useViewportAnimations(frameRef, teams.length > 0);

  const cells = useMemo(
    () =>
      new Map(
        teamTasks.map((value) => [`${value.teamId}:${value.taskId}`, value]),
      ),
    [teamTasks],
  );

  if (!teams.length)
    return (
      <div className="empty-state">
        <h2>{t("scoreboard.table.noTeamsTitle")}</h2>
        <p>{t("scoreboard.table.noTeamsDescription")}</p>
      </div>
    );

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
            <col style={{ width: onCellClick ? "74px" : "var(--scoreboard-place-width, 74px)" }} />
            <col style={{ width: onCellClick ? "18rem" : "15%" }} />
            <col style={{ width: onCellClick ? "9rem" : "6%" }} />
            {tasks.map((task) => (
              <col
                key={task.id}
                style={{
                  width: onCellClick
                    ? `calc((100% - 506px) / ${tasks.length})`
                    : `calc((79% - var(--scoreboard-place-width, 74px)) / ${tasks.length})`,
                }}
              />
            ))}
          </colgroup>

          <thead>
            <tr>
              <th scope="col" className="place-column">
                {t("scoreboard.table.place")}
              </th>
              <th scope="col" className="team-column">
                {t("scoreboard.table.team")}
              </th>
              <th scope="col" className="total-column">
                {t("scoreboard.table.score")}
              </th>

              {tasks.map((task) => (
                <th scope="col" key={task.id}>
                  {onTaskClick ? (
                    <button type="button" onClick={() => onTaskClick(task.id)}>
                      {task.name}
                    </button>
                  ) : (
                    task.name
                  )}
                </th>
              ))}
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
                      onClick={() =>
                        onTeamClick
                          ? onTeamClick(team.id)
                          : navigate(`/team/${team.id}`)
                      }
                    >
                      <TeamAvatar name={team.name} src={team.logo_path} />
                      <span>
                        <span className="team-name">{team.name}</span>
                        <span className="team-ip">{team.ip}</span>
                      </span>
                    </button>
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
                          value={
                            previewLarge && rank === 1 && taskIndex === 0 && value
                              ? { ...value, score: 12345.67, stolen: 1234, lost: 1234 }
                              : value
                          }
                          name={`${team.name} · ${task.name}`}
                          onClick={
                            onCellClick
                              ? () => onCellClick(team.id, task.id)
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
