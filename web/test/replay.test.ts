import { describe, expect, test } from "bun:test";

import {
  battlefieldSectionKind,
  boardZoneKind,
  boardZoneLabel,
  buildReplayBeat,
  buildReplayBoardCensus,
  buildReplayGameGroups,
  buildReplayLifeSeries,
  buildReplayTargetLookup,
  buildReplayTickKinds,
  buildReplayTurnBoundaries,
  findReplayKeyMoments,
  describeReplayChange,
  filterMeaningfulReplayFrames,
  formatReplayWinReason,
  groupBattlefieldCardStacks,
  normalizeReplayWinReason,
  preferredReplayFrameIndex,
  replayFrameHasLifeDelta,
  replayFrameLifeTotalWinner,
  replayFrameTickKind,
  replayLifeDelta,
  replayLifeSeriesDomain,
  replayTurnBoundaryCount,
  replayTurnValue,
  summarizeReplayGame,
} from "../src/lib/replay";
import type { CardPreview } from "../src/lib/scryfall";
import type {
  MatchReplayChange,
  MatchReplayFrame,
  MatchReplayFrameObject,
} from "../src/lib/types";

function change(values: Partial<MatchReplayChange> = {}): MatchReplayChange {
  return {
    instanceId: values.instanceId ?? 1,
    cardId: values.cardId ?? 100,
    cardName: values.cardName,
    playerSide: values.playerSide ?? "self",
    action: values.action ?? "move_public",
    fromZoneType: values.fromZoneType,
    toZoneType: values.toZoneType,
    isToken: values.isToken ?? false,
  };
}

function object(
  values: Partial<MatchReplayFrameObject> = {},
): MatchReplayFrameObject {
  return {
    id: values.id ?? 1,
    frameId: values.frameId ?? 1,
    instanceId: values.instanceId ?? 1,
    cardId: values.cardId ?? 100,
    cardName: values.cardName,
    playerSide: values.playerSide ?? "self",
    zoneType: values.zoneType ?? "Battlefield",
    power: values.power,
    toughness: values.toughness,
    attackState: values.attackState,
    counterSummaryJson: values.counterSummaryJson,
    detailsJson: values.detailsJson,
    isToken: values.isToken ?? false,
    isTapped: values.isTapped ?? false,
    hasSummoningSickness: values.hasSummoningSickness ?? false,
  };
}

function frame(values: Partial<MatchReplayFrame> = {}): MatchReplayFrame {
  return { id: values.id ?? 1, ...values };
}

function preview(typeLine: string): CardPreview {
  return { name: "x", imageUrl: "", typeLine };
}

describe("zone classification", () => {
  test("maps Arena zone strings to a board zone kind", () => {
    expect(boardZoneKind("ZoneType_Hand")).toBe("hand");
    expect(boardZoneKind("Battlefield")).toBe("battlefield");
    expect(boardZoneKind("p1_graveyard")).toBe("graveyard");
    expect(boardZoneKind("")).toBe("other");
    expect(boardZoneLabel("graveyard")).toBe("Graveyard");
  });

  test("sorts permanents into battlefield sections by type line", () => {
    expect(battlefieldSectionKind(preview("Basic Land — Forest"))).toBe("lands");
    expect(battlefieldSectionKind(preview("Creature — Otter"))).toBe("creatures");
    expect(battlefieldSectionKind(preview("Legendary Planeswalker — Jace"))).toBe(
      "planeswalkers",
    );
    expect(battlefieldSectionKind(preview("Enchantment — Class"))).toBe(
      "artifacts_enchantments",
    );
    expect(battlefieldSectionKind(null)).toBe("other");
  });
});

