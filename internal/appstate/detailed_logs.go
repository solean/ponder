package appstate

import (
	"bufio"
	"io"
	"os"
	"strings"
)

// Detailed-logs states reported on Status. An empty value means Ponder could
// not tell: no log yet, or Arena has not written its startup header.
const (
	DetailedLogsEnabled  = "enabled"
	DetailedLogsDisabled = "disabled"
)

const detailedLogsPrefix = "DETAILED LOGS:"

// Arena writes the line about 25 lines into Player.log; this bound keeps the
// check cheap on a huge log that never had the header.
const detailedLogsScanLimit = 256 << 10

// detectDetailedLogs reads Arena's startup header, which states whether the
// "Detailed Logs (Plugin Support)" option was on when the client launched.
func detectDetailedLogs(path string) string {
	if strings.TrimSpace(path) == "" {
		return ""
	}
	file, err := os.Open(path)
	if err != nil {
		return ""
	}
	defer file.Close()
	return scanDetailedLogs(io.LimitReader(file, detailedLogsScanLimit))
}

func scanDetailedLogs(r io.Reader) string {
	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 0, 64<<10), detailedLogsScanLimit)
	for scanner.Scan() {
		value, ok := strings.CutPrefix(strings.TrimSpace(scanner.Text()), detailedLogsPrefix)
		if !ok {
			continue
		}
		switch strings.ToUpper(strings.TrimSpace(value)) {
		case "ENABLED":
			return DetailedLogsEnabled
		case "DISABLED":
			return DetailedLogsDisabled
		}
		return ""
	}
	return ""
}
