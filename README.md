# Jarvis Support Host

A small background app you drop onto **any computer** (a MacBook, a brother's Mac, a Windows PC, a
Linux box). It connects **outward** to your Jarvis server, so Jarvis (the brain, on your own machine)
can look after that computer: run commands, open a terminal, move files, list and close programs,
take screenshots, and use its camera. Call these machines **hosts**. It is tech support, not a doctor.

```
 host (Mac/Windows/Linux)                         Jarvis server ("Hunter")
 jarvis-host  ── WSS, outward only ───────────►  /ws/host   (door: OFF unless "accepting support hosts")
 no open port, no SSH, no static IP               host.* tools · terminal pane · camera.show · notices
```

## Download and install (for everyone else)
Open the [Releases page](../../releases/latest) and download the file for your computer:

| Computer | File | How |
|---|---|---|
| Mac (Apple silicon and Intel) | `JarvisSupportHost-<version>-mac-universal.dmg` | open it, drag **Jarvis Support Host** to Applications, then follow "First open on a Mac" |
| Windows | `JarvisSupportHost-windows-x64.exe` (ARM: `-arm64.exe`) | plain exe, no installer: double-click, follow "First open on Windows" |
| Linux (Debian/Ubuntu/Kali) | `jarvis-support-host_<version>_amd64.deb` | `sudo apt install ./jarvis-support-host_*.deb`, then run `jarvis-host` |
| Linux (other) | `jarvis-host-<version>-linux-amd64.tar.gz` | unpack, run `./jarvis-host` |

A console window opens and asks for the address of the Jarvis server once. Zain approves the computer once.

### First open on a Mac (the app is not signed yet)
macOS blocks apps that are not signed by a registered developer. This is expected; you allow it once:
1. Drag **Jarvis Support Host** into Applications and double-click it. macOS says it cannot be opened. Click **Done** (not "Move to Bin").
2. Open **System Settings > Privacy & Security**, scroll down to the Security section. It says
   *"Jarvis Support Host" was blocked*. Click **Open Anyway**, enter your Mac password, then **Open Anyway** again.
   (Older macOS: right-click the app > **Open** > **Open**.)
3. Terminal opens and asks for the server address.
4. If macOS says the app is "damaged": open Terminal and run
   `xattr -dr com.apple.quarantine "/Applications/Jarvis Support Host.app"`, then open it again.
5. Allow Camera / Screen Recording when asked (System Settings > Privacy & Security) if Jarvis needs them.

### First open on Windows
SmartScreen may say "Windows protected your PC". Click **More info > Run anyway**.

### Building the installers (GitHub Actions)
`.github/workflows/build.yml` builds on GitHub's own Mac, Windows and Linux runners. Push a tag
(`git tag v0.1.0 && git push origin v0.1.0`) to build everything and publish a Release; or Actions >
build > Run workflow for artifacts only. Signing: still a placeholder in `scripts/sign.sh`
(the workflow marks where the secrets would go).

## Run it

1. Build (or copy a build from `dist/`): `scripts/build.sh` (needs Go; no C compiler).
2. On the Jarvis server, open the door (default is closed):
   `jarvis hosts on` (or tell Jarvis "accept support hosts", or `POST /api/hosts/accepting`).
3. On the host: `./jarvis-host https://your-jarvis.example.com`
   - The server address is the **only** input. It can be the first argument, `--server URL`, or the
     `JARVIS_SERVER` environment variable. If none is given it asks once. Nothing is baked in and there
     is no default; it remembers the answer.
4. The first time, the server shows "a new support host wants to connect"; you say "approve it"
   (`hosts.approve`). Or skip that with a one-time invite: Jarvis runs `hosts.invite`, you run
   `./jarvis-host --code ABCD-2345 <server>`. After approval the host keeps a private pass and
   reconnects by itself (backoff 1s to 60s, with jitter).
5. Jarvis says "support host connected: <machine, user>" and waits. Say "show/hide the terminal" and
   Jarvis calls `host.console`.

Other flags: `--background` (detach, runs quietly), `--hidden`/`--show` (console), `--name`,
`--no-indicator`, `--fake-camera`; `jarvis-host autostart install|uninstall` (start at login),
`jarvis-host forget`. Default: console visible on first run.

Safety for whoever sits at the host (none of it limits what Jarvis can do): a banner in the console
and a **"Jarvis is connected" window with a Disconnect button** (AppleScript / PowerShell / zenity),
Ctrl+C or typing `stop` also disconnect, and an **action log** (`actions.log` in the config folder:
every command, file path, app, process and camera start, one JSON line each; never file contents or
keystrokes; passwords in URLs are redacted).

Sudo/admin: Jarvis never types or stores a password. A sudo command from Jarvis opens a terminal window
**on the host's screen** and the person there types the password; the interactive terminal detects
password prompts and Jarvis refuses to type into them.

