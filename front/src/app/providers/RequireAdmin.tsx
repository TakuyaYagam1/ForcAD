// src/app/providers/RequireAdmin.tsx
import type { ReactNode } from "react";
import { useEffect } from "react";
import { Navigate, useLocation } from "react-router-dom";
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
    // можно сделать красивый skeleton
    return (
      <div className="flex min-h-[60vh] items-center justify-center text-sm text-slate-400">
        Checking admin session…
      </div>
    );
  }

  if (!isAuthenticated) {
    return <Navigate to="/admin/login" replace state={{ from: location }} />;
  }

  return <>{children}</>;
}
