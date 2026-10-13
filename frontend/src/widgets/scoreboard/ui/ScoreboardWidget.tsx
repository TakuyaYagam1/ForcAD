import { fetchScoreboardConfig } from "@/entities/scoreboard/api/config";
import { useScoreboardStore } from "@/entities/scoreboard/model/store";
import {
  ScoreboardTable,
  type ScoreboardTask,
  type ScoreboardTeam,
  type ScoreboardTeamTask,
} from "@/features/view-scoreboard/ui/ScoreboardTable";
import { StatusesBar } from "@/features/view-scoreboard/ui/StatusesBar";
import { SCOREBOARD_STATUSES } from "@/shared/config/statuses";
import { BrandIcon } from "@/shared/ui/brand/BrandIcon";
import { PageHero } from "@/shared/ui/brand/PageHero";
import { RecentEvents } from "@/shared/ui/brand/RecentEvents";
import { useQuery } from "@tanstack/react-query";
import { ChevronDown, ChevronUp, Search, WifiOff } from "lucide-react";
import { useEffect, useState } from "react";
import { useScoreboardEntrance } from "../model/useScoreboardEntrance";
import { useScoreboardViewport } from "../model/useScoreboardViewport";
import "./ScoreboardWidget.css";

interface ScoreboardWidgetProps {
  onTeamClick?: (teamId: string) => void;
  onTaskClick?: (taskId: string) => void;
  onCellClick?: (teamId: string, taskId: string) => void;
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
  const [bannerVisible, setBannerVisible] = useState(() => {
    try {
      return localStorage.getItem("cf-scoreboard-banner") !== "off";
    } catch {
      return true;
    }
  });

  function toggleBanner() {
    const visible = !bannerVisible;
    setBannerVisible(visible);
    try {
      localStorage.setItem("cf-scoreboard-banner", visible ? "on" : "off");
    } catch {
      // The toggle still works when browser storage is unavailable.
    }
  }

  useEffect(() => {
    const timer = window.setTimeout(() => setAppliedFilter(filter), 300);
    return () => window.clearTimeout(timer);
  }, [filter]);

  const { pageRef, tableRef } = useScoreboardEntrance(
    !admin,
    appliedFilter,
    teams.length > 0,
  );

  useScoreboardViewport(!admin, pageRef, teams.length);

  const config = useQuery({
    queryKey: ["game-config"],
    queryFn: ({ signal }) => fetchScoreboardConfig(signal),
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
        admin
          ? "scoreboard-page scoreboard-page--admin"
          : "scoreboard-page scoreboard-page--compact"
      }
    >
      {!admin && (
        <div id="scoreboard-banner" hidden={!bannerVisible}>
          <PageHero
            title="Рейтинг команд"
            subtitle="Результаты команд и состояние сервисов по раундам."
          >
            <div className="hero-metadata">
              <span>
                <BrandIcon name="team" plain />
                {teams.length || "-"} команд
              </span>
              <span>
                <BrandIcon name="terminal" plain />
                {tasks.length || "-"} сервиса
              </span>
              <span>Attack-Defense</span>
            </div>
          </PageHero>
        </div>
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
        {!admin && (
          <button
            type="button"
            className="banner-toggle"
            aria-label={bannerVisible ? "Скрыть баннер" : "Показать баннер"}
            title={bannerVisible ? "Скрыть баннер" : "Показать баннер"}
            aria-expanded={bannerVisible}
            aria-controls="scoreboard-banner"
            onClick={toggleBanner}
          >
            {bannerVisible ? (
              <ChevronUp size={18} aria-hidden="true" />
            ) : (
              <ChevronDown size={18} aria-hidden="true" />
            )}
          </button>
        )}
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
