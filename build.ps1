param(
    [string]$Output = "bin\margit.exe",
    [switch]$Test
)

$ErrorActionPreference = "Stop"
Set-Location $PSScriptRoot

function Step([string]$name, [scriptblock]$cmd) {
    Write-Host "==> $name"
    & $cmd
    if ($LASTEXITCODE -ne 0) {
        Write-Host "FAILED: $name" -ForegroundColor Red
        exit $LASTEXITCODE
    }
}

Step "fill log tags" { go run ./cmd/tagger }
Step "check log tags" { go run ./cmd/tagger -check }
Step "gofmt" { gofmt -w . }
Step "vet" { go vet ./... }
if ($Test) {
    Step "test" { go test -race ./... }
}
Step "build" { go build -o $Output . }

Write-Host "built $Output" -ForegroundColor Green
