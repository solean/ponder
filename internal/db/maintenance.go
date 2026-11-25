package db

import (
	"context"
	"fmt"
)

// VACUUM rewrites the whole file while holding the write slot, so live
// tracking stalls for as long as it runs. Only pay that when the free pages it
// would hand back are both sizeable and a meaningful share of the file.
const (
	vacuumMinFreeBytes    = 16 << 20
	vacuumMinFreeFraction = 0.10
)

// appMetadataReplayEncoderLevelKey records which zstd level existing replay
// archives were written with, so the one-time recompress pass runs only once.
const appMetadataReplayEncoderLevelKey = "replay_archive_encoder_level"
const replayEncoderLevelBest = "best"

// MaintenanceResult reports what a maintenance pass reclaimed.
type MaintenanceResult struct {
	ReplaysArchived      int
	ArchivesRecompressed int
	RawEventsPruned      int64
	AnalyticsRefreshed   int
	Vacuumed             bool
}

func (r MaintenanceResult) reclaimedAnything() bool {
	return r.ReplaysArchived > 0 || r.ArchivesRecompressed > 0 || r.RawEventsPruned > 0
}

// RunMaintenance performs the periodic space and hygiene work in one pass:
// compacts finished-match replay rows into archives, recompresses archives
// written at the old zstd level (once), prunes raw events nothing reads,
// backfills draft metadata, and — when anything was reclaimed — truncates the
// WAL and, if enough of the file is free pages, VACUUMs so the space returns to
// the filesystem.
func (s *Store) RunMaintenance(ctx context.Context) (MaintenanceResult, error) {
	result := MaintenanceResult{}

	// Maintenance and log ingest are the two long write workloads; serialize
	// against ingest so neither aborts the other with SQLITE_BUSY.
	release, err := s.AcquireWriter(ctx)
	if err != nil {
		return result, err
	}
	defer release()

	archived, err := s.CompactMatchReplays(ctx)
	result.ReplaysArchived = archived
	if err != nil {
		return result, err
	}

	recompressed, err := s.recompressReplayArchivesOnce(ctx)
	result.ArchivesRecompressed = recompressed
	if err != nil {
		return result, err
	}

	pruned, err := s.PruneRawEvents(ctx)
	result.RawEventsPruned = pruned
	if err != nil {
		return result, err
	}

	if err := s.RepairDraftDataFromRawEvents(ctx); err != nil {
		return result, err
	}
	if err := s.RepairEventRunInstances(ctx); err != nil {
		return result, err
	}

	refreshed, err := s.RefreshPendingMatchAnalytics(ctx)
	result.AnalyticsRefreshed = refreshed
	if err != nil {
		return result, err
	}

	if result.reclaimedAnything() {
		vacuum, err := s.vacuumWorthwhile(ctx)
		if err != nil {
			return result, err
		}
		if vacuum {
			if _, err := s.db.ExecContext(ctx, `VACUUM`); err != nil {
				return result, fmt.Errorf("vacuum after maintenance: %w", err)
			}
			result.Vacuumed = true
		}
		// Hand freed WAL pages back to the filesystem too; busy is fine, the
		// next checkpoint will pick it up.
		var busy, logFrames, checkpointed int64
		_ = s.db.QueryRowContext(ctx, `PRAGMA wal_checkpoint(TRUNCATE)`).Scan(&busy, &logFrames, &checkpointed)
	}

	return result, nil
}

// RunMaintenanceIfIdle runs a maintenance pass unless a match is live, so
// periodic passes never compete with in-game ingest. ran is false when the pass
// was skipped; the caller should try again later.
func (s *Store) RunMaintenanceIfIdle(ctx context.Context) (result MaintenanceResult, ran bool, err error) {
	_, live, err := s.GetLiveMatchID(ctx)
	if err != nil {
		return result, false, err
	}
	if live {
		return result, false, nil
	}
	result, err = s.RunMaintenance(ctx)
	return result, true, err
}

