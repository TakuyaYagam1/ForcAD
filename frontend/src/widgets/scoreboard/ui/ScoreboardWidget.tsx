import { useScoreboardStore } from "@/entities/scoreboard/model/store";
import {
  ScoreboardTable,
  type ScoreboardTask,
  type ScoreboardTeam,
  type ScoreboardTeamTask,
} from "@/features/view-scoreboard/ui/ScoreboardTable";
import { SCOREBOARD_STATUSES } from "@/shared/config/statuses";
import { WifiOff } from "lucide-react";
import { useTranslation } from "react-i18next";
import { useRef } from "react";
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
  const teams = useScoreboardStore((state) => {
    return state.teams
  }) ?? [];
  const tasks = useScoreboardStore((state) => {
    return state.tasks
  }) ?? [];
  const teamTasks = useScoreboardStore((state) => {
    return state.teamTasks
  }) ?? [];
  const error = useScoreboardStore((state) => {
    return state.error
  });
  const pageRef = useRef<HTMLDivElement>(null);
  useScoreboardViewport(!admin, pageRef, teams.length);
  return (
    <div
      ref={pageRef}
      className={
        admin
          ? "scoreboard-page scoreboard-page--admin"
          : `scoreboard-page scoreboard-page--compact${teams.length > 10 ? " scoreboard-page--dense" : ""}`
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


      <div className="scoreboard-toolbar">
        <div
          className="status-legend"
          aria-label={t("scoreboard.serviceStates")}
        >
          {SCOREBOARD_STATUSES.map((status) => {
            return (
              <span
                key={status.code}
                title={status.description}
                style={{
                  backgroundColor: status.color,
                  color: status.foreground
                }}
              >
                {status.label}
              </span>
            )
          })}
        </div>
        <span className="toolbar-note">{t("scoreboard.flagsHint")}</span>
      </div>

      <div className="scoreboard-reveal">
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
