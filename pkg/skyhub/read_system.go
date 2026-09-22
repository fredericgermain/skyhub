package skyhub

import (
	"context"
	"regexp"
	"strconv"
	"strings"
)

var (
	dslSpeedRE = regexp.MustCompile(`Connection Speed\s*</td><td>\s*(\d+)\s*kbps\s*</td><td>\s*(\d+)\s*kbps`)
	dslAttenRE = regexp.MustCompile(`Line Attenuation\s*</td><td>([^<]*)</td><td>([^<]*)</td>`)
	dslNoiseRE = regexp.MustCompile(`Noise Margin\s*</td><td>\s*([\d.]+)\s*dB\s*</td><td>\s*([\d.]+)\s*dB`)
	dbListRE   = regexp.MustCompile(`\(([\d.]+)\s*dB\)`)
)

// SystemStats reads sky_system.html.
func (c *Client) SystemStats(ctx context.Context) (*SystemStats, error) {
	p, err := c.Get(ctx, "sky_system.html")
	if err != nil {
		return nil, err
	}
	return ParseSystemStats(p)
}

// ParseSystemStats parses a fetched statistics page.
func ParseSystemStats(p *Page) (*SystemStats, error) {
	doc, err := p.Doc()
	if err != nil {
		return nil, err
	}
	s := &SystemStats{}
	if n := findByID(doc, "router-stati-uptime-value"); n != nil {
		if d, err := parseHMS(nodeText(n)); err == nil {
			s.Uptime = Duration(d)
		}
	} else {
		return nil, &ParseError{Page: p.Path, What: "uptime span missing"}
	}
	tbl := findByID(doc, "router-statistics-top-table")
	if tbl == nil {
		return nil, &ParseError{Page: p.Path, What: "statistics table missing"}
	}
	for _, row := range tableRows(tbl) {
		if len(row) < 8 || row[0] == "Port" {
			continue
		}
		ps := PortStats{
			Name:       row[0],
			Key:        portKey(row[0]),
			Status:     row[1],
			TxPkts:     parseUint(row[2]),
			RxPkts:     parseUint(row[3]),
			Collisions: parseUint(row[4]),
			TxBps:      parseUint(row[5]),
			RxBps:      parseUint(row[6]),
		}
		if d, err := parseHMS(row[7]); err == nil {
			ps.Uptime = Duration(d)
		}
		s.Ports = append(s.Ports, ps)
	}
	if len(s.Ports) == 0 {
		return nil, &ParseError{Page: p.Path, What: "no port rows", Snippet: snippet(p.Body, 120)}
	}
	if m := dslSpeedRE.FindSubmatch(p.Body); m != nil {
		d := &DSLStats{DownKbps: parseIntDef(string(m[1]), 0), UpKbps: parseIntDef(string(m[2]), 0)}
		if a := dslAttenRE.FindSubmatch(p.Body); a != nil {
			d.AttenuationDownDB = dbList(string(a[1]))
			d.AttenuationUpDB = dbList(string(a[2]))
		}
		if n := dslNoiseRE.FindSubmatch(p.Body); n != nil {
			d.NoiseMarginDownDB, _ = strconv.ParseFloat(string(n[1]), 64)
			d.NoiseMarginUpDB, _ = strconv.ParseFloat(string(n[2]), 64)
		}
		s.DSL = d
	}
	return s, nil
}

func dbList(s string) []float64 {
	var out []float64
	for _, m := range dbListRE.FindAllStringSubmatch(s, -1) {
		v, _ := strconv.ParseFloat(m[1], 64)
		out = append(out, v)
	}
	return out
}

func portKey(name string) string {
	n := strings.ToLower(name)
	switch {
	case strings.HasPrefix(n, "wan"):
		return "wan"
	case strings.HasPrefix(n, "lan"):
		return "lan"
	case strings.Contains(n, "2.4"):
		return "wlan24"
	case strings.Contains(n, "5"):
		return "wlan5"
	}
	return strings.Join(strings.Fields(n), "_")
}
