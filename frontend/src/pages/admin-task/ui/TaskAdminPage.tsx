import { useEffect, useLayoutEffect, useRef, useState } from "react";
import type { FormEvent } from "react";
import { useLocation, useNavigate, useParams } from "react-router-dom";
import type { Task } from "@/entities/task/model/types";
import {
  createTaskAdmin,
  fetchTaskAdmin,
  updateTaskAdmin,
} from "@/entities/task/api/admin";
import { apiErrorMessage } from "@/shared/lib/apiError";
import { validateTask } from "@/shared/lib/adminValidation";
import { queryClient } from "@/shared/lib/queryClient";
import { AppShell } from "@/shared/ui/layout/AppShell";
import { BrandIcon } from "@/shared/ui/brand/BrandIcon";
import { TaskForm } from "./TaskForm";
import { normalizeUuid } from "@/shared/lib/uuid";

interface TaskAdminPageProps {
  mode?: "create" | "edit";
}

export function TaskAdminPage({ mode }: TaskAdminPageProps) {
  const { taskId: taskIdParam } = useParams<{ taskId?: string }>();
  const navigate = useNavigate();
  const location = useLocation();
  const [task, setTask] = useState<Task | null>(null);
  const [loading, setLoading] = useState(true);
  const [saving, setSaving] = useState(false);
  const [error, setError] = useState<string | null>(null);

  const isCreate = mode === "create" || !taskIdParam;
  const taskId = !isCreate ? (normalizeUuid(taskIdParam) ?? null) : null;
  const routeKey = useRef(location.key);
  const [loadedKey, setLoadedKey] = useState<string | null>(null);
  const canSave =
    loadedKey === location.key && !!task && (isCreate || task.id === taskId);

  useLayoutEffect(() => {
    routeKey.current = location.key;
  }, [location.key]);

  useEffect(() => {
    let cancelled = false;

    async function loadTask() {
      setLoading(true);
      setTask(null);
      setLoadedKey(null);
      setSaving(false);
      setError(null);

      try {
        if (isCreate) {
          const emptyTask: Task = {
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
            setTask(emptyTask);
            setLoadedKey(location.key);
          }
        } else if (taskId !== null) {
          const loadedTask = await fetchTaskAdmin(taskId);
          if (!cancelled) {
            setTask({ ...loadedTask, env_path: loadedTask.env_path ?? "" });
            setLoadedKey(location.key);
          }
        } else if (!cancelled) {
          setError("Некорректный ID таска");
        }
      } catch (loadError: unknown) {
        console.error(loadError);
        if (!cancelled) {
          setError(apiErrorMessage(loadError, "Ошибка загрузки данных"));
        }
      } finally {
        if (!cancelled) setLoading(false);
      }
    }

    void loadTask();

    return () => {
      cancelled = true;
    };
  }, [isCreate, taskId, location.key]);

  const handleChange = <Field extends keyof Task,>(
    field: Field,
    value: Task[Field],
  ) => {
    setTask((currentTask) =>
      currentTask ? { ...currentTask, [field]: value } : currentTask,
    );
  };

  const handleSubmit = async (event: FormEvent<HTMLFormElement>) => {
    event.preventDefault();
    if (!task || !canSave || saving) return;

    const validationError = validateTask(task);
    if (validationError) {
      setError(validationError);
      return;
    }

    const submittedRouteKey = location.key;
    setSaving(true);
    setError(null);

    try {
      const payload: Omit<Task, "id"> & { id?: Task["id"] } = { ...task };
      delete payload.id;

      if (isCreate) {
        const createdTask = await createTaskAdmin(payload);
        if (routeKey.current === submittedRouteKey) {
          navigate(`/admin/task/${createdTask.id}`, { replace: true });
        }
      } else if (taskId != null) {
        const updatedTask = await updateTaskAdmin(taskId, payload);
        if (routeKey.current === submittedRouteKey) setTask(updatedTask);
      }

      await queryClient.invalidateQueries({ queryKey: ["admin-tasks"] });
    } catch (saveError: unknown) {
      if (routeKey.current === submittedRouteKey) {
        setError(apiErrorMessage(saveError, "Ошибка сохранения данных"));
      }
    } finally {
      if (routeKey.current === submittedRouteKey) setSaving(false);
    }
  };

  return (
    <AppShell>
      <div className="admin-page">
        <div className="admin-heading">
          <div>
            <p className="eyebrow">Панель организатора</p>
            <h1>{isCreate ? "Создание сервиса" : "Редактирование сервиса"}</h1>
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
          <TaskForm
            task={task}
            isCreate={isCreate}
            saving={saving}
            error={error}
            canSave={canSave}
            onChange={handleChange}
            onSubmit={handleSubmit}
            onCancel={() => navigate(-1)}
          />
        )}
      </div>
    </AppShell>
  );
}
