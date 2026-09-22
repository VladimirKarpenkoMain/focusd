# Сборка Focusd под Windows.
#   powershell -ExecutionPolicy Bypass -File build.ps1
#
# Требуется инструмент Wails:
#   go install github.com/wailsapp/wails/v2/cmd/wails@latest

$ErrorActionPreference = 'Stop'
Set-Location $PSScriptRoot

# Внешние программы пишут в stderr просто так (npm, wails), и при
# ErrorActionPreference='Stop' PowerShell считает это фатальной ошибкой.
# Поэтому код возврата проверяем явно, временно ослабляя режим.
function Invoke-Step {
    param([string]$Title, [scriptblock]$Body)

    Write-Host "== $Title ==" -ForegroundColor Cyan
    $previous = $ErrorActionPreference
    $ErrorActionPreference = 'Continue'
    $code = 0
    try {
        & $Body
        $code = $LASTEXITCODE
    } finally {
        $ErrorActionPreference = $previous
    }
    if ($code -ne 0) { throw "$Title — код возврата $code" }
}

Invoke-Step 'Тесты' { go test ./... }
Invoke-Step 'Статический анализ' { go vet ./... }

if (-not (Test-Path 'build/appicon.png')) {
    Invoke-Step 'Иконка' { go run tools/genicon.go }
}

Invoke-Step 'Сборка интерфейса' {
    Push-Location frontend
    try {
        npm install --no-audit --no-fund
        if ($LASTEXITCODE -eq 0) { npm run build }
    } finally {
        Pop-Location
    }
}

Invoke-Step 'Проверка интерфейса' { go run tools/uicheck.go }
Invoke-Step 'Сборка приложения' { wails build -clean }

$exe = Join-Path $PSScriptRoot 'build/bin/Focusd.exe'
$size = (Get-Item $exe).Length / 1MB
Write-Host ("Готово: {0} ({1:N1} МБ)" -f $exe, $size) -ForegroundColor Green
