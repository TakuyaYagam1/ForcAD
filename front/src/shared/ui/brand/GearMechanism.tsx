import { useId } from "react";

function gearPath(radius: number, teeth: number) {
  const points = Array.from({ length: teeth * 4 }, (_, index) => {
    const angle = (index / (teeth * 4)) * Math.PI * 2;
    const r = index % 4 === 1 || index % 4 === 2 ? radius + 6 : radius;
    return `${index ? "L" : "M"}${(Math.cos(angle) * r).toFixed(2)},${(Math.sin(angle) * r).toFixed(2)}`;
  });
  return `${points.join(" ")}Z`;
}

export function GearMechanism() {
  const id = useId().replace(/:/g, "");
  return (
    <svg
      className="gear-mechanism"
      viewBox="0 0 360 220"
      fill="none"
      aria-hidden="true"
    >
      <defs>
        <linearGradient
          id={id}
          x1="-65"
          y1="-65"
          x2="65"
          y2="65"
          gradientUnits="userSpaceOnUse"
        >
          <stop stopColor="#f1d6a0" />
          <stop offset=".42" stopColor="#6b5534" />
          <stop offset=".68" stopColor="#dabb82" />
          <stop offset="1" stopColor="#3e3425" />
        </linearGradient>
      </defs>
      <path d="M24 110H336M190 20V200" stroke="#b28d50" strokeOpacity=".2" />
      <circle
        cx="190"
        cy="110"
        r="98"
        stroke="#b28d50"
        strokeOpacity=".24"
        strokeDasharray="1 8"
      />
      <circle cx="190" cy="110" r="87" stroke="#b28d50" strokeOpacity=".12" />
      {[
        { x: 190, r: 62, teeth: 24, className: "gear-large" },
        { x: 86, r: 30, teeth: 12, className: "gear-small" },
        { x: 278, r: 18, teeth: 8, className: "gear-tiny" },
      ].map((gear) => (
        <g key={gear.x} transform={`translate(${gear.x} 110)`}>
          <g className={`gear-rotor ${gear.className}`}>
            <path
              d={gearPath(gear.r, gear.teeth)}
              fill={`url(#${id})`}
              fillOpacity=".14"
              stroke={`url(#${id})`}
              strokeWidth="1.2"
            />
            <circle
              r={gear.r * 0.84}
              stroke={`url(#${id})`}
              strokeWidth="1.1"
            />
            <circle r={gear.r * 0.71} stroke={`url(#${id})`} strokeWidth=".7" />
            <circle
              r={gear.r * 0.3}
              fill="#0b121d"
              stroke={`url(#${id})`}
              strokeWidth="2"
            />
            <circle r={gear.r * 0.16} stroke={`url(#${id})`} />
            {Array.from({ length: 6 }, (_, index) => (
              <g key={index} transform={`rotate(${index * 60})`}>
                <path
                  d={`M0 ${gear.r * 0.34}L0 ${gear.r * 0.7}`}
                  stroke={`url(#${id})`}
                  strokeWidth="3"
                />
                <circle cy={gear.r * 0.52} r={gear.r * 0.035} fill="#dcc38c" />
              </g>
            ))}
          </g>
        </g>
      ))}
    </svg>
  );
}
