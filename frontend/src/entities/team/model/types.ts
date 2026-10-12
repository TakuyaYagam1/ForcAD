import type { AdminTeam } from "@/shared/api/generated/schemas/admin/adminTeam.zod";

export type Team = Omit<AdminTeam, "id"> & {
  id: string | null;
  score?: number;
  taskStats?: unknown;
};
