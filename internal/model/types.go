package model

import "time"

type ParseStats struct {
	LogPath          string
	LinesRead        int64
	BytesRead        int64
	RawEventsStored  int64
	MatchesUpserted  int64
	RankSnapshots    int64
	EconomySnapshots int64
	DecksUpserted    int64
	DraftPicksAdded  int64
	StartedAt        time.Time
	CompletedAt      time.Time
}

type MatchRow struct {
	ID                      int64    `json:"id"`
	ArenaMatchID            string   `json:"arenaMatchId"`
	EventName               string   `json:"eventName"`
	BestOf                  string   `json:"bestOf"`
	PlayDraw                string   `json:"playDraw"`
	Opponent                string   `json:"opponent"`
	StartedAt               string   `json:"startedAt"`
	EndedAt                 string   `json:"endedAt"`
	Result                  string   `json:"result"`
	WinReason               string   `json:"winReason"`
	TurnCount               *int64   `json:"turnCount"`
	SecondsCount            *int64   `json:"secondsCount"`
	DeckID                  *int64   `json:"deckId"`
	DeckName                *string  `json:"deckName"`
	DeckVersionID           *int64   `json:"deckVersionId,omitempty"`
	DeckVersionNumber       *int64   `json:"deckVersionNumber,omitempty"`
	DeckColors              []string `json:"deckColors"`
	DeckColorsKnown         bool     `json:"deckColorsKnown"`
	OpponentDeckColors      []string `json:"opponentDeckColors"`
	OpponentDeckColorsKnown bool     `json:"opponentDeckColorsKnown"`
}

type OpponentObservedCardRow struct {
	CardID   int64  `json:"cardId"`
	Quantity int64  `json:"quantity"`
	CardName string `json:"cardName,omitempty"`
}

type MatchCardPlayRow struct {
	ID              int64  `json:"id"`
	GameNumber      *int64 `json:"gameNumber,omitempty"`
	InstanceID      int64  `json:"instanceId"`
	CardID          int64  `json:"cardId"`
	CardName        string `json:"cardName,omitempty"`
	OwnerSeatID     *int64 `json:"ownerSeatId,omitempty"`
	PlayerSide      string `json:"playerSide"`
	FirstPublicZone string `json:"firstPublicZone"`
	TurnNumber      *int64 `json:"turnNumber,omitempty"`
	Phase           string `json:"phase,omitempty"`
	PlayedAt        string `json:"playedAt,omitempty"`
}

type MatchReplayChangeRow struct {
	InstanceID       int64  `json:"instanceId"`
	CardID           int64  `json:"cardId"`
	CardName         string `json:"cardName,omitempty"`
	OwnerSeatID      *int64 `json:"ownerSeatId,omitempty"`
	PlayerSide       string `json:"playerSide"`
	Action           string `json:"action"`
	FromZoneID       *int64 `json:"fromZoneId,omitempty"`
	FromZoneType     string `json:"fromZoneType,omitempty"`
	FromZonePosition *int64 `json:"fromZonePosition,omitempty"`
	ToZoneID         *int64 `json:"toZoneId,omitempty"`
	ToZoneType       string `json:"toZoneType,omitempty"`
	ToZonePosition   *int64 `json:"toZonePosition,omitempty"`
	IsToken          bool   `json:"isToken"`
}

