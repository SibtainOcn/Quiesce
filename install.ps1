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

    $frames = [char[]]@(0x280B, 0x2819, 0x2839, 0x2838, 0x283C, 0x2834, 0x2826, 0x2827, 0x2807, 0x280F)
    $esc = [char]27

    # Returns the HTTP status on failure (0 = no connection).
    function Fetch($label, $url, $outFile) {
        $wc = New-Object Net.WebClient
        $wc.Headers['User-Agent'] = 'Quiesce-installer'
        if ($outFile) { $task = $wc.DownloadFileTaskAsync($url, $outFile) }
        else { $task = $wc.DownloadStringTaskAsync($url) }
        $i = 0
        while (-not $task.IsCompleted) {
            Write-Host -NoNewline "`r  $esc[31m$($frames[$i % $frames.Length])$esc[0m  $label"
            Start-Sleep -Milliseconds 80
            $i++
        }
        $wc.Dispose()
        if ($task.IsFaulted) {
            Write-Host -NoNewline "`r  $esc[33m!$esc[0m  $label`n"
            $ex = $task.Exception.GetBaseException()
            $code = 0
            if ($ex.Response) { $code = [int]$ex.Response.StatusCode }
            return @{ Status = $code; Error = $ex.Message }
        }
        Write-Host -NoNewline "`r  $esc[32m$([char]0x2713)$esc[0m  $label`n"
        if ($outFile) { return @{ Status = 200 } }
        return @{ Status = 200; Content = $task.Result }
    }

    try {
        # Windows PowerShell 5.1 may default to old TLS versions GitHub refuses.
        [Net.ServicePointManager]::SecurityProtocol = [Net.ServicePointManager]::SecurityProtocol -bor [Net.SecurityProtocolType]::Tls12

        New-Item -ItemType Directory -Path $work -Force | Out-Null
        Write-Host ''
        Say 'Official source: github.com/SibtainOcn/Quiesce releases'
        $r = Fetch "Downloading $asset" "$base/$asset" $exe
        if ($r.Status -eq 0) { throw "Could not connect - check your internet connection and try again. ($($r.Error))" }
        if ($r.Status -eq 404) { throw "No $asset was found in the latest Quiesce release. Nothing was installed." }
        if ($r.Status -ne 200) { throw "GitHub returned HTTP $($r.Status) - try again later." }

        $r = Fetch 'Downloading SHA256SUMS.txt' "$base/SHA256SUMS.txt" $null
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
