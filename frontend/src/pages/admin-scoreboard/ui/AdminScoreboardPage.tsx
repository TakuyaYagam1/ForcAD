import { Button } from "@/components/ui/button";
import { AdminEntityLists } from "@/features/manage-entities/ui/AdminEntityLists";
import { GameControls } from "@/features/manage-game/ui/GameControls";
import { AdminShell } from "@/widgets/admin-shell/ui/AdminShell";
import { ScoreboardWidget } from "@/widgets/scoreboard/ui/ScoreboardWidget";
import { Plus } from "lucide-react";
import { useNavigate } from "react-router-dom";
import { useTranslation } from "react-i18next";

export function AdminScoreboardPage() {
  const { t } = useTranslation("admin");
  const navigate = useNavigate();
  return (
    <AdminShell>
      <div className="admin-page">
        <div className="admin-heading">
          <div>
            <p className="eyebrow">{t("common.organizerPanel")}</p>
            <h1>{t("scoreboard.title")}</h1>
            <p>{t("scoreboard.description")}</p>
          </div>
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
        </div>
        <GameControls />
        <AdminEntityLists />
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
