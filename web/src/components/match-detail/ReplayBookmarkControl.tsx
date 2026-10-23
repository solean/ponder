import { useState } from "react";
import { readReplayBookmarks, writeReplayBookmarks, replayBookmarkPath, type ReplayBookmark } from "../../lib/replay/position";

export function ReplayBookmarkControl({ matchId, game, frameId, kind }: ReplayBookmark & { matchId: number }) {
  const [status, setStatus] = useState("");
  const path = replayBookmarkPath(matchId, { game, frameId, kind });
  return <span className="match-replay-bookmark">
    <button type="button" onClick={() => {
      const saved = readReplayBookmarks(matchId).filter(entry => !(entry.game === game && entry.frameId === frameId && entry.kind === kind));
      const ok = writeReplayBookmarks(matchId, [{ matchId, game, frameId, kind }, ...saved].slice(0, 50));
      setStatus(ok ? "Replay bookmark saved on this device." : "Local storage is unavailable. Copy the replay link instead.");
    }}>Save bookmark</button>
    <a href={path} onClick={event => { event.preventDefault(); void (navigator.clipboard?.writeText(new URL(path, window.location.href).href) ?? Promise.reject()).then(() => setStatus("Replay link copied."), () => setStatus("Copy unavailable. Use the link address to save this position.")); }}>Copy replay link</a>
    {status ? <span role="status">{status}</span> : null}
  </span>;
}
