$ErrorActionPreference = 'Stop'

$repo = 'neur0map/prowl-agent'
$base = if ($env:PROWL_RELEASE_BASE) { $env:PROWL_RELEASE_BASE } else { "https://github.com/$repo/releases/download/nightly" }
$dest = if ($env:PROWL_INSTALL_DIR) { $env:PROWL_INSTALL_DIR } else { Join-Path $HOME '.local\bin' }

switch ([System.Runtime.InteropServices.RuntimeInformation]::OSArchitecture.ToString()) {
    'X64' { $arch = 'amd64' }
    default { throw "Unsupported Windows architecture: $($_). Build from source instead." }
}

$name = "prowl-agent-windows-$arch.exe"
$tmp = Join-Path ([System.IO.Path]::GetTempPath()) ("prowl-install-" + [guid]::NewGuid())
New-Item -ItemType Directory -Path $tmp | Out-Null
try {
    $binary = Join-Path $tmp $name
    $checksum = "$binary.sha256"
    Invoke-WebRequest "$base/$name" -OutFile $binary
    Invoke-WebRequest "$base/$name.sha256" -OutFile $checksum

    $want = ((Get-Content $checksum -Raw) -split '\s+')[0].ToLowerInvariant()
    $got = (Get-FileHash -Algorithm SHA256 $binary).Hash.ToLowerInvariant()
    if ($want -ne $got) { throw 'Checksum mismatch; aborting.' }

    New-Item -ItemType Directory -Force -Path $dest | Out-Null
    Copy-Item $binary (Join-Path $dest 'prowl-agent.exe') -Force
    Write-Host "Prowl installed to $(Join-Path $dest 'prowl-agent.exe')"
    Write-Host 'Next: cd <project>; prowl-agent init'
    if (($env:Path -split ';') -notcontains $dest) {
        Write-Host "Note: add $dest to your user PATH."
    }
} finally {
    Remove-Item -Recurse -Force $tmp -ErrorAction SilentlyContinue
}
