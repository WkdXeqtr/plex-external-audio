; Inno Setup 6 script for Plex External Audio.
;
; Division of labour: Inno lays down the files, makes the Start menu shortcut
; (which is also what gets the program into "Recently added"), and creates the
; entry in Apps and features. Everything that changes the system beyond files -
; swapping the Plex transcoder, the scheduled tasks, the autostart entry - is
; done by setup\configure.ps1, called below. That keeps the system logic as
; readable text instead of burying it inside an executable.
;
; Build:  "<inno>\ISCC.exe" installer\plex-external-audio.iss
; Result: installer\Output\PlexExternalAudio-Setup.exe
;
; This file must stay UTF-8 with a BOM: the CustomMessages below are not ASCII,
; and without the BOM Inno reads them in the system codepage and mangles them.

#define AppName "Plex External Audio"
#define AppVersion "1.0.0"
#define AppPublisher "Plex External Audio"
#define AppURL "https://github.com/"

[Setup]
; A real GUID. The previous one had the product name stuffed into the last
; group, which is not hex - Inno accepted it, but it produced a malformed
; registry key. Changing this value makes Windows treat the program as a
; different product, so never change it again without meaning to.
AppId={{B4E7A2C1-3F58-4D9E-A6B0-7C2D1E8F5A93}
AppName={#AppName}
AppVersion={#AppVersion}
AppVerName={#AppName} {#AppVersion}
AppPublisher={#AppPublisher}
AppPublisherURL={#AppURL}
DefaultDirName={autopf}\Plex External Audio
DefaultGroupName=Plex External Audio
DisableProgramGroupPage=yes
; installed for all users: we need Program Files and an HKLM entry
PrivilegesRequired=admin
; our binaries are 64-bit - install into the real Program Files, not (x86)
ArchitecturesInstallIn64BitMode=x64compatible
ArchitecturesAllowed=x64compatible
OutputDir=Output
OutputBaseFilename=PlexExternalAudio-Setup
Compression=lzma2/max
SolidCompression=yes
WizardStyle=modern
SetupIconFile=icon.ico
UninstallDisplayName={#AppName}
UninstallDisplayIcon={app}\Plex External Audio Tray.exe
VersionInfoVersion={#AppVersion}
VersionInfoProductName={#AppName}
VersionInfoDescription=External audio tracks for Plex

[Languages]
; The tray icon itself speaks one more language than this list: Chinese
; (Simplified). Inno does not ship a translation for it, so the installer falls
; back to English there while the program itself comes up in Chinese.
Name: "en";    MessagesFile: "compiler:Default.isl"
Name: "ru";    MessagesFile: "compiler:Languages\Russian.isl"
Name: "uk";    MessagesFile: "compiler:Languages\Ukrainian.isl"
Name: "de";    MessagesFile: "compiler:Languages\German.isl"
Name: "fr";    MessagesFile: "compiler:Languages\French.isl"
Name: "es";    MessagesFile: "compiler:Languages\Spanish.isl"
Name: "it";    MessagesFile: "compiler:Languages\Italian.isl"
Name: "pl";    MessagesFile: "compiler:Languages\Polish.isl"
Name: "pt";    MessagesFile: "compiler:Languages\BrazilianPortuguese.isl"
Name: "tr";    MessagesFile: "compiler:Languages\Turkish.isl"
Name: "ja";    MessagesFile: "compiler:Languages\Japanese.isl"

[CustomMessages]
en.CleanDbQuestion=Also remove the external audio tracks from the Plex database?%n%nYour audio files on disk are not touched - the tracks will simply stop appearing in the list in Plex.
ru.CleanDbQuestion=Удалить из базы Plex записи о внешних дорожках?%n%nФайлы озвучек на диске не трогаются - дорожки просто исчезнут из списка в Plex.
uk.CleanDbQuestion=Видалити з бази Plex записи про зовнішні доріжки?%n%nФайли озвучення на диску не змінюються - доріжки просто зникнуть зі списку в Plex.
de.CleanDbQuestion=Die externen Tonspuren auch aus der Plex-Datenbank entfernen?%n%nIhre Audiodateien auf der Festplatte bleiben unangetastet - die Spuren verschwinden lediglich aus der Liste in Plex.
fr.CleanDbQuestion=Supprimer également les pistes audio externes de la base de données Plex ?%n%nVos fichiers audio sur le disque ne sont pas touchés : les pistes disparaîtront simplement de la liste dans Plex.
es.CleanDbQuestion=¿Eliminar también las pistas de audio externas de la base de datos de Plex?%n%nLos archivos de audio del disco no se tocan: las pistas simplemente dejarán de aparecer en la lista de Plex.
it.CleanDbQuestion=Rimuovere anche le tracce audio esterne dal database di Plex?%n%nI file audio sul disco non vengono toccati: le tracce spariranno semplicemente dall'elenco in Plex.
pl.CleanDbQuestion=Usunąć również zewnętrzne ścieżki audio z bazy danych Plex?%n%nPliki audio na dysku pozostaną nietknięte - ścieżki znikną jedynie z listy w Plex.
pt.CleanDbQuestion=Remover também as faixas de áudio externas do banco de dados do Plex?%n%nSeus arquivos de áudio no disco não são alterados - as faixas apenas deixarão de aparecer na lista do Plex.
tr.CleanDbQuestion=Harici ses parçaları Plex veritabanından da kaldırılsın mı?%n%nDiskteki ses dosyalarınıza dokunulmaz - parçalar yalnızca Plex'teki listeden kaybolur.
ja.CleanDbQuestion=外部音声トラックを Plex のデータベースからも削除しますか？%n%nディスク上の音声ファイルはそのままです。Plex の一覧に表示されなくなるだけです。

[Files]
Source: "..\bin\Plex External Audio Mapper.exe";     DestDir: "{app}"; Flags: ignoreversion
Source: "..\bin\Plex External Audio Transcoder.exe"; DestDir: "{app}"; Flags: ignoreversion
Source: "..\bin\Plex External Audio Guard.exe";      DestDir: "{app}"; Flags: ignoreversion
Source: "..\bin\Plex External Audio Tray.exe";       DestDir: "{app}"; Flags: ignoreversion

; The management scripts are listed one by one on purpose. A "setup\*.ps1" mask
; used to ship whatever happened to be lying in that directory, including
; one-off debugging scripts with hardcoded local paths in them.
Source: "..\setup\configure.ps1";   DestDir: "{app}\setup"; Flags: ignoreversion
Source: "..\setup\tasks.ps1";       DestDir: "{app}\setup"; Flags: ignoreversion
Source: "..\setup\clean-slate.ps1"; DestDir: "{app}\setup"; Flags: ignoreversion
Source: "..\setup\recover.ps1";     DestDir: "{app}\setup"; Flags: ignoreversion

Source: "..\README.md"; DestDir: "{app}"; Flags: ignoreversion skipifsourcedoesntexist
Source: "icon.ico";     DestDir: "{app}"; Flags: ignoreversion

[Icons]
; One shortcut in the Start menu - it is also what puts the program into
; "Recently added".
Name: "{group}\Plex External Audio"; Filename: "{app}\Plex External Audio Tray.exe"; \
      IconFilename: "{app}\Plex External Audio Tray.exe"; \
      Comment: "External audio tracks for Plex"

[Run]
; With rights already elevated: configure the system.
Filename: "powershell.exe"; \
  Parameters: "-NoProfile -ExecutionPolicy Bypass -WindowStyle Hidden -File ""{app}\setup\configure.ps1"" -Dest ""{app}"""; \
  StatusMsg: "Swapping the transcoder, registering tasks, setting up autostart..."; \
  Flags: runhidden waituntilterminated

[UninstallDelete]
Type: filesandordirs; Name: "{app}\ffmpeg"
Type: files;          Name: "{app}\config.json"
Type: files;          Name: "{app}\guard-early.log"

[Code]
// Undoing the system configuration is driven from here rather than from an
// [UninstallRun] entry, because Inno expands the parameters of [UninstallRun]
// at INSTALL time, when it records them into the uninstall log. The user's
// answer about the database is only known at uninstall time, so it could never
// have reached the script that way.
procedure CurUninstallStepChanged(CurUninstallStep: TUninstallStep);
var
  Params: String;
  ResultCode: Integer;
begin
  if CurUninstallStep = usUninstall then
  begin
    Params := '-NoProfile -ExecutionPolicy Bypass -WindowStyle Hidden -File "'
            + ExpandConstant('{app}\setup\configure.ps1') + '" -Dest "'
            + ExpandConstant('{app}') + '" -Uninstall';

    // Removing the rows is not something to decide for the user: the tracks are
    // the whole point of the program, and re-creating them means another full
    // library pass. The audio files themselves are never touched either way.
    if MsgBox(CustomMessage('CleanDbQuestion'), mbConfirmation, MB_YESNO) = IDYES then
      Params := Params + ' -CleanDb';

    Exec('powershell.exe', Params, '', SW_HIDE, ewWaitUntilTerminated, ResultCode);
  end;
end;
