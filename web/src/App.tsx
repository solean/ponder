import { Navigate, Route, Routes } from "react-router-dom";

import { Layout } from "./components/Layout";
import { DeckDetailPage } from "./pages/DeckDetailPage";
import { DecksPage } from "./pages/DecksPage";
import { DraftDetailPage } from "./pages/DraftDetailPage";
import { EconomyPage } from "./pages/EconomyPage";
import { DraftsPage } from "./pages/DraftsPage";
import { MatchDetailPage } from "./pages/MatchDetailPage";
import { MatchesPage } from "./pages/MatchesPage";
import { OverviewPage } from "./pages/OverviewPage";
import { OverlayPage } from "./pages/OverlayPage";
import { RankedPage } from "./pages/RankedPage";
import { SettingsPage } from "./pages/SettingsPage";

export function App() {
  return (
    <Routes>
      <Route path="/overlay" element={<OverlayPage />} />
      <Route path="/" element={<Layout />}>
        <Route index element={<OverviewPage />} />
        <Route path="matches" element={<MatchesPage />} />
        <Route path="matches/:matchId" element={<MatchDetailPage />} />
        <Route path="decks" element={<DecksPage />} />
        <Route path="ranked" element={<RankedPage />} />
        <Route path="economy" element={<EconomyPage />} />
        <Route path="decks/:deckId" element={<DeckDetailPage />} />
        <Route path="drafts" element={<DraftsPage />} />
        <Route path="drafts/:draftId" element={<DraftDetailPage />} />
        <Route path="settings" element={<SettingsPage />} />
        <Route path="*" element={<Navigate to="/" replace />} />
      </Route>
    </Routes>
  );
}
