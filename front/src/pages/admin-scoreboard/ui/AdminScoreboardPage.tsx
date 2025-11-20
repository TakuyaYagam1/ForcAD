import { AppShell } from "@/shared/ui/layout/AppShell";
import { ScoreboardWidget } from "@/widgets/scoreboard/ui/ScoreboardWidget";
import { Button } from "@/components/ui/button";
import { useAdminAuthStore } from "@/features/auth-admin/model/useAdminAuth";
import { useNavigate } from "react-router-dom";

export function AdminScoreboardPage() {
  const navigate = useNavigate();
  const logout = useAdminAuthStore((s) => s.logout);

  const handleLogout = async () => {
    await logout();
    navigate("/admin/login", { replace: true });
  };

  const handleTeamClick = (teamId: number) => {
    navigate(`/admin/team/${teamId}`);
  };

  const handleTaskClick = (taskId: number) => {
    navigate(`/admin/task/${taskId}`);
  };

  // onCellClick можно потом использовать для более продвинутых действий
  const handleCellClick = (teamId: number, taskId: number) => {
    navigate(`/admin/teamtask_log/team/${teamId}/task/${taskId}`);
  };

  const handleCreateTeam = () => {
    navigate("/admin/team/create");
  };

  const handleCreateTask = () => {
    navigate("/admin/task/create");
  };

  return (
    <AppShell>
      <div className="mb-4 flex flex-wrap items-center justify-between gap-3">
        <div>
          <h1 className="text-base font-semibold text-slate-100">
            Admin scoreboard
          </h1>
          <p className="text-xs text-slate-400">
            Клик по команде/таску — редактирование. Клик по ячейке — лог чекера.
          </p>
        </div>

        <div className="flex items-center gap-2">
          <Button
            variant="outline"
            size="sm"
            className="border-slate-700 text-slate-300"
            onClick={handleCreateTeam}
          >
            Create team
          </Button>
          <Button
            variant="outline"
            size="sm"
            className="border-slate-700 text-slate-300"
            onClick={handleCreateTask}
          >
            Create task
          </Button>
          <Button
            variant="outline"
            size="sm"
            className="border-slate-700 text-slate-300"
            onClick={handleLogout}
          >
            Logout
          </Button>
        </div>
      </div>

      <ScoreboardWidget
        onTeamClick={handleTeamClick}
        onTaskClick={handleTaskClick}
        onCellClick={handleCellClick}
      />
    </AppShell>
  );
}
