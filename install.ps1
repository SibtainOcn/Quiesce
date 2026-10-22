# Quiesce installer.
#
#   irm https://raw.githubusercontent.com/SibtainOcn/Quiesce/main/install.ps1 | iex
#
# Downloads the latest release from the official GitHub releases page only,
# checks its SHA-256 against the release's SHA256SUMS.txt, then lets qc.exe
# install itself (qc env): it is copied to %LOCALAPPDATA%\Programs\Quiesce
# and added to your user PATH. No Admin needed.
#
# Set $env:QUIESCE_BASE_URL to install from a different release folder
# (used for testing).

function Install-Quiesce {
    $ErrorActionPreference = 'Stop'
    $oldProgress = $ProgressPreference
    $ProgressPreference = 'SilentlyContinue'

    $base = 'https://github.com/SibtainOcn/Quiesce/releases/latest/download'
    if ($env:QUIESCE_BASE_URL) { $base = $env:QUIESCE_BASE_URL.TrimEnd('/') }

    $asset = 'qc-32bit.exe'
    if ([Environment]::Is64BitOperatingSystem) { $asset = 'qc-64bit.exe' }

    $work = Join-Path ([IO.Path]::GetTempPath()) ('quiesce-install-' + [guid]::NewGuid().ToString('N'))
    $exe = Join-Path $work $asset

    function Say($text, $color = 'Gray') { Write-Host "  $text" -ForegroundColor $color }

    # Fetch returns the HTTP status on failure (0 = no connection).
    function Fetch($url, $outFile) {
        try {
            if ($outFile) {
                Invoke-WebRequest -Uri $url -OutFile $outFile -UseBasicParsing
                return @{ Status = 200 }
            }
            $r = Invoke-WebRequest -Uri $url -UseBasicParsing
            return @{ Status = 200; Content = $r.Content }
        } catch {
            $code = 0
            if ($_.Exception.Response) { $code = [int]$_.Exception.Response.StatusCode }
            return @{ Status = $code; Error = $_.Exception.Message }
        }
    }

    try {
        # Windows PowerShell 5.1 may default to old TLS versions GitHub refuses.
        [Net.ServicePointManager]::SecurityProtocol = [Net.ServicePointManager]::SecurityProtocol -bor [Net.SecurityProtocolType]::Tls12

        New-Item -ItemType Directory -Path $work -Force | Out-Null
        Write-Host ''
        Say "Downloading $asset from the official Quiesce releases..."
        $r = Fetch "$base/$asset" $exe
        if ($r.Status -eq 0) { throw "Could not connect - check your internet connection and try again. ($($r.Error))" }
        if ($r.Status -eq 404) { throw "No $asset was found in the latest Quiesce release. Nothing was installed." }
        if ($r.Status -ne 200) { throw "GitHub returned HTTP $($r.Status) - try again later." }

        $r = Fetch "$base/SHA256SUMS.txt"
        if ($r.Status -eq 0) { throw "Could not connect - check your internet connection and try again. ($($r.Error))" }
        if ($r.Status -ne 200) { throw "This release has no checksum file, so it can't be verified. Nothing was installed." }
        $sums = $r.Content
        if ($sums -is [byte[]]) { $sums = [Text.Encoding]::UTF8.GetString($sums) }

        $expected = $null
        foreach ($line in ($sums -split "`n")) {
            $parts = $line.Trim() -split '\s+'
            if ($parts.Count -eq 2 -and $parts[1].TrimStart('*') -ieq $asset) { $expected = $parts[0].ToLower() }
        }
        if (-not $expected -or $expected.Length -ne 64) {
            throw "This release has no checksum for $asset, so it can't be verified. Nothing was installed."
        }

        Say 'Verifying SHA-256 checksum...'
        $actual = (Get-FileHash -Path $exe -Algorithm SHA256).Hash.ToLower()
        if ($actual -ne $expected) {
            Remove-Item -Path $exe -Force -ErrorAction SilentlyContinue
            throw "Checksum mismatch - the download was deleted and nothing was installed. Expected $expected, got $actual."
        }
        Say "Checksum OK  $actual" 'Green'

        Unblock-File -Path $exe -ErrorAction SilentlyContinue
        & $exe env
        if ($LASTEXITCODE -ne 0) { throw 'Installation did not finish (see above).' }

        # Make qc work in this terminal straight away.
        $dir = Join-Path $env:LOCALAPPDATA 'Programs\Quiesce'
        if (($env:Path -split ';') -notcontains $dir) { $env:Path = "$env:Path;$dir" }
        Say 'Ready - type qc to start.' 'Green'
        Write-Host ''
    } catch {
        Write-Host ''
        Say $_.Exception.Message 'Red'
        Write-Host ''
    } finally {
        Remove-Item -Path $work -Recurse -Force -ErrorAction SilentlyContinue
        $ProgressPreference = $oldProgress
    }
}

Install-Quiesce
Remove-Item Function:\Install-Quiesce -ErrorAction SilentlyContinue
