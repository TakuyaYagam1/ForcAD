import type { FormEvent } from "react";
import type { Team } from "@/entities/team/model/types";
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
import { MAX_TEAM_NAME_LENGTH } from "@/shared/lib/adminValidation";
import { TeamAvatar } from "@/shared/ui/brand/TeamAvatar";

type TeamFieldChange = <Field extends keyof Team>(
  field: Field,
  value: Team[Field],
) => void;

interface TeamFormProps {
  team: Team;
  isCreate: boolean;
  saving: boolean;
  error: string | null;
  canSave: boolean;
  onChange: TeamFieldChange;
  onSubmit: (event: FormEvent<HTMLFormElement>) => void;
  onCancel: () => void;
}

export function TeamForm({
  team,
  isCreate,
  saving,
  error,
  canSave,
  onChange,
  onSubmit,
  onCancel,
}: TeamFormProps) {
  return (
    <form onSubmit={onSubmit}>
      <Card className="admin-form max-w-3xl !p-0">
        <CardHeader>
          <CardTitle className="text-sm text-slate-100">
            {isCreate
              ? "Новая команда"
              : `Команда ${team.name} (${team.id ?? "—"})`}
          </CardTitle>
          <CardDescription className="text-xs text-slate-400">
            Измените поля и сохраните изменения.
          </CardDescription>
        </CardHeader>

        <CardContent className="grid grid-cols-1 gap-5 md:grid-cols-2">
          <div className="logo-preview md:col-span-2">
            <TeamAvatar
              name={team.name || "Команда"}
              src={team.logo_path}
              size="large"
            />
            <p>
              Так логотип будет выглядеть в профиле.
              <br />
              Укажи ссылку или путь к изображению в поле ниже.
            </p>
          </div>

          {error && (
            <div className="notice md:col-span-2" role="alert">
              {error}
            </div>
          )}

          <div className="space-y-2">
            <Label htmlFor="name">Название команды</Label>
            <Input
              disabled={saving}
              id="name"
              required
              maxLength={MAX_TEAM_NAME_LENGTH}
              value={team.name}
              onChange={(event) => onChange("name", event.target.value)}
            />
          </div>

          <div className="space-y-2">
            <Label htmlFor="ip">IP-адрес</Label>
            <Input
              disabled={saving}
              id="ip"
              required
              value={team.ip}
              onChange={(event) => onChange("ip", event.target.value)}
            />
          </div>

          <div className="space-y-2">
            <Label htmlFor="token">
              Токен команды{isCreate ? " (создаётся сервером)" : ""}
            </Label>
            <Input
              disabled={saving}
              id="token"
              readOnly={isCreate}
              placeholder={isCreate ? "Будет создан автоматически" : ""}
              maxLength={16}
              value={team.token}
              onChange={(event) => onChange("token", event.target.value)}
            />
          </div>

          <div className="space-y-2">
            <Label htmlFor="logo_path">Ссылка на логотип</Label>
            <Input
              disabled={saving}
              id="logo_path"
              placeholder="/team-logo.png или https://…"
              value={team.logo_path}
              onChange={(event) => onChange("logo_path", event.target.value)}
            />
          </div>

          <div className="flex flex-wrap gap-4 md:col-span-2">
            <label className="flex items-center gap-2 text-xs text-slate-200">
              <Checkbox
                disabled={saving}
                checked={team.highlighted}
                onCheckedChange={(value) =>
                  onChange("highlighted", Boolean(value))
                }
              />
              Выделить в рейтинге
            </label>

            <label className="flex items-center gap-2 text-xs text-slate-200">
              <Checkbox
                disabled={saving}
                checked={team.active}
                onCheckedChange={(value) => onChange("active", Boolean(value))}
              />
              Активна
            </label>
          </div>
        </CardContent>

        <CardFooter className="form-actions mx-6">
          <Button
            type="button"
            variant="outline"
            size="sm"
            disabled={saving}
            onClick={onCancel}
          >
            Отмена
          </Button>
          <Button type="submit" size="sm" disabled={saving || !canSave}>
            {saving ? "Сохраняем…" : "Сохранить"}
          </Button>
        </CardFooter>
      </Card>
    </form>
  );
}
