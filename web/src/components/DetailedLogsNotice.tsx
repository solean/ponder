import type { MouseEvent } from "react";
import { useQuery } from "@tanstack/react-query";

import { api } from "../lib/api";

const DETAILED_LOGS_HELP_URL =
  "https://mtgarena-support.wizards.com/hc/en-us/articles/360000726823-Creating-Log-Files-on-PC-Mac-Steam";

function openInBrowser(event: MouseEvent<HTMLAnchorElement>) {
  if (event.button !== 0 || event.metaKey || event.ctrlKey || event.shiftKey || event.altKey) return;
  event.preventDefault();
  void api.openExternalURL(DETAILED_LOGS_HELP_URL).catch(() => {
    window.open(DETAILED_LOGS_HELP_URL, "_blank", "noopener,noreferrer");
  });
}

export function DetailedLogsHelpLink() {
  return (
    <a className="text-link" href={DETAILED_LOGS_HELP_URL} target="_blank" rel="noreferrer" onClick={openInBrowser}>
      How to enable Detailed Logs in MTGA
    </a>
  );
}

/**
 * App-wide warning shown when the Arena log says Detailed Logs are off.
 * Without them Arena writes no match, draft, or rank data, so every page
 * would otherwise just look empty.
 */
export function DetailedLogsNotice() {
  // Shares the titlebar's runtime-status query, which does the polling.
  const { data } = useQuery({ queryKey: ["runtime-status"], queryFn: api.runtimeStatus });
  if (data?.detailedLogs !== "disabled") return null;

  return (
    <section className="detailed-logs-notice" role="alert" aria-labelledby="detailed-logs-notice-title">
      <h2 id="detailed-logs-notice-title">Detailed Logs are off in Arena</h2>
      <p>
        Arena isn&rsquo;t recording matches, drafts, or rank changes, so Ponder has nothing to track. In Arena, open{" "}
        <strong>Options → Account</strong>, check <strong>Detailed Logs (Plugin Support)</strong>, then restart Arena.
      </p>
      <DetailedLogsHelpLink />
    </section>
  );
}
