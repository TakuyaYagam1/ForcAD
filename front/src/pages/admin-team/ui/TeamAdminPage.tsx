// src/pages/admin-team/ui/TeamAdminPage.tsx
import { useEffect, useState } from "react";
import { useParams, useNavigate, useLocation } from "react-router-dom";

import { AppShell } from "@/shared/ui/layout/AppShell";
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

  useEffect(() => {
    let cancelled = false;

    async function load() {
      setLoading(true);
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
          }
        } else if (teamId != null && !Number.isNaN(teamId)) {
          const data = await fetchTeamAdmin(teamId);
          if (!cancelled) {
            setTeam(data);
          }
        } else {
          if (!cancelled) {
            setError("Некорректный ID команды");
          }
        }
      } catch (e: any) {
        console.error(e);
        if (!cancelled) {
          setError("Ошибка загрузки команды");
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
    if (!team) return;
    setSaving(true);
    setError(null);

    try {
      if (isCreate) {
        const { id, ...payload } = team;
        const created = await createTeamAdmin(payload);
        navigate(`/admin/team/${created.id}`, { replace: true });
      } else if (teamId != null) {
        const { id, ...payload } = team;
        const updated = await updateTeamAdmin(teamId, payload);
        setTeam(updated);
      }
    } catch (e: any) {
      console.error(e);
      setError("Ошибка сохранения команды");
    } finally {
      setSaving(false);
    }
  };

  return (
    <AppShell>
      <div className="mb-4">
        <h1 className="text-base font-semibold text-slate-100">
          {isCreate ? "Создание команды" : "Редактирование команды"}
        </h1>
        <p className="text-xs text-slate-400">
          Управление командами FinalSibCTF2025.
        </p>
      </div>

      {loading ? (
        <div className="text-sm text-slate-400">Загружаем данные…</div>
      ) : !team ? (
        <div className="text-sm text-red-300">
          {error ?? "Команда не найдена"}
        </div>
      ) : (
        <form onSubmit={handleSubmit}>
          <Card className="max-w-xl border-slate-800 bg-slate-950/80 backdrop-blur">
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
                  value={team.name}
                  onChange={(e) => handleChange("name", e.target.value)}
                />
              </div>

              <div className="space-y-2">
                <Label htmlFor="ip">IP</Label>
                <Input
                  id="ip"
                  value={team.ip}
                  onChange={(e) => handleChange("ip", e.target.value)}
                />
              </div>

              <div className="space-y-2">
                <Label htmlFor="token">Token</Label>
                <Input
                  id="token"
                  value={team.token}
                  onChange={(e) => handleChange("token", e.target.value)}
                />
              </div>

              <div className="space-y-2">
                <Label htmlFor="logo_path">Logo path</Label>
                <Input
                    id="logo_path"
                    value={team.logo_path}
                    onChange={(e) => handleChange("logo_path", e.target.value)}
                />
              </div>

              <div className="flex gap-4">
                <label className="flex items-center gap-2 text-xs text-slate-200">
                  <Checkbox
                    checked={team.highlighted}
                    onCheckedChange={(v) =>
                      handleChange("highlighted", Boolean(v))
                    }
                  />
                  Highlighted
                </label>

                <label className="flex items-center gap-2 text-xs text-slate-200">
                  <Checkbox
                    checked={team.active}
                    onCheckedChange={(v) => handleChange("active", Boolean(v))}
                  />
                  Active
                </label>
              </div>
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