describe("battlefield card stacks", () => {
  const islandPreview: CardPreview = {
    name: "Island",
    imageUrl: "",
    typeLine: "Basic Land — Island",
  };
  const plainsPreview: CardPreview = {
    name: "Plains",
    imageUrl: "",
    typeLine: "Basic Land — Plains",
  };

  test("groups duplicate lands by name and tapped state", () => {
    const previews = new Map<number, CardPreview | null>([
      [1, islandPreview],
      [2, plainsPreview],
    ]);
    const objects = [
      object({ instanceId: 10, cardId: 1 }),
      object({ instanceId: 11, cardId: 1 }),
      object({ instanceId: 12, cardId: 1, isTapped: true }),
      object({ instanceId: 13, cardId: 2 }),
    ];

    const stacks = groupBattlefieldCardStacks(objects, previews);

    expect(stacks).toHaveLength(3);
    expect(stacks[0]?.objects.map((o) => o.instanceId)).toEqual([10, 11]);
    expect(stacks[1]?.objects.map((o) => o.instanceId)).toEqual([12]);
    expect(stacks[2]?.objects.map((o) => o.instanceId)).toEqual([13]);
  });

  test("groups same-name lands across different printings", () => {
    const previews = new Map<number, CardPreview | null>([
      [1, islandPreview],
      [2, { ...islandPreview, imageUrl: "other-art" }],
    ]);
    const objects = [
      object({ instanceId: 10, cardId: 1 }),
      object({ instanceId: 11, cardId: 2 }),
    ];

    const stacks = groupBattlefieldCardStacks(objects, previews);

    expect(stacks).toHaveLength(1);
    expect(stacks[0]?.objects.map((o) => o.instanceId)).toEqual([10, 11]);
  });

  test("keeps lands with extra state as standalone cards", () => {
    const previews = new Map<number, CardPreview | null>([[1, islandPreview]]);
    const objects = [
      object({ instanceId: 10, cardId: 1 }),
      object({ instanceId: 11, cardId: 1, attackState: "attacking" }),
      object({
        instanceId: 12,
        cardId: 1,
        counterSummaryJson: JSON.stringify([{ label: "Flood", count: 2 }]),
      }),
      object({ instanceId: 13, cardId: 1, hasSummoningSickness: true }),
      object({ instanceId: 14, cardId: 1 }),
    ];

    const stacks = groupBattlefieldCardStacks(objects, previews);

    expect(stacks).toHaveLength(4);
    expect(stacks[0]?.objects.map((o) => o.instanceId)).toEqual([10, 14]);
    expect(stacks[1]?.objects.map((o) => o.instanceId)).toEqual([11]);
    expect(stacks[2]?.objects.map((o) => o.instanceId)).toEqual([12]);
    expect(stacks[3]?.objects.map((o) => o.instanceId)).toEqual([13]);
  });

  test("consolidates duplicate tokens under a tokens-only predicate", () => {
    const mutagenPreview: CardPreview = {
      name: "Mutagen",
      imageUrl: "",
      typeLine: "Token Artifact — Mutagen",
    };
    const previews = new Map<number, CardPreview | null>([
      [1, mutagenPreview],
      [2, { name: "Sol Ring", imageUrl: "", typeLine: "Artifact" }],
    ]);
    const objects = [
      object({ instanceId: 10, cardId: 1, isToken: true }),
      object({ instanceId: 11, cardId: 1, isToken: true }),
      object({ instanceId: 12, cardId: 1, isToken: true, isTapped: true }),
      object({ instanceId: 13, cardId: 2 }),
      object({ instanceId: 14, cardId: 2 }),
    ];

    const stacks = groupBattlefieldCardStacks(
      objects,
      previews,
      (o) => o.isToken,
    );

    expect(stacks).toHaveLength(4);
    expect(stacks[0]?.objects.map((o) => o.instanceId)).toEqual([10, 11]);
    expect(stacks[1]?.objects.map((o) => o.instanceId)).toEqual([12]);
    expect(stacks[2]?.objects.map((o) => o.instanceId)).toEqual([13]);
    expect(stacks[3]?.objects.map((o) => o.instanceId)).toEqual([14]);
  });

  test("honors the extra canStack predicate", () => {
    const previews = new Map<number, CardPreview | null>([[1, islandPreview]]);
    const objects = [
      object({ instanceId: 10, cardId: 1 }),
      object({ instanceId: 11, cardId: 1 }),
    ];

    const stacks = groupBattlefieldCardStacks(
      objects,
      previews,
      (o) => o.instanceId !== 11,
    );

    expect(stacks).toHaveLength(2);
  });
});

