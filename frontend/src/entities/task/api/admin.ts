import {
  createAdminTask as createAdminTaskRequest,
  deleteAdminTask as deleteAdminTaskRequest,
  getAdminTask as getAdminTaskRequest,
  getAdminTasks as getAdminTasksRequest,
  updateAdminTask as updateAdminTaskRequest,
} from "@/shared/api/generated/endpoints";
import type { Task } from "@/entities/task/model/types";

export async function fetchTasksAdmin(signal?: AbortSignal) {
  return getAdminTasksRequest(signal);
}

export async function fetchTaskAdmin(id: string, signal?: AbortSignal) {
  return getAdminTaskRequest(id, signal);
}

export async function createTaskAdmin(payload: Omit<Task, "id">) {
  return createAdminTaskRequest(payload);
}

export async function updateTaskAdmin(id: string, payload: Omit<Task, "id">) {
  return updateAdminTaskRequest(id, payload);
}

export async function deleteTaskAdmin(id: string) {
  return deleteAdminTaskRequest(id);
}
