import { isAxiosError } from "axios";
export function apiErrorMessage(error: unknown, fallback: string): string {
  if (isAxiosError(error)) {
    const data: unknown = error.response?.data;
    if (
      data &&
      typeof data === "object" &&
      "error" in data &&
      typeof data.error === "string"
    )
      return data.error;
    if (!error.response)
      return "Не удалось связаться с сервером. Проверь подключение и повтори попытку.";
    if (error.response.status === 401 || error.response.status === 403)
      return "Сессия администратора завершена. Войди снова.";
    if (error.response.status === 404) return "Запись не найдена.";
  }
  return fallback;
}