describe("turn boundaries", () => {
  test("groups items by turn, preserving first/last index", () => {
    const boundaries = buildReplayTurnBoundaries([
      { turnNumber: 1 },
      { turnNumber: 1 },
      { turnNumber: 2 },
    ]);
    expect(boundaries).toHaveLength(2);
    expect(boundaries[0]).toMatchObject({ turnKey: 1, firstIndex: 0, lastIndex: 1 });
    expect(replayTurnBoundaryCount(boundaries[0])).toBe(2);
    expect(boundaries[1]).toMatchObject({ turnKey: 2, firstIndex: 2, lastIndex: 2 });
  });

  test("normalizes missing/zero turns to a sentinel", () => {
    expect(replayTurnValue(undefined)).toBe(-1);
    expect(replayTurnValue(0)).toBe(-1);
    expect(replayTurnValue(3)).toBe(3);
  });
});

describe("meaningful frame filtering", () => {
  test("keeps frames with changes and drops inert ones", () => {
    const f0 = frame({ id: 1 });
    const f1 = frame({ id: 2, changes: [change({ action: "tap" })] });
    const f2 = frame({ id: 3 });
    expect(filterMeaningfulReplayFrames([f0, f1, f2])).toEqual([f1]);
  });

  test("keeps a frame whose only change is a life swing", () => {
    const f0 = frame({ id: 1, selfLifeTotal: 20 });
    const f1 = frame({ id: 2, selfLifeTotal: 18 });
    expect(replayFrameHasLifeDelta(f0, f1)).toBe(true);
    // f0 is inert on its own (no prior frame, no changes) and is dropped; the
    // life-swing frame f1 is retained.
    expect(filterMeaningfulReplayFrames([f0, f1])).toEqual([f1]);
  });

  test("falls back to the last frame when nothing is meaningful", () => {
    const f0 = frame({ id: 1 });
    const f1 = frame({ id: 2 });
    expect(filterMeaningfulReplayFrames([f0, f1])).toEqual([f1]);
  });

  test("drops GRE noise moves (same-zone shuffles like Limbo to Limbo)", () => {
    const noise = change({
      action: "move_public",
      fromZoneType: "Limbo",
      toZoneType: "Limbo",
    });
    const real = frame({ id: 2, changes: [change({ action: "tap" })] });
    expect(
      filterMeaningfulReplayFrames([
        frame({ id: 1, changes: [noise] }),
        real,
        frame({ id: 3, changes: [noise] }),
      ]),
    ).toEqual([real]);
  });
});

describe("replay game grouping", () => {
  test("drops a later game's setup frames when they inherit the prior turn", () => {
    const staleReveal = frame({
      id: 1,
      gameNumber: 2,
      gameStage: "start",
      turnNumber: 15,
      changes: [change({ action: "enter_public", cardName: "Island" })],
    });
    const turnOneStart = frame({
      id: 2,
      gameNumber: 2,
      gameStage: "play",
      turnNumber: 1,
      changes: [change({ action: "leave_public", cardName: "Island" })],
    });
    const firstPlay = frame({
      id: 3,
      gameNumber: 2,
      turnNumber: 1,
      changes: [change({ action: "tap", cardName: "Mountain" })],
    });

    expect(buildReplayGameGroups([staleReveal, turnOneStart, firstPlay])[0]?.frames)
      .toEqual([turnOneStart, firstPlay]);
  });

  test("preserves genuine pre-game frames without an inherited turn", () => {
    const preGame = frame({
      id: 1,
      gameNumber: 1,
      gameStage: "start",
      changes: [change({ action: "enter_public", cardName: "Island" })],
    });
    const turnOne = frame({
      id: 2,
      gameNumber: 1,
      gameStage: "play",
      turnNumber: 1,
      changes: [change({ action: "tap", cardName: "Island" })],
    });

    expect(buildReplayGameGroups([preGame, turnOne])[0]?.frames).toEqual([
      preGame,
      turnOne,
    ]);
  });
});

describe("life series", () => {
  test("carries the last known life total forward across gaps", () => {
    const series = buildReplayLifeSeries([
      frame({ id: 1, selfLifeTotal: 20, opponentLifeTotal: 20 }),
      frame({ id: 2, selfLifeTotal: 18 }),
      frame({ id: 3, opponentLifeTotal: 15 }),
    ]);
    expect(series).toEqual([
      { self: 20, opponent: 20 },
      { self: 18, opponent: 20 },
      { self: 18, opponent: 15 },
    ]);
  });

  test("domain always spans at least 0–20 and widens to extremes", () => {
    expect(replayLifeSeriesDomain([{ self: 12, opponent: 8 }])).toEqual({
      min: 0,
      max: 20,
    });
    expect(
      replayLifeSeriesDomain([
        { self: 24, opponent: -3 },
        { self: 5, opponent: 5 },
      ]),
    ).toEqual({ min: -3, max: 24 });
  });
});

