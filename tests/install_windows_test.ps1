$ErrorActionPreference = 'Stop'
$installer = Join-Path $PSScriptRoot '..\scripts\install.ps1'
$originalLocalAppData = $env:LOCALAPPDATA
$originalPath = $env:Path
$root = Join-Path ([IO.Path]::GetTempPath()) ('rexpresence-test-' + [guid]::NewGuid().ToString('N'))
$env:LOCALAPPDATA = $root
$directory = Join-Path $root 'Programs\rexpresence'
$destination = Join-Path $directory 'rexpresence.exe'
$global:installerTestPath = '%SystemRoot%\System32;C:\Other'
$global:installerTestBytes = [Text.Encoding]::UTF8.GetBytes('verified release binary')
$sha = [Security.Cryptography.SHA256]::Create()
try { $hash = [BitConverter]::ToString($sha.ComputeHash($global:installerTestBytes)).Replace('-', '').ToLowerInvariant() }
finally { $sha.Dispose() }
$global:installerTestSums = "$hash  rexpresence-windows-amd64.exe`n"
$global:installerTestDownloadFails = $false

function curl.exe {
    $arguments = @($args)
    if (($arguments -join '|') -notmatch '^--proto\|=https\|--proto-redir\|=https\|--tlsv1\.2\|-fsSL\|-o\|') { throw 'Missing HTTPS restrictions' }
    $file = $arguments[7]
    $url = $arguments[8]
    if ($url -notin @('https://github.com/Luqueee/rexpresence/releases/latest/download/SHA256SUMS', 'https://github.com/Luqueee/rexpresence/releases/latest/download/rexpresence-windows-amd64.exe')) { throw "Unexpected URL: $url" }
    if ($global:installerTestDownloadFails) { $global:LASTEXITCODE = 1; return }
    if ($url.EndsWith('/SHA256SUMS')) {
        [IO.File]::WriteAllText($file, $global:installerTestSums)
    } else {
        [IO.File]::WriteAllBytes($file, $global:installerTestBytes)
    }
    $global:LASTEXITCODE = 0
}
function Get-Item {
    param($LiteralPath, $ErrorAction)
    if ($LiteralPath -ne 'HKCU:\Environment') { throw "Unexpected registry read: $LiteralPath" }
    $key = [pscustomobject]@{}
    $key | Add-Member ScriptMethod GetValue {
        param($name, $defaultValue, $options)
        if ($name -ne 'Path' -or $options -ne [Microsoft.Win32.RegistryValueOptions]::DoNotExpandEnvironmentNames) { throw 'Unexpected registry value read' }
        return $global:installerTestPath
    }
    $key | Add-Member ScriptMethod GetValueKind { param($name) return [Microsoft.Win32.RegistryValueKind]::ExpandString }
    return $key
}
function New-ItemProperty {
    param($LiteralPath, $Name, $Value, $PropertyType, [switch] $Force, $ErrorAction)
    if ($LiteralPath -ne 'HKCU:\Environment' -or $Name -ne 'Path' -or $PropertyType -ne 'ExpandString') { throw 'Unexpected registry write' }
    $global:installerTestPath = $Value
}
function Assert($condition, $message) { if (-not $condition) { throw $message } }
function Assert-Fails($message) {
    $failed = $false
    try { & $installer } catch { $failed = $true }
    Assert $failed $message
}

try {
    & $installer
    Assert ([IO.File]::ReadAllText($destination) -eq 'verified release binary') 'Installation failed'
    Assert ($global:installerTestPath -eq "%SystemRoot%\System32;C:\Other;$directory") 'User PATH was not appended correctly'
    Assert ($directory -in ($env:Path -split ';')) 'Command not on the current shell PATH'
    & $installer
    Assert ($global:installerTestPath -eq "%SystemRoot%\System32;C:\Other;$directory") 'User PATH contains a duplicate'

    $global:installerTestBytes = [Text.Encoding]::UTF8.GetBytes('tampered')
    Assert-Fails 'Tampered binary was accepted'
    Assert ([IO.File]::ReadAllText($destination) -eq 'verified release binary') 'Tampering replaced the existing binary'
    $global:installerTestBytes = [Text.Encoding]::UTF8.GetBytes('verified release binary')
    $global:installerTestSums = "$hash  rexpresence-windows-amd64.exe`n$hash  rexpresence-windows-amd64.exe`n"
    Assert-Fails 'Duplicate checksum was accepted'
    $global:installerTestSums = "$hash  rexpresence-windows-amd64.exe`n"
    $global:installerTestDownloadFails = $true
    Assert-Fails 'Download failure was accepted'
    $global:installerTestDownloadFails = $false

    $locked = [IO.File]::Open($destination, [IO.FileMode]::Open, [IO.FileAccess]::Read, [IO.FileShare]::None)
    try { Assert-Fails 'Locked executable was replaced' } finally { $locked.Dispose() }
    Assert ([IO.File]::ReadAllText($destination) -eq 'verified release binary') 'Failure changed the existing binary'
    Assert (@(Get-ChildItem -LiteralPath $directory -Force).Count -eq 1) 'Temporary files were not cleaned up'
    Write-Host 'Windows installer smoke tests passed.'
} finally {
    $env:LOCALAPPDATA = $originalLocalAppData
    $env:Path = $originalPath
    if (Test-Path -LiteralPath $root) { Remove-Item -LiteralPath $root -Recurse -Force }
    Remove-Variable installerTestPath, installerTestBytes, installerTestSums, installerTestDownloadFails -Scope Global -ErrorAction SilentlyContinue
}
