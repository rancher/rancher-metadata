Get-NetAdapter | New-NetIPAddress -IPAddress 169.254.169.250 -PrefixLength 32
Start-Sleep 5
C:\rancher-metadata.exe $args
