package main

import (
	"encoding/csv"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// backupTables are the tables PhantomBot's own scripts use; the bot has no API to list tables.
// ponytail: tables created by custom/third-party scripts are missed; add their names here if needed.
// Left out on purpose: commandtoken (the bot refuses to read it) and panelUsers (login data).
var backupTables = []string{
	"adsAnnounceSettings", "adventurePayouts", "adventurePayoutsTEMP", "adventureSettings", "aliases", "auctionresults",
	"auctionSettings", "audioCommands", "audio_hooks", "bettingPanel", "bettingResults", "bettingSettings",
	"bettingState", "bettingVotes", "bitsSettings", "blackList", "blacklistedDiscordRoles", "channelPointsSettings",
	"chatModerator", "clipit", "clipsSettings", "command", "commandCount", "commandPause",
	"commandRestrictions", "commercialSettings", "cooldown", "cooldownSettings", "coolkey", "deaths",
	"disabledCommands", "discordAliascom", "discordBlacklist", "discordChannelcom", "discordCommands", "discordCooldown",
	"discordCooldownSettings", "discordDonations", "discordGambling", "discordKeywords", "discordPermcom", "discordPermsObj",
	"discordPricecom", "discordRanks", "discordRoles", "discordRollReward", "discordSettings", "discordSlotMachineReward",
	"discordStreamStats", "discordToTwitch", "discordWhitelist", "donations", "dualStreamCommand", "emotecache",
	"entered", "externalCommands", "followed", "gambling", "greeting", "greetingCoolDown",
	"greetingSettings", "group", "grouppoints", "grouppointsoffline", "groups", "heistPayouts",
	"hiddenCommands", "highlights", "incoming_raids", "keywords", "lastseen", "modules",
	"notices", "noticeSettings", "noticeTmp", "outgoing_raids", "panelData", "panelstats",
	"pastgames", "paycom", "permcom", "points", "pointSettings", "pollPanel",
	"pollresults", "pollState", "pollVotes", "preSubGroup", "pricecom", "promotebio",
	"promoteids", "promoteonlinetime", "promoterevoke", "promotesettings", "queue", "queueSettings",
	"quotes", "raffleList", "raffleresults", "raffleSettings", "raffleState", "raidSettings",
	"ranksMapping", "rollprizes", "roulette", "settings", "slotmachine", "slotmachineemotes",
	"streamElementsHandler", "streamInfo", "subplan", "subscribeHandler", "tempDisabledCommandScript", "ticketsList",
	"time", "timeSettings", "timevars", "tipeeeStreamHandler", "traffleresults", "traffleSettings",
	"traffleState", "twitchToDiscord", "updates", "viewerRanks", "visited", "welcome",
	"welcome_disabled_users", "whiteList", "wordCounter", "ytcache", "ytPanelPlaylist", "ytpBlacklist",
	"ytpBlacklistedSong", "ytPlaylist_default", "yt_playlists_registry", "ytSettings",
}

// backupAll writes every non-empty table to <dir>/<table>.csv (key,value) and returns the folder.
func backupAll(b *bot) (string, error) {
	exe, err := os.Executable()
	if err != nil {
		return "", err
	}
	dir := filepath.Join(filepath.Dir(exe), "phantombot-backup-"+time.Now().Format("2006-01-02_15-04-05"))
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	saved := 0
	for i, table := range backupTables {
		fmt.Printf("\rbackup %d/%d tables", i+1, len(backupTables))
		rows, err := b.keys(table)
		if err != nil && strings.Contains(err.Error(), "no reply") {
			return "", fmt.Errorf("backup of %s: %w", table, err) // connection is gone, stop
		}
		if err != nil {
			fmt.Printf("\nwarning: %s not backed up: %v\n", table, err) // e.g. panel user lacks access
			continue
		}
		if len(rows) == 0 {
			continue
		}
		if err := writeCSV(filepath.Join(dir, table+".csv"), rows); err != nil {
			return "", err
		}
		saved++
	}
	fmt.Printf("\rbackup: %d tables saved to %s\n", saved, dir)
	return dir, nil
}

func writeCSV(path string, rows map[string]string) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	f.WriteString("\xef\xbb\xbf") // BOM so Excel shows umlauts correctly
	w := csv.NewWriter(f)
	w.Write([]string{"key", "value"})
	keys := make([]string, 0, len(rows))
	for k := range rows {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		w.Write([]string{k, rows[k]})
	}
	w.Flush()
	if err := w.Error(); err != nil {
		f.Close()
		return err
	}
	return f.Close()
}
