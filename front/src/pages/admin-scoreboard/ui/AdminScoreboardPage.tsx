import { AdminEntityLists } from "@/features/manage-entities/ui/AdminEntityLists";
import { AppShell } from "@/shared/ui/layout/AppShell";
import { ScoreboardWidget } from "@/widgets/scoreboard/ui/ScoreboardWidget";
import { Button } from "@/components/ui/button";
import { useAdminAuthStore } from "@/features/auth-admin/model/useAdminAuth";
import { useNavigate } from "react-router-dom";
import { Plus, LogOut } from "lucide-react";
import { BrandIcon } from "@/shared/ui/brand/BrandIcon";

export function AdminScoreboardPage() {
  const navigate = useNavigate();
  const logout = useAdminAuthStore((state) => state.logout);
  const error = useAdminAuthStore((state) => state.error);
  const handleLogout = async () => {
    try {
      await logout();
      navigate("/admin/login", { replace: true });
    } catch {
      /* Store displays the server error; keep the authenticated session. */
    }
  };
  return (
    <AppShell>
      <div className="admin-page">
        <div className="admin-heading">
          <div>
            <p className="eyebrow">Панель организатора</p>
            <h1>Управление соревнованием</h1>
            <p>
              Выбери команду или сервис для редактирования, ячейку — для
              просмотра лога.
            </p>
          </div>
          <div className="admin-actions">
            <Button
              variant="outline"
              size="sm"
              onClick={() => navigate("/admin/team/create")}
            >
              <Plus />
              Команда
            </Button>
            <Button
              variant="outline"
              size="sm"
              onClick={() => navigate("/admin/task/create")}
            >
              <Plus />
              Сервис
            </Button>
            <Button variant="ghost" size="sm" onClick={handleLogout}>
              <LogOut />
              Выйти
            </Button>
            <BrandIcon name="check" />
          </div>
        </div>
        {error && (
          <div className="notice" role="alert">
            {error}
          </div>
        )}
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
    </AppShell>
  );
}
