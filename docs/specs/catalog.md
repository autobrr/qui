# Spec catalog — Business Rules & Use Cases

Every behavioral claim in the wiki has a row here before implementation. The
test named in the row is written in the same step, and it fails until the
behavior exists.

| Kind | Meaning |
| ---- | ------- |
| `asserted-integration` | Across modules, or UI to domain |
| `asserted-unit` | One module |
| `asserted-loopback` | Live protocol on loopback |
| `fail-closed` | Deferred or refused; the test proves refusal |

**ID scheme**

| Prefix | Meaning |
| ------ | ------- |
| `BR-<area>-<nn>` | Invariant |
| `UC-<area>-<nn>` | Acceptance journey |

## Catalog

| ID | Rule (one line) | Source | Owner | Test | Kind |
| -- | --------------- | ------ | ----- | ---- | ---- |
| `BR-desktop-01` | `BiometricAuthService` implements macOS LocalAuthentication evaluation (unit-tested helper; main window does not gate the web UI) | issue #1 | desktop/auth | tests/auth.test.ts | `asserted-unit` |
| `BR-desktop-02` | Docker manager verifies and bootstraps Qui and qbittorrent-nox containers on startup | issue #1 | desktop/docker | tests/docker.test.ts | `asserted-unit` |
| `BR-desktop-03` | Docker update checker queries remote image digests and triggers update prompt when newer image exists | issue #1 | desktop/docker | tests/docker.test.ts | `asserted-unit` |
| `BR-desktop-04` | macOS status item uses the Qui mark with an empty title while idle, and shows live download and upload rates while transfers run | issue #1 | desktop/tray | tests/tray.test.ts | `asserted-unit` |
| `BR-desktop-05` | The Glassmorphism slider keeps that percentage of every theme over a clear window; below 100% the row is on and macOS HUD vibrancy blurs the desktop, and 100% is solid with vibrancy off | docs/desktop/overview.md | desktop/window | tests/window.test.ts, web/src/lib/desktop-window-material.test.ts, web/src/lib/glass.test.ts | `asserted-unit` |
| `BR-desktop-06` | Brand and product name is strictly capitalized as Qui across all platforms | issue #1 | desktop/main | tests/schema.test.ts | `asserted-unit` |
| `BR-desktop-07` | Desktop starts Docker, waits until Qui answers HTTP, then loads `/login`; it does not open the web UI while the port is down | docs/desktop/overview.md | desktop/main | tests/qui-web-url.test.ts, tests/qui-ready.test.ts | `asserted-unit` |
| `BR-desktop-08` | Docker image update dialog is shown at most once per app session unless the user explicitly checks from the tray | docs/desktop/overview.md | desktop/main | tests/docker.test.ts | `asserted-unit` |
| `BR-desktop-09` | Startup page shows the spinner and a process code block on an opaque Minimal background | docs/desktop/overview.md | desktop/main | tests/startup-page.test.ts | `asserted-unit` |
| `BR-desktop-10` | Docker CLI is resolved from known absolute paths; a missing binary reports docker binary not found | docs/desktop/overview.md | desktop/docker | tests/docker.test.ts | `asserted-unit` |
| `BR-desktop-11` | Qui and qBittorrent config are bind-mounted on the host so replacing a container keeps the account | docs/desktop/overview.md | desktop/docker | tests/container-layout.test.ts | `asserted-unit` |
| `BR-desktop-12` | The desktop shell stores the account with an OS cipher and signs in with Touch ID | docs/desktop/overview.md | desktop/account | tests/account-vault.test.ts, web/src/lib/desktop-account.test.ts | `asserted-unit` |
| `BR-desktop-13` | The Docker update dialog stays quiet when a remote digest is already local or was dismissed | docs/desktop/overview.md | desktop/docker | tests/container-layout.test.ts | `asserted-unit` |
| `BR-web-01` | After login or setup in Electron, session flags a one-time Touch ID gate on the authenticated shell | docs/web/electron-desktop.md | web/hooks | web/src/hooks/useAuth.desktop-flag.test.ts | `asserted-unit` |

