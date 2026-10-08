// src/app/App.tsx
import { Routes, Route, Navigate } from "react-router-dom";

import { ScoreboardPage } from "@/pages/scoreboard/ui/ScoreboardPage";
import { LiveScoreboardPage } from "@/pages/live-scoreboard/ui/LiveScoreboardPage";
import { TeamScoreboardPage } from "@/pages/team-scoreboard/ui/TeamScoreboardPage";
import { TeamsPage } from "@/pages/teams/ui/TeamsPage";

import { AdminLoginPage } from "@/pages/admin-login/ui/AdminLoginPage";
import { AdminScoreboardPage } from "@/pages/admin-scoreboard/ui/AdminScoreboardPage";
import { RequireAdmin } from "./providers/RequireAdmin";
import { TeamAdminPage } from "@/pages/admin-team/ui/TeamAdminPage";
import { TaskAdminPage } from "@/pages/admin-task/ui/TaskAdminPage";
import { AdminTeamTaskLogPage } from "@/pages/admin-team-task-log/ui/AdminTeamTaskLogPage";
// Team, service, and checker log administration pages

export default function App() {
  return (
    <Routes>
      {/* Public pages */}
      <Route path="/" element={<ScoreboardPage />} />
      <Route path="/live" element={<LiveScoreboardPage />} />
      <Route path="/teams" element={<TeamsPage />} />
      <Route path="/team/:teamId" element={<TeamScoreboardPage />} />

      {/* Admin sign-in */}
      <Route path="/admin/login" element={<AdminLoginPage />} />

      {/* Protected admin routes */}
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
        path="/admin/teamtask_log/team/:teamId/task/:taskId"
        element={
          <RequireAdmin>
            <AdminTeamTaskLogPage />
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
        path="/admin/task/create"
        element={
          <RequireAdmin>
            <TaskAdminPage mode="create" />
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
        path="/admin/task/:taskId"
        element={
          <RequireAdmin>
            <TaskAdminPage mode="edit" />
          </RequireAdmin>
        }
      />

      {/* Admin pages require an authenticated session. */}

      {/* Redirects */}
      <Route
        path="/admin"
        element={<Navigate to="/admin/scoreboard" replace />}
      />
      <Route
        path="/admin/create_task"
        element={<Navigate to="/admin/task/create" replace />}
      />
      <Route
        path="/admin/create_team"
        element={<Navigate to="/admin/team/create" replace />}
      />
      <Route path="*" element={<Navigate to="/" replace />} />
    </Routes>
  );
}
