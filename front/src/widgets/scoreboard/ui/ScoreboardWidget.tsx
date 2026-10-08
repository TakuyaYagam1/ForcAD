import { useQuery } from "@tanstack/react-query";
import { useEffect, useState } from "react";
import { Search, RefreshCw, WifiOff } from "lucide-react";
import { useScoreboardStore } from "@/entities/scoreboard/model/store";
import { fetchScoreboardConfig } from "@/entities/scoreboard/api/config";
import { StatusesBar } from "@/features/view-scoreboard/ui/StatusesBar";
import {
  ScoreboardTable,
  type ScoreboardTeam,
  type ScoreboardTask,
  type ScoreboardTeamTask,
} from "@/features/view-scoreboard/ui/ScoreboardTable";
import { SCOREBOARD_STATUSES } from "@/shared/config/statuses";
import { BrandIcon } from "@/shared/ui/brand/BrandIcon";
import { TournamentHero } from "@/shared/ui/brand/TournamentHero";
import { RecentEvents } from "@/shared/ui/brand/RecentEvents";
import { DEMO_MODE } from "@/app/demo/mode";
import { useScoreboardEntrance } from "../model/useScoreboardEntrance";
import { useScoreboardViewport } from "../model/useScoreboardViewport";
import "./ScoreboardWidget.css";

interface ScoreboardWidgetProps {
  onTeamClick?: (teamId: number) => void;
  onTaskClick?: (taskId: number) => void;
  onCellClick?: (teamId: number, taskId: number) => void;
  admin?: boolean;
}

export function ScoreboardWidget({
  onTeamClick,
  onTaskClick,
  onCellClick,
  admin = false,
}: ScoreboardWidgetProps) {
  const teams = useScoreboardStore((state) => state.teams) ?? [];
  const tasks = useScoreboardStore((state) => state.tasks) ?? [];
  const teamTasks = useScoreboardStore((state) => state.teamTasks) ?? [];
  const round = useScoreboardStore((state) => state.round);
  const roundStart = useScoreboardStore((state) => state.roundStart);
  const error = useScoreboardStore((state) => state.error);
  const roundTime = useScoreboardStore((state) => state.roundTime);
  const setRoundTime = useScoreboardStore((state) => state.setRoundTime);

  const [filter, setFilter] = useState("");
  const [appliedFilter, setAppliedFilter] = useState("");

  useEffect(() => {
    const timer = window.setTimeout(() => setAppliedFilter(filter), 300);
    return () => window.clearTimeout(timer);
  }, [filter]);

  const { pageRef, tableRef } = useScoreboardEntrance(
    !admin,
    appliedFilter,
    teams.length > 0,
  );

  useScoreboardViewport(!admin, pageRef);

  const config = useQuery({
    queryKey: ["game-config"],
    queryFn: fetchScoreboardConfig,
    enabled: roundTime === null,
    retry: 2,
    refetchInterval: 10000,
  });
  useEffect(() => {
    if (config.data?.round_time) setRoundTime(config.data.round_time);
  }, [config.data, setRoundTime]);

  return (
    <div
      ref={pageRef}
      className={
        admin ? "scoreboard-page scoreboard-page--admin" : "scoreboard-page"
      }
    >
      {!admin && (
        <TournamentHero
          title="Рейтинг команд"
          subtitle="История хранит код. Будущее переписывает."
        >
          <div className="hero-metadata">
            <span>
              <BrandIcon name="team" plain />
              {teams.length || "—"} команд
            </span>
            <span>
              <BrandIcon name="terminal" plain />
              {tasks.length || "—"} сервиса
            </span>
            <span>Attack–Defense</span>
          </div>
        </TournamentHero>
      )}

      {error && (
        <div className="notice" role="status">
          <WifiOff size={16} />
          Соединение с сервером потеряно. Повторное подключение выполняется
          автоматически.
        </div>
      )}

      <StatusesBar round={round} roundStart={roundStart} />

      <div className="scoreboard-toolbar">
        <label className="search-field">
          <Search size={17} />
          <input
            type="search"
            aria-label="Найти команду по названию или IP"
            placeholder="Найти команду или IP…"
            value={filter}
            onChange={(event) => setFilter(event.target.value)}
          />
        </label>

        <div className="status-legend" aria-label="Состояния сервисов">
          {SCOREBOARD_STATUSES.map((status) => (
            <span key={status.code} title={status.description}>
              <i
                className="status-dot"
                style={{ background: status.color, color: status.color }}
              />
              {status.label}
            </span>
          ))}
        </div>

        <span className="toolbar-note">
          <RefreshCw size={12} />
          {DEMO_MODE ? "Демонстрационные данные" : "Обновляется автоматически"}
        </span>
      </div>

      <div ref={tableRef} className="scoreboard-reveal">
        <ScoreboardTable
          teams={teams as ScoreboardTeam[]}
          tasks={tasks as ScoreboardTask[]}
          teamTasks={teamTasks as ScoreboardTeamTask[]}
          filter={appliedFilter}
          onTeamClick={onTeamClick}
          onTaskClick={onTaskClick}
          onCellClick={onCellClick}
        />
      </div>

      <div className="table-hint">
        <span>Нажми на команду или сервис, чтобы открыть подробности</span>
        <span>+ захваченные / − потерянные флаги</span>
      </div>

      {!admin && <RecentEvents />}
    </div>
  );
}