// vacuumWorthwhile reports whether the free pages left by deletes are worth a
// full VACUUM. Deleted rows land on the freelist and are reused by later
// inserts, so a small freelist costs nothing to keep.
func (s *Store) vacuumWorthwhile(ctx context.Context) (bool, error) {
	var pageSize, pageCount, freePages int64
	if err := s.db.QueryRowContext(ctx, `PRAGMA page_size`).Scan(&pageSize); err != nil {
		return false, fmt.Errorf("read page size: %w", err)
	}
	if err := s.db.QueryRowContext(ctx, `PRAGMA page_count`).Scan(&pageCount); err != nil {
		return false, fmt.Errorf("read page count: %w", err)
	}
	if err := s.db.QueryRowContext(ctx, `PRAGMA freelist_count`).Scan(&freePages); err != nil {
		return false, fmt.Errorf("read freelist count: %w", err)
	}
	return shouldVacuum(freePages, pageCount, pageSize), nil
}

func shouldVacuum(freePages, pageCount, pageSize int64) bool {
	if pageCount <= 0 || freePages <= 0 {
		return false
	}
	if freePages*pageSize < vacuumMinFreeBytes {
		return false
	}
	return float64(freePages)/float64(pageCount) >= vacuumMinFreeFraction
}

// recompressReplayArchivesOnce rewrites every replay archive with the current
// zstd encoder level the first time it runs, then records that in
// app_metadata so later maintenance passes skip it.
func (s *Store) recompressReplayArchivesOnce(ctx context.Context) (int, error) {
	var level string
	err := s.db.QueryRowContext(ctx, `
		SELECT value FROM app_metadata WHERE key = ?
	`, appMetadataReplayEncoderLevelKey).Scan(&level)
	if err == nil && level == replayEncoderLevelBest {
		return 0, nil
	}

	rows, err := s.db.QueryContext(ctx, `SELECT match_id FROM match_replay_archives ORDER BY match_id ASC`)
	if err != nil {
		return 0, fmt.Errorf("list replay archives for recompress: %w", err)
	}
	matchIDs := make([]int64, 0)
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return 0, fmt.Errorf("scan replay archive id: %w", err)
		}
		matchIDs = append(matchIDs, id)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return 0, fmt.Errorf("iterate replay archive ids: %w", err)
	}
	rows.Close()

	recompressed := 0
	for _, matchID := range matchIDs {
		if err := ctx.Err(); err != nil {
			return recompressed, err
		}
		var compressed []byte
		if err := s.db.QueryRowContext(ctx, `
			SELECT payload_zstd FROM match_replay_archives WHERE match_id = ?
		`, matchID).Scan(&compressed); err != nil {
			return recompressed, fmt.Errorf("load replay archive %d for recompress: %w", matchID, err)
		}
		raw, err := getZstdDecoder().DecodeAll(compressed, nil)
		if err != nil {
			return recompressed, fmt.Errorf("decompress replay archive %d: %w", matchID, err)
		}
		reencoded := getZstdEncoder().EncodeAll(raw, nil)
		if len(reencoded) >= len(compressed) {
			continue
		}
		if _, err := s.db.ExecContext(ctx, `
			UPDATE match_replay_archives SET payload_zstd = ?, updated_at = ? WHERE match_id = ?
		`, reencoded, nowUTC(), matchID); err != nil {
			return recompressed, fmt.Errorf("update replay archive %d: %w", matchID, err)
		}
		recompressed++
	}

	if _, err := s.db.ExecContext(ctx, `
		INSERT INTO app_metadata (key, value, updated_at)
		VALUES (?, ?, ?)
		ON CONFLICT(key) DO UPDATE SET
			value = excluded.value,
			updated_at = excluded.updated_at
	`, appMetadataReplayEncoderLevelKey, replayEncoderLevelBest, nowUTC()); err != nil {
		return recompressed, fmt.Errorf("record replay encoder level: %w", err)
	}

	return recompressed, nil
}
