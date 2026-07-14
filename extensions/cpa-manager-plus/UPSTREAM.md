# CPA Manager Plus upstream snapshot

- Repository: https://github.com/seakee/CPA-Manager-Plus
- Base commit: `c039853fa5e37e19c6c334b7673c58f61e7e862a`
- Imported: 2026-07-14

This directory is intentionally isolated from CLIProxyAPI core code. Local integration changes are limited to:

- paged/lightweight auth-file API consumption;
- targeted auth snapshot collection;
- bounded request-monitor event pages;
- provider-neutral quota recovery when an explicit reset timestamp exists;
- Codex inspection five-hour quota cooldown and reset-time recovery;
- visual configuration fields for the integrated first-SSE-event timeout and identical retry settings;
- monorepo Docker build paths;
- integrated-container admin credential synchronization;
- embedded multi-file panel asset serving;
- an optional non-single-file Vite build used by Nginx so `management.html` is small and hashed JS/CSS assets are cacheable;
- a root `go.mod` boundary so CLIProxyAPI's `go test ./...` does not traverse frontend dependencies; the real Manager Server module remains under `apps/manager-server`.

When updating the snapshot, compare this file list first and reapply the small local integration delta instead of merging the two repositories at their roots.
