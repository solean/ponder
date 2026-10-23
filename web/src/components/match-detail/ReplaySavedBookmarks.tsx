import { useEffect, useState } from "react";
import { useNavigate } from "react-router-dom";
import { parseReplayLink, readReplayBookmarks, replayBookmarkPath, writeReplayBookmarks } from "../../lib/replay/position";

export function ReplaySavedBookmarks({ matchId }: { matchId: number }) {
  const navigate = useNavigate();
  const [entries, setEntries] = useState(() => readReplayBookmarks(matchId));
  const [link, setLink] = useState("");
  const [error, setError] = useState("");
  useEffect(() => {
    const update = () => setEntries(readReplayBookmarks(matchId));
    window.addEventListener("ponder-replay-bookmarks", update);
    return () => window.removeEventListener("ponder-replay-bookmarks", update);
  }, [matchId]);
  return <details className="match-replay-saved-bookmarks">
    <summary>Local bookmarks ({entries.length})</summary>
    <p>Bookmarks stay on this device. Paste a copied replay link here to reopen it in Ponder.</p>
    <form onSubmit={event => {
      event.preventDefault();
      const target = parseReplayLink(link);
      if (!target) { setError("This replay link is invalid. Paste a full replay link or /matches/… replay path."); return; }
      setError("");
      navigate(replayBookmarkPath(target.matchId, target));
    }}>
      <input aria-label="Replay link" placeholder="Paste replay link" value={link} onChange={event => setLink(event.target.value)} />
      <button type="submit">Open replay link</button>
    </form>
    {error ? <p role="alert">{error}</p> : null}
    {entries.length ? <ul>{entries.map(entry => <li key={`${entry.kind}:${entry.game}:${entry.frameId}`}>
      <button type="button" onClick={() => navigate(replayBookmarkPath(entry.matchId, entry))}>Game {entry.game} · {entry.kind === "frame" ? "Frame" : "Observed action"} {entry.frameId}</button>
      <button type="button" onClick={() => writeReplayBookmarks(matchId, entries.filter(other => other !== entry))}>Remove</button>
    </li>)}</ul> : <p>No saved bookmarks for this match.</p>}
  </details>;
}