type MatchReplayFrameObjectRow struct {
	ID                   int64  `json:"id"`
	FrameID              int64  `json:"frameId"`
	InstanceID           int64  `json:"instanceId"`
	CardID               int64  `json:"cardId"`
	CardName             string `json:"cardName,omitempty"`
	OwnerSeatID          *int64 `json:"ownerSeatId,omitempty"`
	ControllerSeatID     *int64 `json:"controllerSeatId,omitempty"`
	PlayerSide           string `json:"playerSide"`
	ZoneID               *int64 `json:"zoneId,omitempty"`
	ZoneType             string `json:"zoneType"`
	ZonePosition         *int64 `json:"zonePosition,omitempty"`
	Visibility           string `json:"visibility,omitempty"`
	Power                *int64 `json:"power,omitempty"`
	Toughness            *int64 `json:"toughness,omitempty"`
	AttackTargetID       *int64 `json:"attackTargetId,omitempty"`
	BlockAttackerIDsJSON string `json:"blockAttackerIdsJson,omitempty"`
	CounterSummaryJSON   string `json:"counterSummaryJson,omitempty"`
	DetailsJSON          string `json:"detailsJson,omitempty"`
	AttackState          string `json:"attackState,omitempty"`
	BlockState           string `json:"blockState,omitempty"`
	IsToken              bool   `json:"isToken"`
	IsTapped             bool   `json:"isTapped"`
	HasSummoningSickness bool   `json:"hasSummoningSickness"`
}

type MatchReplayFrameRow struct {
	ID                int64                       `json:"id"`
	GameNumber        *int64                      `json:"gameNumber,omitempty"`
	GameStateID       *int64                      `json:"gameStateId,omitempty"`
	PrevGameStateID   *int64                      `json:"prevGameStateId,omitempty"`
	GameStateType     string                      `json:"gameStateType,omitempty"`
	GameStage         string                      `json:"gameStage,omitempty"`
	TurnNumber        *int64                      `json:"turnNumber,omitempty"`
	Phase             string                      `json:"phase,omitempty"`
	SelfLifeTotal     *int64                      `json:"selfLifeTotal,omitempty"`
	OpponentLifeTotal *int64                      `json:"opponentLifeTotal,omitempty"`
	WinningPlayerSide string                      `json:"winningPlayerSide,omitempty"`
	WinReason         string                      `json:"winReason,omitempty"`
	RecordedAt        string                      `json:"recordedAt,omitempty"`
	ActionsJSON       string                      `json:"actionsJson,omitempty"`
	AnnotationsJSON   string                      `json:"annotationsJson,omitempty"`
	Objects           []MatchReplayFrameObjectRow `json:"objects,omitempty"`
	Changes           []MatchReplayChangeRow      `json:"changes,omitempty"`
}

type MatchDetail struct {
	Match                 MatchRow                  `json:"match"`
	OpponentObservedCards []OpponentObservedCardRow `json:"opponentObservedCards"`
	CardPlays             []MatchCardPlayRow        `json:"cardPlays"`
	Games                 []GameRow                 `json:"games"`
	Coverage              MatchAnalyticsCoverage    `json:"coverage"`
}

type OpeningHandCardRow struct {
	CardID   int64  `json:"cardId"`
	Quantity int64  `json:"quantity"`
	CardName string `json:"cardName,omitempty"`
	Kept     bool   `json:"kept"`
}

type OpeningHandRow struct {
	ID              int64                `json:"id"`
	AttemptNumber   int64                `json:"attemptNumber"`
	Decision        string               `json:"decision"`
	OfferedHandSize int64                `json:"offeredHandSize"`
	KeptHandSize    *int64               `json:"keptHandSize,omitempty"`
	ObservedAt      string               `json:"observedAt,omitempty"`
	Source          string               `json:"source"`
	Confidence      string               `json:"confidence"`
	Cards           []OpeningHandCardRow `json:"cards"`
}

