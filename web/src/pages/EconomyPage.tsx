import ReactECharts from "echarts-for-react";
import { useQuery } from "@tanstack/react-query";
import { useMemo, type ReactNode } from "react";
import { Link } from "react-router-dom";

import { EventLabel } from "../components/EventLabel";
import { StatusMessage } from "../components/StatusMessage";
import { api } from "../lib/api";
import {
  eventEntryLabel as entryLabel,
  eventRewardLabel as rewardLabel,
  formatEconomyDelta as formatDelta,
  humanizeInventoryKey,
} from "../lib/economy";
import { parseEventName } from "../lib/events";
import { formatDateTime, formatRelativeTime } from "../lib/format";
import { useEventSets } from "../lib/useEventSets";
import { useTheme } from "../lib/theme";
import type { EconomySnapshot, EventRunEconomy, WildcardBalance } from "../lib/types";

const integerFormatter = new Intl.NumberFormat(undefined, { maximumFractionDigits: 0 });
const decimalFormatter = new Intl.NumberFormat(undefined, {
  minimumFractionDigits: 1,
  maximumFractionDigits: 1,
});
const shortDateFormatter = new Intl.DateTimeFormat(undefined, {
  month: "short",
  day: "numeric",
});
const MAX_RECENT_CHANGES = 12;

const WILDCARD_LABELS: Array<{ key: keyof WildcardBalance; label: string }> = [
  { key: "common", label: "Common" },
  { key: "uncommon", label: "Uncommon" },
  { key: "rare", label: "Rare" },
  { key: "mythic", label: "Mythic" },
];

type SnapshotDelta = {
  current: EconomySnapshot;
  gold: number;
  gems: number;
  vaultProgress: number;
  wildcards: number;
};

function wildcardTotal(wildcards: WildcardBalance): number {
  return wildcards.common + wildcards.uncommon + wildcards.rare + wildcards.mythic;
}

function economyChanges(history: EconomySnapshot[]): SnapshotDelta[] {
  const changes: SnapshotDelta[] = [];
  for (let index = 1; index < history.length; index += 1) {
    const previous = history[index - 1];
    const current = history[index];
    const delta = {
      current,
      gold: current.gold - previous.gold,
      gems: current.gems - previous.gems,
      vaultProgress: current.vaultProgress - previous.vaultProgress,
      wildcards: wildcardTotal(current.wildcards) - wildcardTotal(previous.wildcards),
    };
    if (
      delta.gold !== 0 ||
      delta.gems !== 0 ||
      delta.vaultProgress !== 0 ||
      delta.wildcards !== 0 ||
      current.changeSources.length > 0
    ) {
      changes.push(delta);
    }
  }
  return changes.slice(-MAX_RECENT_CHANGES).reverse();
}


function formatVaultDelta(value: number): string {
  if (value === 0) return "—";
  return `${value > 0 ? "+" : "−"}${decimalFormatter.format(Math.abs(value) / 10)}%`;
}

function deltaTone(value: number): string {
  if (value > 0) return "economy-delta economy-delta--positive";
  if (value < 0) return "economy-delta economy-delta--negative";
  return "economy-delta";
}


function sourceLabel(sources: string[]): string {
  if (sources.length === 0) return "Balance update";
  return sources.map(humanizeInventoryKey).join(", ");
}

const TRANSACTION_SOURCE_LABELS: Record<string, string> = {
  EventPayEntry: "Event entry",
  EventRefund: "Event refund",
  EventReward: "Event prizes",
  EventGrantCardPool: "Draft card pool",
  QuestReward: "Quest reward",
  DailyReward: "Daily reward",
  WeeklyReward: "Weekly reward",
  LoginGrant: "Login grant",
  RedeemVoucherReward: "Voucher redemption",
  PurchasedProductDelivery: "Store purchase",
  BoosterOpen: "Booster opened",
  VaultOpen: "Vault opened",
  WildCardRedemption: "Wildcard crafted",
  RankedSeasonReward: "Ranked season reward",
  BattlePassReward: "Mastery pass reward",
  BattlePassLevelUp: "Mastery level",
};

