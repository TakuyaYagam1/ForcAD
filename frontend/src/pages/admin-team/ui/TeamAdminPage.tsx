import { useEffect, useLayoutEffect, useRef, useState } from "react";
import type { FormEvent } from "react";
import { useLocation, useNavigate, useParams } from "react-router-dom";
import type { Team } from "@/entities/team/model/types";
import {
  createTeamAdmin,
  fetchTeamAdmin,
  updateTeamAdmin,
} from "@/entities/team/api/admin";
import { apiErrorMessage } from "@/shared/lib/apiError";
import { validateTeam } from "@/shared/lib/adminValidation";
import { queryClient } from "@/shared/lib/queryClient";
import { AppShell } from "@/shared/ui/layout/AppShell";
import { BrandIcon } from "@/shared/ui/brand/BrandIcon";
import { TeamForm } from "./TeamForm";
import { normalizeUuid } from "@/shared/lib/uuid";

interface TeamAdminPageProps {
  mode?: "create" | "edit";
}

export function TeamAdminPage({ mode }: TeamAdminPageProps) {
  const { teamId: teamIdParam } = useParams<{ teamId?: string }>();
  const navigate = useNavigate();
  const location = useLocation();
  const [team, setTeam] = useState<Team | null>(null);
  const [loading, setLoading] = useState(true);
  const [saving, setSaving] = useState(false);
  const [error, setError] = useState<string | null>(null);

  const isCreate = mode === "create" || !teamIdParam;
  const teamId = !isCreate ? (normalizeUuid(teamIdParam) ?? null) : null;
  const routeKey = useRef(location.key);
  const [loadedKey, setLoadedKey] = useState<string | null>(null);
  const canSave =
    loadedKey === location.key && !!team && (isCreate || team.id === teamId);

  useLayoutEffect(() => {
    routeKey.current = location.key;
  }, [location.key]);

  useEffect(() => {
    let cancelled = false;

    async function loadTeam() {
      setLoading(true);
      setTeam(null);
      setLoadedKey(null);
      setSaving(false);
      setError(null);

      try {
        if (isCreate) {
          const emptyTeam: Team = {
            id: null,
            name: "",
            ip: "",
            token: "",
            logo_path: "",
            highlighted: false,
            active: true,
          };
          if (!cancelled) {
            setTeam(emptyTeam);
            setLoadedKey(location.key);
          }
        } else if (teamId !== null) {
          const loadedTeam = await fetchTeamAdmin(teamId);
          if (!cancelled) {
            setTeam({ ...loadedTeam, logo_path: loadedTeam.logo_path ?? "" });
            setLoadedKey(location.key);
          }
        } else if (!cancelled) {
          setError("Некорректный ID команды");
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

    void loadTeam();

    return () => {
      cancelled = true;
    };
  }, [isCreate, teamId, location.key]);

  const handleChange = <Field extends keyof Team,>(
    field: Field,
    value: Team[Field],
  ) => {
    setTeam((currentTeam) =>
      currentTeam ? { ...currentTeam, [field]: value } : currentTeam,
    );
  };

  const handleSubmit = async (event: FormEvent<HTMLFormElement>) => {
    event.preventDefault();
    if (!team || !canSave || saving) return;

    const validationError = validateTeam(team, isCreate);
    if (validationError) {
      setError(validationError);
      return;
    }

    const submittedRouteKey = location.key;
    setSaving(true);
    setError(null);

    try {
      const payload: Omit<Team, "id"> & { id?: Team["id"] } = { ...team };
      delete payload.id;

      if (isCreate) {
        const createdTeam = await createTeamAdmin(payload);
        if (routeKey.current === submittedRouteKey) {
          navigate(`/admin/team/${createdTeam.id}`, { replace: true });
        }
      } else if (teamId != null) {
        const updatedTeam = await updateTeamAdmin(teamId, payload);
        if (routeKey.current === submittedRouteKey) setTeam(updatedTeam);
      }

      await queryClient.invalidateQueries({ queryKey: ["admin-teams"] });
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
            <h1>{isCreate ? "Создание команды" : "Редактирование команды"}</h1>
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
          <TeamForm
            team={team}
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
