import { useTranslation } from "react-i18next";
import type { AdminAuthError } from "../model/useAdminAuth";
import { apiErrorMessage } from "@/shared/lib/apiError";

interface AdminAuthErrorMessageProps {
  error: AdminAuthError;
}

export function AdminAuthErrorMessage({
  error,
}: AdminAuthErrorMessageProps) {
  const { t } = useTranslation("admin");

  switch (error.kind) {
    case "invalidCredentials":
      return <>{t("login.invalidCredentials")}</>;
    case "loginFailed":
      return <>{apiErrorMessage(error.cause, t("errors.loginFailed"))}</>;
    case "logoutFailed":
      return <>{apiErrorMessage(error.cause, t("errors.logoutFailed"))}</>;
    case "sessionCheckFailed":
      return (
        <>{apiErrorMessage(error.cause, t("errors.sessionCheckFailed"))}</>
      );
  }
}
