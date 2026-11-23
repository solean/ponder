import { expect, test } from "bun:test";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { renderToStaticMarkup } from "react-dom/server";
import { StaticRouter } from "react-router-dom/server";
import { LiveStatus } from "../src/components/LiveStatus";

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