describe("scrubber tick classification", () => {
  test("ranks life swings, combat, and spells off the change stream", () => {
    const prev = frame({ id: 1, selfLifeTotal: 20 });
    expect(
      replayFrameTickKind(frame({ id: 2, selfLifeTotal: 17 }), prev),
    ).toBe("life");
    expect(
      replayFrameTickKind(
        frame({ id: 2, changes: [change({ action: "block" })] }),
        null,
      ),
    ).toBe("combat");
    expect(
      replayFrameTickKind(
        frame({
          id: 2,
          changes: [change({ action: "move_public", toZoneType: "Stack" })],
        }),
        null,
      ),
    ).toBe("spell");
    expect(
      replayFrameTickKind(frame({ id: 2, changes: [change({ action: "tap" })] }), null),
    ).toBe("other");
  });

  test("builds a tick kind per frame in order", () => {
    const kinds = buildReplayTickKinds([
      frame({ id: 1, selfLifeTotal: 20, opponentLifeTotal: 20 }),
      frame({
        id: 2,
        selfLifeTotal: 20,
        opponentLifeTotal: 20,
        changes: [change({ action: "attack" })],
      }),
      frame({ id: 3, selfLifeTotal: 18, opponentLifeTotal: 20 }),
    ]);
    expect(kinds).toEqual(["other", "combat", "life"]);
  });
});

describe("play-by-play beats", () => {
  test("renders an attack with the creature's power/toughness", () => {
    const f = frame({
      id: 2,
      changes: [change({ action: "attack", playerSide: "opponent", cardName: "Otter", instanceId: 7 })],
      objects: [object({ instanceId: 7, power: 2, toughness: 2 })],
    });
    expect(buildReplayBeat(f, null)).toEqual({
      text: "Opponent attacks with Otter (2/2)",
    });
  });

  test("notes creature deaths on a block", () => {
    const f = frame({
      id: 2,
      changes: [
        change({ action: "block", playerSide: "self", cardName: "Tarmogoyf", instanceId: 3 }),
        change({
          action: "move_public",
          fromZoneType: "Battlefield",
          toZoneType: "Graveyard",
          cardName: "Otter",
        }),
      ],
      objects: [object({ instanceId: 3, power: 4, toughness: 4 })],
    });
    expect(buildReplayBeat(f, null)).toEqual({
      text: "You block with Tarmogoyf (4/4)",
      note: "a creature dies",
    });
  });

  test("summarizes a life swing with before/after totals", () => {
    const prev = frame({ id: 1, selfLifeTotal: 20, opponentLifeTotal: 20 });
    const f = frame({ id: 2, selfLifeTotal: 20, opponentLifeTotal: 18 });
    expect(buildReplayBeat(f, prev)).toEqual({
      text: "Life change · opponent 20 → 18",
    });
  });

  test("ignores noise moves when picking the fallback beat", () => {
    const f = frame({
      id: 2,
      changes: [
        change({ action: "move_public", fromZoneType: "Limbo", toZoneType: "Limbo", cardName: "Kaito" }),
        change({ action: "tap", playerSide: "opponent", cardName: "Island" }),
      ],
    });
    expect(buildReplayBeat(f, null)).toEqual({ text: "Opponent taps Island" });
  });

  test("uses friendly phrasing when a permanent leaves the battlefield", () => {
    expect(
      buildReplayBeat(
        frame({
          id: 2,
          changes: [
            change({
              action: "move_public",
              cardName: "Wistfulness",
              fromZoneType: "Battlefield",
              toZoneType: "Graveyard",
            }),
          ],
        }),
        null,
      ),
    ).toEqual({ text: "Wistfulness is put into the graveyard" });
  });

  test("narrates a card revealed in hand", () => {
    expect(
      buildReplayBeat(
        frame({
          id: 2,
          changes: [
            change({
              action: "enter_public",
              playerSide: "self",
              cardName: "Kaito",
              toZoneType: "Hand",
            }),
          ],
        }),
        null,
      ),
    ).toEqual({ text: "You reveal Kaito" });
  });

  test("marks a tapped land as it enters", () => {
    const f = frame({
      id: 2,
      changes: [
        change({
          action: "move_public",
          playerSide: "opponent",
          cardName: "Steam Vents",
          fromZoneType: "Hand",
          toZoneType: "Battlefield",
          instanceId: 9,
        }),
      ],
      objects: [object({ instanceId: 9, isTapped: true })],
    });
    expect(buildReplayBeat(f, null)).toEqual({
      text: "Opponent plays Steam Vents",
      note: "tapped",
    });
  });

  test("narrates a triggered power boost instead of repeating the spell cast", () => {
    const f = frame({
      id: 2,
      changes: [
        change({
          action: "move_public",
          instanceId: 8,
          cardName: "Burst Lightning",
          fromZoneType: "Stack",
          toZoneType: "Stack",
        }),
        change({
          action: "stat_change",
          instanceId: 7,
          cardName: "Slickshot Show-Off",
          toZoneType: "Battlefield",
        }),
      ],
      annotationsJson: JSON.stringify({
        annotations: [
          {
            affectorId: 10,
            affectedIds: [10],
            type: ["AnnotationType_ResolutionStart"],
          },
          {
            affectorId: 10,
            affectedIds: [7],
            type: ["AnnotationType_PowerToughnessModCreated"],
            details: [
              { key: "power", valueInt32: [2] },
              { key: "toughness", valueInt32: [0] },
            ],
          },
          {
            affectorId: 7,
            affectedIds: [10],
            type: ["AnnotationType_AbilityInstanceDeleted"],
          },
        ],
      }),
      objects: [
        object({
          instanceId: 7,
          cardName: "Slickshot Show-Off",
          power: 3,
          toughness: 2,
        }),
      ],
    });

    expect(buildReplayBeat(f, null)).toEqual({
      text: "Slickshot Show-Off's ability gives it +2/+0",
    });
    expect(replayFrameTickKind(f, null)).toBe("other");
  });

  test("keeps and narrates a spell target selection", () => {
    const targetFrame = frame({
      id: 2,
      annotationsJson: JSON.stringify({
        annotations: [
          {
            affectorId: 8,
            affectedIds: [7],
            type: ["AnnotationType_TargetSpec"],
          },
        ],
      }),
      objects: [
        object({
          instanceId: 8,
          cardName: "Burst Lightning",
          zoneType: "Stack",
        }),
        object({ instanceId: 7, cardName: "Otter" }),
      ],
    });

    expect(filterMeaningfulReplayFrames([frame({ id: 1 }), targetFrame])).toEqual([
      targetFrame,
    ]);
    expect(buildReplayBeat(targetFrame, null)).toEqual({
      text: "Burst Lightning targets Otter",
    });
    expect(buildReplayTargetLookup([targetFrame])).toEqual(
      new Map([[8, [{ targetId: 7, label: "Otter" }]]]),
    );
  });
});

