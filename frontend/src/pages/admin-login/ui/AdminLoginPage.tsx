// src/pages/admin-login/ui/AdminLoginPage.tsx
import { AppShell } from "@/shared/ui/layout/AppShell";
import { AdminLoginForm } from "@/features/auth-admin/ui/AdminLoginForm";
import { brand } from "@/app/config/brand";

export function AdminLoginPage() {
  return (
    <AppShell>
      <div className="login-layout">
        <div className="login-intro">
          <span className="eyebrow">{brand.name} / управление</span>
          <h1>
            За кадром
            <br />
            соревнования
          </h1>
          <p>
            Управление командами, сервисами и игровыми настройками.
          </p>
          <div className="login-art" aria-hidden="true" />
        </div>
        <AdminLoginForm />
      </div>
    </AppShell>
  );
}
