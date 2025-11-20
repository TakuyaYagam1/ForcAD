// src/app/App.tsx
import { Routes, Route, Navigate } from "react-router-dom";

import { ScoreboardPage } from "@/pages/scoreboard/ui/ScoreboardPage";
import { LiveScoreboardPage } from "@/pages/live-scoreboard/ui/LiveScoreboardPage";
import { TeamScoreboardPage } from "@/pages/team-scoreboard/ui/TeamScoreboardPage";

import { AdminLoginPage } from "@/pages/admin-login/ui/AdminLoginPage";
import { AdminScoreboardPage } from "@/pages/admin-scoreboard/ui/AdminScoreboardPage";
import { RequireAdmin } from "./providers/RequireAdmin";
import { TeamAdminPage } from "@/pages/admin-team/ui/TeamAdminPage";
import { TaskAdminPage } from "@/pages/admin-task/ui/TaskAdminPage";
import { AdminTeamTaskLogPage } from "@/pages/admin-team-task-log/ui/AdminTeamTaskLogPage";
// позже добавим TeamAdminPage, TaskAdminPage, AdminTeamTaskLogPage

export default function App() {
  return (
    <Routes>
      {/* Публичные страницы */}
      <Route path="/" element={<ScoreboardPage />} />
      <Route path="/live" element={<LiveScoreboardPage />} />
      <Route path="/team/:teamId" element={<TeamScoreboardPage />} />

      {/* Логин админа */}
      <Route path="/admin/login" element={<AdminLoginPage />} />

      {/* Админка под guard'ом */}
      <Route
        path="/admin/scoreboard"
        element={
          <RequireAdmin>
            <AdminScoreboardPage />
          </RequireAdmin>
        }
      />

      <Route
        path="/admin/task/:taskId"
        element={
          <RequireAdmin>
            <TaskAdminPage />
          </RequireAdmin>
        }
      />

      <Route
        path="/admin/team/create"
        element={
          <RequireAdmin>
            <TeamAdminPage mode="create" />
          </RequireAdmin>
        }
      />
      <Route
        path="/admin/team/:teamId"
        element={
          <RequireAdmin>
            <TeamAdminPage mode="edit" />
          </RequireAdmin>
        }
      />

      <Route
        path="/admin/teamtask_log/team/:teamId/task/:taskId"
        element={
          <RequireAdmin>
            <AdminTeamTaskLogPage />
          </RequireAdmin>
        }
      />

      {/* можно позже добавить: TeamAdminPage, TaskAdminPage, AdminTeamTaskLogPage под RequireAdmin */}

      {/* редиректы */}
      <Route
        path="/admin"
        element={<Navigate to="/admin/scoreboard" replace />}
      />
      <Route path="*" element={<Navigate to="/" replace />} />
    </Routes>
  );
}
