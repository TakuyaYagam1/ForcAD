import { Navigate, useLocation } from "react-router-dom";
import { type ReactNode, useEffect } from "react";
import { useAdminAuthStore } from "@/features/auth-admin/model/useAdminAuth";

interface Props {
  children: ReactNode;
}

export function RequireAdmin({ children }: Props) {
  const location = useLocation();
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
        Проверяем админ-сессию…
      </div>
    );
  }

  if (!isAuthenticated) {
    return (
      <Navigate to="/admin/login" replace state={{ from: location.pathname }} />
    );
  }

  return <>{children}</>;
}
