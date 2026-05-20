# Hearing EQ

Personalized hearing-threshold measurement and per-ear equalization for Linux desktops using PipeWire through the `pipewire-pulse` compatibility layer.

## Requirements

- Linux x86_64
- PipeWire running with `pipewire-pulse` enabled
- Go 1.22 or newer
- Gio's Linux desktop backend requires standard system graphics development libraries at build time

## Build

```bash
go build ./cmd/hearing-eq
```

## Usage

1. Run `hearing-eq` to open the GUI.
2. Review the currently saved profile, if any.
3. Complete the hearing test and save the profile.
4. Leave `hearing-eq` running to process audio through the `Hearing-EQ` sink.
5. Route applications to the `Hearing-EQ` sink with `pavucontrol`, `helvum`, or `wpctl set-default`.

Headless mode:

```bash
./hearing-eq --headless
```

The GUI also shows a live spectrum display for the captured input plus the left and right processed output channels.

The profile is saved to `~/.config/hearing-eq/profile.json`.

System volume must remain at the same level used during the hearing test. Changing it later invalidates the equal-loudness assumptions in the saved profile.

## Disclaimer

This software is not a medical device and does not provide diagnosis, treatment, or hearing-health advice.
