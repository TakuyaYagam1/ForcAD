import type { ReactNode } from "react";
import { BrandIcon, type BrandIconName } from "./BrandIcon";

export function StatCard({
  icon,
  label,
  value,
  detail,
}: {
  icon: BrandIconName;
  label: string;
  value: ReactNode;
  detail?: ReactNode;
}) {
  return (
    <div className="stat-card">
      <BrandIcon name={icon} />
      <div>
        <p className="stat-label">{label}</p>
        <div className="stat-value">{value}</div>
        {detail && <p className="stat-detail">{detail}</p>}
      </div>
    </div>
  );
}
