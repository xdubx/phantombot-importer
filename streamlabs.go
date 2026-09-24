package main

import (
	"bytes"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/xuri/excelize/v2"
)

// export is everything parsed from a Streamlabs Chatbot export, merged across split files.
type export struct {
	points   map[string]int    // lowercase user -> points
	time     map[string]int    // lowercase user -> seconds
	quotes   [][4]string       // PhantomBot quote array: user, quote, epoch ms, game
	ranks    map[string]string // hours -> rank name
	commands []command
	timers   []notice
	warnings []string
}

type command struct {
	name, response string
	perm, cost     int
	enabled        bool
}

// notice is one PhantomBot timer group, as stored in the `notices` table.
type notice struct {
	Name                string   `json:"name"`
	ReqMessages         int      `json:"reqMessages"`
	IntervalMin         int      `json:"intervalMin"`
	IntervalMax         int      `json:"intervalMax"`
	Shuffle             bool     `json:"shuffle"`
	NoticeToggle        bool     `json:"noticeToggle"`
	NoticeOfflineToggle bool     `json:"noticeOfflineToggle"`
	Messages            []string `json:"messages"`
	Disabled            []bool   `json:"disabled"`
	GamesToggle         bool     `json:"gamesToggle"`
	Games               []string `json:"games"`
}

func newExport() *export {
	return &export{points: map[string]int{}, time: map[string]int{}, ranks: map[string]string{}}
}

func (e *export) warn(format string, a ...any) {
	e.warnings = append(e.warnings, fmt.Sprintf(format, a...))
}

// collectFiles expands folders into the .xlsx/.csv files inside them.
func collectFiles(paths []string) ([]string, error) {
	var out []string
	for _, p := range paths {
		p = strings.Trim(strings.TrimSpace(p), `"`)
		err := filepath.WalkDir(p, func(path string, d os.DirEntry, err error) error {
			if err != nil {
				return err
			}
			ext := strings.ToLower(filepath.Ext(path))
			if !d.IsDir() && (ext == ".xlsx" || ext == ".csv") && !strings.HasPrefix(d.Name(), "~$") {
				out = append(out, path)
			}
			return nil
		})
		if err != nil {
			return nil, err
		}
	}
	return out, nil
}

