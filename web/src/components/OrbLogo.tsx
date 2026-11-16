// Same orb geometry as build/icon-source/Ponder.icon/Assets/orbs.svg, with a
// tighter viewBox so the mark stays legible at navigation sizes.
const orbPath = "M 0,-174 A 174,174 0 1,1 0,174 A 174,174 0 1,1 0,-174 Z M 0,-148 C -184,-138 -184,138 0,148 C -128,89 -128,-89 0,-148 Z";

export function OrbLogo({ className }: { className?: string }) {
  return (
    <svg className={className} viewBox="128 114 768 768" fill="currentColor" aria-hidden="true" focusable="false">
      <g fillRule="evenodd">
        <path d={orbPath} transform="translate(512 328)" />
        <path d={orbPath} transform="translate(316 667.5)" />
        <path d={orbPath} transform="translate(708 667.5)" />
      </g>
    </svg>
  );
}
