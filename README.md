# phantombot-importer

> [!WARNING]
> **This is a hobby project, written mostly with AI.** It should work, but it has only been tested once, against a test bot. Use it at your own risk. I take no responsibility or liability for any damage or data loss in your bot.
>
> **Back up first:** stop PhantomBot and copy its `config` folder (it holds `phantombot.db`) before you import anything.

Imports a **Streamlabs Chatbot** export ("Create Split Excel Files", `.xlsx` or `.csv`) into a running **PhantomBot** over its web panel API. You don't need database access or to stop the bot.

Imports:

| Streamlabs | PhantomBot |
|---|---|
| Currency: points | points, **added** to existing |
| Currency: hours | watch time, **added** to existing |
| Quotes | quotes, appended |
| Ranks (requirement in hours) | ranks |
| Commands (response, permission, cost, enabled) | custom commands; existing commands are skipped |
| Timers (message, interval, lines) | one timer group per timer, appended |

## Usage

1. Download `phantombot-importer.exe` (Windows) or `phantombot-importer-linux` from the [Releases](../../releases).
2. Drag the export folder (or files) onto the exe, or start it and paste the path.
3. Enter the bot URL (default `https://localhost:25000`) and your **panel** login.
4. Check the summary and confirm.
5. **Restart PhantomBot** so it loads the imported commands and timers.

Notes:
- Running the import twice imports everything twice (points added again, quotes and timers duplicated).
- Streamlabs variables are translated (`$user`→`(sender)`, `$target`→`(touser)`, `$count`→`(count)`, `$readapi(...)`→`(customapi ...)`, …). Any it can't translate are listed so you can fix them by hand.
- Streamlabs permissions like Min_Points or User_Specific don't exist in PhantomBot. Those commands become Viewer commands, with a warning.
- Rank requirements are imported as hours. If your Streamlabs ranks were points-based, adjust them in the panel afterwards.
- Not imported: sounds, cooldowns, per-user custom ranks.

## Build

```sh
./build.sh   # Docker only: runs tests, writes dist/phantombot-importer.exe and dist/phantombot-importer-linux
```

CI (`.github/workflows/build.yml`) tests and builds on every push and PR. Pushing a `v*` tag publishes a GitHub Release with both binaries.
