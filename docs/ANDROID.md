# Android Notes Core

The Android target uses the existing GoMental note implementation directly:

```text
React / Capacitor
  -> Kotlin Capacitor plugin
  -> build/android/gomental-mobile.aar
  -> Go package ./mobile
  -> workspace, OKF, Bleve, and go-git packages
```

Phase 1 is read-only. It supports a single HTTPS Git repository, local note
listing, full-text search, note reading, resolved links, and note-relative
assets. Graph, editing, SSH, LFS, and submodules are outside this phase.

## Build

Requirements:

- The Go version declared in `go.mod`
- JDK 21 for the Capacitor Android application
- Android SDK with API 26 or newer and a compatible Android NDK
- The module's pinned `gomobile` tool dependencies

Install and build:

```sh
export ANDROID_HOME=/path/to/android-sdk
./build-android.sh
```

The default build contains ARM64 for devices and x86_64 for emulators. Override
the target or minimum API when needed:

```sh
GOMENTAL_ANDROID_TARGET=android/arm64,android/amd64 \
GOMENTAL_ANDROID_API=28 \
./build-android.sh
```

The generated package is `com.gomental.mobile`, and the AAR is written to
`build/android/gomental-mobile.aar`.

`build-android.sh` compiles the module-pinned `gobind` helper into the ignored
build directory before invoking `gomobile`, so no global Go tool installation
is required.

When building from WSL, use Linux builds of Go, Java, and the NDK. A Linux NDK
must be extracted on a case-sensitive filesystem; a normal NTFS directory is
not sufficient because the NDK contains case-distinct headers. The Android SDK
platform directory itself may remain on a mounted Windows drive.

## Android Application

Build the Go AAR first, then build and synchronize the mobile web assets:

```sh
./build-android.sh
cd frontend
npm run android:sync
cd android
./gradlew assembleDebug
```

The debug APK is written to
`frontend/android/app/build/outputs/apk/debug/app-debug.apk`. Gradle refuses to
build when the Go AAR is missing or older than the Go mobile sources.

Install on a connected device or running emulator:

```sh
adb devices
adb install -r frontend/android/app/build/outputs/apk/debug/app-debug.apk
```

Private repository credentials are entered through a native Android dialog.
Tokens are encrypted with an app-owned Android Keystore AES-GCM key and never
passed through the React WebView or persisted in Git configuration.

## Binding API

`mobile.Open` accepts:

```json
{
  "repositoryPath": "/app-private/repository",
  "dataPath": "/app-private/derived-data"
}
```

The paths must not overlap. Keeping Bleve data outside the checkout prevents a
Git reset from affecting the active search projection.

The returned `Core` exposes:

- `Status()`
- `Sync(requestJSON)`
- `Rebuild()`
- `ListNotes(queryJSON)`
- `ReadNote(id)`
- `Search(queryJSON)`
- `LoadAsset(noteID, path)`
- `Cancel()`
- `Close()`

The binding intentionally uses strings containing JSON for structured values.
This keeps domain DTOs in Go and avoids `gomobile bind` restrictions on slices
of Go structs.

## Sync

The Go API accepts an HTTPS remote, branch, and optional in-memory credential:

```json
{
  "remote": "https://github.com/example/notes.git",
  "ref": "main",
  "username": "x-access-token",
  "token": "secret"
}
```

The Kotlin plugin retrieves private credentials from Android Keystore and
passes them directly to this method. JavaScript receives only a boolean stating
whether a credential exists. The Go Git implementation does not persist
credentials in Git configuration.

Sync fetches and hard-resets to `origin/<ref>`. Search indexing is built at a
temporary path and activated only after a successful build. If indexing fails
after a fetch, the working tree is reset to its previous commit and the prior
in-memory projection remains active.

Changing branches requires closing the core and opening a new repository
session. This avoids rewriting a different local branch during activation.

Local absolute paths and `file://` remotes are accepted to support integration
tests and local development. Network remotes must use HTTPS, and credentials
embedded in URLs are rejected.

## Kotlin Rules

- Call blocking Go methods from a background executor, never the main thread.
- Serialize application-level sync requests even though `Core` also serializes
  repository operations.
- Call `Cancel` when the owning activity or job is cancelled.
- Call `Close` when discarding a repository session.
- Store credentials with Android Keystore-backed storage.
- Treat HTML fragments returned by Bleve as untrusted content in the WebView.