function transactionSourceLabel(source: string): string {
  return TRANSACTION_SOURCE_LABELS[source] ?? humanizeInventoryKey(source);
}

function eventRunLabel(run: EventRunEconomy): string {
  const parsed = parseEventName(run.eventName);
  const kind = parsed.kindLabel === "Unknown event" ? "Event" : parsed.kindLabel;
  return parsed.setCode ? `${parsed.setCode} ${kind}` : kind;
}


type EventValueGroup = {
  key: string;
  label: string;
  runs: number;
  totalWins: number;
  totalLosses: number;
  netGold: number;
  netGems: number;
  packs: number;
  runsWithRewardData: number;
  runsCoveredEntry: number;
};

function groupEventRuns(runs: EventRunEconomy[]): EventValueGroup[] {
  const groups = new Map<string, EventValueGroup>();
  for (const run of runs) {
    const key = `${run.eventType}:${run.setCode}`;
    let group = groups.get(key);
    if (!group) {
      group = {
        key,
        label: eventRunLabel(run),
        runs: 0,
        totalWins: 0,
        totalLosses: 0,
        netGold: 0,
        netGems: 0,
        packs: 0,
        runsWithRewardData: 0,
        runsCoveredEntry: 0,
      };
      groups.set(key, group);
    }
    group.runs += 1;
    group.totalWins += run.wins;
    group.totalLosses += run.losses;
    group.netGold += run.netGold;
    group.netGems += run.netGems;
    group.packs += run.rewardBoosters.reduce((sum, booster) => sum + booster.count, 0);
    if (run.linkConfidence !== "none") {
      group.runsWithRewardData += 1;
      if (run.netGold >= 0 && run.netGems >= 0) group.runsCoveredEntry += 1;
    }
  }
  return [...groups.values()].sort((left, right) => right.runs - left.runs);
}

function confidenceBadge(confidence: EventRunEconomy["linkConfidence"]): ReactNode {
  if (confidence === "exact") {
    return <span className="analytics-confidence is-exact">exact</span>;
  }
  if (confidence === "inferred") {
    return (
      <span
        className="analytics-confidence is-derived"
        title="Rewards were matched to this run by time proximity, not an exact log reference."
      >
        inferred
      </span>
    );
  }
  return (
    <span className="analytics-confidence" title="No reward data was captured for this run.">
      no data
    </span>
  );
}

