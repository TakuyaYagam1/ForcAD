import { Button } from "@/components/ui/button";
import { CheckerStatusPanel } from "@/features/view-checker-status/ui/CheckerStatusPanel";
import { GameControls } from "@/features/manage-game/ui/GameControls";
import { GameSettings } from "@/features/manage-game/ui/GameSettings";
import { AdminShell } from "@/widgets/admin-shell/ui/AdminShell";
import { ScoreboardWidget } from "@/widgets/scoreboard/ui/ScoreboardWidget";
import { Plus } from "lucide-react";
import { useNavigate } from "react-router-dom";
import { useTranslation } from "react-i18next";
import "./AdminScoreboardPage.css";

export function AdminScoreboardPage() {
  const { t } = useTranslation("admin");
  const navigate = useNavigate();
  return (
    <AdminShell>
      <div className="admin-page admin-page--scoreboard">
        <div className="admin-dashboard-heading">
          <p className="eyebrow">{t("common.organizerPanel")}</p>
          <h1>{t("scoreboard.title")}</h1>
        </div>
        <div className="admin-dashboard-strip">
          <CheckerStatusPanel />
          <div className="admin-dashboard-actions">
            <div className="admin-actions">
              <Button
                variant="outline"
                onClick={() => {
                  return navigate("/admin/team/create")
                }}
              >
                <Plus />
                {t("scoreboard.addTeam")}
              </Button>
              <Button
                variant="outline"
                onClick={() => {
                  return navigate("/admin/task/create")
                }}
              >
                <Plus />
                {t("scoreboard.addService")}
              </Button>
            </div>
            <GameControls />
            <GameSettings />
          </div>
        </div>
        <ScoreboardWidget
          admin
          onTeamClick={(teamId) => {
            return navigate(`/admin/team/${teamId}`)
          }}
          onTaskClick={(taskId) => {
            return navigate(`/admin/task/${taskId}`)
          }}
          onCellClick={(teamId, taskId) => {
            return navigate(`/admin/teamtask_log/team/${teamId}/task/${taskId}`)
          }
          }
        />
      </div>
    </AdminShell>
  );
}
