import { z } from "zod";
import type { Team } from "@/entities/team/model/types";
import type { Task } from "@/entities/task/model/types";
import { getLocale, i18n } from "../i18n/index.ts";

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

type TaskIntegerField = (typeof TASK_INTEGER_FIELDS)[number]["field"];

export type TeamValidationIssue =
  | { kind: "nameLength" }
  | { kind: "ip" }
  | { kind: "token" }
  | { kind: "logoLength" };

export type TaskValidationIssue =
  | { kind: "nameAndChecker" }
  | { kind: "checkerType" }
  | { kind: "environmentPath" }
  | {
      kind: "integerRange";
      field: TaskIntegerField;
      min: number;
      max: typeof MAX_TASK_INTEGER_VALUE;
    };

export function getTeamValidationIssue(
  team: Omit<Team, "id">,
  creating = false,
): TeamValidationIssue | null {
  const name = team.name.trim();
  if (!name || name.length > MAX_TEAM_NAME_LENGTH)
    return { kind: "nameLength" };
  if (!z.union([z.ipv4(), z.ipv6()]).safeParse(team.ip.trim()).success)
    return { kind: "ip" };
  if (!creating && !/^[0-9a-f]{16}$/.test(team.token))
    return { kind: "token" };
  if ((team.logo_path ?? "").length > 255) return { kind: "logoLength" };
  return null;
}

export function getTaskValidationIssue(
  task: Omit<Task, "id">,
): TaskValidationIssue | null {
  for (const { field, maxLength } of TASK_TEXT_FIELDS) {
    const value = task[field].trim();
    if (!value || value.length > maxLength) return { kind: "nameAndChecker" };
  }
  if (
    typeof task.checker_type !== "string" ||
    task.checker_type.trim().length > 32
  )
    return { kind: "checkerType" };
  if ((task.env_path ?? "").length > 1024)
    return { kind: "environmentPath" };
  for (const { field, min } of TASK_INTEGER_FIELDS) {
    const value = task[field];
    if (
      !Number.isSafeInteger(value) ||
      value < min ||
      value > MAX_TASK_INTEGER_VALUE
    )
      return {
        kind: "integerRange",
        field,
        min,
        max: MAX_TASK_INTEGER_VALUE,
      };
  }
  return null;
}

export function formatTeamValidationIssue(issue: TeamValidationIssue): string {
  switch (issue.kind) {
    case "nameLength":
      return i18n.t("validation.team.nameLength", { ns: "admin" });
    case "ip":
      return i18n.t("validation.team.ip", { ns: "admin" });
    case "token":
      return i18n.t("validation.team.token", { ns: "admin" });
    case "logoLength":
      return i18n.t("validation.team.logoLength", { ns: "admin" });
  }
}

export function formatTaskValidationIssue(issue: TaskValidationIssue): string {
  switch (issue.kind) {
    case "nameAndChecker":
      return i18n.t("validation.task.nameAndChecker", { ns: "admin" });
    case "checkerType":
      return i18n.t("validation.task.checkerType", { ns: "admin" });
    case "environmentPath":
      return i18n.t("validation.task.environmentPath", { ns: "admin" });
    case "integerRange": {
      const numberFormat = new Intl.NumberFormat(getLocale());
      const fields: Record<TaskIntegerField, string> = {
        gets: i18n.t("validation.task.fields.gets", { ns: "admin" }),
        puts: i18n.t("validation.task.fields.puts", { ns: "admin" }),
        places: i18n.t("validation.task.fields.places", { ns: "admin" }),
        checker_timeout: i18n.t("validation.task.fields.checker_timeout", {
          ns: "admin",
        }),
        get_period: i18n.t("validation.task.fields.get_period", {
          ns: "admin",
        }),
        default_score: i18n.t("validation.task.fields.default_score", {
          ns: "admin",
        }),
      };
      return i18n.t("validation.task.integerRange", {
        ns: "admin",
        field: fields[issue.field],
        min: numberFormat.format(issue.min),
        max: numberFormat.format(issue.max),
      });
    }
  }
}

export function validateTeam(
  team: Omit<Team, "id">,
  creating = false,
): string | null {
  const issue = getTeamValidationIssue(team, creating);
  return issue ? formatTeamValidationIssue(issue) : null;
}

export function validateTask(task: Omit<Task, "id">): string | null {
  const issue = getTaskValidationIssue(task);
  return issue ? formatTaskValidationIssue(issue) : null;
}
