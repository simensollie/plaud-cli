# Spec 0002 acceptance walk

Concrete commands and expected outcomes for spec 0002's §8 acceptance criteria. The macOS column records what was observed during the v0.2.0 walk. Linux and Windows columns are the runbook for the cross-platform leg (§8.12).

For why each step matters, read `spec.md` §8. For wire-shape findings discovered during the walk (signed-URL signing, envelope auth status, list trash filtering, §8.8 wording), see `notes.md` 2026-05-03 entries.

---

## 0. Prerequisites

- A Plaud account with at least three recordings, including:
  - One "normal" recording with both transcript and summary ready.
  - One with `is_trans=false` (and ideally `is_summary=true` to match §8.9 literally).
  - One you are willing to move to trash temporarily for §8.10.
- The released `plaud` binary for the target OS (`v0.2.0` and later).
- A scratch archive root (`/tmp/plaud-smoke-0002` on POSIX, `$env:TEMP\plaud-smoke-0002` on Windows). Wiped between steps so idempotency assertions are clean.

A walk takes 15-30 minutes per OS. Server-state-dependent steps (§8.5, §8.8, §8.9, §8.10) are wire-shape concerns and need only be exercised on one OS; OS-sensitive steps (§8.1, §8.2, §8.3, §8.7, §8.11, plus F-18 on Windows) should be exercised on every target.

---

## macOS walk record (v0.2.0, 2026-05-03)

Walked from a `main` build at commit `2115365` against a real account (region `eu`). All eleven §8 items passed. Two implementation bugs surfaced and were fixed mid-walk before the relevant items passed:

- §8.2: `HeadAudio` (HTTP HEAD) → 403 because Plaud's signed S3 URL is signed for `GET` only. Fixed by switching to a one-byte ranged GET (commit `4b87f35`).
- §8.8: invalid token surfaces as HTTP 200 with envelope `{status: -3900, msg: "invalid auth header"}`, not HTTP 401. Fixed by mapping envelope status `-3900` to `ErrUnauthorized` (commit `3c29b16`).

§8.8 spec wording was tightened pre-Done to match implementation (commit `2115365`). §8.10 surfaced that Plaud's list endpoint server-side-filters trashed recordings (note `e52638e`).

---

## Linux walk record (v0.2.0, 2026-05-03)

Walked from the released `v0.2.0` linux/amd64 binary (downloaded from the GitHub release tarball) against the same EU account, on Arch Linux (kernel 6.19). The five OS-sensitive items (§8.1, §8.2, §8.3, §8.7, §8.11) passed. The OS-independent / wire-shape items (§8.4-§8.6, §8.8-§8.10) were not re-exercised on Linux; they are covered by the macOS walk per "OS-independent steps" below.

No implementation bugs surfaced.

§8.7 line endings verified explicitly: 0 CR bytes, 271 LF in `transcript.srt`. §8.11 sha256 byte-identical across the forced run, confirming deterministic `json` and `srt` renderers. §8.2 prefix resolution matched `04-30 readyware` to a single recording even with two other 04-30 entries in the account, exercising F-09's case-insensitive prefix path.

Slug folding observation: `Læring og dokumentasjon, tilgangsroller, beredskapsgrupper og hendelseshåndtering` folded to `04_30_readyware_laering_og_dokumentasjon_tilgangsroller` (æ→ae, comma → `_`, truncated at a `_` boundary inside the last 10 chars of the 60-char cap).

---

## Linux walk runbook

```bash
mkdir -p ~/plaud-v0.2.0-smoke && cd ~/plaud-v0.2.0-smoke
curl -fsSL -o plaud.tar.gz \
  https://github.com/simensollie/plaud-cli/releases/download/v0.2.0/plaud-cli_0.2.0_linux_amd64.tar.gz
tar -xzf plaud.tar.gz plaud
./plaud --version  # plaud version 0.2.0

./plaud login  # interactive OTP, or --token <jwt> --region eu --email <addr>

ARCH=/tmp/plaud-smoke-0002
NORMAL_ID=e1d9aa6c83378b3182cbc20e94b3c6de  # replace with one of your recordings
NORMAL_PREFIX="04-30 readyware"             # replace with the recording's title prefix
```

