param(
    [Parameter(Mandatory = $true)]
    [ValidateSet('Add', 'Remove')]
    [string]$Action
)

# An in-app upgrade keeps the uninstaller from the original installation.
$env:LAZYGITGO_INSTALL_DIR = $env:TANGIT_INSTALL_DIR
& (Join-Path $PSScriptRoot 'lazygitgo-path.ps1') -Action $Action
