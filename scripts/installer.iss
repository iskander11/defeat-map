; Inno Setup script for "Карта поражений" (defeat-map).
; Build the app first (scripts/build.ps1), then compile this with ISCC.exe.
; Produces dist-installer\DefeatMapSetup.exe

#define MyAppName "Карта поражений"
#define MyAppVersion "1.0.0"
#define MyAppPublisher "iskander11"
#define MyAppExeName "defeatmap.exe"
#define MyAppURL "https://github.com/iskander11/defeat-map"

[Setup]
AppId={{8F2C7B9E-5A1D-4E3F-9C6B-2D4A1E7F6B3A}
AppName={#MyAppName}
AppVersion={#MyAppVersion}
AppPublisher={#MyAppPublisher}
AppPublisherURL={#MyAppURL}
AppSupportURL={#MyAppURL}
DefaultDirName={autopf}\DefeatMap
DefaultGroupName={#MyAppName}
DisableProgramGroupPage=yes
PrivilegesRequired=lowest
OutputDir=..\dist-installer
OutputBaseFilename=DefeatMapSetup
SetupIconFile=..\assets\icon\icon.ico
UninstallDisplayIcon={app}\{#MyAppExeName}
Compression=lzma2
SolidCompression=yes
WizardStyle=modern
ArchitecturesInstallIn64BitMode=x64compatible
LicenseFile=
DisableWelcomePage=no

[Languages]
Name: "russian"; MessagesFile: "compiler:Languages\Russian.isl"
Name: "english"; MessagesFile: "compiler:Default.isl"

[Tasks]
Name: "desktopicon"; Description: "{cm:CreateDesktopIcon}"; GroupDescription: "{cm:AdditionalIcons}"

[Files]
Source: "..\dist\defeatmap.exe"; DestDir: "{app}"; Flags: ignoreversion
Source: "..\dist\assets\*"; DestDir: "{app}\assets"; Flags: ignoreversion recursesubdirs createallsubdirs

[Icons]
Name: "{group}\{#MyAppName}"; Filename: "{app}\{#MyAppExeName}"
Name: "{group}\{cm:UninstallProgram,{#MyAppName}}"; Filename: "{uninstallexe}"
Name: "{autodesktop}\{#MyAppName}"; Filename: "{app}\{#MyAppExeName}"; Tasks: desktopicon

[Run]
Filename: "{app}\{#MyAppExeName}"; Description: "{cm:LaunchProgram,{#MyAppName}}"; Flags: nowait postinstall skipifsilent

[UninstallDelete]
; User data (incidents, tile cache) lives under %LOCALAPPDATA%\DefeatMap and
; is intentionally left in place on uninstall so re-installing doesn't lose
; logged incidents. Remove it yourself if you want a full clean wipe.
