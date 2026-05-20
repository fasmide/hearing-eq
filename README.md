# Hearing EQ

Experimental personalized hearing-threshold measurement and per-ear equalization for Linux desktops using PipeWire through the `pipewire-pulse` compatibility layer.

This project should be treated as a hobby / toy audio tool, not as a medically reliable hearing test.

![screenshot](https://github.com/user-attachments/assets/94e0ec89-ba91-4fa4-840d-d004a965b193)

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
2. Review the current profile, if any, or select the built-in `Flat` profile to disable EQ.
3. Complete the hearing test and save the result as a named profile.
4. Leave `hearing-eq` running to process audio through the `Hearing-EQ` sink.
5. Route applications to the `Hearing-EQ` sink with `pavucontrol`, `helvum`, or `wpctl set-default`.

Headless mode:

```bash
./hearing-eq --headless
```

The GUI also shows a live spectrum display for the captured input plus the left and right processed output channels.

Named profiles are stored under `~/.config/hearing-eq/profiles/`.

The currently selected profile is mirrored to `~/.config/hearing-eq/profile.json` for the running EQ engine.

System volume should stay close to the level used during the hearing test. Small changes up or down may still sound acceptable, but the profile will match best near the original test level because perceived balance changes with listening level; see the equal-loudness contour: https://en.wikipedia.org/wiki/Equal-loudness_contour

## Disclaimer

This software is not a medical device and does not provide diagnosis, treatment, or hearing-health advice.

It has not been clinically validated, formally verified, calibrated against audiology equipment, or reviewed for safety-critical use.

Results may be wrong, misleading, or inconsistent depending on headphones, fit, background noise, system audio state, hearing fatigue, listening level, and user response quality.

Do not use it to make medical decisions. If you suspect hearing loss or any hearing-health problem, see a qualified audiologist or physician.
