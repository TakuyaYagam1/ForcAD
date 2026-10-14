import { Button } from "@/components/ui/button";
import {
  Card,
  CardContent,
  CardDescription,
  CardFooter,
  CardHeader,
  CardTitle,
} from "@/components/ui/card";
import { Checkbox } from "@/components/ui/checkbox";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import type { Task } from "@/entities/task/model/types";
import {
  MAX_CHECKER_PATH_LENGTH,
  MAX_TASK_INTEGER_VALUE,
  MAX_TASK_NAME_LENGTH,
} from "@/shared/lib/adminValidation";
import type { FormEvent } from "react";
import { useTranslation } from "react-i18next";

type TaskFieldChange = <Field extends keyof Task>(
  field: Field,
  value: Task[Field],
) => void;

type TaskNumericField =
  | "gets"
  | "puts"
  | "places"
  | "checker_timeout"
  | "get_period"
  | "default_score";

interface TaskNumberFieldProps {
  field: TaskNumericField;
  label: string;
  min: number;
  task: Task;
  disabled: boolean;
  onChange: TaskFieldChange;
}

function TaskNumberField({
  field,
  label,
  min,
  task,
  disabled,
  onChange,
}: TaskNumberFieldProps) {
  return (
    <div className="space-y-1.5">
      <Label htmlFor={field}>{label}</Label>
      <Input
        disabled={disabled}
        id={field}
        min={min}
        step={1}
        max={MAX_TASK_INTEGER_VALUE}
        required
        type="number"
        value={task[field]}
        onChange={(event) =>
          onChange(field, Number(event.currentTarget.value) || 0)
        }
      />
    </div>
  );
}

interface TaskFormProps {
  task: Task;
  isCreate: boolean;
  saving: boolean;
  canSave: boolean;
  onChange: TaskFieldChange;
  onSubmit: (event: FormEvent<HTMLFormElement>) => void;
  onCancel: () => void;
}

export function TaskForm({
  task,
  isCreate,
  saving,
  canSave,
  onChange,
  onSubmit,
  onCancel,
}: TaskFormProps) {
  const { t } = useTranslation("admin");
  return (
    <form onSubmit={onSubmit} noValidate>
      <Card className="admin-form max-w-3xl !p-0">
        <CardHeader>
          <CardTitle className="text-sm text-slate-100">
            {isCreate
              ? t("task.newTitle")
              : t("task.existingTitle", { name: task.name, id: task.id ?? "-" })}
          </CardTitle>
          <CardDescription className="text-xs text-slate-400">
            {t("task.formDescription")}
          </CardDescription>
        </CardHeader>

        <CardContent className="grid grid-cols-1 gap-5 md:grid-cols-2">
          <div className="space-y-2">
            <Label htmlFor="name">{t("task.name")}</Label>
            <Input
              disabled={saving}
              id="name"
              required
              maxLength={MAX_TASK_NAME_LENGTH}
              value={task.name}
              onChange={(event) => onChange("name", event.target.value)}
            />
          </div>

          <div className="space-y-2">
            <Label htmlFor="checker">{t("task.checkerPath")}</Label>
            <Input
              disabled={saving}
              id="checker"
              required
              maxLength={MAX_CHECKER_PATH_LENGTH}
              value={task.checker}
              onChange={(event) => onChange("checker", event.target.value)}
            />
          </div>

          <div className="grid grid-cols-3 gap-3 md:col-span-2">
            <TaskNumberField
              field="gets"
              label={t("task.getChecks")}
              min={0}
              task={task}
              disabled={saving}
              onChange={onChange}
            />
            <TaskNumberField
              field="puts"
              label={t("task.putChecks")}
              min={0}
              task={task}
              disabled={saving}
              onChange={onChange}
            />
            <TaskNumberField
              field="places"
              label={t("task.places")}
              min={1}
              task={task}
              disabled={saving}
              onChange={onChange}
            />
          </div>

          <div className="space-y-2">
            <Label htmlFor="checker_type">{t("task.checkerType")}</Label>
            <Input
              disabled={saving}
              id="checker_type"
              maxLength={32}
              value={task.checker_type}
              onChange={(event) => onChange("checker_type", event.target.value)}
            />
          </div>

          <div className="space-y-2">
            <Label htmlFor="env_path">{t("task.environmentFile")}</Label>
            <Input
              disabled={saving}
              id="env_path"
              value={task.env_path}
              onChange={(event) => onChange("env_path", event.target.value)}
            />
          </div>

          <div className="grid grid-cols-3 gap-3 md:col-span-2">
            <TaskNumberField
              field="checker_timeout"
              label={t("task.timeout")}
              min={1}
              task={task}
              disabled={saving}
              onChange={onChange}
            />
            <TaskNumberField
              field="get_period"
              label={t("task.getPeriod")}
              min={1}
              task={task}
              disabled={saving}
              onChange={onChange}
            />
            <TaskNumberField
              field="default_score"
              label={t("task.initialScore")}
              min={0}
              task={task}
              disabled={saving}
              onChange={onChange}
            />
          </div>

          <label className="flex items-center gap-2 text-xs text-slate-200">
            <Checkbox
              disabled={saving}
              checked={task.active}
              onCheckedChange={(value) => onChange("active", Boolean(value))}
            />
            {t("task.active")}
          </label>
        </CardContent>

        <CardFooter className="form-actions mx-6">
          <Button
            type="button"
            variant="outline"
            size="sm"
            disabled={saving}
            onClick={onCancel}
          >
            {t("common.cancel")}
          </Button>
          <Button type="submit" size="sm" disabled={saving || !canSave}>
            {saving ? t("common.saving") : t("common.save")}
          </Button>
        </CardFooter>
      </Card>
    </form>
  );
}
