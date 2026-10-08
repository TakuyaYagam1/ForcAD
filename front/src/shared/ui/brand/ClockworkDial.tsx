import { useId } from "react";

/** Decorative SVG layers keep the dial and astronomy rings independently animated. */
export function ClockworkDial() {
  const id = `clock-${useId().replace(/:/g, "")}`;
  const numerals = [
    "XII",
    "I",
    "II",
    "III",
    "IV",
    "V",
    "VI",
    "VII",
    "VIII",
    "IX",
    "X",
    "XI",
  ];
  return (
    <svg
      className="clockwork-dial"
      viewBox="0 0 600 600"
      fill="none"
      aria-hidden="true"
    >
      <defs>
        <linearGradient
          id={id}
          x1="80"
          y1="60"
          x2="480"
          y2="560"
          gradientUnits="userSpaceOnUse"
        >
          <stop stopColor="#f9c979" />
          <stop offset=".32" stopColor="#8d622e" />
          <stop offset=".6" stopColor="#e9b567" />
          <stop offset="1" stopColor="#67431e" />
        </linearGradient>
      </defs>
      <g stroke={`url(#${id})`}>
        {[275, 262, 242, 217, 208, 190, 180, 141, 134, 106, 60, 54, 22].map(
          (r) => (
            <circle
              key={r}
              cx="300"
              cy="300"
              r={r}
              strokeWidth={r === 208 ? 3 : 1}
              opacity={r > 242 ? 0.5 : 0.8}
            />
          ),
        )}
        <g className="clock-astronomy">
          {Array.from({ length: 36 }, (_, i) => (
            <g key={i} transform={`rotate(${i * 10} 300 300)`}>
              <path d="M300 25V75" opacity=".45" />
              <circle cx="300" cy="38" r={i % 3 === 0 ? 3 : 1.5} opacity=".7" />
            </g>
          ))}
          <ellipse
            cx="300"
            cy="300"
            rx="280"
            ry="85"
            transform="rotate(-32 300 300)"
            opacity=".3"
          />
          <ellipse
            cx="300"
            cy="300"
            rx="280"
            ry="140"
            transform="rotate(40 300 300)"
            opacity=".3"
          />
        </g>
        <g className="clock-cog">
          {Array.from({ length: 60 }, (_, i) => (
            <g key={i} transform={`rotate(${i * 6} 300 300)`}>
              <path d="M295 83V72H305V83" />
              <path d="M300 108V117" strokeWidth={i % 5 === 0 ? 2 : 1} />
              <circle cx="300" cy="97" r="1.6" />
            </g>
          ))}
          {Array.from({ length: 12 }, (_, i) => (
            <g key={i} transform={`rotate(${i * 30} 300 300)`}>
              <path
                d="M291 244L282 174Q300 151 318 174L309 244Z"
                fill={`url(#${id})`}
                fillOpacity=".13"
              />
              <path
                d="M290 220Q269 202 283 187Q298 171 307 188Q317 208 297 207Q286 208 291 198"
                strokeWidth="1.2"
              />
              <circle cx="300" cy="231" r="3" />
            </g>
          ))}
        </g>
        {numerals.map((n, i) => {
          const a = ((i * 30 - 90) * Math.PI) / 180;
          return (
            <text
              key={n}
              x={300 + 161 * Math.cos(a)}
              y={300 + 161 * Math.sin(a) + 5}
              textAnchor="middle"
              fill={`url(#${id})`}
              stroke="none"
              fontFamily="Georgia, serif"
              fontSize="18"
            >
              {n}
            </text>
          );
        })}
        <g className="clock-hand">
          <path
            d="M294 310L300 126L306 310Z"
            fill={`url(#${id})`}
            fillOpacity=".55"
          />
        </g>
        <path
          d="M292 303L418 235L306 313Z"
          fill={`url(#${id})`}
          fillOpacity=".5"
        />
        <circle cx="300" cy="300" r="12" fill="#06141d" />
        <circle cx="300" cy="300" r="6" />
      </g>
    </svg>
  );
}
