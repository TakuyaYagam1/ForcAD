// src/pages/admin-login/ui/AdminLoginPage.tsx
import { AppShell } from "@/shared/ui/layout/AppShell";
import { AdminLoginForm } from "@/features/auth-admin/ui/AdminLoginForm";
import { GearMechanism } from "@/shared/ui/brand/GearMechanism";

export function AdminLoginPage() {
  return (
    <AppShell>
      <div className="login-layout">
        <div className="login-intro">
          <span className="eyebrow">Кубок Федерации · 2026</span>
          <h1>
            За кадром
            <br />
            соревнования
          </h1>
          <p>
            Управление командами, сервисами и проверками. Всё, что помогает
            соревнованию идти своим ходом.
          </p>
          <div className="login-art" aria-hidden="true">
            <GearMechanism />
          </div>
        </div>
        <AdminLoginForm />
      </div>
    </AppShell>
  );
}
