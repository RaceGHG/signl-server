@echo off
rem SIGNL Download-Server starten (Fenster offen lassen).
rem Ordner für die Filme ändern: set SIGNL_DATA=D:\Medien\SIGNL
cd /d "%~dp0"
signl-server.exe
pause
