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
    !/^[0-9a-f]{16}$/.test(team.token)
  )
    return "Токен: ровно 16 символов 0-9 и a-f в нижнем регистре.";
  if ((team.logo_path ?? "").length > 255)
    return "Ссылка на логотип: максимум 255 символов.";
  return null;
}
export function validateTask(task: Omit<Task, "id">): string | null {
  for (const field of ["name", "checker"] as const) {
    const limit =
      field === "checker" ? 1024 : 255;
    if (!task[field].trim() || task[field].trim().length > limit)
      return "Название и путь чекера должны быть заполнены (максимум 255 и 1024 символа).";
  }
  if (typeof task.checker_type !== "string" || task.checker_type.trim().length > 32)
    return "Тип чекера: строка до 32 символов; может быть пустой.";
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
