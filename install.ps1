param(
    [string]$Version = $(if ($env:LOOKUP_VERSION) { $env:LOOKUP_VERSION } else { "latest" }),
    [string]$InstallDir = $(if ($env:LOOKUP_INSTALL_DIR) { $env:LOOKUP_INSTALL_DIR } else { Join-Path $env:LOCALAPPDATA "Programs\Lookup\bin" })
)
$ErrorActionPreference = "Stop"
$repository = "poizdev/lookup"

function Test-LookupUnicodeTerminal {
    if ([Console]::IsOutputRedirected -or $env:TERM -eq "dumb") { return $false }
    if ($env:WT_SESSION -or $env:TERM_PROGRAM) { return $true }
    return [Console]::OutputEncoding.WebName -eq "utf-8"
}

function Write-LookupWordmark {
    $unicode = Test-LookupUnicodeTerminal
    $width = 0
    if ($unicode) {
        try { $width = $Host.UI.RawUI.WindowSize.Width } catch { $width = 0 }
    }
    if ($unicode -and $width -ge 74) {
        # LOOKUP_WORDMARK_BEGIN
    $wordmark = @'
⠀⠀⠀⠀⠀⠀⠀⠀⠀⠀⠀⠀⠀⠀⠀⠀⠀⠀⠀⠀⣶⣶⠀⠀⠀⠀⠀⠀⠀⠀⠀⠀⠀⠀⠀⠀⠀⠀⠀⠀⠀⠀⠀⢰⣶⡆⠀⠀⠀⠀⠀⠀⠀⠀⠀⠀⠀⠀⠀⠀⠀⠀⣀⣀⡀⠀⢀⣠⣤⣀⣀⠀⠀⠀
⢠⡀⠀⠀⠀⠀⠀⠀⠀⠀⠀⠀⢀⣶⣶⠄⠀⠀⠀⠀⣿⣿⠀⠀⠀⠀⠀⠀⠀⠀⠀⠀⠀⠀⠀⠀⠀⠀⠀⠀⠀⠀⠀⢸⣿⡇⠀⠀⠀⠀⠀⠀⣀⣀⠀⠀⠀⠀⠀⠀⣀⣀⠿⢿⣧⣼⠿⠛⠛⠻⢟⣷⣄⠀
⠘⣧⠀⠀⠀⠀⠀⣀⣤⡀⠀⠀⣼⣟⡟⠀⠀⠀⠀⠀⣿⣿⠀⠀⣠⣴⣶⣿⣷⣶⣤⡀⠀⠀⣀⣴⣶⣾⣿⣶⣤⣀⠀⢸⣿⡇⠀⠀⠀⣠⣶⡶⣿⣻⠀⠀⠀⠀⠀⠀⣿⣻⠀⢸⣯⡇⠀⠀⠀⠀⠀⢻⢿⡄
⠀⢹⡆⠀⠀⠀⢰⣿⠿⣿⣦⢰⣿⡻⠀⠀⠀⠀⠀⠀⣿⣿⢀⣾⣿⠏⠁⠀⠀⠉⢿⣿⡆⣼⣿⠟⠃⠀⠀⠉⠻⣿⣦⢸⣿⡇⠀⣤⣾⣿⠏⠀⣿⣽⠀⠀⠀⠀⠀⠀⣿⣽⠀⢸⣿⠆⠀⠀⠀⠀⠀⢸⣿⡇
⠀⠀⣿⡄⠀⢀⣿⡟⠄⠹⣿⣿⡿⠁⠀⠀⠀⠀⠀⠀⣿⣿⢸⣿⡇⠀⠀⠀⠀⠀⠈⣿⣿⣿⡏⠀⠀⠀⠀⠀⠀⣿⣿⣼⣿⣧⣾⣿⣅⠀⠀⠀⣿⢾⠀⠀⠀⠀⠀⠀⣿⣾⠀⢸⣿⣧⡀⠀⠀⠀⣠⣿⢾⠁
⠀⠀⢸⣧⠀⣼⡿⠁⠀⠀⠀⠀⠀⠀⠀⠀⠀⠀⠀⠀⣿⣿⢸⣿⣇⠀⠀⠀⠀⠀⢀⣿⡿⣿⣧⠀⠀⠀⠀⠀⠀⣿⣿⢻⣿⡟⠁⠻⣿⣦⡀⠀⢻⣟⣇⠀⠀⠀⠀⣰⣿⣽⡀⢺⡿⡜⠿⣷⣶⣿⡻⠝⠁⠀
⠀⠀⠀⢿⣧⣿⠃⠀⠀⠀⠀⠀⠀⠀⠀⠀⠀⠀⠀⠀⣿⣿⠀⠻⣿⣦⣄⣀⣠⣤⣾⡿⠃⠹⣿⣷⣤⣀⣠⣤⣾⣿⠇⢸⣿⡇⠀⠀⠈⠻⣿⣦⠀⠙⢿⣻⣷⣿⡿⠛⠈⣿⣻⢸⣟⡇⠀⠀⠀⠀⠀⠀⠀⠀
⠀⠀⠀⠀⠉⠀⠀⠀⠀⠀⠀⠀⠀⠀⠀⠀⠀⠀⠀⠀⠛⠛⠂⠀⠈⠙⠛⠛⠛⠛⠉⠀⠀⠀⠀⠉⠛⠛⠛⠛⠉⠀⠀⠘⠛⠓⠀⠀⠀⠀⠙⠛⠛⠀⠀⠀⠀⠀⠀⠀⠀⠀⠀⣹⣯⡇⠀⠀⠀⠀⠀⠀⠀⠀
'@
    # LOOKUP_WORDMARK_END
        Write-Host $wordmark
        return
    }
    Write-Host "lookup"
}

