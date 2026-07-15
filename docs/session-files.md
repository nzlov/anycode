# Session files and generated artifacts

AnyCode gives each card a stable generated-file directory outside its project worktree:

```text
ANYCODE_DATA_DIR/attachments/outputs/<sessionID>/
```

The directory is exposed to Codex as `ANYCODE_ARTIFACT_DIR`. New Codex runs receive it through
`--add-dir`; resumed workspace-write runs receive the equivalent writable-root configuration.
Files in this directory do not appear in the card Git diff.

Codex should write screenshots, generated images, PDF, audio, video, archives, and other user
deliverables to this directory. It can publish a stable file immediately with the AnyCode MCP
`publish_artifact` tool. Any files not explicitly published are scanned when the Codex process
exits and immediately before the card closes.

## Storage lifecycle

- The output directory remains available across start, resume, `answer_user`, failure, stop, and
  workflow node retries.
- Publishing copies the working file to an immutable archive at
  `attachments/sessions/<sessionID>/<fileID>/`.
- Closing a card performs a final scan, atomically moves its output directory under
  `attachments/output-trash/`, commits the closed state, and deletes the quarantine directory.
- Startup reconciliation restores quarantined output for a card that was not committed closed and
  deletes quarantine left by an already closed card.
- Startup and six-hour reconciliation scans open-card outputs, removes output directories that
  belong to closed cards, and deletes orphan output directories after 24 hours.
- Archived files are retained until the user explicitly deletes them. A deletion tombstone prevents
  the same output version from being recreated by a later scan.

This design can temporarily use roughly twice the generated file size: one mutable working copy and
one immutable archived copy.

## Configuration

All values are positive decimal byte counts. Invalid values stop service startup.

| Variable | Default | Purpose |
| --- | ---: | --- |
| `ANYCODE_ARTIFACT_MAX_FILE_BYTES` | `536870912` | Maximum archived file size (512 MiB) |
| `ANYCODE_ARTIFACT_MAX_SESSION_BYTES` | `10737418240` | Maximum active artifact bytes per card (10 GiB) |
| `ANYCODE_ARTIFACT_PREVIEW_MAX_BYTES` | `134217728` | Maximum browser preview response (128 MiB) |
| `PLAYWRIGHT_MCP_BIN` | empty outside Docker | Enables Playwright MCP when set |
| `CHROMIUM_BIN` | `/usr/bin/chromium` | Browser executable passed to Playwright MCP |

The Docker image pins `@playwright/mcp@0.0.78`, runs it headless and isolated, and writes each
ProcessRun's browser output under `ANYCODE_ARTIFACT_DIR/browser/<processRunID>/`. The Chromium
profile remains in memory and is discarded with the MCP process; screenshots, downloads, and video
inside the output directory remain available for scanning.

## Preview and security

- File preview and download use authenticated `/files/<id>/preview` and `/files/<id>/download`
  endpoints. The access key is sent as a Bearer header and is never placed in a URL.
- Preview supports safe raster images, PDF, browser-supported audio/video, and escaped text.
  SVG is not rendered inline. Archives and unknown files are download-only and are never extracted.
- Text preview is limited to 1 MiB in the UI. Image preview rejects PNG/JPEG/GIF dimensions above
  40 million pixels, including VP8, VP8L, and VP8X WebP images.
- File responses use `nosniff`, a restrictive CSP, safe content disposition, ETag, Last-Modified,
  Content-Length, and HTTP Range handling.
- Publishing rejects files outside the card output directory, symbolic links, directories, devices,
  unstable files, and quota violations.

Anyone holding `ANYCODE_ACCESS_KEY` already has AnyCode's documented service-account execution
boundary. Generated-file access does not weaken that boundary, but it increases the data that the
holder can preview and download.

## Image generation

AnyCode does not call an image API and does not store a second model credential. The running Codex
agent uses its own `image_generation` capability. Startup probes and logs whether that Codex feature
is enabled. Inline image output up to 25 MiB is archived before its process event is persisted, so
base64 content and raw source paths are not returned through GraphQL.
