import { describe, expect, test } from "bun:test";

import {
  breadcrumbNavigationStateForPath,
  breadcrumbParentsFromState,
  breadcrumbsForPath,
} from "../src/lib/breadcrumbs";

describe("breadcrumbsForPath", () => {
  test("keeps the overview route as the root crumb", () => {
    expect(breadcrumbsForPath("/")).toEqual([{ label: "Overview" }]);
  });

  test("links back to overview from every top-level section", () => {
    expect(breadcrumbsForPath("/matches")).toEqual([
      { label: "Overview", to: "/" },
      { label: "Matches" },
    ]);
  });

  test("builds a linked hierarchy for detail routes", () => {
    expect(breadcrumbsForPath("/decks/42")).toEqual([
      { label: "Overview", to: "/" },
      { label: "Decks", to: "/decks" },
      { label: "Deck #42" },
    ]);
  });

  test("uses a record label when the detail data has loaded", () => {
    expect(breadcrumbsForPath("/decks/42/", "Azorius Control")).toEqual([
      { label: "Overview", to: "/" },
      { label: "Decks", to: "/decks" },
      { label: "Azorius Control" },
    ]);
  });

  test("keeps a draft session in the trail when its submitted deck is opened", () => {
    expect(
      breadcrumbsForPath("/decks/42", "Draft Deck", [
        { label: "Drafts", to: "/drafts" },
        { label: "Draft #5 · Quick Draft", to: "/drafts/5" },
      ]),
    ).toEqual([
      { label: "Overview", to: "/" },
      { label: "Drafts", to: "/drafts" },
      { label: "Draft #5 · Quick Draft", to: "/drafts/5" },
      { label: "Draft Deck" },
    ]);
  });

  test("turns a deck or match detail into contextual parents for related links", () => {
    expect(breadcrumbNavigationStateForPath("/decks/42", "Azorius Control")).toEqual({
      breadcrumbParents: [
        { label: "Decks", to: "/decks" },
        { label: "Azorius Control", to: "/decks/42" },
      ],
    });
    expect(breadcrumbNavigationStateForPath("/matches/7", "Match #7 · Jace")).toEqual({
      breadcrumbParents: [
        { label: "Matches", to: "/matches" },
        { label: "Match #7 · Jace", to: "/matches/7" },
      ],
    });
  });

  test("preserves a multi-hop trail for a new related destination", () => {
    expect(
      breadcrumbNavigationStateForPath(
        "/decks/42",
        "Draft Deck",
        [
          { label: "Drafts", to: "/drafts" },
          { label: "Draft #5 · Quick Draft", to: "/drafts/5" },
        ],
        "/matches/7",
      ),
    ).toEqual({
      breadcrumbParents: [
        { label: "Drafts", to: "/drafts" },
        { label: "Draft #5 · Quick Draft", to: "/drafts/5" },
        { label: "Draft Deck", to: "/decks/42" },
      ],
    });
  });

  test("collapses the trail when navigating back to an existing ancestor", () => {
    expect(
      breadcrumbNavigationStateForPath(
        "/decks/42",
        "Azorius Control",
        [
          { label: "Matches", to: "/matches" },
          { label: "Match #7 · Jace", to: "/matches/7" },
        ],
        "/matches/7",
      ),
    ).toEqual({
      breadcrumbParents: [{ label: "Matches", to: "/matches" }],
    });
  });

  test("accepts only valid internal breadcrumb parents from navigation state", () => {
    expect(
      breadcrumbParentsFromState({
        breadcrumbParents: [
          { label: " Drafts ", to: "/drafts" },
          { label: "External", to: "https://example.com" },
        ],
      }),
    ).toEqual([{ label: "Drafts", to: "/drafts" }]);
    expect(breadcrumbParentsFromState(null)).toEqual([]);
  });

  test("returns no trail for routes the app does not own", () => {
    expect(breadcrumbsForPath("/not-a-route")).toEqual([]);
    expect(breadcrumbsForPath("/ranked/extra")).toEqual([]);
  });
});
