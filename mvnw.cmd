@REM Maven Wrapper for Windows
@echo off

set MAVEN_CMD=mvn

if exist "%USERPROFILE%\.m2\wrapper\dists\apache-maven-3.9.9" (
    for /f "delims=" %%i in ('dir /s /b "%USERPROFILE%\.m2\wrapper\dists\apache-maven-3.9.9\bin\mvn.cmd" 2^>nul') do set MAVEN_CMD=%%i
)

%MAVEN_CMD% %*
