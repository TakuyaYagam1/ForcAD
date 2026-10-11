// src/entities/task/model/types.ts

export interface Task {
  id: number | null;
  name: string;
  checker: string;
  gets: number;
  puts: number;
  places: number;
  checker_timeout: number;
  checker_type: string;
  env_path: string;
  default_score: number;
  get_period: number;
  active: boolean;
}
