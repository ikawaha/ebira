# Builds the desktop app for Windows and installs it in
# %LOCALAPPDATA%\Programs\ebira, with a shortcut in the Start menu, or over the
# one installed before, until the app is distributed signed:
#
#   powershell -ExecutionPolicy Bypass -File apps\desktop\install-windows.ps1 [-NoOpen] [ref]
#
# In a checkout, it builds the checkout. Run from the web, or given a ref (a
# tag or a branch), it clones ebira at the ref, the latest release by default,
# into a temporary directory:
#
#   & ([scriptblock]::Create((irm https://raw.githubusercontent.com/ikawaha/ebira/main/apps/desktop/install-windows.ps1)))
#   & ([scriptblock]::Create((irm https://raw.githubusercontent.com/ikawaha/ebira/main/apps/desktop/install-windows.ps1))) v0.8.1
#
# The app running is asked to close first, which asks about a note being
# edited. The server the installed app started runs its ebira.exe, so it is
# started again with the new one, with the same flags. Windows PowerShell 5.1
# runs it, so it keeps to ASCII.
param(
	[switch]$NoOpen,
	[string]$Ref = ''
)
$ErrorActionPreference = 'Stop'

$repo = 'https://github.com/ikawaha/ebira.git'
$dest = Join-Path $env:LOCALAPPDATA 'Programs\ebira'

if (-not (Get-Command go -ErrorAction SilentlyContinue)) {
	throw 'go is not found; install Go 1.27 or later from https://go.dev/dl/'
}

$tmp = $null
$savedArch = $env:GOARCH
try {
	# The checkout to build: the one of this script, or a clone.
	if (-not $Ref -and $PSScriptRoot -and (Test-Path (Join-Path $PSScriptRoot 'build-windows.ps1'))) {
		$src = $PSScriptRoot
	} else {
		if (-not (Get-Command git -ErrorAction SilentlyContinue)) {
			throw 'git is not found; install it from https://git-scm.com/'
		}
		if (-not $Ref) {
			$tags = git ls-remote --tags --refs $repo 'v*' | ForEach-Object { ($_ -split 'refs/tags/')[1] } |
				Where-Object { $_ -match '^v\d+\.\d+\.\d+$' }
			$Ref = $tags | Sort-Object { [version]$_.Substring(1) } | Select-Object -Last 1
			if (-not $Ref) { throw "no release of ebira is found at $repo" }
		}
		$tmp = Join-Path ([IO.Path]::GetTempPath()) ('ebira-install-' + [guid]::NewGuid())
		Write-Output "Cloning ebira $Ref..."
		# Not shallow, as the build of macOS, which counts the commits.
		git -c advice.detachedHead=false clone -q --filter=blob:none --branch $Ref $repo (Join-Path $tmp 'ebira')
		if ($LASTEXITCODE -ne 0) { throw "git clone failed" }
		$src = Join-Path $tmp 'ebira\apps\desktop'
	}

	$arch = $env:GOARCH
	if (-not $arch) {
		$arch = if ($env:PROCESSOR_ARCHITECTURE -eq 'ARM64') { 'arm64' } else { 'amd64' }
	}
	$env:GOARCH = $arch
	Write-Output 'Building the app...'
	& powershell -NoProfile -ExecutionPolicy Bypass -File (Join-Path $src 'build-windows.ps1') | Out-Null
	if ($LASTEXITCODE -ne 0) { throw 'the build failed' }
	$out = Join-Path $src "build\windows-$arch\ebira"

	$app = Get-Process ebira-desktop -ErrorAction SilentlyContinue
	if ($app) {
		Write-Output 'Closing ebira...'
		foreach ($p in $app) { $p.CloseMainWindow() | Out-Null }
		foreach ($p in $app) {
			if (-not $p.WaitForExit(120000)) {
				throw 'ebira did not close, such as for a note being edited; close it and run this again'
			}
		}
	}

	# The server of the notes of the app, if the installed app started it.
	$helper = Join-Path $dest 'ebira.exe'
	$restart = $null
	if (Test-Path $helper) {
		$notesArgs = @()
		$settings = Join-Path $env:APPDATA 'ebira\desktop.json'
		if (Test-Path $settings) {
			$notes = (Get-Content -Raw $settings | ConvertFrom-Json).notesDir
			if ($notes) { $notesArgs = @('-notes', $notes) }
		}
		# Windows PowerShell 5.1 stops on the standard error of a program even
		# when it is thrown away, under the preference Stop.
		$ErrorActionPreference = 'Continue'
		$status = & $helper server status -json @notesArgs 2>$null | Out-String
		$ErrorActionPreference = 'Stop'
		if ($LASTEXITCODE -eq 0 -and $status) {
			$st = $status | ConvertFrom-Json
			if ($st.running -and $st.state) {
				$p = Get-Process -Id $st.state.pid -ErrorAction SilentlyContinue
				if ($p -and $p.Path -eq $helper) { $restart = $st.notes_dir }
			}
		}
	}

	Write-Output "Installing $dest..."
	New-Item -ItemType Directory -Force $dest | Out-Null
	# A running program cannot be replaced, but it can be renamed: a server
	# that runs the ebira.exe installed keeps the old one until it stops.
	foreach ($name in 'ebira.exe', 'ebira-desktop.exe') {
		$file = Join-Path $dest $name
		if (Test-Path $file) {
			Remove-Item -Force -ErrorAction SilentlyContinue "$file.old"
			Move-Item -Force $file "$file.old"
		}
	}
	Copy-Item -Recurse -Force (Join-Path $out '*') $dest

	if ($restart) {
		Write-Output "Starting the server of $restart again with the new build..."
		& $helper server restart -notes $restart
		if ($LASTEXITCODE -ne 0) { throw 'the server did not start again' }
	}
	foreach ($name in 'ebira.exe', 'ebira-desktop.exe') {
		Remove-Item -Force -ErrorAction SilentlyContinue (Join-Path $dest "$name.old")
	}

	$shortcut = Join-Path ([Environment]::GetFolderPath('Programs')) 'ebira.lnk'
	$link = (New-Object -ComObject WScript.Shell).CreateShortcut($shortcut)
	$link.TargetPath = Join-Path $dest 'ebira-desktop.exe'
	$link.WorkingDirectory = $dest
	$link.Save()

	if (-not $NoOpen) { Start-Process (Join-Path $dest 'ebira-desktop.exe') }
	$version = (& $helper version | Select-String '^version:\s*(.*)').Matches[0].Groups[1].Value
	Write-Output "Installed: $version"
} finally {
	$env:GOARCH = $savedArch
	if ($tmp) { Remove-Item -Recurse -Force -ErrorAction SilentlyContinue $tmp }
}