describe("key moments", () => {
  test("flags the decisive lethal step and the biggest life swings", () => {
    const frames = [
      frame({ id: 1, selfLifeTotal: 20, opponentLifeTotal: 20 }),
      frame({ id: 2, selfLifeTotal: 20, opponentLifeTotal: 13 }), // -7 swing
      frame({ id: 3, selfLifeTotal: 18, opponentLifeTotal: 13 }), // -2 (below threshold)
      frame({ id: 4, selfLifeTotal: 18, opponentLifeTotal: 0 }), // lethal
    ];
    const moments = findReplayKeyMoments(frames);
    expect(moments).toEqual([
      { index: 1, kind: "swing", label: "Life swing · opponent -7" },
      { index: 3, kind: "decisive", label: "Opponent hit 0 life" },
    ]);
  });

  test("returns nothing when life never moves", () => {
    expect(
      findReplayKeyMoments([
        frame({ id: 1, selfLifeTotal: 20, opponentLifeTotal: 20 }),
        frame({ id: 2, selfLifeTotal: 20, opponentLifeTotal: 20 }),
      ]),
    ).toEqual([]);
  });
});

describe("HUD life delta", () => {
  test("returns the signed change for a side, or null when flat/unknown", () => {
    const prev = frame({ id: 1, selfLifeTotal: 20, opponentLifeTotal: 18 });
    const next = frame({ id: 2, selfLifeTotal: 17, opponentLifeTotal: 18 });
    expect(replayLifeDelta(prev, next, "self")).toBe(-3);
    expect(replayLifeDelta(prev, next, "opponent")).toBeNull();
    expect(replayLifeDelta(null, next, "self")).toBeNull();
    expect(
      replayLifeDelta(frame({ id: 1 }), frame({ id: 2, selfLifeTotal: 5 }), "self"),
    ).toBeNull();
  });
});

