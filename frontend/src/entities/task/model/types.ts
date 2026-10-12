import type { AdminTask } from "@/shared/api/generated/schemas/admin/adminTask.zod";

export type Task = Omit<AdminTask, "id"> & { id: string | null };
