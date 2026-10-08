import { useState } from "react";
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
import { BrandIcon } from "./BrandIcon";

interface ServiceValue {
  status: number;
  score: number;
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
  const [open, setOpen] = useState(false);
  const meta = value ? STATUS_META_BY_CODE[value.status] : undefined;
  const trigger = (
    <button
      type="button"
      className="service-cell"
      onClick={onClick}
      aria-label={`${name}: ${meta?.description ?? "ожидание данных"}. Подробнее`}
    >
      <span className="service-score">
        <i
          className="status-dot"
          style={{
            background: meta?.color ?? "#626b78",
            color: meta?.color ?? "#626b78",
          }}
        />
        <span>{value ? formatScore(value.score) : "—"}</span>
      </span>
      <span className="service-meta">
        <span>SLA {value ? formatScore(value.sla) : "—"}%</span>
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
            {meta?.description ?? "Данные сервиса ещё не получены"}
          </DialogDescription>
        </DialogHeader>
        <div className="service-detail-grid">
          <div>
            <span>Очки</span>
            <strong>{formatScore(value?.score ?? 0)}</strong>
          </div>
          <div>
            <span>SLA</span>
            <strong>{(value?.sla ?? 0).toFixed(2)}%</strong>
          </div>
          <div>
            <span>Захвачено флагов</span>
            <strong>+{value?.stolen ?? 0}</strong>
          </div>
          <div>
            <span>Потеряно флагов</span>
            <strong>−{value?.lost ?? 0}</strong>
          </div>
        </div>
        <div className="checker-message">
          <span className="eyebrow">Ответ чекера</span>
          <pre>{value?.message || "Нет сообщения"}</pre>
        </div>
      </DialogContent>
    </Dialog>
  );
}
