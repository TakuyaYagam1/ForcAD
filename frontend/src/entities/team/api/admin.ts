import {
  createAdminTeam as createAdminTeamRequest,
  deleteAdminTeam as deleteAdminTeamRequest,
  getAdminTeam as getAdminTeamRequest,
  getAdminTeams as getAdminTeamsRequest,
  updateAdminTeam as updateAdminTeamRequest,
} from "@/shared/api/generated/endpoints";
import type { Team } from "@/entities/team/model/types";

export async function fetchTeamsAdmin(signal?: AbortSignal) {
  return getAdminTeamsRequest(signal);
}

export async function fetchTeamAdmin(id: string, signal?: AbortSignal) {
  return getAdminTeamRequest(id, signal);
}

export async function createTeamAdmin(payload: Omit<Team, "id">) {
  return createAdminTeamRequest(payload);
}

export async function updateTeamAdmin(id: string, payload: Omit<Team, "id">) {
  return updateAdminTeamRequest(id, payload);
}

export async function deleteTeamAdmin(id: string) {
  return deleteAdminTeamRequest(id);
}
