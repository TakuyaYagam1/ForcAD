import { useId, useState } from "react";
import { useTranslation } from "react-i18next";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { inspectAdminBackup, restoreAdminBackup } from "@/shared/api/generated/endpoints/admin/admin";
import type { BackupManifest } from "@/shared/api/generated/schemas/admin/backupManifest.zod";
import { api } from "@/shared/lib/axios";

interface GameBackupProps {
  canExport: boolean;
  canImport: boolean;
  onRestored: () => void;
}

const transferTimeout = 10 * 60 * 1000;
const maxArchiveSize = 256 * 1024 * 1024;

export function GameBackup({ canExport, canImport, onRestored }: GameBackupProps) {
  const { t, i18n } = useTranslation("admin");
  const id = useId();
  const [name, setName] = useState("");
  const [pending, setPending] = useState(false);
  const [preview, setPreview] = useState<{
    file: File;
    manifest: BackupManifest
  }>();
  const [error, setError] = useState("");

  async function exportArchive() {
    setPending(true);
    setError("");
    try {
      const slug = name.trim() || "event";
      const { data, headers } = await api.get<Blob>("/admin/backup/export/", {
        params: {
          name: slug,
        },
        responseType: "blob",
        timeout: transferTimeout,
      });
      const url = URL.createObjectURL(data);
      const link = document.createElement("a");
      const timestamp = new Date().toISOString().slice(0, 19).replace(/[-:]/g, "");
      link.href = url;
      const disposition = headers["content-disposition"];
      const filename = typeof disposition === "string"
        ? /^attachment; filename="(forcad-[a-zA-Z0-9_-]+\.tar\.gz)"$/.exec(disposition)?.[1]
        : undefined;
      link.download = filename ?? `forcad-${slug}-${timestamp}Z.tar.gz`;
      link.click();
      window.setTimeout(() => {
        URL.revokeObjectURL(url);
      }, 1000);
    } catch {
      setError(t("gameBackup.exportFailed"));
    } finally {
      setPending(false);
    }
  }

  async function inspectArchive(file: File) {
    setPreview(undefined);
    setError("");
    if (!file.name.endsWith(".tar.gz") || file.size > maxArchiveSize) {
      setError(t("gameBackup.invalidFile"));
      return;
    }
    setPending(true);
    try {
      setPreview({
        file,
        manifest: await inspectAdminBackup(file, AbortSignal.timeout(transferTimeout))
      });
    } catch {
      setError(t("gameBackup.inspectFailed"));
    } finally {
      setPending(false);
    }
  }

  async function restoreArchive() {
    if (!preview) {
      return;
    }
    setPending(true);
    setError("");
    try {
      await restoreAdminBackup(
        preview.file,
        {
          "X-Forcad-Confirm": "restore",
        },
        AbortSignal.timeout(transferTimeout),
      );
      setPreview(undefined);
      onRestored();
    } catch {
      setError(t("gameBackup.restoreFailed"));
    } finally {
      setPending(false);
    }
  }

  return (
    <section className="mt-5 space-y-3 rounded-md border p-3" aria-labelledby={`${id}-heading`}>
      <h3 id={`${id}-heading`} className="font-medium">{t("gameBackup.title")}</h3>
      <p className="text-sm text-muted-foreground">{t("gameBackup.description")}</p>
      {canExport && (
        <div className="space-y-1">
          <Label htmlFor={`${id}-name`}>{t("gameBackup.name")}</Label>
          <Input id={`${id}-name`} value={name} maxLength={64} placeholder="event"
            disabled={pending} onChange={(event) => {
              setName(event.currentTarget.value);
            }} />
        </div>
      )}
      <div className="flex flex-wrap gap-2">
        <Button type="button" variant="outline" disabled={!canExport || pending || !/^(?:[A-Za-z0-9][A-Za-z0-9_-]{0,63})?$/.test(name.trim())}
          onClick={() => {
            void exportArchive();
          }}>{t("gameBackup.export")}</Button>
        <Button type="button" variant="outline" disabled={!canImport || pending}
          onClick={() => {
            document.getElementById(`${id}-file`)?.click();
          }}>{t("gameBackup.import")}</Button>
        <input id={`${id}-file`} type="file" accept=".tar.gz,application/gzip" hidden
          onChange={(event) => {
            const file = event.currentTarget.files?.[0];
            event.currentTarget.value = "";
            if (file) {
              void inspectArchive(file);
            }
          }} />
      </div>
      {preview && (
        <div className="space-y-2 rounded-md border p-3">
          <p>{t("gameBackup.preview", {
            name: preview.manifest.name,
            round: preview.manifest.round,
            date: new Date(preview.manifest.created_at).toLocaleString(i18n.language),
          })}</p>
          <p className="text-sm text-muted-foreground">{t("gameBackup.warning")}</p>
          <div className="flex flex-wrap gap-2">
            <Button type="button" variant="outline" disabled={pending} onClick={() => {
              setPreview(undefined);
            }}>
              {t("common.cancel")}
            </Button>
            <Button type="button" disabled={pending || !canImport} onClick={() => {
              void restoreArchive();
            }}>
              {t("gameBackup.confirm")}
            </Button>
          </div>
        </div>
      )}
      {pending && <p role="status">{t("gameBackup.pending")}</p>}
      {error && <p role="alert" className="text-sm text-red-300">{error}</p>}
    </section>
  );
}
