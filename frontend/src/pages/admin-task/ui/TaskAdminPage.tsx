// src/pages/admin-task/ui/TaskAdminPage.tsx
import { useEffect, useLayoutEffect, useState, useRef } from "react";
import { useParams, useNavigate, useLocation } from "react-router-dom";

import { AppShell } from "@/shared/ui/layout/AppShell";
import { BrandIcon } from "@/shared/ui/brand/BrandIcon";
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

import { validateTask } from "@/shared/lib/adminValidation";
import { apiErrorMessage } from "@/shared/lib/apiError";
import { queryClient } from "@/shared/lib/queryClient";

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

  const routeKey = useRef(location.key);
  useLayoutEffect(() => {
    routeKey.current = location.key;
  }, [location.key]);
  const [loadedKey, setLoadedKey] = useState<string | null>(null);
  const canSave =
    loadedKey === location.key && !!task && (isCreate || task.id === taskId);

  useEffect(() => {
    let cancelled = false;

    async function load() {
      setLoading(true);
      setTask(null);
      setLoadedKey(null);
      setSaving(false);
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
            setLoadedKey(location.key);
          }
        } else if (taskId != null && Number.isInteger(taskId) && taskId > 0) {
          const data = await fetchTaskAdmin(taskId);
          if (!cancelled) {
            setTask({ ...data, env_path: data.env_path ?? "" });
            setLoadedKey(location.key);
          }
        } else {
          if (!cancelled) {
            setError("Некорректный ID таска");
          }
        }
      } catch (e: unknown) {
        console.error(e);
        if (!cancelled) {
          setError(apiErrorMessage(e, "Ошибка загрузки данных"));
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
    if (!task || !canSave || saving) return;
    const validation = validateTask(task);
    if (validation) {
      setError(validation);
      return;
    }
    const submittedKey = location.key;
    setSaving(true);
    setError(null);

    try {
      const payload: Omit<Task, "id"> & { id?: Task["id"] } = { ...task };
      delete payload.id;
      if (isCreate) {
        const created = await createTaskAdmin(payload);
        if (routeKey.current === submittedKey)
          navigate(`/admin/task/${created.id}`, { replace: true });
      } else if (taskId != null) {
        const updated = await updateTaskAdmin(taskId, payload);
        if (routeKey.current === submittedKey) setTask(updated);
      }
      await queryClient.invalidateQueries({ queryKey: ["admin-tasks"] });
    } catch (e: unknown) {
      if (routeKey.current === submittedKey)
        setError(apiErrorMessage(e, "Ошибка сохранения данных"));
    } finally {
      if (routeKey.current === submittedKey) setSaving(false);
    }
  };

  return (
    <AppShell>
      <div className="admin-page">
        <div className="admin-heading">
          <div>
            <p className="eyebrow">Панель организатора</p>
            <h1 className="">
              {isCreate ? "Создание сервиса" : "Редактирование сервиса"}
            </h1>
            <p className="text-xs text-slate-400">
              Параметры сервиса и запуска чекера.
            </p>
          </div>
          <BrandIcon name="terminal" />
        </div>

        {loading ? (
          <div className="text-sm text-slate-400">Загружаем данные…</div>
        ) : !task || !canSave ? (
          <div className="text-sm text-red-300">
            {error ?? "Таск не найден"}
          </div>
        ) : (
          <form onSubmit={handleSubmit}>
            <Card className="admin-form max-w-3xl !p-0">
              <CardHeader>
                <CardTitle className="text-sm text-slate-100">
                  {isCreate
                    ? "Новый сервис"
                    : `Сервис ${task.name} (${task.id ?? "—"})`}
                </CardTitle>
                <CardDescription className="text-xs text-slate-400">
                  Редактируйте параметры чекера и сохраните изменения.
                </CardDescription>
              </CardHeader>

              <CardContent className="grid grid-cols-1 gap-5 md:grid-cols-2">
                {error && (
                  <div className="notice md:col-span-2" role="alert">
                    {error}
                  </div>
                )}

                <div className="space-y-2">
                  <Label htmlFor="name">Название сервиса</Label>
                  <Input
                    disabled={saving}
                    id="name"
                    required
                    maxLength={255}
                    value={task.name}
                    onChange={(e) => handleChange("name", e.target.value)}
                  />
                </div>

                <div className="space-y-2">
                  <Label htmlFor="checker">Путь к чекеру</Label>
                  <Input
                    disabled={saving}
                    id="checker"
                    required
                    maxLength={1024}
                    value={task.checker}
                    onChange={(e) => handleChange("checker", e.target.value)}
                  />
                </div>

                <div className="grid grid-cols-3 gap-3 md:col-span-2">
                  <div className="space-y-1.5">
                    <Label htmlFor="gets">GET-проверки</Label>
                    <Input
                      disabled={saving}
                      id="gets"
                      min={0}
                      step={1}
                      max={2147483647}
                      required
                      type="number"
                      value={task.gets}
                      onChange={(e) =>
                        handleChange("gets", Number(e.target.value) || 0)
                      }
                    />
                  </div>
                  <div className="space-y-1.5">
                    <Label htmlFor="puts">PUT-проверки</Label>
                    <Input
                      disabled={saving}
                      id="puts"
                      min={0}
                      step={1}
                      max={2147483647}
                      required
                      type="number"
                      value={task.puts}
                      onChange={(e) =>
                        handleChange("puts", Number(e.target.value) || 0)
                      }
                    />
                  </div>
                  <div className="space-y-1.5">
                    <Label htmlFor="places">Места</Label>
                    <Input
                      disabled={saving}
                      id="places"
                      min={1}
                      step={1}
                      max={2147483647}
                      required
                      type="number"
                      value={task.places}
                      onChange={(e) =>
                        handleChange("places", Number(e.target.value) || 0)
                      }
                    />
                  </div>
                </div>

                <div className="space-y-2">
                  <Label htmlFor="checker_type">Тип чекера</Label>
                  <Input
                    disabled={saving}
                    id="checker_type"
                    maxLength={32}
                    value={task.checker_type}
                    onChange={(e) =>
                      handleChange("checker_type", e.target.value)
                    }
                  />
                </div>

                <div className="space-y-2">
                  <Label htmlFor="env_path">Файл окружения</Label>
                  <Input
                    disabled={saving}
                    id="env_path"
                    value={task.env_path}
                    onChange={(e) => handleChange("env_path", e.target.value)}
                  />
                </div>

                <div className="grid grid-cols-3 gap-3 md:col-span-2">
                  <div className="space-y-1.5">
                    <Label htmlFor="checker_timeout">Таймаут, с</Label>
                    <Input
                      disabled={saving}
                      id="checker_timeout"
                      min={1}
                      step={1}
                      max={2147483647}
                      required
                      type="number"
                      value={task.checker_timeout}
                      onChange={(e) =>
                        handleChange(
                          "checker_timeout",
                          Number(e.target.value) || 0,
                        )
                      }
                    />
                  </div>
                  <div className="space-y-1.5">
                    <Label htmlFor="get_period">Период GET</Label>
                    <Input
                      disabled={saving}
                      id="get_period"
                      min={1}
                      step={1}
                      max={2147483647}
                      required
                      type="number"
                      value={task.get_period}
                      onChange={(e) =>
                        handleChange("get_period", Number(e.target.value) || 0)
                      }
                    />
                  </div>
                  <div className="space-y-1.5">
                    <Label htmlFor="default_score">Начальные очки</Label>
                    <Input
                      disabled={saving}
                      id="default_score"
                      min={0}
                      step={1}
                      max={2147483647}
                      required
                      type="number"
                      value={task.default_score}
                      onChange={(e) =>
                        handleChange(
                          "default_score",
                          Number(e.target.value) || 0,
                        )
                      }
                    />
                  </div>
                </div>

                <label className="flex items-center gap-2 text-xs text-slate-200">
                  <Checkbox
                    disabled={saving}
                    checked={task.active}
                    onCheckedChange={(v) => handleChange("active", Boolean(v))}
                  />
                  Сервис активен
                </label>
              </CardContent>

              <CardFooter className="form-actions mx-6">
                <Button
                  type="button"
                  variant="outline"
                  size="sm"

                  disabled={saving}
                  onClick={() => navigate(-1)}
                >
                  Отмена
                </Button>
                <Button type="submit" size="sm" disabled={saving || !canSave}>
                  {saving ? "Сохраняем…" : "Сохранить"}
                </Button>
              </CardFooter>
            </Card>
          </form>
        )}
      </div>
    </AppShell>
  );
}
