@echo off
rem Startet den Server bei jeder Anmeldung unsichtbar im Hintergrund.
cd /d "%~dp0"
schtasks /Create /TN "SIGNL Download-Server" /TR "\"%~dp0signl-server.exe\"" /SC ONLOGON /RL LIMITED /F
echo.
echo Eingerichtet. Zum Entfernen: schtasks /Delete /TN "SIGNL Download-Server" /F
pause
