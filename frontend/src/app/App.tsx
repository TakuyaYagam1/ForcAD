import { Component, lazy, Suspense, type ReactNode } from "react";
import { Routes, Route, Navigate, Link, useLocation } from "react-router-dom";
import { RequireAdmin } from "./providers/RequireAdmin";
import { useTranslation } from "react-i18next";
import { Button } from "@/components/ui/button";
import { AdminNotificationProvider } from "@/shared/ui/admin-notification/AdminNotification";

import { AppShell } from "@/shared/ui/layout/AppShell";
import { AdminHeaderActions } from "@/widgets/admin-shell/ui/AdminShell";
import { useAdminAuthStore } from "@/features/auth-admin/model/useAdminAuth";
import { ScoreboardPage } from "@/pages/scoreboard/ui/ScoreboardPage";
import { LiveScoreboardPage } from "@/pages/live-scoreboard/ui/LiveScoreboardPage";
import { TeamScoreboardPage } from "@/pages/team-scoreboard/ui/TeamScoreboardPage";
import { TeamsPage } from "@/pages/teams/ui/TeamsPage";

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
const AdminEntitiesPage = lazy(() => {
  return import("@/pages/admin-entities/ui/AdminEntitiesPage")
    .then(({ AdminEntitiesPage }) => {
      return {
        default: AdminEntitiesPage
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
    <div className="route-state">
      <div className="empty-state" role="status" aria-live="polite" aria-busy="true">
        {t("loading")}
      </div>
    </div>
  );
}

function RouteErrorFallback() {
  const { t } = useTranslation("shell");
  return (
    <div className="route-state">
      <section className="empty-state" role="alert">
        <h2>{t("loadFailed")}</h2>
        <p>{t("loadHelp")}</p>
        <Button
          variant="outline"
          type="button"
          onClick={() => {
            return window.location.reload()
          }}
        >
          {t("reload")}
        </Button>
      </section>
    </div>
  );
}

class RouteErrorBoundary extends Component<
  {
    children: ReactNode;
    resetKey: string
  },
  {
    hasError: boolean;
    resetKey: string
  }
> {
  state = {
    hasError: false,
    resetKey: this.props.resetKey
  };

  static getDerivedStateFromProps(props: {
    resetKey: string
  }, state: {
    resetKey: string
  }) {
    return props.resetKey === state.resetKey ? null : {
      hasError: false,
      resetKey: props.resetKey
    };
  }

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

function AppRoutes() {
  const location = useLocation();
  const { t } = useTranslation("admin");
  const isAdmin = location.pathname.startsWith("/admin");
  const isAuthenticated = useAdminAuthStore((state) => {
    return state.isAuthenticated;
  });
  const teamsActive = location.pathname.startsWith("/admin/team")
    && !location.pathname.startsWith("/admin/teamtask_log");
  const gameActive = location.pathname === "/admin/scoreboard"
    || location.pathname.startsWith("/admin/teamtask_log");
  const servicesActive = location.pathname === "/admin/services"
    || location.pathname.startsWith("/admin/task/");

  return (
    <AppShell
      showNavigation={!isAdmin}
      headerActions={isAdmin && isAuthenticated ? <AdminHeaderActions /> : undefined}
      headerNavigation={isAdmin && isAuthenticated ? (
        <nav className="main-nav" aria-label={t("navigation.label")}>
          <Link to="/admin/teams" className={teamsActive ? "active" : undefined} aria-current={teamsActive ? "page" : undefined}>
            {t("navigation.teams")}
          </Link>
          <Link to="/admin/scoreboard" className={gameActive ? "main-nav__scoreboard active" : "main-nav__scoreboard"} aria-current={gameActive ? "page" : undefined}>
            {t("navigation.game")}
          </Link>
          <Link to="/admin/services" className={servicesActive ? "active" : undefined} aria-current={servicesActive ? "page" : undefined}>
            {t("navigation.services")}
          </Link>
        </nav>
      ) : undefined}
    >
      <RouteErrorBoundary resetKey={location.pathname}>
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
              path="/admin/teams"
              element={
                <RequireAdmin>
                  <AdminEntitiesPage kind="teams" />
                </RequireAdmin>
              }
            />
            <Route
              path="/admin/services"
              element={
                <RequireAdmin>
                  <AdminEntitiesPage kind="services" />
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
    </AppShell>
  );
}

export default function App() {
  return (
    <AdminNotificationProvider>
      <AppRoutes />
    </AdminNotificationProvider>
  );
}
