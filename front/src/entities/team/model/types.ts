// src/entities/team/model/types.ts

export interface Team {
  id: number | null;
  name: string;
  ip: string;
  token: string;
  logo_path: string;
  highlighted: boolean;
  active: boolean;
  score?: number;
  taskStats?: any;
}
