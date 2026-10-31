# src-wails — Burrow's Go/Wails v3 backend

- `just dev` — native dev window with hot reload (`wails3 task dev`; config in `build/config.yml`).
- `just build` — frontend + `wails3 task package` → `build/bin/Burrow.app` (see `Taskfile.yml`).
- `wails3 generate bindings -ts -i -clean -d ../src/lib/wailsCompat/bindings` — after changing `DesktopService` (`desktop.go`).
- App version lives in `build/config.yml` (`info.version`); `just bump` syncs it.
