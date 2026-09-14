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

Edit `config.json` to add the accounts to monitor, then either restart the
service or just wait — it's picked up automatically within one poll interval:

```json
{
  "pollIntervalSeconds": 15,
  "warningsMinutesBefore": [15, 10, 5, 1],
  "users": {
    "kidaccount": { "dailyLimitMinutes": 120 }
  }
}
```

- `dailyLimitMinutes` counts **all** logged-on time for that account each
  day (including locked/idle), reset at local midnight. It has a hard floor
  of 30 minutes — a lower value in config.json is silently raised to 30.
- `warningsMinutesBefore` fires a one-time popup in the user's session at
  each of these remaining-minute marks. A 5-minute warning is always sent
  regardless of this list, so a user is never logged off without notice.
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

- `go test ./...` covers the pure accounting logic (`internal/tracker`).
- Session logon/logoff detection and the forced-logoff call depend on
  Windows Terminal Services APIs and can only be fully exercised on a real
  multi-account Windows machine.
