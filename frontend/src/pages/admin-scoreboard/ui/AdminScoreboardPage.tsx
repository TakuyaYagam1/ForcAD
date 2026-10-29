import { Button } from "@/components/ui/button";
import { useScoreboardStore } from "@/entities/scoreboard/model/store";
import { StatusesBar } from "@/features/view-scoreboard/ui/StatusesBar";
import { CheckerStatusPanel } from "@/features/view-checker-status/ui/CheckerStatusPanel";
import { GameControls } from "@/features/manage-game/ui/GameControls";
import { AdminShell } from "@/widgets/admin-shell/ui/AdminShell";
import { ScoreboardWidget } from "@/widgets/scoreboard/ui/ScoreboardWidget";
import { Plus } from "lucide-react";
import { useNavigate } from "react-router-dom";
import { useTranslation } from "react-i18next";
import "./AdminScoreboardPage.css";

export function AdminScoreboardPage() {
  const { t } = useTranslation("admin");
  const navigate = useNavigate();
  const round = useScoreboardStore((state) => state.round);
  const roundStart = useScoreboardStore((state) => state.roundStart);
  return (
    <AdminShell>
      <div className="admin-page admin-page--scoreboard">
        <h1 className="sr-only">{t("scoreboard.title")}</h1>
        <StatusesBar round={round} roundStart={roundStart} />
        <div className="admin-dashboard-strip">
          <CheckerStatusPanel />
          <div className="admin-dashboard-actions">
            <div className="admin-actions">
              <Button
                variant="outline"
                onClick={() => navigate("/admin/team/create")}
              >
                <Plus />
                {t("scoreboard.addTeam")}
              </Button>
              <Button
                variant="outline"
                onClick={() => navigate("/admin/task/create")}
              >
                <Plus />
                {t("scoreboard.addService")}
              </Button>
            </div>
            <GameControls />
          </div>
        </div>
        <ScoreboardWidget
          admin
          onTeamClick={(teamId) => navigate(`/admin/team/${teamId}`)}
          onTaskClick={(taskId) => navigate(`/admin/task/${taskId}`)}
          onCellClick={(teamId, taskId) =>
            navigate(`/admin/teamtask_log/team/${teamId}/task/${taskId}`)
          }
        />
      </div>
    </AdminShell>
  );
}
