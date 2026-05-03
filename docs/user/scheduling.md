# Scheduling `plaud sync`

`plaud sync --watch` is a **foreground** loop tied to your terminal session. For real unattended scheduling (laptop locked, server box, daily run at 03:00), use your OS scheduler instead. This page collects ready-to-paste examples.

In every example, replace `/usr/local/bin/plaud` with the actual path to the binary on your system (`which plaud` will tell you).

## Linux: cron

A daily 03:00 sync, logging to `~/.local/state/plaud/sync.log`:

```cron
# m h  dom mon dow  command
0 3 * * *  /usr/local/bin/plaud sync --format json >> ~/.local/state/plaud/sync.log 2>&1
```

Edit your crontab with `crontab -e`. Cron runs in a minimal environment, so make sure `PLAUD_ARCHIVE_DIR` (if you use it) is set inside the cron entry or your shell rc that cron sources.

To pre-set environment in the crontab itself:

```cron
PLAUD_ARCHIVE_DIR=/home/me/PlaudArchive
PLAUD_DEFAULT_INCLUDE=transcript,summary,metadata
0 3 * * *  /usr/local/bin/plaud sync >> /home/me/.local/state/plaud/sync.log 2>&1
```

## Linux: systemd timer

For better logging and dependency management than cron, use a systemd user timer.

`~/.config/systemd/user/plaud-sync.service`:

```ini
[Unit]
Description=Mirror Plaud.ai recordings to local archive
After=network-online.target
Wants=network-online.target

[Service]
Type=oneshot
Environment="PLAUD_ARCHIVE_DIR=%h/PlaudArchive"
ExecStart=/usr/local/bin/plaud sync --format json
StandardOutput=journal
StandardError=journal
```

`~/.config/systemd/user/plaud-sync.timer`:

```ini
[Unit]
Description=Run plaud sync daily at 03:00

[Timer]
OnCalendar=*-*-* 03:00:00
Persistent=true

[Install]
WantedBy=timers.target
```

Enable and start:

```bash
systemctl --user daemon-reload
systemctl --user enable --now plaud-sync.timer
systemctl --user list-timers plaud-sync.timer
journalctl --user -u plaud-sync.service -f
```

`Persistent=true` means a missed run (laptop asleep at 03:00) catches up on the next boot.

## macOS: launchd

Launchd runs jobs through the user's login session, so it works even when the terminal is closed (provided the user is logged in).

`~/Library/LaunchAgents/io.sollie.plaud-sync.plist`:

```xml
<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
  <dict>
    <key>Label</key>
    <string>io.sollie.plaud-sync</string>
    <key>ProgramArguments</key>
    <array>
      <string>/usr/local/bin/plaud</string>
      <string>sync</string>
      <string>--format</string>
      <string>json</string>
    </array>
    <key>EnvironmentVariables</key>
    <dict>
      <key>PLAUD_ARCHIVE_DIR</key>
      <string>/Users/me/PlaudArchive</string>
    </dict>
    <key>StartCalendarInterval</key>
    <dict>
      <key>Hour</key>
      <integer>3</integer>
      <key>Minute</key>
      <integer>0</integer>
    </dict>
    <key>StandardOutPath</key>
    <string>/Users/me/Library/Logs/plaud-sync.log</string>
    <key>StandardErrorPath</key>
    <string>/Users/me/Library/Logs/plaud-sync.log</string>
  </dict>
</plist>
```

Load it:

```bash
launchctl load ~/Library/LaunchAgents/io.sollie.plaud-sync.plist
launchctl list | grep plaud
tail -f ~/Library/Logs/plaud-sync.log
```

To run on demand (without waiting for the schedule):

```bash
launchctl start io.sollie.plaud-sync
```

To unload:

```bash
launchctl unload ~/Library/LaunchAgents/io.sollie.plaud-sync.plist
```

Replace `me` with your username throughout.

## Windows: Task Scheduler

Open **Task Scheduler** → **Create Task...** (not "Create Basic Task") and configure:

- **General** tab:
  - *Name*: `plaud sync`.
  - *Run whether user is logged on or not*: optional. Leaving it as "only when logged on" avoids needing a stored password.
- **Triggers** tab → *New*:
  - *Begin the task*: `On a schedule`.
  - *Daily*, start `03:00:00`.
- **Actions** tab → *New*:
  - *Action*: `Start a program`.
  - *Program/script*: `C:\Path\To\plaud.exe`.
  - *Add arguments*: `sync --format json`.
- **Conditions** tab:
  - Disable "Start the task only if the computer is on AC power" if you want it to run on battery.
- **Settings** tab:
  - Enable "Run task as soon as possible after a scheduled start is missed" — equivalent to systemd's `Persistent=true`.

To run from PowerShell (lets you check it in to dotfiles):

```powershell
$action  = New-ScheduledTaskAction -Execute "C:\Path\To\plaud.exe" -Argument "sync --format json"
$trigger = New-ScheduledTaskTrigger -Daily -At 3am
$settings = New-ScheduledTaskSettingsSet -StartWhenAvailable
Register-ScheduledTask -TaskName "plaud sync" -Action $action -Trigger $trigger -Settings $settings
```

## Why not just `plaud sync --watch` in a `screen`/`tmux`?

You can. It's exactly what `--watch` is built for: a desk session you walk away from. The downsides:

- A reboot, sleep, or terminal hang kills the loop.
- The OS scheduler handles "the laptop was asleep at 03:00" gracefully (cron with `anacron`, systemd `Persistent=true`, launchd `StartCalendarInterval`, Task Scheduler "missed start"). Watch mode does not.
- Watch mode exits non-zero after 5 consecutive failed cycles; cron / systemd / launchd / Task Scheduler retry on the next scheduled tick regardless.

For "I want to use my Mac all day and have new recordings show up before lunch," watch in a tmux is fine. For "this should keep working unattended while I'm on holiday for two weeks," use the OS scheduler.

## Logging

`--format json` writes structured NDJSON to stdout. Pipe it to a log file in your scheduler's redirect (`>> sync.log 2>&1` for cron, `StandardOutPath` for launchd, journald for systemd).

To make a quick human-readable summary from the JSON log:

```bash
jq -r 'select(.event == "done") | "\(.ts)\t\(.details.fetched) fetched\t\(.details.skipped) skipped\t\(.details.failed) failed"' ~/.local/state/plaud/sync.log | tail -10
```

## Authentication and unattended scheduling

`plaud sync` reads the bearer token from `${XDG_CONFIG_HOME:-~/.config}/plaud/credentials.json` (POSIX) or `%APPDATA%\plaud\credentials.json` (Windows). For an unattended job to work:

1. The user the scheduler runs as must own that credentials file.
2. The token must still be valid (long-lived JWT; rotates when you log out from `web.plaud.ai`).

Plaud's tokens don't auto-refresh from the CLI yet — when one eventually expires, scheduled syncs will start failing with `Token expired or invalid. Run \`plaud login\` again.` Re-run `plaud login` interactively to refresh; the next scheduled tick picks up the new token automatically.

Watch mode and OS schedulers both surface auth failures the same way: a single line on stderr, exit code non-zero. cron / systemd / launchd will email or log the failure depending on your environment.

## Related

- [`commands/sync.md`](./commands/sync.md): the full sync reference.
- [`troubleshooting.md`](./troubleshooting.md): what to do when a scheduled run fails.
