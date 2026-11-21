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

export function DetailedLogsHelpLink({ label = "How to enable Detailed Logs in MTGA" }: { label?: string }) {
  return (
    <a className="text-link" href={DETAILED_LOGS_HELP_URL} target="_blank" rel="noreferrer" onClick={openInBrowser}>
      {label}
    </a>
  );
}

/**
 * App-wide setup checklist shown when the Arena log says Detailed Logs are
 * off. Without them Arena writes no match, draft, or rank data, so every
 * page would otherwise just look empty.
 */
export function DetailedLogsNotice() {
  // Shares the titlebar's runtime-status query, which does the polling.
  const { data } = useQuery({ queryKey: ["runtime-status"], queryFn: api.runtimeStatus });
  if (data?.detailedLogs !== "disabled") return null;

  return (
    <section className="detailed-logs-notice" role="alert" aria-labelledby="detailed-logs-notice-title">
      <div className="detailed-logs-notice-head">
        <div className="detailed-logs-notice-intro">
          <span className="detailed-logs-notice-kicker">One-time setup</span>
          <h2 id="detailed-logs-notice-title">Turn on Detailed Logs in Arena</h2>
          <p>
            Arena only records matches, drafts, and rank changes for trackers when this setting is on. Until then,
            Ponder has nothing to read.
          </p>
        </div>
        <DetailedLogsHelpLink label="Arena help article ↗" />
      </div>
      <ol className="detailed-logs-notice-steps">
        <li>
          <span aria-hidden="true">01</span>
          <p>
            In Arena, open <strong>Options</strong> (the gear), then <strong>Account</strong>.
          </p>
        </li>
        <li>
          <span aria-hidden="true">02</span>
          <p>
            Check <strong>Detailed Logs (Plugin Support)</strong>.
          </p>
        </li>
        <li>
          <span aria-hidden="true">03</span>
          <p>
            <strong>Restart Arena.</strong> The setting only takes effect on a fresh launch.
          </p>
        </li>
      </ol>
      <p className="detailed-logs-notice-footer">
        <svg viewBox="0 0 24 24" aria-hidden="true">
          <path d="M21 12a9 9 0 1 1-3-6.7" />
          <path d="M21 4v5h-5" />
        </svg>
        Watching Player.log. This goes away on its own once Arena restarts with the setting on.
      </p>
    </section>
  );
}
