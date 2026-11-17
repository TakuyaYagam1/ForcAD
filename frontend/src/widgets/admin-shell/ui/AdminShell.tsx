import { Button } from "@/components/ui/button";
import { useAdminAuthStore } from "@/features/auth-admin/model/useAdminAuth";
import { AdminAuthErrorMessage } from "@/features/auth-admin/ui/AdminAuthErrorMessage";
import { AppShell } from "@/shared/ui/layout/AppShell";
import { LogOut, LoaderCircle } from "lucide-react";
import { useState, type ReactNode } from "react";
import { Link, useLocation, useNavigate } from "react-router-dom";
import { useTranslation } from "react-i18next";
import "./AdminShell.css";

interface AdminShellProps {
  children: ReactNode;
  headerProgress?: ReactNode;
}

export function AdminShell({ children, headerProgress }: AdminShellProps) {
  const { t } = useTranslation("admin");
  const navigate = useNavigate();
  const location = useLocation();
  const teamsActive = location.pathname === "/admin/teams"
    || location.pathname.startsWith("/admin/team/");
  const gameActive = location.pathname === "/admin/scoreboard"
    || location.pathname.startsWith("/admin/teamtask_log/");
  const servicesActive = location.pathname === "/admin/services"
    || location.pathname.startsWith("/admin/task/");
  const logout = useAdminAuthStore((state) => {
    return state.logout
  });
  const authError = useAdminAuthStore((state) => {
    return state.error
  });
  const [logoutPending, setLogoutPending] = useState(false);
  const logoutError = authError?.kind === "logoutFailed" ? authError : null;

  const handleLogout = async () => {
    if (logoutPending) {
      return;
    }
    setLogoutPending(true);
    try {
      await logout();
      navigate("/admin/login", {
        replace: true
      });
    } catch {
      // The auth store keeps the session and records the error for display.
    } finally {
      setLogoutPending(false);
    }
  };

  return (
    <AppShell
      showNavigation={false}
      headerProgress={headerProgress}
      headerNavigation={(
        <nav className="main-nav" aria-label={t("navigation.label")}>
          <Link
            to="/admin/teams"
            className={teamsActive ? "active" : undefined}
            aria-current={teamsActive ? "page" : undefined}
          >
            {t("navigation.teams")}
          </Link>
          <Link
            to="/admin/scoreboard"
            className={`main-nav__scoreboard${gameActive ? " active" : ""}`}
            aria-current={gameActive ? "page" : undefined}
          >
            {t("navigation.game")}
          </Link>
          <Link
            to="/admin/services"
            className={servicesActive ? "active" : undefined}
            aria-current={servicesActive ? "page" : undefined}
          >
            {t("navigation.services")}
          </Link>
        </nav>
      )}
      headerActions={(
        <Button
          type="button"
          className="admin-shell__logout"
          variant="ghost"
          size="sm"
          disabled={logoutPending}
          onClick={() => {
            return void handleLogout()
          }}
        >
          {logoutPending
            ? <LoaderCircle className="animate-spin" aria-hidden="true" />
            : <LogOut aria-hidden="true" />}
          {logoutPending ? t("scoreboard.loggingOut") : t("scoreboard.logout")}
        </Button>
      )}
    >
      {logoutError && (
        <div className="admin-shell__logout-error notice" role="alert">
          <AdminAuthErrorMessage error={logoutError} />
        </div>
      )}
      {children}
    </AppShell>
  );
}
