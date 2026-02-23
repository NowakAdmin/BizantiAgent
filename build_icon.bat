@echo off
cd /d "%~dp0"
echo Building app.ico from bizanti_logo.png...
"C:\Program Files\ImageMagick-7.1.2-Q16-HDRI\magick.exe" assets\bizanti_logo.png -define icon:auto-resize=256,128,96,64,48,32,16 assets\app.ico
if exist assets\app.ico (
    echo ✓ app.ico created successfully
    for %%F in (assets\app.ico) do echo Size: %%~zF bytes
) else (
    echo ✗ Failed to create app.ico
    exit /b 1
)
