import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogHeader,
  DialogTitle,
  DialogTrigger,
} from "@/components/ui/dialog";
import { STATUS_META_BY_CODE } from "@/shared/config/statuses";
import { formatScore } from "@/shared/lib/formatScore";
import { useTranslation } from "react-i18next";
import { useState } from "react";
import { BrandIcon } from "./BrandIcon";

interface ServiceValue {
  status: number;
  score: number;
  checks: number;
  sla: number;
  stolen: number;
  lost: number;
  message: string;
}
export function ServiceCell({
  value,
  name,
  onClick,
}: {
  value?: ServiceValue;
  name: string;
  onClick?: () => void;
}) {
  const { t } = useTranslation("common");
  const [open, setOpen] = useState(false);
  const meta = value ? STATUS_META_BY_CODE[value.status] : undefined;
  const sla = value && value.checks > 0 ? `${formatScore(value.sla)}%` : "-";
  const trigger = (
    <button
      type="button"
      className="service-cell"
      onClick={onClick}
      aria-label={t("service.ariaLabel", {
        name,
        description: meta?.description ?? t("service.waitingData"),
      })}
    >
      <span className="service-score">
        <i
          className="status-dot"
          style={{
            background: meta?.color ?? "#626b78",
            color: meta?.color ?? "#626b78",
          }}
        />
        <span>{value ? formatScore(value.score) : "-"}</span>
      </span>
      <span className="service-meta">
        <span>SLA {sla}</span>
        <span className="flag-count">
          <b>+{value?.stolen ?? 0}</b>
          <em>−{value?.lost ?? 0}</em>
        </span>
      </span>
    </button>
  );
  if (onClick) return trigger;
  return (
    <Dialog open={open} onOpenChange={setOpen}>
      <DialogTrigger asChild>{trigger}</DialogTrigger>
      <DialogContent className="brand-dialog">
        <DialogHeader>
          <BrandIcon name="terminal" />
          <DialogTitle>{name}</DialogTitle>
          <DialogDescription>
            {meta?.description ?? t("service.noData")}
          </DialogDescription>
        </DialogHeader>
        <div className="service-detail-grid">
          <div>
            <span>{t("service.score")}</span>
            <strong>{formatScore(value?.score ?? 0)}</strong>
          </div>
          <div>
            <span>SLA</span>
            <strong>{sla}</strong>
          </div>
          <div>
            <span>{t("service.capturedFlags")}</span>
            <strong>+{formatScore(value?.stolen ?? 0)}</strong>
          </div>
          <div>
            <span>{t("service.lostFlags")}</span>
            <strong>-{formatScore(value?.lost ?? 0)}</strong>
          </div>
        </div>
        <div className="checker-message">
          <span className="eyebrow">{t("service.checkerResponse")}</span>
          <pre>{value?.message || t("service.noCheckerMessage")}</pre>
        </div>
      </DialogContent>
    </Dialog>
  );
}
