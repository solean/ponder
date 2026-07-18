export type LiveMatch = {
  match: Match;
  opponentObservedCards: OpponentObservedCard[];
  deck: DeckCard[];
  deckTotal: number;
  landCount: number;
  gameNumber: number;
  turnNumber: number;
  libraryEstimate: number;
};

export type SetInfo = {
  code: string;
  name: string;
  iconSvgUri?: string;
  releasedAt?: string;
};

export type Match = {
  id: number;
  arenaMatchId: string;
  eventName: string;
  bestOf?: "bo1" | "bo3" | "";
  playDraw?: "play" | "draw" | "";
  opponent: string;
  startedAt: string;
  endedAt: string;
  result: "win" | "loss" | "unknown";
  winReason: string;
  turnCount?: number | null;
  secondsCount?: number | null;
  deckId?: number | null;
  deckName?: string | null;
  deckVersionId?: number | null;
  deckVersionNumber?: number | null;
  deckColors?: string[] | null;
  deckColorsKnown?: boolean;
  opponentDeckColors?: string[] | null;
  opponentDeckColorsKnown?: boolean;
};

export type OpponentObservedCard = {
  cardId: number;
  quantity: number;
  cardName?: string;
};

export type MatchCardPlay = {
  id: number;
  gameNumber?: number;
  instanceId: number;
  cardId: number;
  cardName?: string;
  ownerSeatId?: number;
  playerSide: "self" | "opponent" | "unknown";
  firstPublicZone: string;
  turnNumber?: number;
  phase?: string;
  playedAt?: string;
};

export type MatchReplayChange = {
  instanceId: number;
  cardId: number;
  cardName?: string;
  ownerSeatId?: number;
  playerSide: "self" | "opponent" | "unknown";
  action: string;
  fromZoneId?: number;
  fromZoneType?: string;
  fromZonePosition?: number;
  toZoneId?: number;
  toZoneType?: string;
  toZonePosition?: number;
  isToken: boolean;
};

export type MatchReplayFrameObject = {
  id: number;
  frameId: number;
  instanceId: number;
  cardId: number;
  cardName?: string;
  ownerSeatId?: number;
  controllerSeatId?: number;
  playerSide: "self" | "opponent" | "unknown";
  zoneId?: number;
  zoneType: string;
  zonePosition?: number;
  visibility?: string;
  power?: number;
  toughness?: number;
  attackTargetId?: number;
  blockAttackerIdsJson?: string;
  counterSummaryJson?: string;
  detailsJson?: string;
  attackState?: string;
  blockState?: string;
  isToken: boolean;
  isTapped: boolean;
  hasSummoningSickness: boolean;
};

export type MatchReplayFrame = {
  id: number;
  gameNumber?: number;
  gameStateId?: number;
  prevGameStateId?: number;
  gameStateType?: string;
  gameStage?: string;
  turnNumber?: number;
  phase?: string;
  selfLifeTotal?: number;
  opponentLifeTotal?: number;
  winningPlayerSide?: "self" | "opponent" | "unknown";
  winReason?: string;
  recordedAt?: string;
  actionsJson?: string;
  annotationsJson?: string;
  objects?: MatchReplayFrameObject[];
  changes?: MatchReplayChange[];
};

export type MatchDetail = {
  match: Match;
  opponentObservedCards: OpponentObservedCard[];
  cardPlays: MatchCardPlay[];
  games: GameAnalytics[];
  coverage: MatchAnalyticsCoverage;
};

export type OpeningHandCard = {
  cardId: number;
  quantity: number;
  cardName?: string;
  kept: boolean;
};

export type OpeningHand = {
  id: number;
  attemptNumber: number;
  decision: "keep" | "mulligan" | "unknown";
  offeredHandSize: number;
  keptHandSize?: number;
  observedAt?: string;
  source: string;
  confidence: "exact" | "derived" | "unknown";
  cards: OpeningHandCard[];
};

export type GameAnalytics = {
  id: number;
  gameNumber: number;
  result: "win" | "loss" | "draw" | "unknown";
  winReason?: string;
  playDraw?: "play" | "draw" | "";
  startedAt?: string;
  endedAt?: string;
  turnCount?: number;
  openingLifeTotal?: number;
  endingLifeTotal?: number;
  mulliganCount?: number;
  keptHandSize?: number;
  resultSource?: string;
  resultConfidence: "exact" | "derived" | "unknown";
  playDrawSource?: string;
  playDrawConfidence: "exact" | "derived" | "unknown";
  openingHandSource?: string;
  openingHandConfidence: "exact" | "derived" | "unknown";
  openingHands: OpeningHand[];
};

export type MatchAnalyticsCoverage = {
  replayAvailable: boolean;
  replayFrameCount: number;
  gameCount: number;
  gamesWithResult: number;
  gamesWithOpeningHand: number;
  gamesWithPlayDraw: number;
  deckSnapshotAvailable: boolean;
  deckVersionAvailable: boolean;
  overallConfidence: "complete" | "partial" | "unknown";
  derivedAt?: string;
};

