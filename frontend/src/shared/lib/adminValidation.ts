import { z } from "zod";
import type { Team } from "@/entities/team/model/types";
import type { Task } from "@/entities/task/model/types";

export const MAX_TASK_INTEGER_VALUE = 2_147_483_647;
export const MAX_TEAM_NAME_LENGTH = 255;
export const MAX_TASK_NAME_LENGTH = 255;
export const MAX_CHECKER_PATH_LENGTH = 1024;

const TASK_TEXT_FIELDS = [
  { field: "name", maxLength: MAX_TASK_NAME_LENGTH },
  { field: "checker", maxLength: MAX_CHECKER_PATH_LENGTH },
] as const;

const TASK_INTEGER_FIELDS = [
  { field: "gets", min: 0 },
  { field: "puts", min: 0 },
  { field: "places", min: 1 },
  { field: "checker_timeout", min: 1 },
  { field: "get_period", min: 1 },
  { field: "default_score", min: 0 },
] as const;

export function validateTeam(
  team: Omit<Team, "id">,
  creating = false,
): string | null {
  const name = team.name.trim();
  if (!name || name.length > MAX_TEAM_NAME_LENGTH)
    return "Название команды: от 1 до 255 символов.";
  if (!z.union([z.ipv4(), z.ipv6()]).safeParse(team.ip.trim()).success)
    return "Укажите корректный IPv4 или IPv6 адрес.";
  if (
    !creating &&
    !/^[0-9a-f]{16}$/.test(team.token)
  )
    return "Токен: ровно 16 символов 0-9 и a-f в нижнем регистре.";
  if ((team.logo_path ?? "").length > 255)
    return "Ссылка на логотип: максимум 255 символов.";
  return null;
}
export function validateTask(task: Omit<Task, "id">): string | null {
  for (const { field, maxLength } of TASK_TEXT_FIELDS) {
    const value = task[field].trim();
    if (!value || value.length > maxLength)
      return "Название и путь чекера должны быть заполнены (максимум 255 и 1024 символа).";
  }
  if (typeof task.checker_type !== "string" || task.checker_type.trim().length > 32)
    return "Тип чекера: строка до 32 символов; может быть пустой.";
  if ((task.env_path ?? "").length > 1024)
    return "Путь к окружению: максимум 1024 символов.";
  for (const { field, min } of TASK_INTEGER_FIELDS) {
    const value = task[field];
    if (
      !Number.isSafeInteger(value) ||
      value < min ||
      value > MAX_TASK_INTEGER_VALUE
    )
      return `${field}: целое число от ${min} до ${MAX_TASK_INTEGER_VALUE}.`;
  }
  return null;
}
