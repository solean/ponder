import type { MouseEvent } from "react";
import { useQuery } from "@tanstack/react-query";

import { api } from "../lib/api";
import { APP_NAME } from "../lib/branding";
import type { UpdateCheck } from "../lib/types";

/** The release worth announcing, or null when there is nothing to open. */
function updateToOffer(check: UpdateCheck | undefined) {
  if (!check?.updateAvailable || !check.releaseUrl?.startsWith("https://")) return null;
  return { version: check.latestVersion, url: check.releaseUrl };
}

/**
 * Titlebar notice for a newer release, so users who never open Settings still
 * learn about it. Links to the release page in the default browser.
 */
export function UpdateIndicator() {
  // Shares the titlebar's runtime-status query, which does the polling.
  const { data } = useQuery({ queryKey: ["runtime-status"], queryFn: api.runtimeStatus });
  const update = updateToOffer(data?.updateCheck);
  if (!update) return null;

  const openRelease = (event: MouseEvent<HTMLAnchorElement>) => {
    if (event.button !== 0 || event.metaKey || event.ctrlKey || event.shiftKey || event.altKey) return;
    event.preventDefault();
    void api.openExternalURL(update.url).catch(() => {
      window.open(update.url, "_blank", "noopener,noreferrer");
    });
  };
  const release = update.version ? `${APP_NAME} ${update.version}` : `A new version of ${APP_NAME}`;

  return (
    <a
      className="update-indicator"
      href={update.url}
      target="_blank"
      rel="noreferrer"
      onClick={openRelease}
      aria-label={`${release} is available. Open the release page.`}
      title={`${release} is available. Open the release page.`}
    >
      <svg viewBox="0 0 24 24" aria-hidden="true">
        <path d="M12 19V5" />
        <path d="m5 12 7-7 7 7" />
      </svg>
      <span className="update-indicator-label">Update</span>
      {update.version ? <span className="update-indicator-version">{update.version}</span> : null}
    </a>
  );
}
