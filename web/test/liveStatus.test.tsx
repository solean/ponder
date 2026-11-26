import { expect, test } from "bun:test";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { renderToStaticMarkup } from "react-dom/server";
import { StaticRouter } from "react-router-dom/server";
import { LiveStatus, trackerSummary } from "../src/components/LiveStatus";
import type { RuntimeStatus } from "../src/lib/types";

test("a failed status poll hides cached tracker activity until the next successful poll", async () => {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  const queryKey = ["runtime-status"];
  const render = () => renderToStaticMarkup(
    <QueryClientProvider client={client}>
      <StaticRouter location="/"><LiveStatus /></StaticRouter>
    </QueryClientProvider>,
  );

  expect(render()).toContain("checking status");
  await client.fetchQuery({ queryKey, queryFn: () => ({ liveRunning: true, liveLastTickAt: new Date().toISOString() }) });
  expect(render()).toContain("live tracker on");

  await expect(client.fetchQuery({ queryKey, queryFn: () => Promise.reject(new Error("offline")) })).rejects.toThrow("offline");
  const failed = render();
  expect(failed).toContain("live tracker unknown");
  expect(failed).toContain("sync-dot is-unknown");
  expect(failed).not.toContain("is-live");
  expect(failed).not.toContain("live-status-detail");

  await client.fetchQuery({ queryKey, queryFn: () => ({ liveRunning: false }) });
  const recovered = render();
  expect(recovered).toContain("live tracker off");
  expect(recovered).toContain("never synced");
  expect(recovered).not.toContain("is-unknown");
});

test("titlebar flags Detailed Logs being off ahead of the tracker state", async () => {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  await client.fetchQuery({ queryKey: ["runtime-status"], queryFn: () => ({ liveRunning: true, detailedLogs: "disabled" }) });
  const html = renderToStaticMarkup(
    <QueryClientProvider client={client}>
      <StaticRouter location="/"><LiveStatus /></StaticRouter>
    </QueryClientProvider>,
  );
  expect(html).toContain("detailed logs off");
  expect(html).toContain("sync-dot is-warning");
  expect(html).not.toContain("is-live");
});

test("the titlebar shows that tracking is waiting for the Arena log", () => {
  const client = new QueryClient();
  client.setQueryData(["runtime-status"], { liveRunning: true, activeLogPathExists: false });
  client.setQueryData(["live"], { live: { match: { id: 1, eventName: "", opponent: "Rival" } } });
  const render = () => renderToStaticMarkup(
    <QueryClientProvider client={client}>
      <StaticRouter location="/"><LiveStatus /></StaticRouter>
    </QueryClientProvider>,
  );

  const waiting = render();
  expect(waiting).toContain("Waiting for MTGA log");
  expect(waiting).toContain('href="/settings"');
  expect(waiting).not.toContain("Rival");

  client.setQueryData(["runtime-status"], { liveRunning: true, activeLogPathExists: true });
  const tracking = render();
  expect(tracking).toContain("vs Rival");
  expect(tracking).not.toContain("Waiting for MTGA log");
});

test("a tracking error reads as Needs attention, like the menu bar", () => {
  for (const liveRunning of [true, false]) {
    const status = { liveRunning, activeLogPathExists: true, lastError: "open log: permission denied" } as RuntimeStatus;
    const summary = trackerSummary(status, false);
    expect(summary.state).toBe("Needs attention");
    expect(summary.detail).toBe("open log: permission denied");
    expect(summary.dotClass).toBe("sync-dot is-error");
  }

  // As in trackingMenuBarStatus, the error outranks a missing log and Detailed Logs being off.
  const both = { liveRunning: true, activeLogPathExists: false, detailedLogs: "disabled", lastError: "boom" } as RuntimeStatus;
  expect(trackerSummary(both, false).state).toBe("Needs attention");
});

test("the titlebar shows a tracking error over the match in progress and links to settings", () => {
  const client = new QueryClient();
  client.setQueryData(["runtime-status"], { liveRunning: true, activeLogPathExists: true, lastError: "open log: permission denied" });
  client.setQueryData(["live"], { live: { match: { id: 1, eventName: "", opponent: "Rival" }, gameNumber: 1, turnNumber: 3 } });
  const render = () => renderToStaticMarkup(
    <QueryClientProvider client={client}>
      <StaticRouter location="/"><LiveStatus /></StaticRouter>
    </QueryClientProvider>,
  );

  const failing = render();
  expect(failing).toContain("Needs attention");
  expect(failing).toContain("open log: permission denied");
  expect(failing).toContain("live-status is-attention");
  expect(failing).toContain('href="/settings"');
  expect(failing).not.toContain("Rival");

  client.setQueryData(["runtime-status"], { liveRunning: true, activeLogPathExists: true });
  const tracking = render();
  expect(tracking).toContain("vs Rival");
  expect(tracking).not.toContain("Needs attention");
});
