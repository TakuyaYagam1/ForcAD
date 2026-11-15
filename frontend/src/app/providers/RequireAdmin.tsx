import { Navigate, useLocation } from "react-router-dom";
import { type ReactNode, useEffect } from "react";
import { useAdminAuthStore } from "@/features/auth-admin/model/useAdminAuth";
import { AdminAuthErrorMessage } from "@/features/auth-admin/ui/AdminAuthErrorMessage";
import { useTranslation } from "react-i18next";
import { Button } from "@/components/ui/button";

interface Props {
  children: ReactNode;
}

export function RequireAdmin({ children }: Props) {
  const { t } = useTranslation("admin");
  const location = useLocation();
  const error = useAdminAuthStore((s) => {
    return s.error
  });
  const isAuthenticated = useAdminAuthStore((s) => {
    return s.isAuthenticated
  });
  const isAuthChecked = useAdminAuthStore((s) => {
    return s.isAuthChecked
  });
  const checkSession = useAdminAuthStore((s) => {
    return s.checkSession
  });

  useEffect(() => {
    if (!isAuthChecked) {
      void checkSession();
    }
  }, [isAuthChecked, checkSession]);

  if (!isAuthChecked) {
    return (
      <>
        <div className="empty-state" role="status" aria-busy="true">
          {t("common.checkingSession")}
        </div>
      </>
    );
  }

  if (!isAuthenticated && error) {
    return (
      <>
        <div className="notice" role="alert">
          <AdminAuthErrorMessage error={error} />
          <Button variant="outline" type="button" onClick={() => {
            return void checkSession()
          }}>
            {t("common.retry")}
          </Button>
        </div>
      </>
    );
  }

  if (!isAuthenticated) {
    return (
      <Navigate to="/admin/login" replace state={{
        from: location.pathname
      }} />
    );
  }

  return <>{children}</>;
}
