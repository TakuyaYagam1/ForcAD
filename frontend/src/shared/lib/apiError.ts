import { isAxiosError } from "axios";
import { i18n } from "../i18n/index.ts";

export function apiErrorMessage(error: unknown, fallback: string): string {
  if (isAxiosError(error)) {
    if (!error.response)
      return i18n.t("errors.connectionFailed", { ns: "admin" });

    const status = error.response.status;
    if (status === 401 || status === 403)
      return i18n.t("errors.sessionExpired", { ns: "admin" });
    if (status === 404)
      return i18n.t("errors.recordNotFound", { ns: "admin" });

    const data: unknown = error.response.data;
    if (
      data &&
      typeof data === "object" &&
      "error" in data &&
      typeof data.error === "string"
    )
      return data.error;
  }
  return fallback;
}
