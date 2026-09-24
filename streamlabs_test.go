package main

import (
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/xuri/excelize/v2"
)

func TestExport(t *testing.T) {
	e := newExport()
	// the same user in two files (currency + points file) is imported once, not summed
	e.add("currency1.xlsx", [][]string{{"Name", "Rank", "Points", "Hours"}, {"Alice", "Newbie", "100", "1.5"}, {"@Bob", "", "7", "0,25"}})
	e.add("currency2.xlsx", [][]string{{"Name", "Rank", "Points", "Hours"}, {"alice", "", "50", "0.5"}})
	e.add("quotes.xlsx", [][]string{{"ID", "Quote", "Game"}, {"1", `he said "hi"`, "Chess"}})
	e.add("ranks.xlsx", [][]string{{"Name", "Requirement"}, {"Regular", "10"}})
	e.add("commands.xlsx", [][]string{
		{"Command", "Permission", "Response", "Cost", "Enabled"},
		{"!Hug", "Moderator", "$user hugs $target $weird", "5", "False"},
		{"!api", "Everyone", "$readapi(https://x.y/z) $param1", "0", "True"},
	})
	e.add("timers.xlsx", [][]string{{"Name", "Response", "Interval", "Lines"}, {"discord", "join $channel", "15", "20"}})
	e.add("junk.xlsx", [][]string{{"foo", "bar"}, {"1", "2"}})

	if want := map[string]int{"alice": 100, "bob": 7}; !reflect.DeepEqual(e.points, want) {
		t.Errorf("points = %v, want %v", e.points, want)
	}
	if want := map[string]int{"alice": 5400, "bob": 900}; !reflect.DeepEqual(e.time, want) {
		t.Errorf("time = %v, want %v", e.time, want)
	}
	if q := e.quotes; len(q) != 1 || q[0][1] != "he said ''hi''" || q[0][3] != "Chess" || q[0][2] == "" {
		t.Errorf("quotes = %v", q)
	}
	if want := map[string]string{"10": "Regular"}; !reflect.DeepEqual(e.ranks, want) {
		t.Errorf("ranks = %v", e.ranks)
	}
	want := []command{
		{"hug", "(sender) hugs (touser) $weird", 2, 5, false},
		{"api", "(customapi https://x.y/z) (1)", 7, 0, true},
	}
	if !reflect.DeepEqual(e.commands, want) {
		t.Errorf("commands = %+v, want %+v", e.commands, want)
	}
	if got := mustJSON(e.timers[0]); got != `{"name":"discord","reqMessages":20,"intervalMin":15,"intervalMax":15,"shuffle":false,"noticeToggle":true,"noticeOfflineToggle":false,"messages":["join (channelname)"],"disabled":[false],"gamesToggle":false,"games":[]}` {
		t.Errorf("timer json = %s", got)
	}
	w := strings.Join(e.warnings, "\n")
	if !strings.Contains(w, "$weird") || !strings.Contains(w, "junk.xlsx") {
		t.Errorf("warnings = %s", w)
	}
}

func TestNewBotURL(t *testing.T) {
	for in, want := range map[string]string{"https://h:25000/": "wss://h:25000", "http://h": "ws://h", "h:25000": "wss://h:25000"} {
		if got := newBot(in).base; got != want {
			t.Errorf("newBot(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestReadXLSX(t *testing.T) {
	path := filepath.Join(t.TempDir(), "Currency_1.xlsx")
	f := excelize.NewFile()
	f.SetSheetRow("Sheet1", "A1", &[]any{"Name", "Rank", "Points", "Hours"})
	f.SetSheetRow("Sheet1", "A2", &[]any{"Carol", "Newbie", 1234, 2.5})
	if err := f.SaveAs(path); err != nil {
		t.Fatal(err)
	}
	files, err := collectFiles([]string{`"` + filepath.Dir(path) + `"`}) // quoted like Windows "Copy as path"
	if err != nil || len(files) != 1 {
		t.Fatalf("collectFiles = %v, %v", files, err)
	}
	tables, err := readTables(files[0])
	if err != nil {
		t.Fatal(err)
	}
	e := newExport()
	e.add(files[0], tables[0])
	if e.points["carol"] != 1234 || e.time["carol"] != 9000 {
		t.Errorf("points=%v time=%v warnings=%v", e.points, e.time, e.warnings)
	}
}
