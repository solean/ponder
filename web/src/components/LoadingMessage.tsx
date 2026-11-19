import type { ReactNode } from "react";

import { OrbLogo } from "./OrbLogo";
import "./LoadingMessage.css";

export function LoadingMark() {
  return <OrbLogo className="loading-mark" />;
}

export function LoadingLabel({ children }: { children: ReactNode }) {
  return (
    <span className="loading-label">
      <LoadingMark />
      <span>{children}</span>
    </span>
  );
}

export function LoadingMessage({
  children,
  page = false,
  className = "state",
}: {
  children: ReactNode;
  page?: boolean;
  className?: string;
}) {
  return (
    <p className={page ? `${className} loading-page` : className} role="status">
      <LoadingLabel>{children}</LoadingLabel>
    </p>
  );
}
