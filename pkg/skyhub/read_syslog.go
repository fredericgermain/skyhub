package skyhub

import (
	"bufio"
	"bytes"
	"context"
	"regexp"
	"time"
)

var syslogLineRE = regexp.MustCompile(`^(\d{4}-\d\d-\d\d \d\d:\d\d:\d\d)\s+([^:]+):\s*(.*)$`)

// Syslog reads sky_sys.log.
func (c *Client) Syslog(ctx context.Context) ([]SyslogEntry, error) {
	b, err := c.GetText(ctx, "sky_sys.log")
	if err != nil {
		return nil, err
	}
	return ParseSyslog(b), nil
}

// ParseSyslog parses "YYYY-MM-DD HH:MM:SS facility: message" lines. Times
// are in the hub's local zone and returned as time.Local.
func ParseSyslog(b []byte) []SyslogEntry {
	var out []SyslogEntry
	sc := bufio.NewScanner(bytes.NewReader(b))
	sc.Buffer(make([]byte, 0, 64*1024), 1<<20)
	for sc.Scan() {
		line := sc.Text()
		m := syslogLineRE.FindStringSubmatch(line)
		if m == nil {
			if line != "" && len(out) > 0 {
				out[len(out)-1].Message += "\n" + line
			}
			continue
		}
		t, _ := time.ParseInLocation("2006-01-02 15:04:05", m[1], time.Local)
		out = append(out, SyslogEntry{Time: t, Facility: m[2], Message: m[3]})
	}
	return out
}
