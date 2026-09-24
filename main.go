// Command phantombot-importer imports a Streamlabs Chatbot "Create Split Excel Files" export into PhantomBot.
package main

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"sort"
	"strconv"
	"strings"
	"time"

	"golang.org/x/term"
)

var in = bufio.NewReader(os.Stdin)

func ask(prompt, def string) string {
	if def != "" {
		fmt.Printf("%s [%s]: ", prompt, def)
	} else {
		fmt.Printf("%s: ", prompt)
	}
	s, _ := in.ReadString('\n')
	if s = strings.TrimSpace(s); s == "" {
		return def
	}
	return s
}

func yes(prompt string) bool {
	return strings.HasPrefix(strings.ToLower(ask(prompt+" [y/N]", "")), "y")
}

func main() {
	err := run()
	if err != nil {
		fmt.Println("\nERROR:", err)
	}
	fmt.Print("\nPress Enter to exit...")
	in.ReadString('\n')
	if err != nil {
		os.Exit(1)
	}
}

func run() error {
	fmt.Println("PhantomBot importer for Streamlabs Chatbot exports")
	fmt.Println()

	paths := os.Args[1:]
	if len(paths) == 0 {
		paths = []string{ask("Export folder or file (drag it here)", "")}
	}
	files, err := collectFiles(paths)
	if err != nil {
		return err
	}
	if len(files) == 0 {
		return fmt.Errorf("no .xlsx or .csv files found in %v", paths)
	}

	e := newExport()
	for _, f := range files {
		tables, err := readTables(f)
		if err != nil {
			e.warn("%s: %v", f, err)
			continue
		}
		for _, t := range tables {
			e.add(f, t)
		}
	}
	for _, w := range e.warnings {
		fmt.Println("warning:", w)
	}
	fmt.Printf("\nFound in %d file(s): %d users with points, %d with watch time, %d quotes, %d ranks, %d commands, %d timers\n\n",
		len(files), len(e.points), len(e.time), len(e.quotes), len(e.ranks), len(e.commands), len(e.timers))

	b := newBot(ask("PhantomBot URL", "https://localhost:25000"))
	user := ask("Panel username", "")
	var pw []byte
	if term.IsTerminal(int(os.Stdin.Fd())) {
		fmt.Print("Panel password: ")
		pw, err = term.ReadPassword(int(os.Stdin.Fd()))
		fmt.Println()
		if err != nil {
			return err
		}
	} else {
		pw = []byte(ask("Panel password", ""))
	}

	login := func() error {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		return b.login(ctx, user, string(pw))
	}
	err = login()
	if isCertError(err) && yes("The bot uses a self-signed certificate. Trust it?") {
		b.insecure = true
		err = login()
	}
	if err != nil {
		return fmt.Errorf("login failed: %w", err)
	}
	defer b.conn.CloseNow()
	fmt.Println("Logged in.")

	fmt.Println("\nPoints and watch time are ADDED to what PhantomBot already has; quotes and timers are appended.")
	fmt.Println("Running the import twice imports everything twice.")
	if !yes("Import now?") {
		return nil
	}
	return importAll(b, e)
}

func importAll(b *bot, e *export) error {
	progress := func(label string, i, n int) {
		if i == n || i%100 == 0 {
			fmt.Printf("\r%-10s %d/%d", label, i, n)
		}
		if i == n {
			fmt.Println()
		}
	}
	incrAll := func(label, table string, m map[string]int) error {
		i := 0
		for user, v := range m {
			i++
			if v != 0 {
				if err := b.incr(table, user, v); err != nil {
					return err
				}
			}
			progress(label, i, len(m))
		}
		return nil
	}
	if err := incrAll("points", "points", e.points); err != nil {
		return err
	}
	if err := incrAll("time", "time", e.time); err != nil {
		return err
	}

	for hours, name := range e.ranks {
		if err := b.set("ranksMapping", hours, name); err != nil {
			return err
		}
	}
	if len(e.ranks) > 0 {
		fmt.Printf("ranks      %d\n", len(e.ranks))
	}

	if len(e.quotes) > 0 {
		existing, err := b.keys("quotes")
		if err != nil {
			return err
		}
		for i, q := range e.quotes {
			if err := b.set("quotes", strconv.Itoa(len(existing)+i), mustJSON(q[:])); err != nil {
				return err
			}
			progress("quotes", i+1, len(e.quotes))
		}
	}

	var skipped []string
	if len(e.commands) > 0 {
		existing, err := b.keys("command")
		if err != nil {
			return err
		}
		for i, c := range e.commands {
			if _, ok := existing[c.name]; ok {
				skipped = append(skipped, "!"+c.name)
				progress("commands", i+1, len(e.commands))
				continue
			}
			existing[c.name] = c.response
			if err := b.set("command", c.name, c.response); err != nil {
				return err
			}
			if c.perm != 7 {
				if err := b.set("permcom", c.name, strconv.Itoa(c.perm)); err != nil {
					return err
				}
			}
			if c.cost > 0 {
				if err := b.set("pricecom", c.name, strconv.Itoa(c.cost)); err != nil {
					return err
				}
			}
			if !c.enabled {
				if err := b.set("disabledCommands", c.name, "true"); err != nil {
					return err
				}
			}
			progress("commands", i+1, len(e.commands))
		}
	}

	if len(e.timers) > 0 {
		existing, err := b.keys("notices")
		if err != nil {
			return err
		}
		for i, t := range e.timers {
			if err := b.set("notices", strconv.Itoa(len(existing)+i), mustJSON(t)); err != nil {
				return err
			}
			progress("timers", i+1, len(e.timers))
		}
	}

	fmt.Println("\nDone.")
	if len(skipped) > 0 {
		sort.Strings(skipped)
		fmt.Println("Skipped commands that already exist in PhantomBot:", strings.Join(skipped, " "))
	}
	if len(e.commands) > 0 || len(e.timers) > 0 {
		fmt.Println("Restart PhantomBot to load the imported commands and timers.")
	}
	return nil
}
