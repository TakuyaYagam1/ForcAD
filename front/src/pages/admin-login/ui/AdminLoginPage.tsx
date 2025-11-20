// src/pages/admin-login/ui/AdminLoginPage.tsx
import { AppShell } from "@/shared/ui/layout/AppShell";
import { AdminLoginForm } from "@/features/auth-admin/ui/AdminLoginForm";

export function AdminLoginPage() {
  return (
    <AppShell>
      <div className="flex min-h-[60vh] items-center justify-center">
        <AdminLoginForm />
      </div>
    </AppShell>
  );
}
