import { useViewportAnimations } from "@/shared/lib/useViewportAnimations";
import { BrandIcon } from "@/shared/ui/brand/BrandIcon";
import { RankBadge } from "@/shared/ui/brand/RankBadge";
import { ScoreValue } from "@/shared/ui/brand/ScoreValue";
import { ServiceCell } from "@/shared/ui/brand/ServiceCell";
import { TeamAvatar } from "@/shared/ui/brand/TeamAvatar";
import { useTranslation } from "react-i18next";
import { getLocale } from "@/shared/i18n";
import { useLayoutEffect, useMemo, useRef } from "react";
import { useNavigate } from "react-router-dom";
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
  filter?: string;
  onTeamClick?: (teamId: string) => void;
  onTaskClick?: (taskId: string) => void;
  onCellClick?: (teamId: string, taskId: string) => void;
}

export function ScoreboardTable({
  teams,
  tasks,
  teamTasks,
  filter = "",
  onTeamClick,
  onTaskClick,
  onCellClick,
}: ScoreboardTableProps) {
  const { t } = useTranslation("public");
  const navigate = useNavigate();
  const frameRef = useRef<HTMLDivElement>(null);
  const rows = useRef(new Map<string, HTMLTableRowElement>());
  const previous = useRef(new Map<string, { rank: number; top: number }>());

  const locale = getLocale();
  const query = filter.trim().toLocaleLowerCase(locale);
  const visible = teams.filter((team) =>
    `${team.name} ${team.ip}`.toLocaleLowerCase(locale).includes(query),
  );
  const ranks = new Map(teams.map((team, index) => [team.id, index + 1]));
  useViewportAnimations(frameRef, visible.length > 0);

  const cells = useMemo(
    () =>
      new Map(
        teamTasks.map((value) => [`${value.teamId}:${value.taskId}`, value]),
      ),
    [teamTasks],
  );

  useLayoutEffect(() => {
    const next = new Map<string, { rank: number; top: number }>();
    const animations: Animation[] = [];
    const timers: number[] = [];
    const animatedRows: HTMLTableRowElement[] = [];

    const reduced =
      window.matchMedia("(prefers-reduced-motion: reduce)").matches ||
      document.documentElement.dataset.motion === "off";

    teams.forEach((team, index) => {
      const row = rows.current.get(team.id);
      if (!row) return;

      const top = row.offsetTop;
      const rank = index + 1;
      const old = previous.current.get(team.id);

      next.set(team.id, { rank, top });

      if (!old || old.rank === rank || reduced) return;

      if (old.top !== top)
        animations.push(
          row.animate(
            [
              { transform: `translateY(${old.top - top}px)` },
              { transform: "translateY(0)" },
            ],
            { duration: 600, easing: "cubic-bezier(.22,1,.36,1)" },
          ),
        );

      row.style.setProperty(
        "--glint-delay",
        `${80 + Math.min(index, 6) * 25}ms`,
      );
      row.style.setProperty("--glint-travel", `${row.offsetWidth + 150}px`);

      row.classList.remove("is-sweeping");
      void row.offsetWidth;
      row.classList.add("is-sweeping");

      animatedRows.push(row);
      timers.push(
        window.setTimeout(() => row.classList.remove("is-sweeping"), 1100),
      );
    });

    previous.current = next;

    return () => {
      animations.forEach((animation) => animation.cancel());
      timers.forEach(window.clearTimeout);
      animatedRows.forEach((row) => row.classList.remove("is-sweeping"));
    };
  }, [teams, filter]);

  if (!teams.length)
    return (
      <div className="empty-state">
        <BrandIcon name="podium" />
        <h2>{t("scoreboard.table.noTeamsTitle")}</h2>
        <p>{t("scoreboard.table.noTeamsDescription")}</p>
      </div>
    );

  if (!visible.length)
    return (
      <div className="empty-state">
        <BrandIcon name="team" />
        <h2>{t("scoreboard.table.noMatchTitle")}</h2>
        <p>{t("scoreboard.table.noMatchDescription")}</p>
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
            {visible.map((team) => {
              const rank = ranks.get(team.id) ?? 0;

              return (
                <tr
                  key={team.id}
                  data-team-id={team.id}
                  ref={(row) => {
                    if (row) rows.current.set(team.id, row);
                    else rows.current.delete(team.id);
                  }}
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

                  {tasks.map((task) => (
                    <td key={task.id}>
                      <ServiceCell
                        value={cells.get(`${team.id}:${task.id}`)}
                        name={`${team.name} · ${task.name}`}
                        onClick={
                          onCellClick
                            ? () => onCellClick(team.id, task.id)
                            : undefined
                        }
                      />
                    </td>
                  ))}
                </tr>
              );
            })}
          </tbody>
        </table>
      </div>
    </div>
  );
}
