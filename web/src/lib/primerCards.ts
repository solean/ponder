import type { Root } from "mdast";

export type PrimerCard = {
  cardId: number;
  cardName?: string;
};

type MarkdownNode = {
  type: string;
  value?: string;
  children?: MarkdownNode[];
  url?: string;
  title?: string | null;
};

type NamedPrimerCard = PrimerCard & { cardName: string };

const PRIMER_CARD_HREF_PREFIX = "#primer-card-";
const WORD_CHARACTER = /[\p{L}\p{N}]/u;
const SKIPPED_PARENT_TYPES = new Set(["code", "image", "imageReference", "inlineCode", "link", "linkReference"]);

function escapeRegExp(value: string): string {
  return value.replace(/[.*+?^${}()|[\]\\]/g, "\\$&");
}

function adjacentCodePoint(value: string, index: number, direction: -1 | 1): string {
  if (direction === 1) {
    const codePoint = value.codePointAt(index);
    return codePoint == null ? "" : String.fromCodePoint(codePoint);
  }

  if (index <= 0) {
    return "";
  }
  const trailingUnit = value.charCodeAt(index - 1);
  const start = trailingUnit >= 0xdc00 && trailingUnit <= 0xdfff && index >= 2 ? index - 2 : index - 1;
  const codePoint = value.codePointAt(start);
  return codePoint == null ? "" : String.fromCodePoint(codePoint);
}

function namedCards(cards: readonly PrimerCard[]): NamedPrimerCard[] {
  const cardsByName = new Map<string, NamedPrimerCard>();
  for (const card of cards) {
    const cardName = card.cardName?.trim();
    if (!cardName || !Number.isFinite(card.cardId) || card.cardId <= 0) {
      continue;
    }
    const key = cardName.toLocaleLowerCase();
    if (!cardsByName.has(key)) {
      cardsByName.set(key, { cardId: card.cardId, cardName });
    }
  }
  return [...cardsByName.values()].sort((left, right) => right.cardName.length - left.cardName.length);
}

function linkedTextNodes(value: string, cards: readonly NamedPrimerCard[], pattern: RegExp): MarkdownNode[] {
  const nodes: MarkdownNode[] = [];
  let cursor = 0;
  pattern.lastIndex = 0;

  for (let match = pattern.exec(value); match; match = pattern.exec(value)) {
    const matchedName = match[0];
    const start = match.index;
    const end = start + matchedName.length;
    const before = adjacentCodePoint(value, start, -1);
    const after = adjacentCodePoint(value, end, 1);

    if ((before && WORD_CHARACTER.test(before)) || (after && WORD_CHARACTER.test(after))) {
      continue;
    }

    if (start > cursor) {
      nodes.push({ type: "text", value: value.slice(cursor, start) });
    }
    const card = cards.find((candidate) => candidate.cardName.localeCompare(matchedName, undefined, { sensitivity: "accent" }) === 0);
    if (!card) {
      continue;
    }
    nodes.push({
      type: "link",
      url: primerCardHref(card.cardId),
      title: null,
      children: [{ type: "text", value: matchedName }],
    });
    cursor = end;
  }

  if (cursor === 0) {
    return [{ type: "text", value }];
  }
  if (cursor < value.length) {
    nodes.push({ type: "text", value: value.slice(cursor) });
  }
  return nodes;
}

function linkTextChildren(node: MarkdownNode, cards: readonly NamedPrimerCard[], pattern: RegExp): void {
  if (!node.children || SKIPPED_PARENT_TYPES.has(node.type)) {
    return;
  }

  const children: MarkdownNode[] = [];
  for (const child of node.children) {
    if (child.type === "text" && typeof child.value === "string") {
      children.push(...linkedTextNodes(child.value, cards, pattern));
    } else {
      linkTextChildren(child, cards, pattern);
      children.push(child);
    }
  }
  node.children = children;
}

export function primerCardHref(cardId: number): string {
  return `${PRIMER_CARD_HREF_PREFIX}${cardId}`;
}

export function primerCardIdFromHref(href?: string): number | null {
  if (!href?.startsWith(PRIMER_CARD_HREF_PREFIX)) {
    return null;
  }
  const cardId = Number(href.slice(PRIMER_CARD_HREF_PREFIX.length));
  return Number.isInteger(cardId) && cardId > 0 ? cardId : null;
}

export function linkPrimerCardNames(root: MarkdownNode, cards: readonly PrimerCard[]): void {
  const availableCards = namedCards(cards);
  if (availableCards.length === 0) {
    return;
  }
  const pattern = new RegExp(availableCards.map((card) => escapeRegExp(card.cardName)).join("|"), "giu");
  linkTextChildren(root, availableCards, pattern);
}

export function remarkPrimerCardNames(options: { cards: readonly PrimerCard[] }) {
  return (tree: Root) => linkPrimerCardNames(tree as MarkdownNode, options.cards);
}
