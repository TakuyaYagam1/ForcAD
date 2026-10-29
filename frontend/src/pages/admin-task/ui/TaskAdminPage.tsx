import { useEffect, useLayoutEffect, useRef, useState } from "react";
import type { FormEvent } from "react";
import { Link, useLocation, useNavigate, useParams } from "react-router-dom";
import type { Task } from "@/entities/task/model/types";
import {
  createTaskAdmin,
  fetchTaskAdmin,
  updateTaskAdmin,
} from "@/entities/task/api/admin";
import { apiErrorMessage } from "@/shared/lib/apiError";
import {
  formatTaskValidationIssue,
  getTaskValidationIssue,
} from "@/shared/lib/adminValidation";
import type { TaskValidationIssue } from "@/shared/lib/adminValidation";
import { queryClient } from "@/shared/lib/queryClient";
import { ArrowLeft } from "lucide-react";
import { TaskForm } from "./TaskForm";
import { normalizeUuid } from "@/shared/lib/uuid";
import { useTranslation } from "react-i18next";
import { AdminShell } from "@/widgets/admin-shell/ui/AdminShell";

type TaskPageError =
  | { kind: "invalidId" }
  | { kind: "validation"; issue: TaskValidationIssue }
  | { kind: "request"; operation: "load" | "save"; cause: unknown };

interface TaskAdminPageProps {
  mode?: "create" | "edit";
}

export function TaskAdminPage({ mode }: TaskAdminPageProps) {
  const { t } = useTranslation("admin");
  const { taskId: taskIdParam } = useParams<{ taskId?: string }>();
  const navigate = useNavigate();
  const location = useLocation();
  const [task, setTask] = useState<Task | null>(null);
  const [loading, setLoading] = useState(true);
  const [saving, setSaving] = useState(false);
  const [error, setError] = useState<TaskPageError | null>(null);

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
          setError({ kind: "invalidId" });
        }
      } catch (loadError: unknown) {
        console.error(loadError);
        if (!cancelled) {
          setError({ kind: "request", operation: "load", cause: loadError });
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

    const validationError = getTaskValidationIssue(task);
    if (validationError) {
      setError({ kind: "validation", issue: validationError });
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
        setError({ kind: "request", operation: "save", cause: saveError });
      }
    } finally {
      if (routeKey.current === submittedRouteKey) setSaving(false);
    }
  };

  const errorMessage = error === null
    ? null
    : error.kind === "invalidId"
      ? t("task.invalidId")
      : error.kind === "validation"
        ? formatTaskValidationIssue(error.issue)
        : error.operation === "load"
          ? apiErrorMessage(error.cause, t("errors.loadData"))
          : apiErrorMessage(error.cause, t("errors.saveData"));

  return (
    <AdminShell>
      <div className="admin-page">
        <Link to="/admin/scoreboard" className="back-link admin-back-link">
          <ArrowLeft size={18} aria-hidden="true" />
          {t("common.backToDashboard")}
        </Link>
        <div className="admin-heading">
          <div>
            <p className="eyebrow">{t("common.organizerPanel")}</p>
            <h1>{isCreate ? t("task.createTitle") : t("task.editTitle")}</h1>
            <p className="text-xs text-slate-400">{t("task.pageDescription")}</p>
          </div>
        </div>

        {loading ? (
          <div className="text-sm text-slate-400">{t("common.loadingData")}</div>
        ) : !task || !canSave ? (
          <div className="text-sm text-red-300">
            {errorMessage ?? t("task.notFound")}
          </div>
        ) : (
          <>
            {errorMessage && (
              <div className="notice mb-4" role="alert">
                {errorMessage}
              </div>
            )}
            <TaskForm
              task={task}
              isCreate={isCreate}
              saving={saving}
              canSave={canSave}
              onChange={handleChange}
              onSubmit={handleSubmit}
              onCancel={() => navigate("/admin/scoreboard")}
            />
          </>
        )}
      </div>
    </AdminShell>
  );
}
