# Hearing EQ

Personalized hearing-threshold measurement and per-ear equalization for Linux desktops using PipeWire through the `pipewire-pulse` compatibility layer.

## Requirements

- Linux x86_64
- PipeWire running with `pipewire-pulse` enabled
- Go 1.22 or newer
- For `hearprofile`, Gio's Linux desktop backend requires standard system graphics development libraries at build time

## Build

```bash
go build ./cmd/hearprofile
go build ./cmd/heareq
```

## Usage

1. Run `hearprofile` once.
2. Complete the hearing test and save the profile.
3. Run `heareq` in a terminal.
4. Route applications to the `Hearing-EQ` sink with `pavucontrol`, `helvum`, or `wpctl set-default`.

The profile is saved to `~/.config/hearing-eq/profile.json`.

System volume must remain at the same level used during the hearing test. Changing it later invalidates the equal-loudness assumptions in the saved profile.

## Disclaimer

This software is not a medical device and does not provide diagnosis, treatment, or hearing-health advice.
