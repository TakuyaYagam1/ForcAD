import { Button } from "@/components/ui/button";
import { useAdminAuthStore } from "@/features/auth-admin/model/useAdminAuth";
import { AdminAuthErrorMessage } from "@/features/auth-admin/ui/AdminAuthErrorMessage";
import { LogOut, LoaderCircle } from "lucide-react";
import { useState, type ReactNode } from "react";
import { useNavigate } from "react-router-dom";
import { useTranslation } from "react-i18next";
import "./AdminShell.css";
import "./AdminEditor.css";

interface AdminShellProps {
  children: ReactNode;
}

export function AdminHeaderActions() {
  const { t } = useTranslation("admin");
  const navigate = useNavigate();
  const logout = useAdminAuthStore((state) => {
    return state.logout
  });
  const [logoutPending, setLogoutPending] = useState(false);

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
  );
}

export function AdminShell({ children }: AdminShellProps) {
  const authError = useAdminAuthStore((state) => {
    return state.error;
  });
  const logoutError = authError?.kind === "logoutFailed" ? authError : null;
  return (
    <>
      {logoutError && (
        <div className="admin-shell__logout-error notice" role="alert">
          <AdminAuthErrorMessage error={logoutError} />
        </div>
      )}
      {children}
    </>
  );
}