function Write-LookupSuccess([string]$releaseVersion, [string]$installPath, [string]$installDir) {
    Write-LookupWordmark
    Write-Host ""
    if (Test-LookupUnicodeTerminal) {
        Write-Host "✓ Lookup $releaseVersion installed"
    } else {
        Write-Host "Lookup $releaseVersion installed"
    }
    Write-Host ""
    Write-Host "Binary"
    Write-Host "  $installPath"
    Write-Host ""
    if (($env:PATH -split ';') -contains $installDir) {
        Write-Host "Get started"
        Write-Host "  lookup init"
        Write-Host ""
        Write-Host "Then analyze a project"
        Write-Host "  lookup ."
        return
    }
    Write-Warning "$installDir is not in PATH"
    Write-Host ""
    Write-Host "Add $installDir to your user PATH, then open a new terminal."
    Write-Host ""
    Write-Host "Then run:"
    Write-Host "  lookup init"
}

if ($Version -eq "latest") {
    $Version = (Invoke-RestMethod "https://api.github.com/repos/$repository/releases/latest").tag_name
}
if ($Version -notmatch '^v\d+\.\d+\.\d+(-[0-9A-Za-z]+([.-][0-9A-Za-z]+)*)?$') { throw "Invalid version '$Version'; expected vX.Y.Z or vX.Y.Z-prerelease" }
if (-not [Environment]::Is64BitOperatingSystem) { throw "Lookup supports only Windows amd64" }
$releaseVersion = $Version.Substring(1)
$archive = "lookup_${releaseVersion}_windows_amd64.zip"
$baseUrl = "https://github.com/$repository/releases/download/$Version"
$temp = Join-Path ([IO.Path]::GetTempPath()) ("lookup-" + [Guid]::NewGuid())
New-Item -ItemType Directory $temp | Out-Null
try {
    Invoke-WebRequest "$baseUrl/$archive" -OutFile (Join-Path $temp $archive)
    Invoke-WebRequest "$baseUrl/checksums.txt" -OutFile (Join-Path $temp "checksums.txt")
    $entry = Get-Content (Join-Path $temp "checksums.txt") | Where-Object { $_ -match "\s$([regex]::Escape($archive))$" }
    if (-not $entry) { throw "Checksum entry not found for $archive" }
    $expected = ($entry -split '\s+')[0].ToLowerInvariant()
    $actual = (Get-FileHash (Join-Path $temp $archive) -Algorithm SHA256).Hash.ToLowerInvariant()
    if ($actual -ne $expected) { throw "Checksum verification failed for $archive" }
    Expand-Archive (Join-Path $temp $archive) -DestinationPath $temp -Force
    New-Item -ItemType Directory -Force $InstallDir | Out-Null
    $InstallDir = [IO.Path]::GetFullPath($InstallDir)
    $installPath = Join-Path $InstallDir "lookup.exe"
    Copy-Item (Join-Path $temp "lookup.exe") $installPath -Force
    $receiptDir = Join-Path $env:APPDATA "lookup"
    $receiptPath = Join-Path $receiptDir "install.json"
    New-Item -ItemType Directory -Force $receiptDir | Out-Null
    $receipt = @{ method = "official-installer"; install_path = $installPath; repository = "poizdev/lookup" } | ConvertTo-Json
    [IO.File]::WriteAllText($receiptPath, $receipt, (New-Object Text.UTF8Encoding($false)))
    Write-LookupSuccess $releaseVersion $installPath $InstallDir
} finally {
    Remove-Item $temp -Recurse -Force -ErrorAction SilentlyContinue
}