### §8.1 default no-flag

```bash
rm -rf "$ARCH"
./plaud download "$NORMAL_ID" --out "$ARCH"
find "$ARCH" -type f | sort
```

Expect: `transcript.json`, `transcript.md`, `summary.plaud.md`, `metadata.json` under `2026/MM/<slug>/`. No `audio.mp3`.

### §8.2 all-on plus MD5 chain

```bash
rm -rf "$ARCH"
./plaud download "$NORMAL_PREFIX" --out "$ARCH" --include audio,transcript,summary,metadata
F=$(find "$ARCH" -name audio.mp3); D=$(dirname "$F")
echo "audio md5sum: $(md5sum "$F" | cut -d' ' -f1)"
jq '.audio | {local_md5, s3_etag, original_upload_md5}' "$D/metadata.json"
```

Expect: `local_md5 == s3_etag == md5sum` of `audio.mp3`. `original_upload_md5` is populated and differs from `s3_etag` (it is the MD5 of Plaud's original `.opus` upload, not the served `.mp3`).

### §8.3 idempotent re-run

```bash
./plaud download "$NORMAL_PREFIX" --out "$ARCH" --include audio,transcript,summary,metadata --format json
jq '{fetched_at, last_verified_at}' "$D/metadata.json"
```

Expect: JSON line carries `"status":"skipped"`. `fetched_at` unchanged from the previous step; `last_verified_at` bumped to now.

### §8.7 transcript format selection plus line endings

```bash
rm -rf "$ARCH"
./plaud download "$NORMAL_ID" --out "$ARCH" --transcript-format json,srt
SRT=$(find "$ARCH" -name transcript.srt)
file "$SRT"
test -f "$(dirname "$SRT")/transcript.md" && echo "FAIL: md present" || echo "OK: md absent"
```

Expect: `file` reports UTF-8 / ASCII text with LF line terminators (not CRLF). `transcript.md` absent.

### §8.11 force semantics

```bash
TR_JSON="$(dirname "$SRT")/transcript.json"
sha256sum "$SRT" "$TR_JSON" > /tmp/lin-t1.sha
sleep 1
./plaud download "$NORMAL_ID" --out "$ARCH" --transcript-format json,srt --force
sha256sum "$SRT" "$TR_JSON" > /tmp/lin-t2.sha
diff /tmp/lin-t1.sha /tmp/lin-t2.sha && echo "OK: byte-identical"
jq '{fetched_at, last_verified_at}' "$(dirname "$SRT")/metadata.json"
```

Expect: `diff` is empty (byte-identical sha256 across the force run). Both `fetched_at` and `last_verified_at` bumped between T1 and T2.

---

## Windows walk runbook (PowerShell)

```powershell
mkdir $env:USERPROFILE\plaud-v0.2.0-smoke -Force; cd $env:USERPROFILE\plaud-v0.2.0-smoke
Invoke-WebRequest -OutFile plaud.zip `
  -Uri https://github.com/simensollie/plaud-cli/releases/download/v0.2.0/plaud-cli_0.2.0_windows_amd64.zip
Expand-Archive plaud.zip -DestinationPath . -Force
.\plaud.exe --version  # plaud version 0.2.0

.\plaud.exe login

$ARCH = "$env:TEMP\plaud-smoke-0002"
$NORMAL_ID = "e1d9aa6c83378b3182cbc20e94b3c6de"
$NORMAL_PREFIX = "04-30 readyware"
```

### §8.1, §8.2, §8.3, §8.7, §8.11

Same shape as Linux, with PowerShell idioms. Wipe `$ARCH` between steps, run the same `plaud download` invocations, and verify the same outcomes (file set, MD5 chain, `status:"skipped"`, byte-identical sha256).

```powershell
Remove-Item -Recurse -Force $ARCH -ErrorAction SilentlyContinue
.\plaud.exe download $NORMAL_ID --out $ARCH
Get-ChildItem -Recurse $ARCH | Select-Object FullName

# §8.2 MD5 chain (after a full audio run via $NORMAL_PREFIX)
$AUDIO = Get-ChildItem -Recurse $ARCH -Filter audio.mp3 | Select-Object -First 1
(Get-FileHash $AUDIO.FullName -Algorithm MD5).Hash.ToLower()
Get-Content (Join-Path $AUDIO.Directory.FullName metadata.json) | ConvertFrom-Json |
  Select-Object -ExpandProperty audio | Format-List local_md5, s3_etag, original_upload_md5

# §8.7 line endings (Windows-specific)
$SRT = Get-ChildItem -Recurse $ARCH -Filter transcript.srt | Select-Object -First 1
$bytes = [IO.File]::ReadAllBytes($SRT.FullName)
$crlf = ($bytes | Where-Object { $_ -eq 13 }).Count
"CR bytes in SRT: $crlf  (golden tests use LF; expect 0 unless renderer is line-ending-aware)"
```

### F-18 long-path validation

The default archive root on Windows is `$env:USERPROFILE\PlaudArchive` (matches `--help`). On a long-username jumphost plus a long-titled recording, the absolute file path can exceed Windows' classic 260-char limit; the implementation wraps writes with `\\?\` prefix to lift the limit.

Pick the longest-titled recording in the account (in this codebase's smoke pool: `90f04ab715ac9b20f5cbbbbcc651b767`, "04-08 Dugnadsmøte: ..." which produces a near-60-char slug). Force a full run:

```powershell
.\plaud.exe download 90f04ab715ac9b20f5cbbbbcc651b767 --include audio,transcript,summary,metadata
$LONG = Get-ChildItem -Recurse $env:USERPROFILE\PlaudArchive -Filter audio.mp3 -ErrorAction SilentlyContinue |
  Sort-Object {$_.FullName.Length} -Descending | Select-Object -First 1
"resolved path length: $($LONG.FullName.Length) chars"
"path: $($LONG.FullName)"
Test-Path $LONG.FullName
```

Expect: `Test-Path` returns `True`; total absolute path length may be 200-300+ characters depending on the username and the recording's slug. If the username is short on this jumphost and the path stays well under 260, F-18 is exercised in code (the `\\?\` wrapper still runs) but not stress-tested. The failure mode F-18 protects against (Windows API rejecting long paths with `ERROR_PATH_NOT_FOUND` 0x03) is what to watch for.

---

## OS-independent steps (one-OS coverage)

The following §8 items exercise wire-shape contracts (server responses, error mapping, ID resolution) that do not vary by OS. They were verified once during the macOS walk and need not be repeated per OS unless the contract is suspected to have shifted.

- §8.4 parallel three IDs (goroutine scheduling, identical across OS).
- §8.5 partial failure (good ID + bad ID; server response).
- §8.6 selective include set (CLI flag parsing).
- §8.8 token rotation mid-run (envelope `status=-3900` mapping).
- §8.9 partial server state `is_trans=false` (server response and F-19 warning text).
- §8.10 trashed direct-ID (detail-endpoint behavior; list endpoint hides trashed).

If a contributor wants strict per-OS coverage of the above, the same commands from the macOS walk apply with the OS-appropriate path conventions.

---

## Closing the walk

When all relevant items pass on every target OS:

1. Update `spec.md` to `Status: Done <today>` and bump `Updated:`.
2. Update the row for spec 0002 in `specs/README.md` to `Done`.
3. Commit citing the platforms walked.
4. The spec is then immutable. Any further work goes in a new spec.

If a step fails, do not flip status. Either fix the underlying code (which may mean reopening a phase), capture the failure in `notes.md`, or update the spec wording if the implementation behavior is correct and the spec text overshot. Acceptance bars cannot be lowered to make a spec ship.
