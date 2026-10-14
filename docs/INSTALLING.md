# Installing SituCraft

SituCraft is one executable. The browser application is inside it, so nothing
else needs to be installed. Example data are downloaded only when you ask for
them, never with the program.

## Release files

| Operating system | Architecture | Release file |
| --- | --- | --- |
| macOS | Apple silicon (ARM64) | `situcraft-darwin-arm64` |
| macOS | Intel (x64) | `situcraft-darwin-amd64` |
| Linux | x64 | `situcraft-linux-amd64` |
| Linux | ARM64 | `situcraft-linux-arm64` |
| Windows | x64 | `situcraft-windows-amd64.exe` |
| Windows | ARM64 | `situcraft-windows-arm64.exe` |

Each release also has `SHA256SUMS`, the SHA-256 checksum of every file. The
installers need it.

## macOS and Linux

Install the latest release as `$HOME/.local/bin/situcraft`:

```sh
curl --proto '=https' --tlsv1.2 -fsSL \
  https://raw.githubusercontent.com/robert-mcdermott/situcraft/main/install.sh | sh
```

The installer detects the operating system and CPU and downloads the matching
file and `SHA256SUMS` from the same release. It requires exactly one matching
checksum and runs the downloaded program's `--version`, and only then moves it
into place, so a failed download never replaces a working copy. It does not use
`sudo`, change `PATH` or start SituCraft. If `$HOME/.local/bin` is not on
your `PATH`, it prints the full path to run.

To read the installer before running it:

```sh
curl --proto '=https' --tlsv1.2 -fsSLo install-situcraft.sh \
  https://raw.githubusercontent.com/robert-mcdermott/situcraft/main/install.sh
less install-situcraft.sh
sh install-situcraft.sh
```

