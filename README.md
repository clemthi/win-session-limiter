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

This creates `C:\ProgramData\WinSessionLimiter\config.json` (if it doesn't
already exist), locks that folder down to Administrators/SYSTEM only, and
registers + starts the `WinSessionLimiter` service (auto-start).

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
commands.

## Configuration file

The commands above edit `C:\ProgramData\WinSessionLimiter\config.json` for
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

Stops and removes the service. Files under `C:\ProgramData\WinSessionLimiter`
are left in place (delete manually if you also want the usage history gone).

## Local testing without installing

```
limiterd.exe debug
```

Runs the same logic in a console window (Ctrl+C to stop) instead of as a
service — useful for checking config changes take effect and usage accrues
as expected before installing for real.

## Notes

- `go test ./...` covers the pure accounting logic (`internal/tracker`) and
  config parsing/editing (`internal/config`).
- Session logon/logoff detection and the forced-logoff call depend on
  Windows Terminal Services APIs and can only be fully exercised on a real
  multi-account Windows machine.
