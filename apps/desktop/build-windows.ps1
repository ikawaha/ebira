# Builds build/windows-<arch>/ebira, as build-windows.sh does, on Windows with
# PowerShell alone: the desktop app for Windows (GOARCH, amd64 by default) with
# the ebira server beside it. Windows PowerShell 5.1 runs it, so it keeps to
# ASCII. Scripts are not allowed to run by default, so run it as
#
#   powershell -ExecutionPolicy Bypass -File apps\desktop\build-windows.ps1
$ErrorActionPreference = 'Stop'

function Invoke-Go {
	go @args
	if ($LASTEXITCODE -ne 0) { throw "go $args failed" }
}

Set-Location $PSScriptRoot
$arch = if ($env:GOARCH) { $env:GOARCH } else { 'amd64' }
$out = "build/windows-$arch/ebira"

# The settings of go are for this script only, not the rest of the session.
$saved = @{}
foreach ($name in 'GOOS', 'GOARCH', 'CGO_ENABLED') {
	$saved[$name] = [Environment]::GetEnvironmentVariable($name, 'Process')
}
try {
	$env:GOOS = 'windows'
	$env:GOARCH = $arch
	$env:CGO_ENABLED = '0'

	if (Test-Path $out) { Remove-Item -Recurse -Force $out }
	New-Item -ItemType Directory -Force $out | Out-Null
	Push-Location ../..
	try {
		Invoke-Go build -o "apps/desktop/$out/ebira.exe" ./cmd/ebira
	} finally {
		Pop-Location
	}
	# The version of the app is the one of the server beside it, built from the
	# same checkout: the mod line of go version -m, "<tab>mod<tab>path<tab>version".
	$mod = Invoke-Go version -m "$out/ebira.exe" | Where-Object { $_ -match '^\s*mod\s' } | Select-Object -First 1
	$version = ($mod.Trim() -split '\s+')[2]
	Invoke-Go build -ldflags "-H windowsgui -X main.buildVersion=$version" -o "$out/ebira-desktop.exe" .
	# The license of the icon of the web UI, which is not the license of ebira.
	Copy-Item ../../internal/ui/web/static/favicon.LICENSE $out
} finally {
	foreach ($name in $saved.Keys) {
		[Environment]::SetEnvironmentVariable($name, $saved[$name], 'Process')
	}
}
Write-Output $out
