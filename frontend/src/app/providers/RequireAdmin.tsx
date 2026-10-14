import { Navigate, useLocation } from "react-router-dom";
import { type ReactNode, useEffect } from "react";
import { useAdminAuthStore } from "@/features/auth-admin/model/useAdminAuth";
import { AdminAuthErrorMessage } from "@/features/auth-admin/ui/AdminAuthErrorMessage";
import { useTranslation } from "react-i18next";

interface Props {
  children: ReactNode;
}

export function RequireAdmin({ children }: Props) {
  const { t } = useTranslation("admin");
  const location = useLocation();
  const error = useAdminAuthStore((s) => s.error);
  const isAuthenticated = useAdminAuthStore((s) => s.isAuthenticated);
  const isAuthChecked = useAdminAuthStore((s) => s.isAuthChecked);
  const checkSession = useAdminAuthStore((s) => s.checkSession);

  useEffect(() => {
    if (!isAuthChecked) {
      void checkSession();
    }
  }, [isAuthChecked, checkSession]);

  if (!isAuthChecked) {
    return (
      <div className="flex min-h-screen items-center justify-center bg-slate-950 text-slate-200">
        {t("common.checkingSession")}
      </div>
    );
  }

  if (!isAuthenticated && error)
    return (
      <div className="notice" role="alert">
        <AdminAuthErrorMessage error={error} />
        <button type="button" onClick={() => void checkSession()}>
          {t("common.retry")}
        </button>
      </div>
    );

  if (!isAuthenticated) {
    return (
      <Navigate to="/admin/login" replace state={{ from: location.pathname }} />
    );
  }

  return <>{children}</>;
}
