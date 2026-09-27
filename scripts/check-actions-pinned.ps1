$ErrorActionPreference = 'Stop'
$bad = Select-String -Path '.github/workflows/*.yml' -Pattern 'uses:\s+[^\s]+@(?![0-9a-f]{40}(?:\s|$))'
if ($bad) {
  $bad | ForEach-Object { Write-Error $_.Line }
}