## What Jarvis can do (server tools, all with `host=` to pick the machine)
`host.run` (streamed stdout/stderr, exit code, cwd, timeout, cancel kills the process tree),
`terminal.open host=…` (a real PTY/ConPTY in the normal terminal pane), `host.upload/download`
(SHA-256 checked, downloads land in Jarvis's Files), `host.files`, `host.processes`, `host.kill`,
`host.app_launch/app_close`, `host.screenshot`, `host.info`, `host.console`, `host.disconnect`,
`host.camera_list`, `camera.show/snapshot` (host cameras show up as "<camera> on <machine>"),
`host.camera_relay` (RTSP/HTTP camera the host can reach; login kept in memory only),
`hosts.status/accepting/approve/invite/forget`.

## Why Go
One static binary per OS, no runtime to install on someone else's machine; `CGO_ENABLED=0`, so all six
targets (Mac arm64/x64, Windows x64/arm64, Linux x64/arm64) cross-compile from one Linux laptop with
`go build`; first-class WebSocket/TLS/concurrency (many streams over one socket), small (~7 MB), starts
instantly. Alternatives were worse here: Python/Node need an interpreter on the host or a bundler that
cannot cross-build, Rust is a fine fit but cross-compiling to Mac/Windows needs linkers and SDKs.
Cost: no native GUI without cgo, so the indicator uses each OS's own scripting, and cameras go through
a separate ffmpeg (see Limits).

## Architecture
`cmd/jarvis-host` (flags, the one input, autostart) → `internal/agent` (dial, hello/approval/pass,
reconnect, request dispatch, cancellation) → `internal/ops` (exec, files, processes, apps,
screenshot) · `internal/term` (PTY / ConPTY) · `internal/camera` (ffmpeg) · `internal/sweep` (identity)
· `internal/ui` (console, indicator) · `internal/audit` (action log) · `internal/config` (server, id, pass).

Protocol v1 (documented on the server side in `docs/support-hosts.md`): JSON text frames
(`hello/ready/pending/refused`, `req/res/evt/cancel/end`, `identity`, `ping`) plus binary frames
`channel|idlen|id|payload` for terminal bytes, camera fMP4 and file chunks. The server side lives on
branch `feat/support-host` of `project-jarvis` (`server/jarvis/hosts/`, `tools/hosts.py`): the door
(`/ws/host`) with the **accepting** switch (default off; `jarvis hosts on|off`, tool, REST), enrolment by
approval/invite/pass (only an HMAC is stored), notices and spoken announcement on connect, host
terminals in the existing terminal pane, host cameras in the existing camera system.

## Build, USB, signing
`scripts/build.sh` (all targets → `dist/`, checksums; universal Mac binary if run on a Mac),
`scripts/build.ps1` (on Windows), `scripts/make-usb.sh <dest>` (folders mac/windows/linux with
double-click launchers and a plain-English README; optional bundled ffmpeg).
**Code signing is deliberately not done.** The place for it is `sign_artifact()` in `scripts/build.sh`
→ `scripts/sign.sh` (notes for Developer ID + notarization, signtool); unsigned means Mac Gatekeeper
(right-click > Open; the launcher strips the quarantine flag) and Windows SmartScreen will warn.

## Tests
`scripts/test.sh` runs: Go unit and agent tests (fake server), a cross-compile check for all six
targets, and the end-to-end suite (`e2e/`): the real binary against a throwaway instance of the real
server code on a spare port. Server tests: `tests/integration/test_hosts.py` in the project-jarvis branch.

## Verified vs not (plain list)
Verified here (Linux x64): connect, identity sweep, approval/invite/pass/reconnect, door closed by
default, commands (streaming, exit code, cwd, timeout, cancel kills children), file transfer both ways
with checksums, PTY terminal through the real terminal pane, processes/kill/launch, console switch +
persistence, fake camera enumerate + still + live fMP4 to viewers, network-camera relay login never on
disk, action log, Disconnect/Ctrl+C/`host.disconnect`, one input (arg/env/prompt, no default).
**Compiles but NOT run:** macOS (arm64/x64) and Windows (x64/arm64) binaries. So **not verified**: the
Mac and Windows indicator windows, ConPTY, Windows console hide, `screencapture`/PowerShell screenshots,
`open -a`/`start` app launching, the visible-window sudo flow (Terminal.app / cmd), real webcam
capture via avfoundation/dshow (the device-list parsers are unit-tested on sample output only),
autostart (launchd/registry/systemd), the macOS Camera/Screen-Recording permission prompts, universal
binary (needs `lipo` on a Mac). Linux webcam hardware and the Linux indicator (needs zenity/yad/kdialog
and a desktop) were not exercised either: this machine had no camera or display session.

## Limits to know
- Follow-up (not done): bundle a static ffmpeg per OS in the installers.
- Cameras need **ffmpeg** on the host (next to the app, in `bin/`, or on PATH); video only (no audio).
- A relayed camera's login is in memory only, but is visible to other users on the host in the ffmpeg
  command line while streaming.
- Hiding the console: Windows hides the window; on Mac/Linux it mutes output (use `--background` to detach).
- On macOS the user must grant Screen Recording (screenshots) and Camera permission the first time.
- The host trusts the server it was pointed at completely (that is the point); use https/wss over the internet.
