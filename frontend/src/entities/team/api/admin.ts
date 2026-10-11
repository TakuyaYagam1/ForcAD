// src/entities/team/api/admin.ts
import { api } from "@/shared/lib/axios";
import type { Team } from "@/entities/team/model/types";

export async function fetchTeamsAdmin() {
  const { data } = await api.get<Team[]>("/admin/teams/");
  return data;
}

export async function fetchTeamAdmin(id: number) {
  const { data } = await api.get<Team>(`/admin/teams/${id}/`);
  return data;
}

export async function createTeamAdmin(payload: Omit<Team, "id">) {
  const { data } = await api.post<Team>("/admin/teams/", payload);
  return data;
}

export async function updateTeamAdmin(id: number, payload: Omit<Team, "id">) {
  const { data } = await api.put<Team>(`/admin/teams/${id}/`, payload);
  return data;
}

export async function deleteTeamAdmin(id: number) {
  await api.delete(`/admin/teams/${id}/`);
}
