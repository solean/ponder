package ingest

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"math"
	"os"
	"strconv"
	"strings"
	"time"
)

// Unity log headers carry a wall-clock time with no zone. Since Arena's
// Oct 20 2026 update, bare headers (the ones before front-door responses)
// are written in UTC while match-connection headers (": Match to …",
// ": … to Match: …") stay in local time. Each kind's zone is therefore learned
// from UTC anchors in the log instead of assumed.
type headerZone string

const (
	headerZoneUnknown headerZone = ""
	headerZoneLocal   headerZone = "local"
	headerZoneUTC     headerZone = "utc"
)

type headerKind int

const (
	headerKindBare headerKind = iota
	headerKindSuffixed
	headerKindCount
)

const (
	// The two readings of a header differ by the local UTC offset, so an
	// anchor this close picks one unambiguously for any offset of an hour or
	// more. With a UTC local zone both readings agree and nothing is learned.
	headerAnchorTolerance = 15 * time.Minute
	// A bare header is only compared with an anchor written shortly before it
	// in the log; a long idle gap could otherwise line up with the wrong
	// reading.
	headerAnchorMaxLines = 2000
	// How far primeHeaderZones reads ahead for evidence. Arena reaches its
	// first match-connection anchor within a few thousand lines of launch.
	headerPrimeMaxLines = 20000
)

// parseUnityLogHeader returns a header's wall-clock time with the zone left
// unresolved (as UTC fields) and whether text follows the timestamp.
func parseUnityLogHeader(line string) (time.Time, headerKind, bool) {
	m := reUnityLogTimestamp.FindStringSubmatch(strings.TrimSpace(line))
	if len(m) != 3 {
		return time.Time{}, headerKindBare, false
	}
	wall, err := time.Parse("1/2/2006 3:04:05 PM", m[1])
	if err != nil {
		return time.Time{}, headerKindBare, false
	}
	if strings.TrimSpace(m[2]) != "" {
		return wall, headerKindSuffixed, true
	}
	return wall, headerKindBare, true
}

func (z headerZone) resolve(wall time.Time, loc *time.Location) time.Time {
	if z == headerZoneUTC {
		return wall
	}
	return time.Date(wall.Year(), wall.Month(), wall.Day(), wall.Hour(), wall.Minute(), wall.Second(), 0, loc).UTC()
}

// headerZoneNear reports which reading of wall lands within tolerance of
// anchor, or unknown when neither or both do.
func headerZoneNear(wall, anchor time.Time, loc *time.Location) headerZone {
	utcOK := absDuration(headerZoneUTC.resolve(wall, loc).Sub(anchor)) <= headerAnchorTolerance
	localOK := absDuration(headerZoneLocal.resolve(wall, loc).Sub(anchor)) <= headerAnchorTolerance
	switch {
	case utcOK && !localOK:
		return headerZoneUTC
	case localOK && !utcOK:
		return headerZoneLocal
	}
	return headerZoneUnknown
}

func absDuration(d time.Duration) time.Duration {
	if d < 0 {
		return -d
	}
	return d
}

// parseLogClockAnchor reads the UTC "timestamp" that match-connection JSON
// carries, either as Unix milliseconds (GRE) or as .NET ticks (envelopes).
func parseLogClockAnchor(line string) (time.Time, bool) {
	idx := strings.Index(line, `"timestamp"`)
	if idx < 0 {
		return time.Time{}, false
	}
	rest := strings.TrimLeft(line[idx+len(`"timestamp"`):], " \t")
	rest, ok := strings.CutPrefix(rest, ":")
	if !ok {
		return time.Time{}, false
	}
	rest, ok = strings.CutPrefix(strings.TrimLeft(rest, " \t"), `"`)
	if !ok {
		return time.Time{}, false
	}
	end := strings.IndexByte(rest, '"')
	if end < 0 {
		return time.Time{}, false
	}
	n, err := strconv.ParseInt(rest[:end], 10, 64)
	if err != nil {
		return time.Time{}, false
	}
	var anchor time.Time
	switch end {
	case 13:
		anchor = time.UnixMilli(n).UTC()
	case 18:
		const ticksPerSecond = 10_000_000
		const unixEpochTicks = 621355968000000000
		anchor = time.Unix(0, (n-unixEpochTicks)*(1_000_000_000/ticksPerSecond)).UTC()
	default:
		return time.Time{}, false
	}
	if anchor.Year() < 2020 || anchor.Year() > 2100 {
		return time.Time{}, false
	}
	return anchor, true
}