export type Overview = {
  playerName?: string;
  totalMatches: number;
  wins: number;
  losses: number;
  winRate: number;
  recent: Match[];
};

export type WildcardBalance = {
  common: number;
  uncommon: number;
  rare: number;
  mythic: number;
};

export type EconomyBoosterCount = {
  setCode: string;
  count: number;
};

export type EconomySnapshot = {
  id: number;
  observedAt: string;
  sequenceId: number;
  gold: number;
  gems: number;
  vaultProgress: number;
  wildcardTrackPosition: number;
  wildcards: WildcardBalance;
  customTokens: Record<string, number>;
  boosters: EconomyBoosterCount[];
  vouchers: Record<string, number>;
  changeSources: string[];
};

export type EconomyHistory = {
  latest: EconomySnapshot | null;
  history: EconomySnapshot[];
};

export type RankState = {
  seasonOrdinal?: number | null;
  rankClass: string;
  level?: number | null;
  step?: number | null;
  matchesWon?: number | null;
  matchesLost?: number | null;
};

export type RankHistoryPoint = {
  matchId: number;
  arenaMatchId: string;
  eventName: string;
  opponent: string;
  result: "win" | "loss" | "unknown";
  observedAt: string;
  endedAt: string;
  constructed: RankState;
  limited: RankState;
};

export type DeckSummary = {
  deckId: number;
  deckName: string;
  format: string;
  eventName: string;
  matches: number;
  wins: number;
  losses: number;
  winRate: number;
  firstPlayedAt?: string;
  lastUpdatedAt?: string;
};

export type DeckCard = {
  section: string;
  cardId: number;
  quantity: number;
  cardName?: string;
};

export type DeckDetail = {
  deckId: number;
  arenaDeckId: string;
  name: string;
  format: string;
  eventName: string;
  cards: DeckCard[];
  matches: Match[] | null;
  versions: DeckVersion[];
};

export type DeckVersion = {
  id: number;
  versionNumber: number;
  cardsHash: string;
  source?: string;
  effectiveAt?: string;
  cards: DeckCard[];
};

export type AiStatus = {
  available: boolean;
  cliPath?: string;
  version?: string;
  detail?: string;
};

export type DeckPrimer = {
  deckId: number;
  cardsHash: string;
  model: string;
  content: string;
  createdAt: string;
  stale: boolean;
};

export type DraftSession = {
  id: number;
  eventName: string;
  draftId?: string | null;
  isBotDraft: boolean;
  startedAt: string;
  completedAt: string;
  picks: number;
  wins?: number | null;
  losses?: number | null;
};

export type DraftPick = {
  id: number;
  packNumber: number;
  pickNumber: number;
  pickedCardIds: string;
  packCardIds: string;
  pickTs: string;
  pickedCards?: DraftPickCard[];
  packCards?: DraftPickCard[];
};

export type DraftPickCard = {
  cardId: number;
  cardName?: string;
};

export type RuntimeConfig = {
  logPath: string;
  pollIntervalSeconds: number;
  includePrev: boolean;
  autoStartLive: boolean;
  autoCheckUpdates: boolean;
};

export type RuntimeOperation = {
  kind: "import" | "live";
  files: string[];
  linesRead: number;
  bytesRead: number;
  rawEventsStored: number;
  matchesUpserted: number;
  rankSnapshots: number;
  economySnapshots: number;
  decksUpserted: number;
  draftPicksAdded: number;
  startedAt: string;
  completedAt: string;
  durationMs: number;
  hasActivity: boolean;
};

export type RuntimeStatus = {
  version: string;
  dbPath: string;
  dbSizeBytes: number;
  supportDir: string;
  configPath: string;
  defaultLogPath: string;
  defaultPrevLogPath: string;
  config: RuntimeConfig;
  activeLogPath: string;
  previousLogPath?: string;
  activeLogPathExists: boolean;
  previousLogPathExists: boolean;
  liveRunning: boolean;
  liveStartedAt?: string;
  liveLastTickAt?: string;
  lastImport?: RuntimeOperation;
  lastLiveActivity?: RuntimeOperation;
  lastError?: string;
  capabilities?: RuntimeCapabilities;
  updateCheck?: UpdateCheck;
};

export type RuntimeCapabilities = {
  pickFile: boolean;
  reveal: boolean;
};

export type AutostartStatus = {
  supported: boolean;
  enabled: boolean;
  agentPath?: string;
  executable?: string;
  note?: string;
};

export type UpdateCheck = {
  currentVersion: string;
  latestVersion?: string;
  updateAvailable: boolean;
  releaseUrl?: string;
  note?: string;
  checkedAt?: string;
};
