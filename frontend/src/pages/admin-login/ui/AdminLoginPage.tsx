// src/pages/admin-login/ui/AdminLoginPage.tsx
import { AppShell } from "@/shared/ui/layout/AppShell";
import { AdminLoginForm } from "@/features/auth-admin/ui/AdminLoginForm";
import { brand } from "@/app/config/brand";
import { useTranslation } from "react-i18next";

export function AdminLoginPage() {
  const { t } = useTranslation("admin");
  return (
    <AppShell>
      <div className="login-layout">
        <div className="login-intro">
          <span className="eyebrow">{brand.name} / {t("login.brand")}</span>
          <h1>
            {t("login.heroTitleFirst")}
            <br />
            {t("login.heroTitleSecond")}
          </h1>
          <p>{t("login.heroDescription")}</p>
          <div className="login-art" aria-hidden="true" />
        </div>
        <AdminLoginForm />
      </div>
    </AppShell>
  );
}
