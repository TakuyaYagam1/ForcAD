// src/entities/team/model/types.ts

export interface Team {
  id: number | null;
  name: string;
  ip: string;
  token: string;
  highlighted: boolean;
  active: boolean;
  score?: number;
  taskStats?: any;
}