type GameRow struct {
	ID                    int64            `json:"id"`
	GameNumber            int64            `json:"gameNumber"`
	Result                string           `json:"result"`
	WinReason             string           `json:"winReason,omitempty"`
	PlayDraw              string           `json:"playDraw,omitempty"`
	StartedAt             string           `json:"startedAt,omitempty"`
	EndedAt               string           `json:"endedAt,omitempty"`
	TurnCount             *int64           `json:"turnCount,omitempty"`
	OpeningLifeTotal      *int64           `json:"openingLifeTotal,omitempty"`
	EndingLifeTotal       *int64           `json:"endingLifeTotal,omitempty"`
	MulliganCount         *int64           `json:"mulliganCount,omitempty"`
	KeptHandSize          *int64           `json:"keptHandSize,omitempty"`
	ResultSource          string           `json:"resultSource,omitempty"`
	ResultConfidence      string           `json:"resultConfidence"`
	PlayDrawSource        string           `json:"playDrawSource,omitempty"`
	PlayDrawConfidence    string           `json:"playDrawConfidence"`
	OpeningHandSource     string           `json:"openingHandSource,omitempty"`
	OpeningHandConfidence string           `json:"openingHandConfidence"`
	OpeningHands          []OpeningHandRow `json:"openingHands"`
}

type MatchAnalyticsCoverage struct {
	ReplayAvailable       bool   `json:"replayAvailable"`
	ReplayFrameCount      int64  `json:"replayFrameCount"`
	GameCount             int64  `json:"gameCount"`
	GamesWithResult       int64  `json:"gamesWithResult"`
	GamesWithOpeningHand  int64  `json:"gamesWithOpeningHand"`
	GamesWithPlayDraw     int64  `json:"gamesWithPlayDraw"`
	DeckSnapshotAvailable bool   `json:"deckSnapshotAvailable"`
	DeckVersionAvailable  bool   `json:"deckVersionAvailable"`
	OverallConfidence     string `json:"overallConfidence"`
	DerivedAt             string `json:"derivedAt,omitempty"`
}

type DeckSummaryRow struct {
	DeckID        int64   `json:"deckId"`
	DeckName      string  `json:"deckName"`
	Format        string  `json:"format"`
	EventName     string  `json:"eventName"`
	Matches       int64   `json:"matches"`
	Wins          int64   `json:"wins"`
	Losses        int64   `json:"losses"`
	WinRate       float64 `json:"winRate"`
	FirstPlayedAt string  `json:"firstPlayedAt,omitempty"`
	LastUpdatedAt string  `json:"lastUpdatedAt,omitempty"`
}

type DeckCardRow struct {
	Section  string `json:"section"`
	CardID   int64  `json:"cardId"`
	Quantity int64  `json:"quantity"`
	CardName string `json:"cardName,omitempty"`
}

type DeckDetail struct {
	DeckID      int64            `json:"deckId"`
	ArenaDeckID string           `json:"arenaDeckId"`
	Name        string           `json:"name"`
	Format      string           `json:"format"`
	EventName   string           `json:"eventName"`
	Cards       []DeckCardRow    `json:"cards"`
	Matches     []MatchRow       `json:"matches"`
	Versions    []DeckVersionRow `json:"versions"`
}

type DeckVersionRow struct {
	ID            int64         `json:"id"`
	VersionNumber int64         `json:"versionNumber"`
	CardsHash     string        `json:"cardsHash"`
	Source        string        `json:"source,omitempty"`
	EffectiveAt   string        `json:"effectiveAt,omitempty"`
	Cards         []DeckCardRow `json:"cards,omitempty"`
}

// DeckPrimer is a cached AI-generated strategy primer for a deck. Stale is
// computed at read time by comparing CardsHash against the current deck list.
type DeckPrimer struct {
	DeckID    int64  `json:"deckId"`
	CardsHash string `json:"cardsHash"`
	Model     string `json:"model"`
	Content   string `json:"content"`
	CreatedAt string `json:"createdAt"`
	Stale     bool   `json:"stale"`
}

type DraftSessionRow struct {
	ID          int64   `json:"id"`
	EventName   string  `json:"eventName"`
	DraftID     *string `json:"draftId"`
	IsBotDraft  bool    `json:"isBotDraft"`
	StartedAt   string  `json:"startedAt"`
	CompletedAt string  `json:"completedAt"`
	Picks       int64   `json:"picks"`
	Wins        *int64  `json:"wins,omitempty"`
	Losses      *int64  `json:"losses,omitempty"`
}

