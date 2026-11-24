// One stroke icon system: 24-unit grid, 1.75 stroke, round caps.

const PATHS = {
  refresh: ["M19.5 12a7.5 7.5 0 1 1-2.2-5.3L19.5 9", "M19.5 4v5h-5"],
  left: ["M14.5 6l-6 6 6 6"],
  right: ["M9.5 6l6 6-6 6"],
  alert: ["M12 3.5a8.5 8.5 0 1 0 0 17 8.5 8.5 0 0 0 0-17z", "M12 7.75v5.25", "M12 16.25v.01"],
  plus: ["M12 5.5v13", "M5.5 12h13"],
  check: ["M5.5 12.5l4.25 4.25L18.5 7.5"],
} as const;

export type IconName = keyof typeof PATHS;

export function Icon(p: { name: IconName; class?: string }) {
  return (
    <svg
      class={p.class}
      viewBox="0 0 24 24"
      width="16"
      height="16"
      fill="none"
      stroke="currentColor"
      stroke-width="1.75"
      stroke-linecap="round"
      stroke-linejoin="round"
      aria-hidden="true"
      focusable="false"
    >
      {PATHS[p.name].map((d) => (
        <path key={d} d={d} />
      ))}
    </svg>
  );
}
