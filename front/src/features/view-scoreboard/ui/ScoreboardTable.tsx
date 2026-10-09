import { useLayoutEffect, useMemo, useRef } from "react";
import { useNavigate } from "react-router-dom";
import { BrandIcon } from "@/shared/ui/brand/BrandIcon";
import { TeamAvatar } from "@/shared/ui/brand/TeamAvatar";
import { ScoreValue } from "@/shared/ui/brand/ScoreValue";
import { ServiceCell } from "@/shared/ui/brand/ServiceCell";
import { RankBadge } from "@/shared/ui/brand/RankBadge";
import "./ScoreboardTable.css";
import { useViewportAnimations } from "@/shared/lib/useViewportAnimations";

export interface ScoreboardTeam {
  id: number;
  name: string;
  score: number;
  highlighted?: boolean;
  logo_path?: string;
  ip: string;
}

export interface ScoreboardTask {
  id: number;
  name: string;
}

export interface ScoreboardTeamTask {
  id: number;
  teamId: number;
  taskId: number;
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
  onTeamClick?: (teamId: number) => void;
  onTaskClick?: (taskId: number) => void;
  onCellClick?: (teamId: number, taskId: number) => void;
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
  const navigate = useNavigate();
  const frameRef = useRef<HTMLDivElement>(null);
  const rows = useRef(new Map<number, HTMLTableRowElement>());
  const previous = useRef(new Map<number, { rank: number; top: number }>());

  const query = filter.trim().toLocaleLowerCase("ru-RU");
  const visible = teams.filter((team) =>
    `${team.name} ${team.ip}`.toLocaleLowerCase("ru-RU").includes(query),
  );
  const ranks = new Map(teams.map((team, index) => [team.id, index + 1]));
  const leaderVisible = visible.some((team) => ranks.get(team.id) === 1);
  useViewportAnimations(frameRef, visible.length > 0);

  const cells = useMemo(
    () =>
      new Map(
        teamTasks.map((value) => [`${value.teamId}:${value.taskId}`, value]),
      ),
    [teamTasks],
  );

  useLayoutEffect(() => {
    const next = new Map<number, { rank: number; top: number }>();
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

    const frame = frameRef.current;
    const scroll = frame?.querySelector<HTMLDivElement>(".table-scroll");
    const leader = teams[0] ? rows.current.get(teams[0].id) : undefined;

    const positionParticles = () => {
      if (!frame || !scroll || !leader) return;

      let top = leader.offsetTop;
      let parent = leader.offsetParent as HTMLElement | null;

      while (parent && parent !== frame) {
        top += parent.offsetTop;
        parent = parent.offsetParent as HTMLElement | null;
      }

      frame.style.setProperty("--leader-top", `${top - scroll.scrollTop}px`);
      frame.style.setProperty("--leader-height", `${leader.offsetHeight}px`);
    };

    const observer = new ResizeObserver(positionParticles);

    if (frame && scroll && leader) {
      observer.observe(frame);
      observer.observe(leader);
      scroll.addEventListener("scroll", positionParticles, { passive: true });
      positionParticles();
    }

    return () => {
      animations.forEach((animation) => animation.cancel());
      timers.forEach(window.clearTimeout);
      animatedRows.forEach((row) => row.classList.remove("is-sweeping"));
      observer.disconnect();
      scroll?.removeEventListener("scroll", positionParticles);
    };
  }, [teams, filter]);

  if (!teams.length)
    return (
      <div className="empty-state">
        <BrandIcon name="podium" />
        <h2>Ожидаем команды</h2>
        <p>Рейтинг появится после подключения к серверу соревнования.</p>
      </div>
    );

  if (!visible.length)
    return (
      <div className="empty-state">
        <BrandIcon name="team" />
        <h2>Команда не найдена</h2>
        <p>Попробуй другое название или IP-адрес.</p>
      </div>
    );

  return (
    <div
      ref={frameRef}
      data-viewport-animation="running"
      className={`table-frame ${leaderVisible ? "has-leader" : ""}`}
    >
      {leaderVisible && <div className="leader-particles" aria-hidden="true" />}
      <div
        className="table-scroll"
        role="region"
        aria-label="Рейтинг команд — таблица с горизонтальной прокруткой"
        tabIndex={0}
      >
        <table className="scoreboard-table">
          <caption className="sr-only">
            Текущий рейтинг команд, очки, состояние сервисов и SLA
          </caption>

          <thead>
            <tr>
              <th scope="col" className="place-column">
                Место
              </th>
              <th scope="col" className="team-column">
                Команда
              </th>
              <th scope="col" className="total-column">
                Очки
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
