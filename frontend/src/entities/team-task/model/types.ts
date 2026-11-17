import type { AdminTeamTaskLogEntry } from "@/shared/api/generated/schemas/admin/adminTeamTaskLogEntry.zod";

export interface TeamTask {
  teamId: string;
  taskId: string;
  status: number;
  stolen: number;
  lost: number;
  checks: number;
  checksPassed: number;
  sla: number;
  score: number;
  message: string;
}

export type TeamTaskLogEntry = AdminTeamTaskLogEntry;
