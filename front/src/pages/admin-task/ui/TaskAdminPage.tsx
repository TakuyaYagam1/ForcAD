// src/pages/admin-task/ui/TaskAdminPage.tsx
import { useEffect, useState } from "react";
import { useParams, useNavigate, useLocation } from "react-router-dom";

import { AppShell } from "@/shared/ui/layout/AppShell";
import type { Task } from "@/entities/task/model/types";
import {
  fetchTaskAdmin,
  createTaskAdmin,
  updateTaskAdmin,
} from "@/entities/task/api/admin";

import {
  Card,
  CardHeader,
  CardTitle,
  CardDescription,
  CardContent,
  CardFooter,
} from "@/components/ui/card";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Button } from "@/components/ui/button";
import { Checkbox } from "@/components/ui/checkbox";

interface TaskAdminPageProps {
  mode?: "create" | "edit";
}

export function TaskAdminPage({ mode }: TaskAdminPageProps) {
  const params = useParams<{ taskId?: string }>();
  const navigate = useNavigate();
  const location = useLocation();

  const [task, setTask] = useState<Task | null>(null);
  const [loading, setLoading] = useState(true);
  const [saving, setSaving] = useState(false);
  const [error, setError] = useState<string | null>(null);

  const taskIdParam = params.taskId;
  const isCreate = mode === "create" || !taskIdParam;
  const taskId = !isCreate && taskIdParam ? Number(taskIdParam) : null;

  useEffect(() => {
    let cancelled = false;

    async function load() {
      setLoading(true);
      setError(null);

      try {
        if (isCreate) {
          const empty: Task = {
            id: null,
            name: "",
            checker: "",
            gets: 1,
            puts: 1,
            places: 1,
            checker_timeout: 20,
            checker_type: "hackerdom",
            env_path: "",
            get_period: 10,
            default_score: 2500,
            active: true,
          };
          if (!cancelled) {
            setTask(empty);
          }
        } else if (taskId != null && !Number.isNaN(taskId)) {
          const data = await fetchTaskAdmin(taskId);
          if (!cancelled) {
            setTask(data);
          }
        } else {
          if (!cancelled) {
            setError("Некорректный ID таска");
          }
        }
      } catch (e: any) {
        console.error(e);
        if (!cancelled) {
          setError("Ошибка загрузки таска");
        }
      } finally {
        if (!cancelled) setLoading(false);
      }
    }

    void load();

    return () => {
      cancelled = true;
    };
  }, [isCreate, taskId, location.key]);

  const handleChange = (field: keyof Task, value: unknown) => {
    setTask((prev) => (prev ? { ...prev, [field]: value } : prev));
  };

  const handleSubmit = async (e: React.FormEvent) => {
    e.preventDefault();
    if (!task) return;
    setSaving(true);
    setError(null);

    try {
      if (isCreate) {
        const { id, ...payload } = task;
        const created = await createTaskAdmin(payload);
        navigate(`/admin/task/${created.id}`, { replace: true });
      } else if (taskId != null) {
        const { id, ...payload } = task;
        const updated = await updateTaskAdmin(taskId, payload);
        setTask(updated);
      }
    } catch (e: any) {
      console.error(e);
      setError("Ошибка сохранения таска");
    } finally {
      setSaving(false);
    }
  };

  return (
    <AppShell>
      <div className="mb-4">
        <h1 className="text-base font-semibold text-slate-100">
          {isCreate ? "Создание таска" : "Редактирование таска"}
        </h1>
        <p className="text-xs text-slate-400">Управление задачами чекеров.</p>
      </div>

      {loading ? (
        <div className="text-sm text-slate-400">Загружаем данные…</div>
      ) : !task ? (
        <div className="text-sm text-red-300">{error ?? "Таск не найден"}</div>
      ) : (
        <form onSubmit={handleSubmit}>
          <Card className="max-w-xl border-slate-800 bg-slate-950/80 backdrop-blur">
            <CardHeader>
              <CardTitle className="text-sm text-slate-100">
                {isCreate
                  ? "Новый таск"
                  : `Таск ${task.name} (${task.id ?? "—"})`}
              </CardTitle>
              <CardDescription className="text-xs text-slate-400">
                Редактируйте параметры чекера и сохраните изменения.
              </CardDescription>
            </CardHeader>

            <CardContent className="space-y-4">
              {error && (
                <div className="rounded-md border border-red-500/40 bg-red-500/10 px-3 py-2 text-xs text-red-200">
                  {error}
                </div>
              )}

              <div className="space-y-2">
                <Label htmlFor="name">Name</Label>
                <Input
                  id="name"
                  value={task.name}
                  onChange={(e) => handleChange("name", e.target.value)}
                />
              </div>

              <div className="space-y-2">
                <Label htmlFor="checker">Checker</Label>
                <Input
                  id="checker"
                  value={task.checker}
                  onChange={(e) => handleChange("checker", e.target.value)}
                />
              </div>

              <div className="grid grid-cols-3 gap-3">
                <div className="space-y-1.5">
                  <Label htmlFor="gets">Gets</Label>
                  <Input
                    id="gets"
                    type="number"
                    value={task.gets}
                    onChange={(e) =>
                      handleChange("gets", Number(e.target.value) || 0)
                    }
                  />
                </div>
                <div className="space-y-1.5">
                  <Label htmlFor="puts">Puts</Label>
                  <Input
                    id="puts"
                    type="number"
                    value={task.puts}
                    onChange={(e) =>
                      handleChange("puts", Number(e.target.value) || 0)
                    }
                  />
                </div>
                <div className="space-y-1.5">
                  <Label htmlFor="places">Places</Label>
                  <Input
                    id="places"
                    type="number"
                    value={task.places}
                    onChange={(e) =>
                      handleChange("places", Number(e.target.value) || 0)
                    }
                  />
                </div>
              </div>

              <div className="space-y-2">
                <Label htmlFor="checker_type">Checker type</Label>
                <Input
                  id="checker_type"
                  value={task.checker_type}
                  onChange={(e) => handleChange("checker_type", e.target.value)}
                />
              </div>

              <div className="space-y-2">
                <Label htmlFor="env_path">Env path</Label>
                <Input
                  id="env_path"
                  value={task.env_path}
                  onChange={(e) => handleChange("env_path", e.target.value)}
                />
              </div>

              <div className="grid grid-cols-3 gap-3">
                <div className="space-y-1.5">
                  <Label htmlFor="checker_timeout">Timeout</Label>
                  <Input
                    id="checker_timeout"
                    type="number"
                    value={task.checker_timeout}
                    onChange={(e) =>
                      handleChange(
                        "checker_timeout",
                        Number(e.target.value) || 0
                      )
                    }
                  />
                </div>
                <div className="space-y-1.5">
                  <Label htmlFor="get_period">Get period</Label>
                  <Input
                    id="get_period"
                    type="number"
                    value={task.get_period}
                    onChange={(e) =>
                      handleChange("get_period", Number(e.target.value) || 0)
                    }
                  />
                </div>
                <div className="space-y-1.5">
                  <Label htmlFor="default_score">Default score</Label>
                  <Input
                    id="default_score"
                    type="number"
                    value={task.default_score}
                    onChange={(e) =>
                      handleChange("default_score", Number(e.target.value) || 0)
                    }
                  />
                </div>
              </div>

              <label className="flex items-center gap-2 text-xs text-slate-200">
                <Checkbox
                  checked={task.active}
                  onCheckedChange={(v) => handleChange("active", Boolean(v))}
                />
                Active
              </label>
            </CardContent>

            <CardFooter className="flex justify-end gap-2">
              <Button
                type="button"
                variant="outline"
                size="sm"
                className="border-slate-700 text-slate-300"
                onClick={() => navigate(-1)}
              >
                Cancel
              </Button>
              <Button type="submit" size="sm" disabled={saving}>
                {saving ? "Saving…" : "Save"}
              </Button>
            </CardFooter>
          </Card>
        </form>
      )}
    </AppShell>
  );
}
