param(
    [Parameter(Mandatory = $true)]
    [ValidateSet('Add', 'Remove')]
    [string]$Action
)

$installDir = $env:TANGIT_INSTALL_DIR
if ([string]::IsNullOrWhiteSpace($installDir)) {
    throw 'TANGIT_INSTALL_DIR is not set'
}
$installDir = $installDir.TrimEnd('\')

$path = [Environment]::GetEnvironmentVariable('Path', 'User')
$parts = @(
    $path -split ';' | Where-Object {
        $_ -and $_.TrimEnd('\') -ine $installDir
    }
)
if ($Action -eq 'Add') {
    $parts += $installDir
}

[Environment]::SetEnvironmentVariable('Path', ($parts -join ';'), 'User')
