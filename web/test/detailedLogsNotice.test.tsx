import { expect, test } from "bun:test";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { renderToStaticMarkup } from "react-dom/server";
import { DetailedLogsNotice } from "../src/components/DetailedLogsNotice";

async function renderWith(detailedLogs?: "enabled" | "disabled") {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  await client.fetchQuery({ queryKey: ["runtime-status"], queryFn: () => ({ liveRunning: true, detailedLogs }) });
  return renderToStaticMarkup(
    <QueryClientProvider client={client}>
      <DetailedLogsNotice />
    </QueryClientProvider>,
  );
}

test("notice explains how to turn on Detailed Logs only when they are off", async () => {
  const off = await renderWith("disabled");
  expect(off).toContain("Turn on Detailed Logs in Arena");
  expect(off).toContain("Detailed Logs (Plugin Support)");
  expect(off).toContain("Restart Arena.");

  expect(await renderWith("enabled")).toBe("");
  expect(await renderWith(undefined)).toBe("");
});
