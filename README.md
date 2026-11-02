# Win Session Limiter

Enforces a daily logon-time budget for chosen Windows user accounts. Runs as
a Windows service (LocalSystem), so it starts at boot and is not something
the monitored account can stop or kill.

## Build

```
go build -o limiterd.exe ./cmd/limiterd
```

## Install (run from an elevated PowerShell/cmd prompt)

```
limiterd.exe install
```

This:

1. Creates `C:\ProgramData\WinSessionL\config.json` (if it doesn't already
   exist) and locks that folder down to Administrators/SYSTEM only.
2. Copies `limiterd.exe` to `C:\Program Files\WinSessionL\WinSessionL.exe`.
   Standard users can't write there, so the monitored account can't replace
   or delete it.
3. Registers and starts the `WinSessionL` service (auto-start), running the
   copy in Program Files. In `services.msc` it appears as "Session Helper",
   with a neutral description, so it's not obvious what it does.

Because the service runs its own copy, you can move or delete the
`limiterd.exe` you ran `install` from. To upgrade, run `uninstall` and then
`install` with the new binary.

## Managing monitored users (run from an elevated prompt)

```
limiterd.exe add <user> <time>    # start limiting an account
limiterd.exe edit <user> <time>   # change an account's daily limit
limiterd.exe del <user>           # stop limiting an account
```

Examples:

```
limiterd.exe add kidaccount 90
limiterd.exe edit kidaccount 1h30m
limiterd.exe del kidaccount
```

- `<user>` is the Windows account name, matched case-insensitively.
- `<time>` is either a number of minutes (`90`) or a duration (`2h`, `45m`,
  `1h30m`). It must be a whole number of minutes.
- A limit below 5 minutes is raised to 5, with a warning.
- `add` fails if the account is already limited; `edit` and `del` fail if it
  isn't.
- Changes take effect in the running service within one poll interval; no
  restart is needed.
- These commands work before `install` too: they create the locked-down data
  folder if it doesn't exist yet.

Run `limiterd.exe help` (or `limiterd.exe` with no arguments) to list all
commands, and `limiterd.exe version` to print the version number.

## Version

Releases follow [SemVer](https://semver.org) and are created automatically:
every merge to `main` publishes a GitHub release tagged `vX.Y.Z`, with the
built `limiterd.exe` attached. The bump is derived from the
[Conventional Commits](https://www.conventionalcommits.org) prefixes of the
commits merged since the previous release:

- `feat!:` (any type followed by `!`) or a `BREAKING CHANGE:` line in the
  commit body: major (`v1.4.2` → `v2.0.0`)
- `feat:`: minor (`v1.4.2` → `v1.5.0`)
- anything else (`fix:`, `chore:`, …): patch (`v1.4.2` → `v1.4.3`)

The release build stamps that version into the binary; a local `go build`
reports `dev`. Besides `limiterd.exe version`, it's printed by `install` and recorded in the
Event Viewer when the service starts. It does not appear in the file's
Windows Properties (Details tab).

## Configuration file

The commands above edit `C:\ProgramData\WinSessionL\config.json` for
you. You can also edit it by hand from an elevated editor; changes are
picked up the same way:

```json
{
  "pollIntervalSeconds": 15,
  "warningsMinutesBefore": [15, 10, 5, 3, 1],
  "users": {
    "kidaccount": { "dailyLimitMinutes": 120 }
  }
}
```

- `dailyLimitMinutes` counts **all** logged-on time for that account each
  day (including locked/idle), reset at local midnight. It has a hard floor
  of 5 minutes — a lower value in config.json is silently raised to 5. (The
  minimum is defined once, as `MinDailyLimitMinutes` in
  `internal/config/config.go`.)
- `warningsMinutesBefore` fires a one-time popup in the user's session at
  each of these remaining-minute marks. The 5- and 3-minute warnings are
  always sent, even if they're missing from this list, so a user is never
  logged off without notice. Even at the 5-minute minimum limit, the user
  still gets the 3-minute warning.
- When the limit hits zero, the user is logged off automatically.

## Uninstall

```
limiterd.exe uninstall
```

Stops and removes the service, and deletes `C:\Program Files\WinSessionL\`.
Files under `C:\ProgramData\WinSessionL` are left in place (delete them
manually if you also want the config and usage history gone).

Run `uninstall` from a copy outside `C:\Program Files\WinSessionL\`. If you
run the installed copy itself, the service is still removed, but Windows
won't let a running program delete its own folder, so you'll be told to
delete that folder manually.

## Local testing without installing

```
limiterd.exe debug
```

Runs the same logic in a console window (Ctrl+C to stop) instead of as a
service — useful for checking config changes take effect and usage accrues
as expected before installing for real.

## Notes

- `go test ./...` covers the pure accounting logic (`internal/tracker`),
  config parsing/editing (`internal/config`), and copying the binary
  (`internal/install`).
- The folder names under Program Files and ProgramData, and the installed
  binary's name, all come from the service name, `service.Name` in
  `internal/service/service.go`. The name and description shown in
  `services.msc` are `DisplayName` and `Description` in
  `internal/install/install.go`.
- Session logon/logoff detection and the forced-logoff call depend on
  Windows Terminal Services APIs and can only be fully exercised on a real
  multi-account Windows machine.