type DraftPickRow struct {
	ID            int64           `json:"id"`
	PackNumber    int64           `json:"packNumber"`
	PickNumber    int64           `json:"pickNumber"`
	PickedCardIDs string          `json:"pickedCardIds"`
	PackCardIDs   string          `json:"packCardIds"`
	PickTs        string          `json:"pickTs"`
	PickedCards   []DraftPickCard `json:"pickedCards,omitempty"`
	PackCards     []DraftPickCard `json:"packCards,omitempty"`
}

type DraftPickCard struct {
	CardID   int64  `json:"cardId"`
	CardName string `json:"cardName,omitempty"`
}

type LiveMatch struct {
	Match                 MatchRow                  `json:"match"`
	OpponentObservedCards []OpponentObservedCardRow `json:"opponentObservedCards"`
	Deck                  []DeckCardRow             `json:"deck"`
	DeckTotal             int64                     `json:"deckTotal"`
	LandCount             int64                     `json:"landCount"`
	GameNumber            int64                     `json:"gameNumber"`
	TurnNumber            int64                     `json:"turnNumber"`
	LibraryEstimate       int64                     `json:"libraryEstimate"`
}

type SetInfo struct {
	Code       string `json:"code"`
	Name       string `json:"name"`
	IconSvgURI string `json:"iconSvgUri,omitempty"`
	ReleasedAt string `json:"releasedAt,omitempty"`
}

type Overview struct {
	PlayerName   string     `json:"playerName,omitempty"`
	TotalMatches int64      `json:"totalMatches"`
	Wins         int64      `json:"wins"`
	Losses       int64      `json:"losses"`
	WinRate      float64    `json:"winRate"`
	Recent       []MatchRow `json:"recent"`
}

type WildcardBalance struct {
	Common   int64 `json:"common"`
	Uncommon int64 `json:"uncommon"`
	Rare     int64 `json:"rare"`
	Mythic   int64 `json:"mythic"`
}

type EconomyBoosterCount struct {
	SetCode string `json:"setCode"`
	Count   int64  `json:"count"`
}

type EconomySnapshot struct {
	ID                    int64                 `json:"id"`
	ObservedAt            string                `json:"observedAt"`
	SequenceID            int64                 `json:"sequenceId"`
	Gold                  int64                 `json:"gold"`
	Gems                  int64                 `json:"gems"`
	VaultProgress         int64                 `json:"vaultProgress"`
	WildcardTrackPosition int64                 `json:"wildcardTrackPosition"`
	Wildcards             WildcardBalance       `json:"wildcards"`
	CustomTokens          map[string]int64      `json:"customTokens"`
	Boosters              []EconomyBoosterCount `json:"boosters"`
	Vouchers              map[string]int64      `json:"vouchers"`
	ChangeSources         []string              `json:"changeSources"`
}

type EconomyHistory struct {
	Latest  *EconomySnapshot  `json:"latest"`
	History []EconomySnapshot `json:"history"`
}

type RankState struct {
	SeasonOrdinal *int64 `json:"seasonOrdinal"`
	RankClass     string `json:"rankClass"`
	Level         *int64 `json:"level"`
	Step          *int64 `json:"step"`
	MatchesWon    *int64 `json:"matchesWon"`
	MatchesLost   *int64 `json:"matchesLost"`
}

type RankHistoryPoint struct {
	MatchID      int64     `json:"matchId"`
	ArenaMatchID string    `json:"arenaMatchId"`
	EventName    string    `json:"eventName"`
	Opponent     string    `json:"opponent"`
	Result       string    `json:"result"`
	ObservedAt   string    `json:"observedAt"`
	EndedAt      string    `json:"endedAt"`
	Constructed  RankState `json:"constructed"`
	Limited      RankState `json:"limited"`
}