func (p *Parser) headerLocation() *time.Location {
	if p.headerLoc != nil {
		return p.headerLoc
	}
	return time.Local
}

func (p *Parser) headerZone(kind headerKind) headerZone {
	p.stateMu.Lock()
	defer p.stateMu.Unlock()
	return p.headerZones[kind]
}

func (p *Parser) learnHeaderZone(kind headerKind, zone headerZone) {
	if zone == headerZoneUnknown {
		return
	}
	p.stateMu.Lock()
	defer p.stateMu.Unlock()
	p.headerZones[kind] = zone
}

// observeLogClock updates the header zones from any anchor on this line and
// stamps state.lastUnityLogTimestamp from a header line.
func (p *Parser) observeLogClock(state *parseState, lineNo int64, line string) {
	loc := p.headerLocation()
	if anchor, ok := parseLogClockAnchor(line); ok {
		state.lastClockAnchor = anchor
		state.lastClockAnchorLine = lineNo
		// The JSON right after a match-connection header carries that
		// header's own time.
		if !state.pendingSuffixedHeader.IsZero() {
			zone := headerZoneNear(state.pendingSuffixedHeader, anchor, loc)
			if zone != headerZoneUnknown && zone != p.headerZone(headerKindSuffixed) {
				p.learnHeaderZone(headerKindSuffixed, zone)
				state.lastUnityLogTimestamp = zone.resolve(state.pendingSuffixedHeader, loc).Format(time.RFC3339Nano)
			}
			state.pendingSuffixedHeader = time.Time{}
		}
	}

	wall, kind, ok := parseUnityLogHeader(line)
	if !ok {
		return
	}
	state.pendingSuffixedHeader = time.Time{}
	switch kind {
	case headerKindSuffixed:
		state.pendingSuffixedHeader = wall
	case headerKindBare:
		if !state.lastClockAnchor.IsZero() && lineNo-state.lastClockAnchorLine <= headerAnchorMaxLines {
			p.learnHeaderZone(headerKindBare, headerZoneNear(wall, state.lastClockAnchor, loc))
		}
	}
	zone := p.headerZone(kind)
	if zone == headerZoneUnknown {
		zone = headerZoneLocal
	}
	state.lastUnityLogTimestamp = zone.resolve(wall, loc).Format(time.RFC3339Nano)
}

// primeHeaderZones learns any still-unknown header zone from the lines ahead
// of offset, so the headers before a log's first anchor resolve correctly.
// Once both zones are known (and checkpointed) this is a no-op.
func (p *Parser) primeHeaderZones(file *os.File, offset int64) error {
	if p.headerZone(headerKindBare) != headerZoneUnknown && p.headerZone(headerKindSuffixed) != headerZoneUnknown {
		return nil
	}
	reader := bufio.NewReaderSize(io.NewSectionReader(file, offset, math.MaxInt64-offset), 1024*1024)
	scratch := &parseState{}
	for lineNo := int64(1); lineNo <= headerPrimeMaxLines; lineNo++ {
		line, err := reader.ReadString('\n')
		if line != "" {
			p.observeLogClock(scratch, lineNo, line)
			if p.headerZone(headerKindBare) != headerZoneUnknown && p.headerZone(headerKindSuffixed) != headerZoneUnknown {
				return nil
			}
		}
		if errors.Is(err, io.EOF) {
			return nil
		}
		if err != nil {
			return fmt.Errorf("read log to prime header zones: %w", err)
		}
	}
	return nil
}
