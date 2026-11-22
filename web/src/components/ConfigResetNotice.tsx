import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";

import { api } from "../lib/api";

function fileName(path: string): string {
  return path.split(/[\\/]/).pop() ?? path;
}

/**
 * App-wide notice shown when config.json couldn't be read at startup. Ponder
 * moved the damaged file aside and fell back to default settings; without this
 * the reset would go unnoticed until something behaved differently.
 */
export function ConfigResetNotice() {
  const queryClient = useQueryClient();
  // Shares the titlebar's runtime-status query, which does the polling.
  const { data } = useQuery({ queryKey: ["runtime-status"], queryFn: api.runtimeStatus });
  const dismiss = useMutation({
    mutationFn: api.dismissConfigReset,
    onSuccess: (status) => queryClient.setQueryData(["runtime-status"], status),
  });
  const reveal = useMutation({ mutationFn: (path: string) => api.revealPath(path) });

  const reset = data?.configReset;
  if (!reset) return null;
  const backupPath = reset.backupPath;
  const canReveal = Boolean(data.capabilities?.reveal && backupPath);

  return (
    <section className="config-reset-notice" role="alert" aria-labelledby="config-reset-notice-title">
      <div className="config-reset-notice-text">
        <span className="config-reset-notice-kicker">Settings reset</span>
        <h2 id="config-reset-notice-title">Your settings couldn’t be read, so Ponder is using the defaults</h2>
        {backupPath ? (
          <p>
            The damaged file was kept as <code title={backupPath}>{fileName(backupPath)}</code> in the same folder.
            Re-apply anything you had changed in Settings.
          </p>
        ) : (
          <p>The damaged file couldn’t be backed up. Re-apply anything you had changed in Settings.</p>
        )}
        <p className="config-reset-notice-reason">{reset.reason}</p>
      </div>
      <div className="config-reset-notice-actions">
        {canReveal && backupPath ? (
          <button
            type="button"
            className={`settings-button${reveal.error ? " is-failed" : ""}`}
            onClick={() => reveal.mutate(backupPath)}
            disabled={reveal.isPending}
            title={reveal.error ? `Could not show the file: ${(reveal.error as Error).message}` : undefined}
          >
            Show in Finder
          </button>
        ) : null}
        <button
          type="button"
          className="settings-button"
          onClick={() => dismiss.mutate()}
          disabled={dismiss.isPending}
        >
          Dismiss
        </button>
      </div>
    </section>
  );
}
