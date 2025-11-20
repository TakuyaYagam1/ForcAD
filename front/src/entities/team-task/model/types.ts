// src/entities/team-task/model/types.ts

export interface TeamTask {
  id: number;
  teamId: number;
  taskId: number;
  status: number;
  stolen: boolean;
  lost: boolean;
  sla: number;
  score: number;
  message: string;
}

export interface TeamTaskLogEntry {
  id: number;
  ts: number; // timestamp (unix seconds)
  round: number;
  team_id: number;
  task_id: number;

  status: number;
  score: number;

  stolen: number;
  lost: number;

  checks: number;
  checks_passed: number;

  public_message: string | null;
  private_message: string | null;
  command: string | null;
}
