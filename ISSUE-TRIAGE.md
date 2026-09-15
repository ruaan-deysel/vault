# Vault — Open Issue Triage & Implementation Order

31 open issues as of 2026-09-15. Ordered for sequential implementation by an agent.
Rules for the implementing agent: **work top to bottom**, one issue per PR, respect the
"blocked by / conflicts with" notes — several issues touch the same files and will
conflict if picked up out of order.

Complexity scale: **S** = < ~200 LOC, localised · **M** = multi-file, needs new tests ·
**L** = cross-cutting (engine + runner + restore + UI) · **XL** = new subsystem.

---

## Wave 0 — Silent data loss / data corruption (fix first)

These produce wrong data with no error shown to the user. Everything else waits.

| # | Issue | Complexity | Why first |
|---|---|---|---|
| **352** | Container volume archives correlate by mount index | **M** | Mount reorder silently unions *different* volumes across restore points. Corrupts restores with no error. Fix: key volume archives + merge + restore by mount source path end-to-end (`volume_N.tar` → stable key). Touches `engine/container.go`, `container_merge.go`, `container_restore_paths.go`. |
| **345** | Container chain restores resurrect deleted files | **M** | Chain merge is a pure union; files deleted after the base full come back. Folder path already solved this via `pruneChainResurrected` in `internal/runner/runner.go` — port the equivalent for containers. **Do after 352** (both rewrite the merge correlation; 345 builds on the stable key). |
| **353** | Symlinked container volume sources bypass stale-mtime detection | **S** | Re-introduces the #320 data-loss class for any symlinked bind mount. Fix is small: `filepath.EvalSymlinks` the mount source before change detection, matching `WriteEffectiveListing`. Self-contained. |
| **351** | Plugin backups always do a full backup | **M** | `changed_since` is never injected for `plugin` items (`runner.go:~1205`), so incremental/differential plugin jobs are a lie. Issue body already contains a precise 4-step fix + test plan. Must land in **both** classic and dedup paths. **Do after 353** — reuses the same effective-listing plumbing. |

## Wave 1 — Destructive / misleading restore behaviour

User-visible, actively risky, but the failure is at least observable.

| # | Issue | Complexity | Notes |
|---|---|---|---|
| **321** | Restores do not overwrite the target directory | **M** | UI promises "This will overwrite existing data"; restore only appends. Decide semantics explicitly (clean-target vs. merge-overwrite) and make the UI copy match. Foundational for 336 — do it first. |
| **336** | Container restore volume remapping is broken + unannounced | **M** | Two parts: (a) make remapping **opt-in** with an explicit destructive-action warning in the restore wizard; (b) fix the rewrite so Unraid's container *edit* view shows the new mapping, not just the Docker tab (template.xml must be rewritten, not only the live container). **Blocked by 321** (shared restore-destination semantics) and overlaps **379** (template.xml handling). |
| **393** | Flash backup aborts on `input/output error` | **S** | A single unreadable file on the flash drive kills the whole job. Treat per-file read errors as a recorded skip + warning, not a fatal abort (with a job-level "N files unreadable" summary). Small, high user impact, no dependencies — good early parallel task. |

## Wave 2 — Dedup/classic parity (umbrella #350)

`#350` is the tracking issue; its four children are already split and scoped. Do them in
this order — all four touch `internal/engine/container_chunked*.go`.

| # | Issue | Complexity | Notes |
|---|---|---|---|
| **379** | Dedup backups don't capture/restore `template.xml` | **S** | Mirror the classic path. Coordinate with **336** (both write template.xml). |
| **380** | Dedup skips single-file bind mounts / doesn't skip socket+pipe mounts | **S** | Add `__volfile__` key + `os.Lstat`-based skip with `SkipReason`, mirroring classic. |
| **381** | Classic `PluginHandler.Restore` ignores `restore_destination` | **S** | Port the validation already added to the chunked path in #274. Independent of the other three. |
| **382** | No post-backup chunk verification on dedup | **M** | Thread `job.VerifyBackup` into `backupItemChunked`; extract a re-read/re-hash helper from `runVerifyLoopDedup`; record result in restore-point metadata. |
| **350** | *(umbrella — close when 379–382 land)* | — | No code of its own. |

## Wave 3 — Engine & scheduling enhancements

Real feature work; safe to start once Waves 0–1 are merged.