To install a particular release, give its tag (the
[releases page](https://github.com/robert-mcdermott/situcraft/releases)
lists them):

```sh
curl --proto '=https' --tlsv1.2 -fsSL \
  https://raw.githubusercontent.com/robert-mcdermott/situcraft/main/install.sh |
  SITUCRAFT_VERSION=v0.1.0 sh
```

To install somewhere else, use a directory you can write to:

```sh
curl --proto '=https' --tlsv1.2 -fsSL \
  https://raw.githubusercontent.com/robert-mcdermott/situcraft/main/install.sh |
  SITUCRAFT_INSTALL_DIR="$HOME/bin" sh
```

A downloaded installer takes the same settings as options:

```sh
sh install-situcraft.sh --version v0.1.0 --install-dir "$HOME/bin"
```

`SITUCRAFT_REPOSITORY=owner/repository` installs from a fork. For a
system-wide installation, download and read the script, then run it with an
administrator-owned `--install-dir`. Never pipe a download straight into
`sudo sh`.

## Windows

In PowerShell:

```powershell
irm https://raw.githubusercontent.com/robert-mcdermott/situcraft/main/install.ps1 | iex
```

The program is installed as
`$env:LOCALAPPDATA\Programs\SituCraft\situcraft.exe`, and that folder is
added to your user `PATH`. Terminals that are already open keep their old
`PATH`, so open a new one before running `situcraft`. The installer detects
x64 or ARM64, downloads the program and `SHA256SUMS`, requires exactly one
matching checksum, runs the downloaded program's version check, and replaces
an existing copy only after every check passes. It does not need administrator
rights. Windows cannot replace a program that is running, so stop SituCraft
before upgrading.

### Execution policy

`irm … | iex` is not affected by the PowerShell execution policy, which
governs script *files*: this form reads the script into memory and runs it, so
it works under `Restricted`, `AllSigned` and `RemoteSigned` alike. There is no
need for `Set-ExecutionPolicy` or `Unblock-File`.

The policy does apply to a saved copy of the installer. Bypass it for that one
run instead of changing the setting:

```powershell
powershell -ExecutionPolicy Bypass -File .\install-situcraft.ps1
```

### Options

Piping into `iex` cannot pass parameters. Set environment variables, which the
piped form reads:

```powershell
$env:SITUCRAFT_VERSION = 'v0.1.0'
$env:SITUCRAFT_INSTALL_DIR = "$HOME\bin"
irm https://raw.githubusercontent.com/robert-mcdermott/situcraft/main/install.ps1 | iex
```

or build a script block, which accepts parameters:

```powershell
& ([scriptblock]::Create((irm https://raw.githubusercontent.com/robert-mcdermott/situcraft/main/install.ps1))) `
  -Version v0.1.0 -InstallDir "$HOME\bin"
```

| Parameter | Environment variable | Purpose |
| --- | --- | --- |
| `-Version` | `SITUCRAFT_VERSION` | Release tag to install. Defaults to the latest release. |
| `-InstallDir` | `SITUCRAFT_INSTALL_DIR` | Absolute installation folder. |
| `-Repository` | `SITUCRAFT_REPOSITORY` | GitHub `owner/repository`, for forks. |
| `-Architecture` | `SITUCRAFT_ARCH` | `amd64` or `arm64`, when detection fails. |
| `-NoPathUpdate` | `SITUCRAFT_NO_PATH_UPDATE` | Leave the user `PATH` unchanged. |

A parameter takes precedence over its environment variable.

### Reading the installer first

```powershell
$Installer = Join-Path $env:TEMP 'install-situcraft.ps1'
Invoke-WebRequest -UseBasicParsing `
  'https://raw.githubusercontent.com/robert-mcdermott/situcraft/main/install.ps1' `
  -OutFile $Installer
Get-Content $Installer
powershell -ExecutionPolicy Bypass -File $Installer
```

## Downloading and verifying by hand

On macOS or Linux, choose the file for your computer from the table above:

```sh
ASSET=situcraft-linux-amd64
curl -fLO "https://github.com/robert-mcdermott/situcraft/releases/latest/download/$ASSET"
curl -fLO https://github.com/robert-mcdermott/situcraft/releases/latest/download/SHA256SUMS
grep "  $ASSET\$" SHA256SUMS | sha256sum --check -
chmod +x "$ASSET"
./"$ASSET" --version
```

On macOS, use `shasum -a 256 --check -` instead of `sha256sum --check -`.

In PowerShell:

```powershell
$Asset = 'situcraft-windows-amd64.exe'
$Base = 'https://github.com/robert-mcdermott/situcraft/releases/latest/download'
Invoke-WebRequest -UseBasicParsing "$Base/$Asset" -OutFile $Asset
Invoke-WebRequest -UseBasicParsing "$Base/SHA256SUMS" -OutFile SHA256SUMS
$Expected = (Select-String -Path SHA256SUMS -Pattern ('  ' + [regex]::Escape($Asset) + '$')).Line.Split(' ')[0]
$Actual = (Get-FileHash -Algorithm SHA256 $Asset).Hash.ToLowerInvariant()
if ($Actual -ne $Expected) { throw 'Checksum verification failed' }
& ".\$Asset" --version
```

The checksum detects a damaged download and a file that differs from the
release's list. Both come from the same release, so it cannot protect against
someone able to replace both.

### Files downloaded in a browser

SituCraft's programs are not signed with an Apple or Microsoft certificate.
The installers and the commands above download without a browser, so the
operating system does not flag the file. A file saved from a browser is
flagged, and the first start asks for confirmation.

**macOS.** A warning such as *Apple could not verify "situcraft-darwin-arm64"
is free of malware* appears. Either open **System Settings → Privacy &
Security**, find the message about SituCraft in the Security section and
click **Open Anyway**, or remove the flag in Terminal:

```sh
xattr -d com.apple.quarantine ./situcraft-darwin-arm64
chmod +x ./situcraft-darwin-arm64
```

If `xattr` says the attribute was not found, the file was not flagged.

**Windows.** If SmartScreen blocks the program, click **More info**, then
**Run anyway**. Or clear the flag before starting it: right-click the `.exe`,
choose **Properties**, check **Unblock** on the **General** tab and click
**OK**, or run `Unblock-File .\situcraft-windows-amd64.exe` in PowerShell.

## Upgrading and going back

Running the installer again upgrades SituCraft. A failed download, checksum
or version check leaves the installed copy as it was. The project library is
not touched.

To go back to an earlier release (v0.1.0 or later), install it by tag:

```sh
SITUCRAFT_VERSION=v0.1.0 sh install-situcraft.sh
```

```powershell
$env:SITUCRAFT_VERSION = 'v0.1.0'
irm https://raw.githubusercontent.com/robert-mcdermott/situcraft/main/install.ps1 | iex
```

## Uninstalling

Delete the program: `~/.local/bin/situcraft` on macOS and Linux, or the
`%LocalAppData%\Programs\SituCraft` folder on Windows, whose entry you can
also remove from the user `PATH` (**Settings → System → About → Advanced
system settings → Environment Variables**).

SituCraft keeps your project library (saved projects and small files they use,
stored by checksum) in `~/Library/Application Support/SituCraft` on macOS,
`~/.config/SituCraft` on Linux (or `$XDG_CONFIG_HOME/SituCraft`), and
`%AppData%\SituCraft` on Windows. `--data-dir` puts it elsewhere. Delete it only
if you no longer need those projects; deleted projects are first moved to its
`trash` folder. Your Xenium bundles are never copied into the library, moved or
changed: projects refer to them where they are. Example datasets you downloaded
are in its `examples` folder (or `--examples-dir`); **Remove** on the start page
deletes one, and it can be downloaded again. Its `jobs` folder keeps the jobs (the Jobs view
shows them), and its `artifacts` folder what jobs make and register (segmentations), which
projects use. With `--remote-control`, SituCraft also
keeps `remote.json` there while it runs ([remote control](REMOTE-CONTROL.md)).

SituCraft also keeps a cache of what it derives from your bundles (image tiles,
transcript indexes, tiled cell outlines) in `~/Library/Caches/situcraft` on
macOS, `~/.cache/situcraft` on Linux (or `$XDG_CACHE_HOME/situcraft`), and
`%LocalAppData%\situcraft` on Windows; `--cache-dir` puts it elsewhere. It stays
within `--cache-limit` (10 GB by default) and can be deleted at any time: what
is needed is built again from the bundles.

## Building from source

With the Go version in `go.mod` or newer:

```sh
git clone https://github.com/robert-mcdermott/situcraft.git
cd situcraft
go test ./...
go build -o situcraft .
./situcraft --version
```

The [README](../README.md#development) describes development and releases.