describe("preferred starting frame", () => {
  test("prefers the last frame that has visible objects", () => {
    const frames = [
      frame({ id: 1, objects: [] }),
      frame({ id: 2, objects: [object()] }),
      frame({ id: 3, objects: [] }),
    ];
    expect(preferredReplayFrameIndex(frames)).toBe(1);
  });
});

describe("change narration", () => {
  test("renders human-readable beats with the acting player", () => {
    expect(
      describeReplayChange(
        change({ action: "block", playerSide: "opponent", cardName: "Otter" }),
      ),
    ).toBe("Opponent declared Otter as a blocker.");
    expect(
      describeReplayChange(
        change({
          action: "move_public",
          playerSide: "self",
          cardName: "Tarmogoyf",
          fromZoneType: "Hand",
          toZoneType: "Battlefield",
        }),
      ),
    ).toBe("You moved Tarmogoyf from Hand to Battlefield.");
  });

  test("falls back to a card id when the name is unknown", () => {
    expect(
      describeReplayChange(change({ action: "tap", cardId: 42, cardName: undefined })),
    ).toBe("You tapped Card 42.");
  });
});

describe("win reason formatting", () => {
  test("strips Arena prefixes and humanizes the reason", () => {
    expect(normalizeReplayWinReason("ResultReason_Concede")).toBe("Concede");
    expect(formatReplayWinReason("ResultReason_Concede")).toBe("concede");
    expect(normalizeReplayWinReason(null)).toBe("");
  });
});

describe("game result inference", () => {
  test("reads the winner from terminal life totals", () => {
    expect(
      replayFrameLifeTotalWinner(frame({ selfLifeTotal: 0, opponentLifeTotal: 5 })),
    ).toBe("opponent");
    expect(
      replayFrameLifeTotalWinner(frame({ selfLifeTotal: 12, opponentLifeTotal: 0 })),
    ).toBe("self");
    expect(
      replayFrameLifeTotalWinner(frame({ selfLifeTotal: 3, opponentLifeTotal: 4 })),
    ).toBe("unknown");
  });

  test("summarizes a lethal-damage loss", () => {
    const summary = summarizeReplayGame([
      frame({ id: 1, selfLifeTotal: 4, opponentLifeTotal: 6 }),
      frame({ id: 2, selfLifeTotal: 0, opponentLifeTotal: 6 }),
    ]);
    expect(summary).toEqual({ result: "loss", detail: "You went to 0 life." });
  });

  test("summarizes an opponent concession", () => {
    const summary = summarizeReplayGame([
      frame({
        id: 1,
        selfLifeTotal: 20,
        opponentLifeTotal: 20,
        winningPlayerSide: "self",
        winReason: "ResultReason_Concede",
      }),
    ]);
    expect(summary).toEqual({ result: "win", detail: "Opponent conceded." });
  });

  test("preserves concession results when terminal frames are filtered from display", () => {
    const groups = buildReplayGameGroups(
      [
        frame({
          id: 1,
          gameNumber: 1,
          selfLifeTotal: 13,
          opponentLifeTotal: 14,
          changes: [change({ action: "tap" })],
        }),
        frame({
          id: 2,
          gameNumber: 1,
          gameStage: "gameover",
          selfLifeTotal: 13,
          opponentLifeTotal: 14,
          winningPlayerSide: "opponent",
          winReason: "ResultReason_Concede",
        }),
        frame({
          id: 3,
          gameNumber: 2,
          selfLifeTotal: 16,
          opponentLifeTotal: 11,
          changes: [change({ action: "tap" })],
        }),
        frame({
          id: 4,
          gameNumber: 2,
          gameStage: "gameover",
          selfLifeTotal: 16,
          opponentLifeTotal: 11,
          winningPlayerSide: "self",
          winReason: "ResultReason_Concede",
        }),
      ],
      "win",
    );

    expect(
      groups.map(({ gameNumber, frames, summary }) => ({
        gameNumber,
        frameIDs: frames.map(({ id }) => id),
        summary,
      })),
    ).toEqual([
      {
        gameNumber: 1,
        frameIDs: [1],
        summary: { result: "loss", detail: "You conceded." },
      },
      {
        gameNumber: 2,
        frameIDs: [3],
        summary: { result: "win", detail: "Opponent conceded." },
      },
    ]);
  });

  test("ignores stale result metadata inherited by a later game", () => {
    const groups = buildReplayGameGroups(
      [
        frame({
          id: 1,
          gameNumber: 1,
          gameStage: "gameover",
          winningPlayerSide: "self",
          winReason: "ResultReason_Concede",
        }),
        frame({
          id: 2,
          gameNumber: 2,
          gameStage: "start",
          winningPlayerSide: "self",
          winReason: "ResultReason_Concede",
        }),
        frame({
          id: 3,
          gameNumber: 2,
          changes: [change({ action: "tap" })],
        }),
        frame({
          id: 4,
          gameNumber: 3,
          gameStage: "gameover",
          winningPlayerSide: "self",
          winReason: "ResultReason_Concede",
        }),
      ],
      "win",
    );

    expect(groups[1]).toEqual({
      gameNumber: 2,
      frames: [
        frame({
          id: 3,
          gameNumber: 2,
          changes: [change({ action: "tap" })],
        }),
      ],
      summary: { result: "unknown", detail: "Game result recorded." },
    });
  });

  test("lets a known match result override an ambiguous final game", () => {
    const summary = summarizeReplayGame(
      [frame({ id: 1, selfLifeTotal: 6, opponentLifeTotal: 7 })],
      { isFinalGame: true, matchResult: "win" },
    );
    expect(summary?.result).toBe("win");
  });
});