// readTables returns every sheet of a file as rows of cells; the first row is the header.
func readTables(path string) ([][][]string, error) {
	if strings.EqualFold(filepath.Ext(path), ".csv") {
		data, err := os.ReadFile(path)
		if err != nil {
			return nil, err
		}
		data = bytes.TrimPrefix(data, []byte("\xef\xbb\xbf"))
		r := csv.NewReader(bytes.NewReader(data))
		r.FieldsPerRecord, r.LazyQuotes = -1, true
		first, _, _ := bytes.Cut(data, []byte("\n"))
		if bytes.Count(first, []byte(";")) > bytes.Count(first, []byte(",")) {
			r.Comma = ';' // Excel with a German/European locale
		}
		rows, err := r.ReadAll()
		return [][][]string{rows}, err
	}
	f, err := excelize.OpenFile(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	var tables [][][]string
	for _, s := range f.GetSheetList() {
		rows, err := f.GetRows(s, excelize.Options{RawCellValue: true})
		if err != nil {
			return nil, err
		}
		tables = append(tables, rows)
	}
	return tables, nil
}

// row maps lowercase header names to cell values.
type row map[string]string

func (r row) get(aliases ...string) string {
	for _, a := range aliases {
		if v, ok := r[a]; ok {
			return strings.TrimSpace(v)
		}
	}
	return ""
}

func (r row) has(aliases ...string) bool {
	for _, a := range aliases {
		if _, ok := r[a]; ok {
			return true
		}
	}
	return false
}

var (
	colUser     = []string{"name", "username", "user", "viewer"}
	colPoints   = []string{"points", "currency", "amount"}
	colHours    = []string{"hours", "time"}
	colCommand  = []string{"command", "name"}
	colResponse = []string{"response", "message", "text"}
	colQuote    = []string{"quote", "text"}
	colEnabled  = []string{"enabled", "status", "active"}
	colRankName = []string{"name", "rank"}
	colRankReq  = []string{"requirement", "hours"}
)

// kind detects the export type from a header row.
func kind(h row) string {
	switch {
	case h.has("interval"):
		return "timers"
	case h.has("requirement"):
		return "ranks"
	case h.has("command") && h.has("response", "message"):
		return "commands"
	case h.has(colQuote...):
		return "quotes"
	case h.has(colUser...) && h.has(colPoints...) || h.has(colHours...):
		return "currency"
	}
	return ""
}

// add parses one table (header + rows) into the export.
func (e *export) add(source string, rows [][]string) {
	if len(rows) == 0 {
		return
	}
	header := make([]string, len(rows[0]))
	h := row{}
	for i, c := range rows[0] {
		header[i] = strings.ToLower(strings.TrimSpace(c))
		h[header[i]] = ""
	}
	k := kind(h)
	if k == "" {
		e.warn("%s: unknown columns %v, skipped", source, rows[0])
		return
	}
	for n, cells := range rows[1:] {
		r := row{}
		for i, c := range cells {
			if i < len(header) {
				r[header[i]] = c
			}
		}
		if err := e.addRow(k, r); err != nil {
			e.warn("%s row %d: %v", source, n+2, err)
		}
	}
}

func (e *export) addRow(k string, r row) error {
	switch k {
	case "currency":
		user := strings.ToLower(strings.TrimPrefix(r.get(colUser...), "@"))
		if user == "" {
			return nil
		}
		if s := r.get(colPoints...); s != "" {
			p, err := num(s)
			if err != nil {
				return err
			}
			// the export repeats users across files (e.g. a Points file next to the currency files): keep one value, never sum
			e.points[user] = max(e.points[user], int(math.Round(p)))
		}
		if s := r.get(colHours...); s != "" {
			h, err := num(s)
			if err != nil {
				return err
			}
			e.time[user] = max(e.time[user], int(math.Round(h*3600)))
		}
	case "quotes":
		q := r.get(colQuote...)
		if q == "" {
			return nil
		}
		user := r.get("user", "added by", "addedby")
		if user == "" {
			user = "Streamlabs"
		}
		e.quotes = append(e.quotes, [4]string{user, strings.ReplaceAll(q, `"`, "''"), date(r.get("date", "created")), r.get("game")})
	case "ranks":
		name := r.get(colRankName...)
		req, err := num(r.get(colRankReq...))
		if err != nil || name == "" {
			return fmt.Errorf("bad rank %q/%q", name, r.get(colRankReq...))
		}
		e.ranks[strconv.Itoa(int(math.Round(req)))] = name
	case "commands":
		name := strings.ToLower(strings.TrimPrefix(r.get(colCommand...), "!"))
		resp := r.get(colResponse...)
		if name == "" || resp == "" {
			return nil
		}
		if strings.ContainsAny(name, " \t") {
			return fmt.Errorf("command %q contains spaces, PhantomBot does not support that", name)
		}
		resp, unknown := translate(resp)
		if len(unknown) > 0 {
			e.warn("!%s: fix these variables by hand: %s", name, strings.Join(unknown, " "))
		}
		perm, ok := permission(r.get("permission", "permissions"))
		if !ok {
			e.warn("!%s: permission %q has no PhantomBot equivalent, set to Viewer", name, r.get("permission", "permissions"))
		}
		cost, _ := num(r.get("cost"))
		e.commands = append(e.commands, command{name, resp, perm, int(cost), boolean(r.get(colEnabled...))})
	case "timers":
		msg := r.get(colResponse...)
		if msg == "" {
			return nil
		}
		interval, _ := num(r.get("interval"))
		lines, _ := num(r.get("lines", "messages"))
		msg, unknown := translate(msg)
		if len(unknown) > 0 {
			e.warn("timer %q: fix these variables by hand: %s", r.get("name"), strings.Join(unknown, " "))
		}
		iv := max(1, int(interval))
		e.timers = append(e.timers, notice{
			Name: r.get("name"), ReqMessages: int(lines), IntervalMin: iv, IntervalMax: iv,
			NoticeToggle: boolean(r.get(colEnabled...)), Messages: []string{msg}, Disabled: []bool{false}, Games: []string{},
		})
	}
	return nil
}

// num parses a number, accepting a comma as the decimal separator.
// ponytail: "1,234" is read as 1.234; Excel stores raw numbers so only hand-made CSVs can hit this.
func num(s string) (float64, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return 0, nil
	}
	if strings.Contains(s, ".") {
		s = strings.ReplaceAll(s, ",", "")
	} else {
		s = strings.ReplaceAll(s, ",", ".")
	}
	return strconv.ParseFloat(s, 64)
}

