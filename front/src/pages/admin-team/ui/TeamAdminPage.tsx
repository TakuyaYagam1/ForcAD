// src/pages/admin-team/ui/TeamAdminPage.tsx
import { useEffect, useState, useRef } from "react";
import { useParams, useNavigate, useLocation } from "react-router-dom";

import { AppShell } from "@/shared/ui/layout/AppShell";
import { BrandIcon } from "@/shared/ui/brand/BrandIcon";
import { TeamAvatar } from "@/shared/ui/brand/TeamAvatar";
import type { Team } from "@/entities/team/model/types";
import {
  fetchTeamAdmin,
  createTeamAdmin,
  updateTeamAdmin,
} from "@/entities/team/api/admin";

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

import { validateTeam } from "@/shared/lib/adminValidation";
import { apiErrorMessage } from "@/shared/lib/apiError";
import { queryClient } from "@/shared/lib/queryClient";

interface TeamAdminPageProps {
  mode?: "create" | "edit";
}

export function TeamAdminPage({ mode }: TeamAdminPageProps) {
  const params = useParams<{ teamId?: string }>();
  const navigate = useNavigate();
  const location = useLocation();

  const [team, setTeam] = useState<Team | null>(null);
  const [loading, setLoading] = useState(true);
  const [saving, setSaving] = useState(false);
  const [error, setError] = useState<string | null>(null);

  const teamIdParam = params.teamId;
  const isCreate = mode === "create" || !teamIdParam;
  const teamId = !isCreate && teamIdParam ? Number(teamIdParam) : null;

  const routeKey = useRef(location.key);
  routeKey.current = location.key;
  const [loadedKey, setLoadedKey] = useState<string | null>(null);
  const canSave =
    loadedKey === location.key && !!team && (isCreate || team.id === teamId);

  useEffect(() => {
    let cancelled = false;

    async function load() {
      setLoading(true);
      setTeam(null);
      setLoadedKey(null);
      setSaving(false);
      setError(null);

      try {
        if (isCreate) {
          // дефолты как во Vue Team.vue
          const empty: Team = {
            id: null,
            name: "",
            ip: "",
            token: "",
            logo_path: "",
            highlighted: false,
            active: true,
          };
          if (!cancelled) {
            setTeam(empty);
            setLoadedKey(location.key);
          }
        } else if (teamId != null && Number.isInteger(teamId) && teamId > 0) {
          const data = await fetchTeamAdmin(teamId);
          if (!cancelled) {
            setTeam({ ...data, logo_path: data.logo_path ?? "" });
            setLoadedKey(location.key);
          }
        } else {
          if (!cancelled) {
            setError("Некорректный ID команды");
          }
        }
      } catch (e: unknown) {
        console.error(e);
        if (!cancelled) {
          setError(apiErrorMessage(e, "Ошибка загрузки данных"));
        }
      } finally {
        if (!cancelled) {
          setLoading(false);
        }
      }
    }

    void load();

    return () => {
      cancelled = true;
    };
  }, [isCreate, teamId, location.key]);

  const handleChange = (field: keyof Team, value: unknown) => {
    setTeam((prev) => (prev ? { ...prev, [field]: value } : prev));
  };

  const handleSubmit = async (e: React.FormEvent) => {
    e.preventDefault();
    if (!team || !canSave || saving) return;
    const validation = validateTeam(team, isCreate);
    if (validation) {
      setError(validation);
      return;
    }
    const submittedKey = location.key;
    setSaving(true);
    setError(null);

    try {
      const payload: Omit<Team, "id"> & { id?: Team["id"] } = { ...team };
      delete payload.id;
      if (isCreate) {
        const created = await createTeamAdmin(payload);
        if (routeKey.current === submittedKey)
          navigate(`/admin/team/${created.id}`, { replace: true });
      } else if (teamId != null) {
        const updated = await updateTeamAdmin(teamId, payload);
        if (routeKey.current === submittedKey) setTeam(updated);
      }
      await queryClient.invalidateQueries({ queryKey: ["admin-teams"] });
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
              {isCreate ? "Создание команды" : "Редактирование команды"}
            </h1>
            <p className="text-xs text-slate-400">
              Название, доступ и оформление команды.
            </p>
          </div>
          <BrandIcon name="team" />
        </div>

        {loading ? (
          <div className="text-sm text-slate-400">Загружаем данные…</div>
        ) : !team || !canSave ? (
          <div className="text-sm text-red-300">
            {error ?? "Команда не найдена"}
          </div>
        ) : (
          <form onSubmit={handleSubmit}>
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
                    maxLength={255}
                    value={team.name}
                    onChange={(e) => handleChange("name", e.target.value)}
                  />
                </div>

                <div className="space-y-2">
                  <Label htmlFor="ip">IP-адрес</Label>
                  <Input
                    disabled={saving}
                    id="ip"
                    required
                    value={team.ip}
                    onChange={(e) => handleChange("ip", e.target.value)}
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
                    onChange={(e) => handleChange("token", e.target.value)}
                  />
                </div>

                <div className="space-y-2">
                  <Label htmlFor="logo_path">Ссылка на логотип</Label>
                  <Input
                    disabled={saving}
                    id="logo_path"
                    placeholder="/team-logo.png или https://…"
                    value={team.logo_path}
                    onChange={(e) => handleChange("logo_path", e.target.value)}
                  />
                </div>

                <div className="flex flex-wrap gap-4 md:col-span-2">
                  <label className="flex items-center gap-2 text-xs text-slate-200">
                    <Checkbox
                      disabled={saving}
                      checked={team.highlighted}
                      onCheckedChange={(v) =>
                        handleChange("highlighted", Boolean(v))
                      }
                    />
                    Выделить в рейтинге
                  </label>

                  <label className="flex items-center gap-2 text-xs text-slate-200">
                    <Checkbox
                      disabled={saving}
                      checked={team.active}
                      onCheckedChange={(v) =>
                        handleChange("active", Boolean(v))
                      }
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
