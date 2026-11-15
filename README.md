# ESP32 NVS Key-Pair Manager

A Wails v3 desktop application for managing the GNSS ESP32's configuration over
USB. Load a CSV schema or version-2 JSON, read the device, edit a draft, review
selected changes, and explicitly write them. The four tabs are Connect, Key Pair
Management, Logs, and About.

The application sends only `hello`, `set`, `get`, and `erase`. The App service in
`app.go` uses the shared Wails-free `nvs` package, including `nvs.SaveNamespace`.
The implementation is split between the Wails service and the shared backend.

## Build and run

Requirements: Go 1.26 or newer, Node.js/npm, and the platform's Wails desktop
prerequisites. This Windows build was tested with Go 1.27.4, Node 24.13.0, and
WebView2. Both the Go module and CLI use Wails **v3.1.0**; the frontend runtime is
also pinned to **3.1.0**, matching that release. Go and npm lockfiles are included.

```powershell
go install github.com/wailsapp/wails/v3/cmd/wails3@v3.1.0
npm --prefix frontend ci
wails3 dev
```

Production build:

```powershell
wails3 build
.\bin\esp32-nvs-manager.exe
```

The About page shows the license as MIT and the version as the build date
(`YYYY-MM-DD`). Wails development, release, and Docker builds stamp this date
into the executable. Set `BUILD_DATE=YYYY-MM-DD` to override it for a build.

On Windows, `wails3 build` produces the standalone executable. `wails3 package`
uses the retained template installer tasks and requires the platform packaging
tools; an installer was not generated in this session.

### Cross-platform builds with Docker

The `wails-cross` Docker image (`build/docker/Dockerfile.cross`) builds the Linux and macOS
binaries from any host. It contains Go, GCC, the GTK/WebKit headers, Zig and the
macOS SDK. Docker must be running. Build the image once (about 800 MB to download):

```powershell
wails3 task setup:docker
```

Then call the platform tasks, with the target architecture (`amd64` or `arm64`)
and an output name per target so the builds don't overwrite each other:

```powershell
wails3 task linux:build   ARCH=amd64 OUTPUT=bin/esp32-nvs-manager-linux-amd64
wails3 task linux:build   ARCH=arm64 OUTPUT=bin/esp32-nvs-manager-linux-arm64
wails3 task darwin:build  ARCH=arm64 OUTPUT=bin/esp32-nvs-manager-darwin-arm64
wails3 task darwin:build  ARCH=amd64 OUTPUT=bin/esp32-nvs-manager-darwin-amd64
wails3 task windows:build ARCH=amd64
```

| Target | On a Windows host | On a Linux host | On a macOS host |
|---|---|---|---|
| Windows | native Go (no CGO) | native Go (no CGO) | native Go (no CGO) |
| Linux | Docker | native if the CPU and a C compiler match, otherwise Docker | Docker |
| macOS | Docker | Docker | native |

On the host, each task builds the frontend, generates the bindings and icons, then runs the
container with the repository mounted on `/app`. The binary lands in `bin/`. The
container uses Zig for macOS and Windows and GCC for Linux, with `CGO_ENABLED=1`.

The image can also be run directly. Build the frontend first: the container
only builds it when `frontend/dist/` is missing. The tracked `.gitkeep` means that folder always exists.

```powershell
npm --prefix frontend run build
docker run --rm -v "${PWD}:/app" -e APP_NAME=esp32-nvs-manager wails-cross linux amd64
# → bin/esp32-nvs-manager-linux-amd64
```

Limitations: the macOS output is a bare, unsigned binary. Making a `.app` bundle
or DMG, signing and notarising all need a Mac (`wails3 package`). The Linux
binary needs the GTK/WebKitGTK runtime on the target machine. On Linux, the user
running the app needs serial-port access, usually through the `dialout` group. These Docker
builds have not been tested from this Windows host yet.

Wails generates `frontend/bindings/`; do not edit those files. Components use the
typed adapter in `frontend/src/services/wailsApi.ts`. No HTTP application server
or browser serial API is used. A Vite-only preview cannot exercise native ports,
dialogs, or file drops.

The dependency helper runs `npm ci` when package inputs change and skips it when
they match. This avoids replacing native Node modules while Vite holds them open
on Windows. Stop dev mode before changing dependencies or running a clean install.

## Use

1. Browse for one `.csv` schema or `.json` configuration, or drop it on the import
   panel. Go parses and validates it. Loading works offline; cancel preserves the
   current schema and draft. Copy `nvs_keys.csv.example` to a working `.csv` file before loading or editing it. Local root CSV files are
   ignored because they may contain credentials.
2. Select the board's serial port and Connect. A loaded schema is read
   automatically after the handshake. Loading another file while connected offers
   Read device rather than silently replacing draft edits.
3. Edit draft values and select changed keys. Conflicts require Keep draft or Take
   device. Forbidden rows are excluded from writes and export; a `read_only`
   refusal disables its namespace.
4. Write to ESP32 always requires confirmation. Namespaces are saved in the order
   gnss, ntrip, wifi, stopping at the first failure. A partial save identifies
   Saved, failed/unknown, and Not attempted rows. Nothing is rolled back or retried
   by the frontend. Read back verifies written namespaces; Saved and Verified are
   separate states.
5. Erase requires confirmation. An erased key cannot be set until the next boot
   seeds the default. Save as JSON works offline and warns about cleartext
   passwords. Export excludes forbidden keys and uses device values when a draft
   value is absent.

The application does not reboot the board. NTRIP configuration takes effect
within about 5 seconds; GNSS and WiFi configuration takes effect at the next boot.
Drafts, schema contents, and device values are never persisted in browser storage.
Logs contain operation messages, not raw frames or configuration contents.

Official [Wails development/build guidance](https://v3.wails.io/getting-started/your-first-app/)
and [troubleshooting](https://v3.wails.io/faq/) cover platform setup.