func boolean(s string) bool {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "false", "no", "0", "disabled", "inactive", "off":
		return false
	}
	return true
}

// date turns an Excel serial or a common date string into epoch milliseconds (now if unparseable).
func date(s string) string {
	if f, err := strconv.ParseFloat(s, 64); err == nil && f > 0 {
		if t, err := excelize.ExcelDateToTime(f, false); err == nil {
			return strconv.FormatInt(t.UnixMilli(), 10)
		}
	}
	for _, layout := range []string{"2006-01-02 15:04:05", "2006-01-02", "01/02/2006 15:04:05", "01/02/2006", "1/2/2006 3:04:05 PM", "02.01.2006 15:04:05", "02.01.2006"} {
		if t, err := time.ParseInLocation(layout, s, time.Local); err == nil {
			return strconv.FormatInt(t.UnixMilli(), 10)
		}
	}
	return strconv.FormatInt(time.Now().UnixMilli(), 10)
}

var permissions = []struct {
	match string
	id    int
}{{"everyone", 7}, {"regular", 6}, {"vip", 5}, {"subscriber", 3}, {"moderator", 2}, {"editor", 1}, {"caster", 0}, {"streamer", 0}}

// permission maps a Streamlabs permission to a PhantomBot group id.
func permission(s string) (int, bool) {
	s = strings.ToLower(s)
	if s == "" {
		return 7, true
	}
	for _, p := range permissions {
		if strings.Contains(s, p.match) {
			return p.id, true
		}
	}
	return 7, false
}

var (
	varRe = regexp.MustCompile(`\$(\w+)(\(([^)]*)\))?`)
	vars  = map[string]string{
		"user": "(sender)", "username": "(sender)", "target": "(touser)", "touser": "(touser)",
		"randuser": "(random)", "randusername": "(random)", "count": "(count)", "game": "(game)",
		"title": "(status)", "uptime": "(uptime)", "followage": "(followage)", "followdate": "(followdate)",
		"points": "(points)", "currencyname": "(pointname)", "channel": "(channelname)",
		"mychannel": "(channelname)", "rank": "(senderrankonly)", "hours": "(hours)", "msg": "(echo)",
	}
	paramRe = regexp.MustCompile(`^param(\d+)$`)
)

// translate converts Streamlabs $variables to PhantomBot (tags) and returns the ones it couldn't map.
func translate(s string) (string, []string) {
	var unknown []string
	out := varRe.ReplaceAllStringFunc(s, func(m string) string {
		p := varRe.FindStringSubmatch(m)
		name := strings.ToLower(p[1])
		if name == "readapi" && p[2] != "" {
			return "(customapi " + p[3] + ")"
		}
		if t, ok := vars[name]; ok && p[2] == "" {
			return t
		}
		if pm := paramRe.FindStringSubmatch(name); pm != nil {
			return "(" + pm[1] + ")"
		}
		unknown = append(unknown, m)
		return m
	})
	return out, unknown
}

func mustJSON(v any) string {
	b, _ := json.Marshal(v)
	return string(b)
}
