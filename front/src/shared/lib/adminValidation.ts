import { z } from "zod";
import type { Team } from "@/entities/team/model/types";
import type { Task } from "@/entities/task/model/types";

export function validateTeam(
  team: Omit<Team, "id">,
  creating = false,
): string | null {
  if (!team.name.trim() || team.name.trim().length > 255)
    return "Название команды: от 1 до 255 символов.";
  if (!z.union([z.ipv4(), z.ipv6()]).safeParse(team.ip.trim()).success)
    return "Укажите корректный IPv4 или IPv6 адрес.";
  if (
    !creating &&
    (!team.token ||
      team.token.length > 16 ||
      !/^[A-Za-z0-9_-]+$/.test(team.token))
  )
    return "Токен: от 1 до 16 латинских букв, цифр, символов _ или -.";
  if ((team.logo_path ?? "").length > 255)
    return "Ссылка на логотип: максимум 255 символов.";
  return null;
}
export function validateTask(task: Omit<Task, "id">): string | null {
  for (const field of ["name", "checker", "checker_type"] as const) {
    const limit =
      field === "checker" ? 1024 : field === "checker_type" ? 32 : 255;
    if (!task[field].trim() || task[field].trim().length > limit)
      return "Название, путь и тип чекера должны быть заполнены (до 255 символов).";
  }
  if ((task.env_path ?? "").length > 1024)
    return "Путь к окружению: максимум 1024 символов.";
  for (const field of [
    "gets",
    "puts",
    "places",
    "checker_timeout",
    "get_period",
    "default_score",
  ] as const) {
    const min = ["gets", "puts", "default_score"].includes(field) ? 0 : 1;
    if (
      !Number.isSafeInteger(task[field]) ||
      task[field] < min ||
      task[field] > 2147483647
    )
      return `${field}: целое число от ${min} до 2147483647.`;
  }
  return null;
}
