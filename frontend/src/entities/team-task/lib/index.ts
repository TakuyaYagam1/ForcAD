import { STATUS_COLOR_BY_CODE } from "@/shared/config/statuses";

export function getTeamTaskBackground(status?: number): string | undefined {
  if (!status) return undefined;
  return STATUS_COLOR_BY_CODE[status] ?? undefined;
}