| # | Issue | Complexity | Notes |
|---|---|---|---|
| **366** | Stage classic folder backups on the selected local destination | **M** | Today staging picks the first cache pool and fails with ENOSPC while the destination has TBs free. Add per-destination staging (`<dest>/.vault-stage/…`), capacity-aware selection, and a preflight free-space check. **Must exclude the stage dir from the source walk** when it sits inside a selected source. Related: #241, #255. |
| **322** | Schedule FULL backups alongside DIFF/INC | **M** | Add a "full backup frequency" field to diff/inc job config + scheduler support. **Pairs with 309** — build on the same schedule model, land 309 first. |
| **309** | Custom cron schedule | **S–M** | Add a `custom` option to the Schedule Frequency control everywhere it appears + a cron expression field with validation. Enables 322. |
| **317** | Prompt for appdata path; don't select non-appdata volumes by default | **M** | Behaviour change to container job creation: derive from an appdata path (CA Appdata Backup style) instead of whitelisting every mount. **Blocks 324.** |
| **324** | Auto-add new containers to existing jobs (ALL vs CUSTOM) | **M** | **Blocked by 317** — without it, auto-add pulls in terabyte-scale library mounts by default. Also handle auto-removal of deleted containers. |
| **319** | Drop the job-run-ID prefix from folder names | **S** | Naming/migration change. Needs a back-compat path for reading existing restore points — don't break discovery of old backups. |
| **307** | Databases silently excluded, no dump checkbox | **S–M** | Likely a UI gap rather than engine: surface the per-mount DB-dump toggle, and allow backing up the rest of a mount that merely *contains* a DB. Investigate before scoping. |
| **305** | Containers without volumes fail to back up | **S** | Should produce a config/template-only restore point, not an error. |
| **325** | Encrypt `manifest.json` | **M** | Encrypt with the job key. **Careful:** the manifest is used for discovery/listing — design a readable envelope (plaintext header + encrypted body) or restores/browse break. Needs a back-compat read path for existing plaintext manifests. |
| **302** | Future jobs queue visibility | **M** | Surface queued backups/deletes/restores (History tab or dashboard) with cancel-if-not-started. |

## Wave 4 — Restore wizard UX (tracking #386)

**Do these as one grouped effort, in this exact order** — they all edit
`web/src/components/RestoreWizard.svelte` and each independently proposes adding its own
type-label helper to `web/src/lib/utils.js`.

1. **Land the shared helper once** (type label + icon + colour map in `web/src/lib/utils.js`). Not an issue — a prerequisite commit.
2. **327** — Restore UI page 1 (**M**): floating Next button, compact items, Flash Drive filter, alphabetical sort, fix plugin icon.
3. **332** — Restore UI page 2 (**M**): relabel "X selected"/"X items", show hypothetical restore size, N-step date labels on the timeline, two-click delete confirmation UX. Also touches `RestorePointTimeline.svelte`.
4. **316** — Restore status on the restore page (**S–M**): **scope is reduced** — the `/runner/status` state recovery already landed in PR #371. Remaining: progress bar, log buffer, completion toast.
5. **323** — Unflatten file contents (**L**, separate review): lazy-loaded tree, all selected by default, and the behavioural inversion of "no selection means restore everything". Keep this out of the UX-polish batch; it changes restore semantics.
6. **311** — Download files from the UI (**S**): the API handler (`StorageHandler.DownloadFile`) already exists; wire clickable files in the browser.

## Wave 5 — New subsystems (defer)

| # | Issue | Complexity | Notes |
|---|---|---|---|
| **312** | FUSE read-only mounts for backups | **XL** | New subsystem + layered mounts across restore points. Big value for triage workflows, but nothing else depends on it. |
| **313** | Windows CLI client for backups | **XL** | Cross-platform extract/restore binary. Needs the dedup format to be stable first — depends on Wave 2 landing. |

---

## Suggested parallel tracks

If more than one agent/session is running:

- **Track A (engine, serial):** 352 → 345 → 353 → 351 → 379 → 380 → 382
- **Track B (restore semantics):** 321 → 336 (waits on 352 landing before touching merge code)
- **Track C (independent quick wins):** 393 → 381 → 305 → 319
- **Track D (UI, serial):** shared helper → 327 → 332 → 316 → 323 → 311

Tracks A and B both touch `internal/engine/container*.go` — rebase B on A, not the reverse.

## Guardrails for the implementing agent

- Follow `AGENTS.md` and `CONTEXT.md` in the repo root before writing code.
- Every engine fix in Waves 0–2 needs a **regression test that reproduces the issue's
  "Steps to reproduce"** — several of these bugs are silent, so tests are the only proof.
- Parity issues must land in **both** the classic tar path and the dedup/chunked path.
- Changes to on-disk layout or manifest format (319, 325) need a back-compat read path.
