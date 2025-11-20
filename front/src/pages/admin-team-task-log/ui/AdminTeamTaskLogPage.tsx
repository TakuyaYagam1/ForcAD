import { useParams } from "react-router-dom";
import { useQuery } from "@tanstack/react-query";

import { AppShell } from "@/shared/ui/layout/AppShell";
import { fetchTeamTaskLog } from "@/entities/team-task/api";
import { getTeamTaskBackground } from "@/entities/team-task/lib";
import { fetchTeamAdmin } from "@/entities/team/api/admin";
import { fetchTaskAdmin } from "@/entities/task/api/admin";

import {
  Table,
  TableHeader,
  TableBody,
  TableRow,
  TableHead,
  TableCell,
} from "@/components/ui/table";

export function AdminTeamTaskLogPage() {
  const { teamId: teamIdRaw, taskId: taskIdRaw } = useParams();
  const teamId = Number(teamIdRaw);
  const taskId = Number(taskIdRaw);

  // лог чекера
  const logQuery = useQuery({
    queryKey: ["teamtask-log", teamId, taskId],
    queryFn: () => fetchTeamTaskLog(teamId, taskId),
  });

  // название команды
  const teamQuery = useQuery({
    queryKey: ["team", teamId],
    queryFn: () => fetchTeamAdmin(teamId),
  });

  // название таска
  const taskQuery = useQuery({
    queryKey: ["task", taskId],
    queryFn: () => fetchTaskAdmin(taskId),
  });

  return (
    <AppShell>
      <div className="mb-4">
        <h1 className="text-xl font-semibold text-slate-100">Team/Task log</h1>
        <p className="text-sm text-slate-400">
          Команда:{" "}
          <span className="text-slate-100">
            {teamQuery.data?.name ?? "..."}
          </span>
          {"  |  "}
          Таск:{" "}
          <span className="text-slate-100">
            {taskQuery.data?.name ?? "..."}
          </span>
        </p>
      </div>

      <div className="rounded-xl border border-slate-800 bg-slate-950 p-4">
        <Table>
          <TableHeader>
            <TableRow className="border-slate-800">
              <TableHead>Round</TableHead>
              <TableHead>Status</TableHead>
              <TableHead>Score</TableHead>
              <TableHead>Stolen</TableHead>
              <TableHead>Lost</TableHead>
              <TableHead>Checks</TableHead>
              <TableHead>Public message</TableHead>
              <TableHead>Private message</TableHead>
              <TableHead>Command</TableHead>
            </TableRow>
          </TableHeader>

          <TableBody>
            {logQuery.data?.map((entry) => (
              <TableRow
                key={entry.id}
                style={{
                  backgroundColor: getTeamTaskBackground(entry.status),
                }}
                className="text-xs border-slate-800"
              >
                <TableCell>{entry.round}</TableCell>
                <TableCell>{entry.status}</TableCell>
                <TableCell>{entry.score}</TableCell>
                <TableCell>{entry.stolen}</TableCell>
                <TableCell>{entry.lost}</TableCell>
                <TableCell>
                  {entry.checks_passed}/{entry.checks}
                </TableCell>
                <TableCell className="max-w-xs break-words">
                  {entry.public_message || ""}
                </TableCell>
                <TableCell className="max-w-xs break-words">
                  {entry.private_message || ""}
                </TableCell>
                <TableCell className="max-w-xs break-words">
                  {entry.command || ""}
                </TableCell>
              </TableRow>
            ))}
          </TableBody>
        </Table>
      </div>
    </AppShell>
  );
}
