// src/entities/task/api/admin.ts
import { api } from "@/shared/lib/axios";
import type { Task } from "@/entities/task/model/types";

export async function fetchTasksAdmin() {
  const { data } = await api.get<Task[]>("/admin/tasks/");
  return data;
}

export async function fetchTaskAdmin(id: number) {
  const { data } = await api.get<Task>(`/admin/tasks/${id}/`);
  return data;
}

export async function createTaskAdmin(payload: Omit<Task, "id">) {
  const { data } = await api.post<Task>("/admin/tasks/", payload);
  return data;
}

export async function updateTaskAdmin(id: number, payload: Omit<Task, "id">) {
  const { data } = await api.put<Task>(`/admin/tasks/${id}/`, payload);
  return data;
}

export async function deleteTaskAdmin(id: number) {
  await api.delete(`/admin/tasks/${id}/`);
}
