import type { EventRunEconomy } from "./types";

const integerFormatter = new Intl.NumberFormat(undefined, { maximumFractionDigits: 0 });

export function formatEconomyDelta(value: number): string {
  if (value === 0) return "—";
  return `${value > 0 ? "+" : "−"}${integerFormatter.format(Math.abs(value))}`;
}

export function humanizeInventoryKey(value: string): string {
  const known: Record<string, string> = {
    PlayInToken: "Play-In token",
    Token_JumpIn: "Jump In token",
  };
  if (known[value]) return known[value];
  const battlePassOrb = value.match(/^BattlePass_([^_]+)_Orb$/i);
  if (battlePassOrb) return `${battlePassOrb[1].toUpperCase()} mastery orb`;
  return value
    .replace(/^Token_/, "")
    .replace(/_/g, " ")
    .replace(/([a-z])([A-Z])/g, "$1 $2");
}

export function eventEntryLabel(run: EventRunEconomy): string {
  if (run.entryGold < 0) return `${formatEconomyDelta(run.entryGold)} gold`;
  if (run.entryGems < 0) return `${formatEconomyDelta(run.entryGems)} gems`;
  const type = run.entryCurrencyType;
  if (!type || type === "None") return "Free";
  if (run.entryCurrencyPaid != null && run.entryCurrencyPaid > 1) {
    return `${integerFormatter.format(run.entryCurrencyPaid)} × ${humanizeInventoryKey(type)}`;
  }
  return humanizeInventoryKey(type);
}

export function eventRewardLabel(run: EventRunEconomy): string {
  const parts: string[] = [];
  if (run.rewardGold !== 0) parts.push(`${formatEconomyDelta(run.rewardGold)} gold`);
  if (run.rewardGems !== 0) parts.push(`${formatEconomyDelta(run.rewardGems)} gems`);
  const packCount = run.rewardBoosters.reduce((sum, booster) => sum + booster.count, 0);
  if (packCount > 0) parts.push(`${integerFormatter.format(packCount)} pack${packCount === 1 ? "" : "s"}`);
  if (run.rewardCards > 0) parts.push(`${integerFormatter.format(run.rewardCards)} cards`);
  if (parts.length === 0) return run.linkConfidence === "none" ? "not captured" : "—";
  return parts.join(" · ");
}
