import type { MouseEvent, ReactNode } from "react";

import { api } from "../lib/api";

function openInBrowser(event: MouseEvent<HTMLAnchorElement>) {
  if (event.button !== 0 || event.metaKey || event.ctrlKey || event.shiftKey || event.altKey) return;
  event.preventDefault();
  const href = event.currentTarget.href;
  void api.openExternalURL(href).catch(() => {
    window.open(href, "_blank", "noopener,noreferrer");
  });
}

/**
 * A link to a web page outside the app. The desktop webview cannot open new
 * windows, so a plain click asks the app to open the system browser.
 */
export function ExternalLink({
  href,
  className = "text-link",
  children,
}: {
  href: string;
  className?: string;
  children: ReactNode;
}) {
  return (
    <a className={className} href={href} target="_blank" rel="noreferrer" onClick={openInBrowser}>
      {children}
    </a>
  );
}
