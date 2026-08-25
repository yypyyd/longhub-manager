[CmdletBinding()]
param(
  [Parameter(Mandatory = $true)]
  [ValidatePattern('^(0|[1-9][0-9]{0,8})\.(0|[1-9][0-9]{0,8})\.(0|[1-9][0-9]{0,8})$')]
  [string]$Version,

  [Parameter(Mandatory = $true)]
  [ValidatePattern('^https://[^/?#]+$')]
  [string]$CloudApiBaseUrl,

  [string]$OutputDirectory = (Join-Path $PSScriptRoot '..\release'),
  [switch]$AllowUnsigned
)

$ErrorActionPreference = 'Stop'
$managerRoot = [IO.Path]::GetFullPath((Join-Path $PSScriptRoot '..'))
$outputRoot = [IO.Path]::GetFullPath($OutputDirectory)
$managerBoundary = $managerRoot.TrimEnd([IO.Path]::DirectorySeparatorChar) + [IO.Path]::DirectorySeparatorChar
if (-not $outputRoot.StartsWith($managerBoundary, [StringComparison]::OrdinalIgnoreCase)) {
  throw 'OutputDirectory must remain inside the LongHub Manager repository'
}

$releaseId = [DateTime]::UtcNow.ToString('yyyyMMddTHHmmssfffZ')
$stage = Join-Path $outputRoot ".stage-$releaseId"
$resourceSyso = Join-Path $managerRoot 'cmd\longhub-manager\resource.syso'
New-Item -ItemType Directory -Path $stage -Force | Out-Null
Push-Location $managerRoot
try {
  $managerExe = Join-Path $stage 'LongHubManager.exe'
  $versionParts = $Version.Split('.')
  $versionInfoArguments = @(
    '-64',
    '-o', $resourceSyso,
    '-ver-major', $versionParts[0],
    '-ver-minor', $versionParts[1],
    '-ver-patch', $versionParts[2],
    '-ver-build', '0',
    '-product-ver-major', $versionParts[0],
    '-product-ver-minor', $versionParts[1],
    '-product-ver-patch', $versionParts[2],
    '-product-ver-build', '0',
    '-file-version', $Version,
    '-product-version', $Version,
    '-company', 'LongHub',
    '-copyright', 'Copyright 2026 LongHub Manager contributors',
    '-description', 'LongHub Manager for native OpenClaw',
    '-internal-name', 'LongHubManager',
    '-original-name', 'LongHubManager.exe',
    '-product-name', 'LongHub Manager',
    '-trademark', 'LongHub is a trademark of its respective owner',
    '-propagate-ver-strings',
    '-manifest', (Join-Path $managerRoot 'scripts\longhub-manager.manifest'),
    (Join-Path $managerRoot 'scripts\versioninfo.json')
  )
  & go tool goversioninfo @versionInfoArguments
  if ($LASTEXITCODE -ne 0 -or -not (Test-Path -LiteralPath $resourceSyso -PathType Leaf)) {
    throw 'Go Manager version resource generation failed'
  }
  $previousGoos = $env:GOOS
  $previousGoarch = $env:GOARCH
  try {
    $env:GOOS = 'windows'
    $env:GOARCH = 'amd64'
    & go build -trimpath -ldflags "-s -w -H=windowsgui -X main.version=$Version" -o $managerExe ./cmd/longhub-manager
    if ($LASTEXITCODE -ne 0) { throw 'Go Manager build failed' }
  } finally {
    $env:GOOS = $previousGoos
    $env:GOARCH = $previousGoarch
  }

  $releaseConfig = [ordered]@{
    schema_version = 'longhub/manager-release-config/v1'
    cloud_api_base_url = $CloudApiBaseUrl
  } | ConvertTo-Json
  [IO.File]::WriteAllText((Join-Path $stage 'release-config.json'), $releaseConfig + "`n", [Text.UTF8Encoding]::new($false))

  $signTool = Get-ChildItem 'C:\Program Files (x86)\Windows Kits\10\bin' -Recurse -Filter signtool.exe -ErrorAction SilentlyContinue |
    Sort-Object FullName -Descending | Select-Object -First 1 -ExpandProperty FullName
  $thumbprint = ($env:LONGHUB_WINDOWS_SIGNING_SHA1 ?? '').Replace(' ', '')
  function Invoke-CodeSign([string]$Path) {
    if ($AllowUnsigned) { return }
    if (-not $signTool -or $thumbprint -notmatch '^[A-Fa-f0-9]{40}$') {
      throw 'A trusted code-signing certificate and signtool are required; use -AllowUnsigned only for isolated candidate tests'
    }
    & $signTool sign /sha1 $thumbprint /fd SHA256 /tr 'http://timestamp.digicert.com' /td SHA256 $Path
    if ($LASTEXITCODE -ne 0) { throw "Code signing failed: $Path" }
    $signature = Get-AuthenticodeSignature -LiteralPath $Path
    if ($signature.Status -ne 'Valid') { throw "Authenticode validation failed: $Path" }
  }
  Invoke-CodeSign $managerExe

  $makeNsis = Join-Path ${env:ProgramFiles(x86)} 'NSIS\makensis.exe'
  if (-not (Test-Path -LiteralPath $makeNsis -PathType Leaf)) { throw 'NSIS 3 is not installed' }
  New-Item -ItemType Directory -Path $outputRoot -Force | Out-Null
  $nsisArguments = @(
    "/DVERSION=$Version",
    "/DSTAGE_DIR=$stage",
    "/DOUTPUT_DIR=$outputRoot"
  )
  $nsisArguments += (Join-Path $managerRoot 'installer\manager.nsi')
  & $makeNsis @nsisArguments
  if ($LASTEXITCODE -ne 0) { throw 'NSIS build failed' }

  $installer = Join-Path $outputRoot "LongHub-Manager-Setup-$Version.exe"
  Invoke-CodeSign $installer
  $installerInfo = Get-Item -LiteralPath $installer
  $installerHash = (Get-FileHash -LiteralPath $installer -Algorithm SHA256).Hash.ToLowerInvariant()
  [pscustomobject]@{
    version = $Version
    installer = $installerInfo.FullName
    size = $installerInfo.Length
    sha256 = $installerHash
    signed = -not $AllowUnsigned
    cloud_api_base_url = $CloudApiBaseUrl
  } | ConvertTo-Json -Compress
} finally {
  Pop-Location
  Remove-Item -LiteralPath $resourceSyso -Force -ErrorAction SilentlyContinue
  $resolvedStage = [IO.Path]::GetFullPath($stage)
  if ($resolvedStage.StartsWith($outputRoot, [StringComparison]::OrdinalIgnoreCase) -and
      (Split-Path -Leaf $resolvedStage).StartsWith('.stage-', [StringComparison]::Ordinal)) {
    Remove-Item -LiteralPath $resolvedStage -Recurse -Force -ErrorAction SilentlyContinue
  }
}