export function EconomyPage() {
  const { mode, scheme } = useTheme();
  const { data, isLoading, error } = useQuery({
    queryKey: ["economy"],
    queryFn: api.economy,
    refetchInterval: 60_000,
  });

  const changes = useMemo(() => economyChanges(data?.history ?? []), [data?.history]);
  const recentTransactions = useMemo(
    () => (data?.transactions ?? []).slice(-MAX_RECENT_CHANGES).reverse(),
    [data?.transactions],
  );
  const eventRuns = data?.eventRuns ?? [];
  const eventGroups = useMemo(() => groupEventRuns(data?.eventRuns ?? []), [data?.eventRuns]);
  const { lookup: setLookup } = useEventSets(eventRuns.map((run) => run.eventName));
  const chartPoints = useMemo(
    () =>
      (data?.history ?? []).flatMap((snapshot) => {
        const timestamp = new Date(snapshot.observedAt).getTime();
        return Number.isFinite(timestamp) ? [{ snapshot, timestamp }] : [];
      }),
    [data?.history],
  );
  const chartOption = useMemo(() => {
    if (chartPoints.length < 2) return null;
    const dark = mode === "dark";
    const textColor = dark ? "#b8aaa0" : "#5f5148";
    const lineColor = dark ? "rgba(255,255,255,0.10)" : "rgba(30,20,12,0.12)";
    const tooltipBackground = dark ? "rgba(12, 10, 9, 0.97)" : "rgba(255, 252, 248, 0.98)";
    const tooltipBorder = dark ? "rgba(255, 255, 255, 0.20)" : "rgba(35, 25, 18, 0.20)";
    return {
      animation: false,
      aria: {
        enabled: true,
        description: "Gold and gem balances over time.",
      },
      color: ["#e69f00", "#56b4e9"],
      grid: { top: 34, right: 56, bottom: 34, left: 58 },
      legend: {
        top: 0,
        textStyle: { color: textColor, fontFamily: "Inter, sans-serif" },
      },
      tooltip: {
        trigger: "axis",
        backgroundColor: tooltipBackground,
        borderColor: tooltipBorder,
        textStyle: { color: dark ? "#f4ece5" : "#291f19" },
        valueFormatter: (value: number) => integerFormatter.format(value),
      },
      xAxis: {
        type: "time",
        boundaryGap: false,
        axisLine: { lineStyle: { color: lineColor } },
        axisLabel: {
          color: textColor,
          formatter: (value: number) => shortDateFormatter.format(new Date(value)),
        },
        splitLine: { show: false },
      },
      yAxis: [
        {
          type: "value",
          name: "Gold",
          nameTextStyle: { color: textColor },
          axisLabel: { color: textColor, formatter: (value: number) => integerFormatter.format(value) },
          splitLine: { lineStyle: { color: lineColor } },
        },
        {
          type: "value",
          name: "Gems",
          nameTextStyle: { color: textColor },
          axisLabel: { color: textColor, formatter: (value: number) => integerFormatter.format(value) },
          splitLine: { show: false },
        },
      ],
      series: [
        {
          name: "Gold",
          type: "line",
          showSymbol: chartPoints.length < 16,
          symbolSize: 7,
          lineStyle: { width: 2.5 },
          data: chartPoints.map(({ snapshot, timestamp }) => [timestamp, snapshot.gold]),
        },
        {
          name: "Gems",
          type: "line",
          yAxisIndex: 1,
          showSymbol: chartPoints.length < 16,
          symbolSize: 7,
          lineStyle: { width: 2.5 },
          data: chartPoints.map(({ snapshot, timestamp }) => [timestamp, snapshot.gems]),
        },
      ],
    };
  }, [chartPoints, mode]);

  if (isLoading) return <StatusMessage>Loading economy data…</StatusMessage>;
  if (error) return <StatusMessage tone="error">{(error as Error).message}</StatusMessage>;

  const latest = data?.latest;
  if (!latest) {
    return (
      <div className="stack-lg economy-page">
        <header className="page-heading">
          <div>
            <p className="eyebrow">Arena inventory</p>
            <h2>Economy</h2>
          </div>
        </header>
        <section className="panel empty-panel">
          <h3>No economy snapshots yet</h3>
          <p>
            Import an Arena log containing an <code>InventoryInfo</code> response. Ponder will then
            track gold, gems, wildcards, vault progress, boosters, and tokens automatically.
          </p>
          <Link to="/settings" className="control-button">
            Import Arena Logs
          </Link>
        </section>
      </div>
    );
  }

  const previous = data.history.length > 1 ? data.history[data.history.length - 2] : null;
  const currentWildcardTotal = wildcardTotal(latest.wildcards);
  const wildcardDelta = previous ? currentWildcardTotal - wildcardTotal(previous.wildcards) : 0;
  const tokenEntries = Object.entries(latest.customTokens)
    .filter(([, count]) => count > 0)
    .sort(([left], [right]) => left.localeCompare(right));
  const voucherEntries = Object.entries(latest.vouchers)
    .filter(([, count]) => count > 0)
    .sort(([left], [right]) => left.localeCompare(right));
  const boosterTotal = latest.boosters.reduce((sum, booster) => sum + booster.count, 0);

  return (
    <div className="stack-lg economy-page">
      <header className="page-heading economy-heading">
        <div>
          <p className="eyebrow">Arena inventory</p>
          <h2>Economy</h2>
        </div>
        <div className="economy-freshness">
          <span>Last snapshot</span>
          <strong title={formatDateTime(latest.observedAt)}>
            {latest.observedAt ? formatRelativeTime(latest.observedAt) : "time unavailable"}
          </strong>
          <small>{integerFormatter.format(data.history.length)} snapshots tracked</small>
        </div>
      </header>

      <section className="metrics-grid" aria-label="Current economy balances">
        <article className="metric-card economy-metric economy-metric--gold">
          <p>Gold</p>
          <div className="metric-value">{integerFormatter.format(latest.gold)}</div>
          <small className={previous ? deltaTone(latest.gold - previous.gold) : "metric-sub"}>
            {previous ? `${formatDelta(latest.gold - previous.gold)} since prior snapshot` : "current balance"}
          </small>
        </article>
        <article className="metric-card economy-metric economy-metric--gems">
          <p>Gems</p>
          <div className="metric-value">{integerFormatter.format(latest.gems)}</div>
          <small className={previous ? deltaTone(latest.gems - previous.gems) : "metric-sub"}>
            {previous ? `${formatDelta(latest.gems - previous.gems)} since prior snapshot` : "current balance"}
          </small>
        </article>
        <article className="metric-card economy-metric">
          <p>Vault progress</p>
          <div className="metric-value">{decimalFormatter.format(latest.vaultProgress / 10)}%</div>
          <div className="economy-progress" aria-hidden="true">
            <span style={{ width: `${Math.min(latest.vaultProgress / 10, 100)}%` }} />
          </div>
          <small className="metric-sub">{latest.vaultProgress >= 1000 ? "vault ready to open" : "toward next vault"}</small>
        </article>
        <article className="metric-card economy-metric">
          <p>Wildcards</p>
          <div className="metric-value">{integerFormatter.format(currentWildcardTotal)}</div>
          <small className={previous ? deltaTone(wildcardDelta) : "metric-sub"}>
            {previous ? `${formatDelta(wildcardDelta)} since prior snapshot` : "across all rarities"}
          </small>
        </article>
      </section>

      <section className="panel economy-wildcards" aria-labelledby="wildcard-heading">
        <div className="panel-head">
          <div>
            <h3 id="wildcard-heading">Wildcard inventory</h3>
            <p>Current craftable balances by rarity</p>
          </div>
          <span className="economy-track-position">
            Track position <strong>{integerFormatter.format(latest.wildcardTrackPosition)}</strong>
          </span>
        </div>
        <div className="wildcard-grid">
          {WILDCARD_LABELS.map(({ key, label }) => (
            <article key={key} className={`wildcard-card wildcard-card--${key}`}>
              <span className="rarity-gem" aria-hidden="true" />
              <div>
                <p>{label}</p>
                <strong>{integerFormatter.format(latest.wildcards[key])}</strong>
              </div>
            </article>
          ))}
        </div>
      </section>

      <section className="panel economy-chart-panel" aria-labelledby="balance-history-heading">
        <div className="panel-head">
          <div>
            <h3 id="balance-history-heading">Balance history</h3>
            <p>Gold and gems from Arena inventory snapshots</p>
          </div>
        </div>
        {chartOption ? (
          <ReactECharts
            key={`${scheme}-${mode}`}
            option={chartOption}
            style={{ height: 320 }}
            opts={{ renderer: "svg" }}
          />
        ) : (
          <p className="state">A second timestamped snapshot will unlock the balance chart.</p>
        )}
      </section>

      <section className="panel" aria-labelledby="event-value-heading">
        <div className="panel-head">
          <div>
            <h3 id="event-value-heading">Event value</h3>
            <p>Entry costs and prizes per tracked event run — gold and gems stay separate</p>
          </div>
        </div>
        {eventRuns.length > 0 ? (
          <div className="stack-md">
            {eventGroups.length > 1 || (eventGroups.length === 1 && eventGroups[0].runs > 1) ? (
              <div className="table-wrap">
                <table className="data-table compact">
                  <thead>
                    <tr>
                      <th scope="col">Event</th>
                      <th scope="col">Runs</th>
                      <th scope="col">Record</th>
                      <th scope="col">Avg wins</th>
                      <th scope="col">Net gold</th>
                      <th scope="col">Net gems</th>
                      <th scope="col">Packs</th>
                      <th scope="col">Covered entry</th>
                    </tr>
                  </thead>
                  <tbody>
                    {eventGroups.map((group) => (
                      <tr key={group.key}>
                        <td>{group.label}</td>
                        <td>{integerFormatter.format(group.runs)}</td>
                        <td>
                          {integerFormatter.format(group.totalWins)}–{integerFormatter.format(group.totalLosses)}
                        </td>
                        <td>{decimalFormatter.format(group.totalWins / group.runs)}</td>
                        <td className={deltaTone(group.netGold)}>{formatDelta(group.netGold)}</td>
                        <td className={deltaTone(group.netGems)}>{formatDelta(group.netGems)}</td>
                        <td>{group.packs > 0 ? integerFormatter.format(group.packs) : "—"}</td>
                        <td>
                          {group.runsWithRewardData > 0
                            ? `${group.runsCoveredEntry} of ${group.runsWithRewardData}`
                            : "no reward data"}
                        </td>
                      </tr>
                    ))}
                  </tbody>
                </table>
              </div>
            ) : null}
            <div className="table-wrap">
              <table className="data-table compact">
                <thead>
                  <tr>
                    <th scope="col">Event</th>
                    <th scope="col">Record</th>
                    <th scope="col">Entry</th>
                    <th scope="col">Rewards</th>
                    <th scope="col">Net gold</th>
                    <th scope="col">Net gems</th>
                    <th scope="col">Data</th>
                  </tr>
                </thead>
                <tbody>
                  {eventRuns.map((run) => (
                    <tr key={run.id}>
                      <td title={run.startedAt ? formatDateTime(run.startedAt) : run.eventName}>
                        <EventLabel eventName={run.eventName} lookup={setLookup} />
                      </td>
                      <td>
                        {integerFormatter.format(run.wins)}–{integerFormatter.format(run.losses)}
                      </td>
                      <td>{entryLabel(run)}</td>
                      <td>{rewardLabel(run)}</td>
                      <td className={deltaTone(run.netGold)}>{formatDelta(run.netGold)}</td>
                      <td className={deltaTone(run.netGems)}>{formatDelta(run.netGems)}</td>
                      <td>{confidenceBadge(run.linkConfidence)}</td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </div>
            <p className="state">
              "Covered entry" counts runs whose captured rewards at least repaid the entry cost, out
              of runs with reward data. Runs marked "no data" predate reward tracking or have not
              been claimed yet; their net values reflect only the entry cost.
            </p>
          </div>
        ) : (
          <p className="state">
            No paid event runs tracked yet. Join a draft or other paid event and its entry cost and
            prizes will appear here.
          </p>
        )}
      </section>

      <div className="economy-inventory-grid">
        <section className="panel" aria-labelledby="tokens-heading">
          <div className="panel-head">
            <div>
              <h3 id="tokens-heading">Tokens and vouchers</h3>
              <p>Event entries and current-set rewards</p>
            </div>
          </div>
          {tokenEntries.length + voucherEntries.length > 0 ? (
            <dl className="economy-inventory-list">
              {[...tokenEntries, ...voucherEntries].map(([name, count]) => (
                <div key={name}>
                  <dt>{humanizeInventoryKey(name)}</dt>
                  <dd>{integerFormatter.format(count)}</dd>
                </div>
              ))}
            </dl>
          ) : (
            <p className="state">No tokens or vouchers in the latest snapshot.</p>
          )}
        </section>

        <section className="panel" aria-labelledby="boosters-heading">
          <div className="panel-head">
            <div>
              <h3 id="boosters-heading">Boosters</h3>
              <p>{integerFormatter.format(boosterTotal)} unopened across tracked sets</p>
            </div>
          </div>
          {latest.boosters.length > 0 ? (
            <dl className="economy-inventory-list">
              {latest.boosters.map((booster) => (
                <div key={booster.setCode}>
                  <dt>{booster.setCode}</dt>
                  <dd>{integerFormatter.format(booster.count)}</dd>
                </div>
              ))}
            </dl>
          ) : (
            <p className="state">No unopened boosters in the latest snapshot.</p>
          )}
        </section>
      </div>

      <section className="panel" aria-labelledby="recent-economy-heading">
        <div className="panel-head">
          <div>
            <h3 id="recent-economy-heading">Recent balance changes</h3>
            <p>
              {recentTransactions.length > 0
                ? "Itemized inventory changes reported by Arena"
                : "Changes calculated between consecutive Arena snapshots"}
            </p>
          </div>
        </div>
        {recentTransactions.length > 0 ? (
          <div className="table-wrap economy-history-wrap">
            <table className="data-table compact economy-history-table">
              <thead>
                <tr>
                  <th scope="col">Observed</th>
                  <th scope="col">Source</th>
                  <th scope="col">Event</th>
                  <th scope="col">Gold</th>
                  <th scope="col">Gems</th>
                  <th scope="col">Packs</th>
                  <th scope="col">Cards</th>
                </tr>
              </thead>
              <tbody>
                {recentTransactions.map((txn) => {
                  const packDelta = txn.boosters.reduce((sum, booster) => sum + booster.count, 0);
                  return (
                    <tr key={txn.id}>
                      <td title={formatDateTime(txn.observedAt)}>
                        {txn.observedAt ? formatRelativeTime(txn.observedAt) : "Unknown"}
                      </td>
                      <td>{transactionSourceLabel(txn.source)}</td>
                      <td>
                        {txn.eventName ? (
                          <EventLabel eventName={txn.eventName} lookup={setLookup} showSymbol={false} />
                        ) : (
                          "—"
                        )}
                      </td>
                      <td className={deltaTone(txn.goldDelta)}>{formatDelta(txn.goldDelta)}</td>
                      <td className={deltaTone(txn.gemsDelta)}>{formatDelta(txn.gemsDelta)}</td>
                      <td className={deltaTone(packDelta)}>{formatDelta(packDelta)}</td>
                      <td>{txn.cardsGranted > 0 ? `+${integerFormatter.format(txn.cardsGranted)}` : "—"}</td>
                    </tr>
                  );
                })}
              </tbody>
            </table>
          </div>
        ) : changes.length > 0 ? (
          <div className="table-wrap economy-history-wrap">
            <table className="data-table compact economy-history-table">
              <thead>
                <tr>
                  <th scope="col">Observed</th>
                  <th scope="col">Source</th>
                  <th scope="col">Gold</th>
                  <th scope="col">Gems</th>
                  <th scope="col">Vault</th>
                  <th scope="col">Wildcards</th>
                </tr>
              </thead>
              <tbody>
                {changes.map((change) => (
                  <tr key={change.current.id}>
                    <td title={formatDateTime(change.current.observedAt)}>
                      {change.current.observedAt ? formatRelativeTime(change.current.observedAt) : "Unknown"}
                    </td>
                    <td>{sourceLabel(change.current.changeSources)}</td>
                    <td className={deltaTone(change.gold)}>{formatDelta(change.gold)}</td>
                    <td className={deltaTone(change.gems)}>{formatDelta(change.gems)}</td>
                    <td className={deltaTone(change.vaultProgress)}>{formatVaultDelta(change.vaultProgress)}</td>
                    <td className={deltaTone(change.wildcards)}>{formatDelta(change.wildcards)}</td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        ) : (
          <p className="state">No balance changes detected between the tracked snapshots yet.</p>
        )}
      </section>
    </div>
  );
}
