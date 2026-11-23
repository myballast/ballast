## Verify your download

Every binary of this release is listed with its SHA-256 sum in `checksums.txt`. Download the binary for your system and `checksums.txt` into the same folder, then check the one you downloaded. Each command prints `<file>: OK` and succeeds when the file is the one that was released. Anything else, no output included, means it does not verify: do not run it.

Linux:

    grep ' ballast_linux_amd64$' checksums.txt | sha256sum -c - | grep ': OK$'

macOS:

    grep ' ballast_darwin_arm64$' checksums.txt | shasum -a 256 -c - | grep ': OK$'

Windows (PowerShell):

    $expected = ((Select-String -Path checksums.txt -Pattern 'ballast_windows_amd64.exe$').Line -split '  ')[0]
    $actual = (Get-FileHash ballast_windows_amd64.exe -Algorithm SHA256).Hash.ToLower()
    if ($expected -and $actual -eq $expected) { 'OK' } else { 'DOES NOT VERIFY' }

For another platform, use the name of the file you downloaded.

The sums protect against a damaged or incomplete download. They are published in this same release, next to the binaries, so they are not a signature: they cannot show that the release itself is genuine.

## Download

The file names do not change from one release to the next:

    curl -fsSLO https://github.com/@REPO@/releases/download/@TAG@/ballast_linux_amd64
    curl -fsSLO https://github.com/@REPO@/releases/download/@TAG@/checksums.txt