describe("board census", () => {
  test("counts creatures with power, lands, hand, and graveyard per side", () => {
    const snapshot = buildReplayBoardCensus(
      frame({
        objects: [
          // Your side: 2 creatures (3 + 2 power), 2 lands, 1 hand, 1 graveyard.
          object({ instanceId: 1, power: 3, toughness: 3 }),
          object({
            instanceId: 2,
            power: 2,
            toughness: 2,
            detailsJson: JSON.stringify({ cardTypes: ["CardType_Creature"] }),
          }),
          object({
            instanceId: 3,
            detailsJson: JSON.stringify({ cardTypes: ["CardType_Land"] }),
          }),
          object({
            instanceId: 4,
            detailsJson: JSON.stringify({ cardTypes: ["CardType_Land"] }),
          }),
          object({ instanceId: 5, zoneType: "ZoneType_Hand" }),
          object({ instanceId: 6, zoneType: "ZoneType_Graveyard" }),
          // Opponent: 1 creature, 1 land, plus a stack object that must not count.
          object({
            instanceId: 7,
            playerSide: "opponent",
            power: 4,
            toughness: 4,
          }),
          object({
            instanceId: 8,
            playerSide: "opponent",
            detailsJson: JSON.stringify({ cardTypes: ["CardType_Land"] }),
          }),
          object({
            instanceId: 9,
            playerSide: "opponent",
            zoneType: "ZoneType_Stack",
          }),
          // Unknown side is ignored.
          object({ instanceId: 10, playerSide: "unknown" }),
        ],
      }),
    );

    expect(snapshot.self).toEqual({
      creatures: 2,
      power: 5,
      lands: 2,
      hand: 1,
      graveyard: 1,
    });
    expect(snapshot.opponent).toEqual({
      creatures: 1,
      power: 4,
      lands: 1,
      // Hidden zone with no revealed cards reads as unknown, not empty.
      hand: null,
      graveyard: 0,
    });
  });

  test("animated lands count as creatures, not lands", () => {
    const snapshot = buildReplayBoardCensus(
      frame({
        objects: [
          object({
            instanceId: 1,
            power: 2,
            toughness: 2,
            detailsJson: JSON.stringify({
              cardTypes: ["CardType_Land", "CardType_Creature"],
            }),
          }),
        ],
      }),
    );
    expect(snapshot.self.creatures).toBe(1);
    expect(snapshot.self.lands).toBe(0);
    expect(snapshot.self.power).toBe(2);
  });

  test("handles frames without objects", () => {
    const snapshot = buildReplayBoardCensus(frame({}));
    expect(snapshot.self.creatures).toBe(0);
    expect(snapshot.self.hand).toBe(0);
    expect(snapshot.opponent.hand).toBeNull();
  });
});
