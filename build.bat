@echo off
REM Compila la app para Windows (necesita Go instalado: https://go.dev/dl/)
setlocal
echo Compilando conexion-remota.exe ...
set GOOS=windows
set GOARCH=amd64
go build -ldflags "-s -w" -o conexion-remota.exe .
if %ERRORLEVEL% NEQ 0 (
  echo.
  echo ERROR al compilar. Revisa que Go este instalado.
  pause
  exit /b 1
)
echo.
echo Listo: conexion-remota.exe
pause
