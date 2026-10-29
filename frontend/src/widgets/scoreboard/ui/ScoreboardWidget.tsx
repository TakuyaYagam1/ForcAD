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
import { useQuery } from "@tanstack/react-query";
import { WifiOff } from "lucide-react";
import { useTranslation } from "react-i18next";
import { useEffect } from "react";
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
  const { t } = useTranslation("public");
  const teams = useScoreboardStore((state) => state.teams) ?? [];
  const tasks = useScoreboardStore((state) => state.tasks) ?? [];
  const teamTasks = useScoreboardStore((state) => state.teamTasks) ?? [];
  const round = useScoreboardStore((state) => state.round);
  const roundStart = useScoreboardStore((state) => state.roundStart);
  const error = useScoreboardStore((state) => state.error);
  const roundTime = useScoreboardStore((state) => state.roundTime);
  const setRoundTime = useScoreboardStore((state) => state.setRoundTime);

  const { pageRef, tableRef } = useScoreboardEntrance(!admin, teams.length > 0);

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
      {!admin && <h1 className="sr-only">{t("scoreboard.title")}</h1>}

      {error && (
        <div className="notice" role="status">
          <WifiOff size={16} />
          {error === "stateUnavailable"
            ? t("scoreboard.stateUnavailable")
            : t("scoreboard.connectionLost")}
        </div>
      )}

      {!admin && <StatusesBar round={round} roundStart={roundStart} />}

      <div className="scoreboard-toolbar">
        <div
          className="status-legend"
          aria-label={t("scoreboard.serviceStates")}
        >
          {SCOREBOARD_STATUSES.map((status) => (
            <span
              key={status.code}
              title={status.description}
              style={{ backgroundColor: status.color }}
            >
              {status.label}
            </span>
          ))}
        </div>
      </div>

      <div ref={tableRef} className="scoreboard-reveal">
        <ScoreboardTable
          teams={teams as ScoreboardTeam[]}
          tasks={tasks as ScoreboardTask[]}
          teamTasks={teamTasks as ScoreboardTeamTask[]}
          onTeamClick={onTeamClick}
          onTaskClick={onTaskClick}
          onCellClick={onCellClick}
        />
      </div>

    </div>
  );
}
