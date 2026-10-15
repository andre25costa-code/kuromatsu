# Kuromatsu installer for Windows (PowerShell 5.1+), no admin rights needed.
#
#   irm https://raw.githubusercontent.com/andre25costa-code/kuromatsu/main/scripts/install.ps1 | iex
#
# It downloads the release zip for this architecture, checks its sha256
# against checksums.txt, installs kuromatsu.exe in a user directory, adds
# that directory to the user PATH and runs `kuromatsu onboard`.
#
# Environment:
#   KUROMATSU_INSTALL_BASE_URL    where the release files are; http(s):// or
#                                 file:// (default: the latest GitHub release)
#   KUROMATSU_INSTALL_DIR         install directory
#                                 (default: %LOCALAPPDATA%\Kuromatsu\bin)
#   KUROMATSU_INSTALL_NO_ONBOARD  set to 1 to skip `kuromatsu onboard`
#
# Uninstall with `kuromatsu uninstall` (add --purge to delete the data too).
#
# The whole installer is the Install-Kuromatsu function, called on the last
# line: if the download is cut short, nothing runs.

function Install-Kuromatsu {
    [CmdletBinding()]
    param()

    $ErrorActionPreference = 'Stop'
    $ProgressPreference = 'SilentlyContinue'  # the progress bar makes downloads slow in 5.1

    $defaultBase = 'https://github.com/andre25costa-code/kuromatsu/releases/latest/download'
    $base = $env:KUROMATSU_INSTALL_BASE_URL
    if (-not $base) { $base = $defaultBase }
    $base = $base.TrimEnd('/')

    # A 32-bit PowerShell on 64-bit Windows reports the real CPU here.
    $cpu = $env:PROCESSOR_ARCHITEW6432
    if (-not $cpu) { $cpu = $env:PROCESSOR_ARCHITECTURE }
    switch ($cpu) {
        'AMD64' { $arch = 'x86_64' }
        'ARM64' { $arch = 'arm64' }
        default { throw "unsupported OS/arch: Windows/$cpu" }
    }

    $dest = $env:KUROMATSU_INSTALL_DIR
    if (-not $dest) { $dest = Join-Path $env:LOCALAPPDATA 'Kuromatsu\bin' }
    $dest = [IO.Path]::GetFullPath($dest).TrimEnd('\')

    $asset = "kuromatsu_Windows_$arch.zip"
    $tmp = Join-Path ([IO.Path]::GetTempPath()) ('kuromatsu-install-' + [Guid]::NewGuid().ToString('N'))
    New-Item -ItemType Directory -Path $tmp | Out-Null

    function Get-ReleaseFile([string]$name) {
        $url = "$base/$name"
        $out = Join-Path $tmp $name
        if ($url.StartsWith('file://')) {
            Copy-Item -LiteralPath ([Uri]$url).LocalPath -Destination $out
        } elseif ($url.StartsWith('http://') -or $url.StartsWith('https://')) {
            [Net.ServicePointManager]::SecurityProtocol = [Net.ServicePointManager]::SecurityProtocol -bor [Net.SecurityProtocolType]::Tls12
            try {
                Invoke-WebRequest -UseBasicParsing -Uri $url -OutFile $out
            } catch [System.Net.WebException] {
                $resp = $_.Exception.Response
                if ($resp -and [int]$resp.StatusCode -eq 404) {
                    if ($base -eq $defaultBase) {
                        throw 'no release published yet; build from source (README) or set KUROMATSU_INSTALL_BASE_URL'
                    }
                    throw "not found: $url"
                }
                throw "download failed: $url ($($_.Exception.Message))"
            }
        } else {
            throw "KUROMATSU_INSTALL_BASE_URL must start with http://, https:// or file:// (got $base)"
        }
        return $out
    }

    try {
        Write-Host "Downloading $asset from $base"
        $zip = Get-ReleaseFile $asset
        $sums = Get-ReleaseFile 'checksums.txt'

        # Exactly one line whose file field is the asset name.
        $expected = @(Get-Content -LiteralPath $sums | ForEach-Object {
            $parts = $_.Trim() -split '\s+'
            if ($parts.Count -ge 2 -and ($parts[1] -eq $asset -or $parts[1] -eq "*$asset")) { $parts[0] }
        })
        $hex = '0123456789abcdefABCDEF'
        if ($expected.Count -ne 1 -or $expected[0].Length -ne 64 -or
            @($expected[0].ToCharArray() | Where-Object { $hex.IndexOf($_) -lt 0 }).Count -ne 0) {
            throw "checksum mismatch for $asset"
        }
        $actual = (Get-FileHash -Algorithm SHA256 -LiteralPath $zip).Hash
        if (-not ($actual -eq $expected[0])) {
            throw "checksum mismatch for $asset"
        }

        New-Item -ItemType Directory -Force -Path $dest | Out-Null

        $root = [IO.Path]::GetPathRoot($dest)
        if ($root -and $root.Length -ge 2 -and $root[1] -eq ':') {
            $need = 3 * (Get-Item -LiteralPath $zip).Length
            $free = (Get-PSDrive -Name $root.Substring(0, 1)).Free
            if ($free -lt $need) {
                throw "not enough free space on ${root}: need $need bytes, available $free bytes"
            }
        } else {
            Write-Warning "could not read the free space of $dest; continuing"
        }

        $unpacked = Join-Path $tmp 'unpacked'
        Expand-Archive -LiteralPath $zip -DestinationPath $unpacked -Force
        $exe = Join-Path $dest 'kuromatsu.exe'

        # `kuromatsu uninstall` renames a running binary aside; drop those leftovers.
        Get-ChildItem -LiteralPath $dest -Filter 'kuromatsu.exe.uninstalled-*' -ErrorAction SilentlyContinue |
            ForEach-Object { Remove-Item -LiteralPath $_.FullName -Force -ErrorAction SilentlyContinue }

        try {
            Copy-Item -LiteralPath (Join-Path $unpacked 'kuromatsu.exe') -Destination $exe -Force
        } catch [System.IO.IOException] {
            throw "cannot replace ${exe}: stop the running kuromatsu first"
        } catch [System.UnauthorizedAccessException] {
            throw "cannot replace ${exe}: stop the running kuromatsu first"
        }
        Write-Host "Installed $exe"

        # User PATH, keeping the value type: REG_EXPAND_SZ stays REG_EXPAND_SZ so
        # entries like %USERPROFILE%\go\bin keep working.
        $pathChanged = $false
        $key = Get-Item -LiteralPath 'HKCU:\Environment'
        $raw = $key.GetValue('Path', '', 'DoNotExpandEnvironmentNames')
        $kind = if ($key.GetValueNames() -contains 'Path') { $key.GetValueKind('Path') } else { 'ExpandString' }
        $present = $false
        foreach ($entry in ($raw -split ';')) {
            if ($entry.Trim() -eq '') { continue }
            $expanded = [Environment]::ExpandEnvironmentVariables($entry).TrimEnd('\')
            if ($expanded -eq $dest) { $present = $true }
        }
        if (-not $present) {
            if ($raw -eq '') { $newPath = $dest }
            elseif ($raw.EndsWith(';')) { $newPath = "$raw$dest" }
            else { $newPath = "$raw;$dest" }
            Set-ItemProperty -Path 'HKCU:\Environment' -Name Path -Value $newPath -Type $kind
            # Tells `kuromatsu uninstall` this entry is ours to remove; an entry
            # that was already there (e.g. a shared tools directory) stays.
            Set-Content -LiteralPath (Join-Path $dest '.kuromatsu-path-added') -Value $dest -Encoding ASCII
            $pathChanged = $true
            Write-Host "Added $dest to the user PATH"
            try {
                if (-not ('Kuromatsu.Installer.NativeMethods' -as [type])) {
                    Add-Type -Namespace Kuromatsu.Installer -Name NativeMethods -MemberDefinition @'
[DllImport("user32.dll", SetLastError = true, CharSet = CharSet.Unicode)]
public static extern IntPtr SendMessageTimeout(IntPtr hWnd, uint Msg, UIntPtr wParam, string lParam, uint fuFlags, uint uTimeout, out UIntPtr lpdwResult);
'@
                }
                $result = [UIntPtr]::Zero
                [void][Kuromatsu.Installer.NativeMethods]::SendMessageTimeout(
                    [IntPtr]0xffff, 0x1A, [UIntPtr]::Zero, 'Environment', 2, 5000, [ref]$result)
            } catch {
                Write-Warning "PATH saved; new terminals pick it up after the next logon ($($_.Exception.Message))"
            }
        }
        if (-not (($env:Path -split ';') -contains $dest)) {
            $env:Path = "$env:Path;$dest"
        }

        if ($env:KUROMATSU_INSTALL_NO_ONBOARD -eq '1') {
            Write-Host "Skipping onboard (KUROMATSU_INSTALL_NO_ONBOARD=1); run 'kuromatsu onboard' later."
        } elseif ([Environment]::UserInteractive -and -not [Console]::IsInputRedirected) {
            Write-Host 'Running kuromatsu onboard'
            & $exe onboard
            if ($LASTEXITCODE -ne 0) { throw "onboard failed; run 'kuromatsu onboard' again" }
        } else {
            Write-Host 'Running kuromatsu onboard --non-interactive (no terminal)'
            & $exe onboard --non-interactive
            if ($LASTEXITCODE -ne 0) { throw "onboard failed; run 'kuromatsu onboard' again" }
        }

        Write-Host ''
        Write-Host "kuromatsu is installed at $exe"
        & $exe version
        if ($pathChanged) {
            Write-Host ''
            Write-Host 'Open a new terminal to use kuromatsu from any directory.'
        }
    } finally {
        Remove-Item -LiteralPath $tmp -Recurse -Force -ErrorAction SilentlyContinue
    }
}

Install-Kuromatsu
