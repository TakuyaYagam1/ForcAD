import { Component, lazy, Suspense, type ReactNode } from "react";
import { Routes, Route, Navigate, useLocation } from "react-router-dom";
import { RequireAdmin } from "./providers/RequireAdmin";
import { useTranslation } from "react-i18next";

const ScoreboardPage = lazy(() => {
  return import("@/pages/scoreboard/ui/ScoreboardPage")
    .then(({ ScoreboardPage }) => {
      return {
        default: ScoreboardPage
      }
    })
});
const LiveScoreboardPage = lazy(() => {
  return import("@/pages/live-scoreboard/ui/LiveScoreboardPage")
    .then(({ LiveScoreboardPage }) => {
      return {
        default: LiveScoreboardPage
      }
    })
});
const TeamScoreboardPage = lazy(() => {
  return import("@/pages/team-scoreboard/ui/TeamScoreboardPage")
    .then(({ TeamScoreboardPage }) => {
      return {
        default: TeamScoreboardPage
      }
    })
});
const TeamsPage = lazy(() => {
  return import("@/pages/teams/ui/TeamsPage")
    .then(({ TeamsPage }) => {
      return {
        default: TeamsPage
      }
    })
});
const AdminLoginPage = lazy(() => {
  return import("@/pages/admin-login/ui/AdminLoginPage")
    .then(({ AdminLoginPage }) => {
      return {
        default: AdminLoginPage
      }
    })
});
const AdminScoreboardPage = lazy(() => {
  return import("@/pages/admin-scoreboard/ui/AdminScoreboardPage")
    .then(({ AdminScoreboardPage }) => {
      return {
        default: AdminScoreboardPage
      }
    })
});
const TeamAdminPage = lazy(() => {
  return import("@/pages/admin-team/ui/TeamAdminPage")
    .then(({ TeamAdminPage }) => {
      return {
        default: TeamAdminPage
      }
    })
});
const TaskAdminPage = lazy(() => {
  return import("@/pages/admin-task/ui/TaskAdminPage")
    .then(({ TaskAdminPage }) => {
      return {
        default: TaskAdminPage
      }
    })
});
const AdminTeamTaskLogPage = lazy(() => {
  return import("@/pages/admin-team-task-log/ui/AdminTeamTaskLogPage")
    .then(({ AdminTeamTaskLogPage }) => {
      return {
        default: AdminTeamTaskLogPage
      }
    })
});

function RouteLoading() {
  const { t } = useTranslation("shell");
  return (
    <main id="main-content" className="site-main">
      <div className="empty-state" role="status" aria-live="polite" aria-busy="true">
        {t("loading")}
      </div>
    </main>
  );
}

function RouteErrorFallback() {
  const { t } = useTranslation("shell");
  return (
    <main id="main-content" className="site-main">
      <section className="empty-state" role="alert">
        <h2>{t("loadFailed")}</h2>
        <p>{t("loadHelp")}</p>
        <button
          className="rounded-md border border-white/15 px-4 py-2 text-sm text-slate-100 hover:bg-white/5 focus-visible:outline focus-visible:outline-2 focus-visible:outline-offset-2"
          type="button"
          onClick={() => {
            return window.location.reload()
          }}
        >
          {t("reload")}
        </button>
      </section>
    </main>
  );
}

class RouteErrorBoundary extends Component<
  {
    children: ReactNode
  },
  {
    hasError: boolean
  }
> {
  state = {
    hasError: false
  };

  static getDerivedStateFromError() {
    return {
      hasError: true
    };
  }

  render() {
    if (this.state.hasError) {
      return <RouteErrorFallback />;
    }
    return this.props.children;
  }
}

export default function App() {
  const location = useLocation();

  return (
    <RouteErrorBoundary key={location.key}>
      <Suspense fallback={<RouteLoading />}>
        <Routes>
          <Route path="/" element={<ScoreboardPage />} />
          <Route path="/live" element={<LiveScoreboardPage />} />
          <Route path="/teams" element={<TeamsPage />} />
          <Route path="/team/:teamId" element={<TeamScoreboardPage />} />

          <Route path="/admin/login" element={<AdminLoginPage />} />

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

          <Route path="/admin" element={<Navigate to="/admin/scoreboard" replace />} />
          <Route path="/admin/create_task" element={<Navigate to="/admin/task/create" replace />} />
          <Route path="/admin/create_team" element={<Navigate to="/admin/team/create" replace />} />
          <Route path="*" element={<Navigate to="/" replace />} />
        </Routes>
      </Suspense>
    </RouteErrorBoundary>
  );
}
