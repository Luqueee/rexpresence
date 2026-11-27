$ErrorActionPreference = 'Stop'

$architecture = if ($env:PROCESSOR_ARCHITEW6432) { $env:PROCESSOR_ARCHITEW6432 } else { $env:PROCESSOR_ARCHITECTURE }
if ($env:OS -ne 'Windows_NT' -or $architecture -ne 'AMD64') {
    throw 'rexpresence requires 64-bit Windows on x64.'
}
if (-not $env:LOCALAPPDATA) { throw 'LOCALAPPDATA is not set.' }

$asset = 'rexpresence-windows-amd64.exe'
$base = 'https://github.com/Luqueee/rexpresence/releases/latest/download/'
$directory = Join-Path $env:LOCALAPPDATA 'Programs\rexpresence'
$destination = Join-Path $directory 'rexpresence.exe'
$id = [guid]::NewGuid().ToString('N')
$staged = Join-Path $directory ('.rexpresence-' + $id + '.tmp')
$sumsFile = Join-Path $directory ('.rexpresence-' + $id + '.sha256')

try {
    [IO.Directory]::CreateDirectory($directory) | Out-Null
    & curl.exe --proto '=https' --proto-redir '=https' --tlsv1.2 -fsSL -o $sumsFile ($base + 'SHA256SUMS')
    if ($LASTEXITCODE -ne 0) { throw 'Could not download SHA256SUMS over HTTPS.' }
    $sums = [IO.File]::ReadAllText($sumsFile)
    $entries = @($sums -split "`n" | Where-Object { $_ -cmatch ('^[0-9a-fA-F]{64}  ' + [regex]::Escape($asset) + '\r?$') })
    if ($entries.Count -ne 1) { throw "SHA256SUMS must contain exactly one entry for $asset." }
    & curl.exe --proto '=https' --proto-redir '=https' --tlsv1.2 -fsSL -o $staged ($base + $asset)
    if ($LASTEXITCODE -ne 0) { throw "Could not download $asset over HTTPS." }
    if ((Get-FileHash -LiteralPath $staged -Algorithm SHA256).Hash -ne $entries[0].Substring(0, 64)) {
        throw "SHA-256 verification failed for $asset; existing installation was not changed."
    }

    if ([IO.File]::Exists($destination)) {
        [IO.File]::Replace($staged, $destination, [System.Management.Automation.Language.NullString]::Value)
    } else {
        [IO.File]::Move($staged, $destination)
    }

    try {
        $key = Get-Item -LiteralPath 'HKCU:\Environment' -ErrorAction Stop
        $userPath = $key.GetValue('Path', $null, [Microsoft.Win32.RegistryValueOptions]::DoNotExpandEnvironmentNames)
        $onPath = $false
        if ($userPath) {
            $onPath = @($userPath -split ';' | Where-Object {
                [Environment]::ExpandEnvironmentVariables($_.Trim().Trim('"')).TrimEnd('\') -ieq $directory.TrimEnd('\')
            }).Count -gt 0
        }
        if (-not $onPath) {
            $separator = if ($userPath -and -not $userPath.EndsWith(';')) { ';' } else { '' }
            $newPath = $userPath + $separator + $directory
            $kind = if ($null -eq $userPath) { 'ExpandString' } else { $key.GetValueKind('Path').ToString() }
            New-ItemProperty -LiteralPath 'HKCU:\Environment' -Name Path -Value $newPath -PropertyType $kind -Force -ErrorAction Stop | Out-Null
            Write-Host "Added $directory to your user PATH."
        }
        if ($directory -notin ($env:Path -split ';')) {
            $env:Path += ";$directory"
        }
    } catch {
        Write-Warning "Installed the binary, but could not update user PATH: $_"
        Write-Host "Run it directly: & '$destination'"
        return
    }
    Write-Host "Installed $destination. Run rexpresence when ready."
    Write-Host 'Optional Rex palette setup: https://github.com/Luqueee/rexpresence#change-your-presence-from-rex'
} finally {
    if ([IO.File]::Exists($staged)) { [IO.File]::Delete($staged) }
    if ([IO.File]::Exists($sumsFile)) { [IO.File]::Delete($sumsFile) }
}
