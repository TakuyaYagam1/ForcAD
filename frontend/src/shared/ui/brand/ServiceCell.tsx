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
import { ServiceDetails } from "./ServiceDetails";

export interface ServiceValue {
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
  teamId,
  taskId,
  teamName,
  snapshotRound,
}: {
  value?: ServiceValue;
  name: string;
  onClick?: () => void;
  teamId?: string;
  taskId?: string;
  teamName?: string;
  snapshotRound?: number;
}) {
  const { t } = useTranslation("common");
  const [open, setOpen] = useState(false);
  const meta = value ? STATUS_META_BY_CODE[value.status] : undefined;
  const sla = value && value.checks > 0 ? `${formatScore(value.sla)}%` : "-";
  const score = value ? formatScore(value.score) : "-";
  const flags = `+${formatScore(value?.stolen ?? 0)}/-${formatScore(value?.lost ?? 0)}`;
  const wideValues =
    (value?.score ?? 0) >= 10000 ||
    (value?.stolen ?? 0) >= 1000 ||
    (value?.lost ?? 0) >= 1000;
  const trigger = (
    <button
      type="button"
      className={`service-cell${meta ? " service-cell--active" : ""}${wideValues ? " service-cell--wide-values" : ""}`}
      style={meta ? {
        backgroundColor: meta.color,
        color: meta.foreground
      } : undefined}
      onClick={onClick}
      aria-label={t("service.ariaLabel", {
        name,
        description: meta?.description ?? t("service.waitingData"),
      })}
    >
      {!meta && (
        <span className="service-state">{t("service.noDataShort")}</span>
      )}
      <span className="service-metrics">
        <span className="service-metric service-metric--sla">
          <span>SLA</span>
          <strong>{sla}</strong>
        </span>
        <span className="service-metric">
          <span>FP</span>
          <strong>{score}</strong>
        </span>
        <span className="service-metric">
          <span>FLAGS</span>
          <strong>{flags}</strong>
        </span>
      </span>
    </button>
  );
  if (onClick) {
    return trigger;
  }
  return (
    <Dialog open={open} onOpenChange={setOpen}>
      <DialogTrigger asChild>{trigger}</DialogTrigger>
      <DialogContent className="brand-dialog service-dialog service-log-dialog">
        <DialogHeader className="service-inspector-header">
          <DialogTitle>{name}</DialogTitle>
          <DialogDescription>
            {teamName ? `${teamName} · ` : ""}{t("service.detailsDescription")}
          </DialogDescription>
        </DialogHeader>
        <ServiceDetails teamId={teamId} taskId={taskId} value={value} snapshotRound={snapshotRound} />
      </DialogContent>
    </Dialog>
  );
}
