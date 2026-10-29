import type { ReactNode } from "react";

export function StatCard({
  label,
  value,
  detail,
}: {
  label: string;
  value: ReactNode;
  detail?: ReactNode;
}) {
  return (
    <div className="stat-card">
      <div>
        <p className="stat-label">{label}</p>
        <div className="stat-value">{value}</div>
        {detail && <p className="stat-detail">{detail}</p>}
      </div>
    </div>
  );
}
