import { useScoreboardStore } from "@/entities/scoreboard/model/store";
import { StatusesBar } from "@/features/view-scoreboard/ui/StatusesBar";
import {
  ScoreboardTable,
  type ScoreboardTeam,
  type ScoreboardTask,
  type ScoreboardTeamTask,
} from "@/features/view-scoreboard/ui/ScoreboardTable";
import { fetchScoreboardConfig } from "@/entities/scoreboard/api/config";
import { useEffect } from "react";

interface ScoreboardWidgetProps {
  onTeamClick?: (teamId: number) => void;
  onTaskClick?: (taskId: number) => void;
  onCellClick?: (teamId: number, taskId: number) => void;
}

export function ScoreboardWidget({
  onTeamClick,
  onTaskClick,
  onCellClick,
}: ScoreboardWidgetProps) {
  const teams = useScoreboardStore((s) => s.teams) ?? [];
  const tasks = useScoreboardStore((s) => s.tasks) ?? [];
  const teamTasks = useScoreboardStore((s) => s.teamTasks) ?? [];
  const round = useScoreboardStore((s) => s.round);
  const roundStart = useScoreboardStore((s) => s.roundStart);
  const error = useScoreboardStore((s) => s.error);

  const roundTime = useScoreboardStore((s) => s.roundTime);
  const setRoundTime = useScoreboardStore((s) => s.setRoundTime);

  useEffect(() => {
    if (roundTime !== null) return;

    let cancelled = false;

    fetchScoreboardConfig()
      .then((data) => {
        if (!cancelled) {
          setRoundTime(data.round_time);
        }
      })
      .catch((err) => {
        console.error("fetchScoreboardConfig error", err);
      });

    return () => {
      cancelled = true;
    };
  }, [roundTime, setRoundTime]);

  return (
    <div className="space-y-4">
      {error && (
        <div className="rounded-xl border border-red-500/40 bg-red-500/10 px-3 py-2 text-sm text-red-200">
          {error}
        </div>
      )}

      <StatusesBar round={round} roundStart={roundStart} />

      <ScoreboardTable
        teams={teams as ScoreboardTeam[]}
        tasks={tasks as ScoreboardTask[]}
        teamTasks={teamTasks as ScoreboardTeamTask[]}
        onTeamClick={onTeamClick}
        onTaskClick={onTaskClick}
        onCellClick={onCellClick}
      />
    </div>
  );
}
