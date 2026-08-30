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
; The release workflow passes /DAppVersion=<tag>; this is only the fallback for
; a build started by hand.
#ifndef AppVersion
  #define AppVersion "1.0.0"
#endif
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

; Inno closes running programs through Restart Manager, which finds them by
; broadcasting to top-level windows. Our tray icon does not have one: it owns a
; message-only window, and message-only windows are deliberately excluded from
; those broadcasts. So the offer to "close the applications automatically"
; could never work, and the user was left staring at a dialog that failed no
; matter which button was pressed. We shut the programs down ourselves in
; PrepareToInstall instead, where we can simply end the processes by name.
CloseApplications=no
RestartApplications=no

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
en.FfmpegDownloadFailed=ffmpeg could not be downloaded, so installation will carry on without it.%n%nThe program needs ffprobe to read your audio files. Install ffmpeg yourself, then put the path to ffprobe.exe into config.json in the program folder.
ru.FfmpegDownloadFailed=Не удалось скачать ffmpeg, установка продолжится без него.%n%nБез ffprobe программа не сможет читать звуковые файлы. Установите ffmpeg сами и впишите путь к ffprobe.exe в config.json в папке программы.
uk.FfmpegDownloadFailed=Не вдалося завантажити ffmpeg, встановлення триватиме без нього.%n%nБез ffprobe програма не зможе читати звукові файли. Установіть ffmpeg самотужки та впишіть шлях до ffprobe.exe у config.json у теці програми.
de.FfmpegDownloadFailed=ffmpeg konnte nicht heruntergeladen werden, die Installation wird ohne fortgesetzt.%n%nOhne ffprobe kann das Programm Ihre Audiodateien nicht lesen. Installieren Sie ffmpeg selbst und tragen Sie den Pfad zu ffprobe.exe in die config.json im Programmordner ein.
fr.FfmpegDownloadFailed=Le téléchargement de ffmpeg a échoué, l'installation continuera sans lui.%n%nSans ffprobe, le programme ne peut pas lire vos fichiers audio. Installez ffmpeg vous-même, puis indiquez le chemin de ffprobe.exe dans config.json, dans le dossier du programme.
es.FfmpegDownloadFailed=No se pudo descargar ffmpeg; la instalación continuará sin él.%n%nSin ffprobe el programa no puede leer sus archivos de audio. Instale ffmpeg usted mismo y escriba la ruta de ffprobe.exe en config.json, en la carpeta del programa.
it.FfmpegDownloadFailed=Non è stato possibile scaricare ffmpeg, l'installazione proseguirà senza.%n%nSenza ffprobe il programma non può leggere i file audio. Installa ffmpeg manualmente e indica il percorso di ffprobe.exe in config.json nella cartella del programma.
pl.FfmpegDownloadFailed=Nie udało się pobrać ffmpeg, instalacja będzie kontynuowana bez niego.%n%nBez ffprobe program nie odczyta plików dźwiękowych. Zainstaluj ffmpeg samodzielnie i wpisz ścieżkę do ffprobe.exe w config.json w folderze programu.
pt.FfmpegDownloadFailed=Não foi possível baixar o ffmpeg; a instalação continuará sem ele.%n%nSem o ffprobe o programa não consegue ler seus arquivos de áudio. Instale o ffmpeg por conta própria e informe o caminho do ffprobe.exe no config.json, na pasta do programa.
tr.FfmpegDownloadFailed=ffmpeg indirilemedi, kurulum onsuz devam edecek.%n%nffprobe olmadan program ses dosyalarınızı okuyamaz. ffmpeg'i kendiniz kurun ve ffprobe.exe yolunu program klasöründeki config.json dosyasına yazın.
ja.FfmpegDownloadFailed=ffmpeg をダウンロードできませんでした。インストールはこのまま続行します。%n%nffprobe がないと音声ファイルを読み取れません。ffmpeg をご自身でインストールし、プログラムフォルダーの config.json に ffprobe.exe のパスを記入してください。

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
Source: "icon.ico";       DestDir: "{app}"; Flags: ignoreversion
; The notification centre reads its header icon from a plain bitmap, not from an
; icon container, so the PNG ships alongside the .ico rather than instead of it.
Source: "..\docs\icon.png"; DestDir: "{app}"; Flags: ignoreversion

[Icons]
; One shortcut in the Start menu - it is also what puts the program into
; "Recently added".
Name: "{group}\Plex External Audio"; Filename: "{app}\Plex External Audio Tray.exe"; \
      IconFilename: "{app}\Plex External Audio Tray.exe"; \
      Comment: "External audio tracks for Plex"

[Run]
; With rights already elevated: configure the system.
Filename: "powershell.exe"; \
  Parameters: "-NoProfile -ExecutionPolicy Bypass -WindowStyle Hidden -File ""{app}\setup\configure.ps1"" -Dest ""{app}""{code:FfmpegZipArg}"; \
  StatusMsg: "Swapping the transcoder, registering tasks, setting up autostart..."; \
  Flags: runhidden waituntilterminated

[UninstallDelete]
Type: filesandordirs; Name: "{app}\ffmpeg"
Type: files;          Name: "{app}\config.json"
Type: files;          Name: "{app}\guard-early.log"

[Code]

var
  DownloadPage: TDownloadWizardPage;
  FfmpegZip: String;

// ffprobe is not optional: without it nothing can read the audio files. Rather
// than tell people to go and install ffmpeg first, fetch it here.
//
// It is downloaded rather than bundled on purpose. The published Windows builds
// of ffmpeg are GPL, and shipping one inside an MIT installer would drag that
// licence over this project. Downloading leaves the user getting ffmpeg from
// its own publisher, with this installer only doing the fetching.
// Two sources, tried in order. Neither is a mirror of the other, they are just
// the two best known Windows builds - and having a second one matters: on the
// machine this was developed on, Kaspersky refuses the gyan.dev download
// outright with HTTP 499 and a signature match, while the GitHub-hosted one
// goes through untouched.
const
  FfmpegUrl1 = 'https://github.com/BtbN/FFmpeg-Builds/releases/download/latest/ffmpeg-master-latest-win64-gpl.zip';
  FfmpegUrl2 = 'https://www.gyan.dev/ffmpeg/builds/ffmpeg-release-essentials.zip';

// TryDownload fetches one URL and says whether it worked.
function TryDownload(Url: String): Boolean;
begin
  DownloadPage.Clear;
  DownloadPage.Add(Url, 'ffmpeg.zip', '');
  try
    DownloadPage.Download;
    Result := True;
  except
    Result := False;
  end;
end;

// HaveFfprobe reports whether the machine already has one.
//
// Only presence is checked here; whether it is recent enough is decided later by
// configure.ps1, which can actually run it and read the version.
function HaveFfprobe(): Boolean;
var
  ResultCode: Integer;
begin
  // on PATH
  if Exec(ExpandConstant('{sys}\where.exe'), 'ffprobe.exe', '', SW_HIDE,
          ewWaitUntilTerminated, ResultCode) and (ResultCode = 0) then
  begin
    Result := True;
    exit;
  end;
  // the usual places people unpack it to
  Result := FileExists(ExpandConstant('{pf}\ffmpeg\bin\ffprobe.exe'))
         or FileExists('C:\ffmpeg\bin\ffprobe.exe')
         or FileExists('C:\ProgramData\chocolatey\bin\ffprobe.exe')
         or FileExists(ExpandConstant('{app}\ffmpeg\ffprobe.exe'));
end;

procedure InitializeWizard();
begin
  DownloadPage := CreateDownloadPage(
    SetupMessage(msgWizardPreparing), SetupMessage(msgPreparingDesc), nil);
end;

function NextButtonClick(CurPageID: Integer): Boolean;
begin
  Result := True;
  if CurPageID <> wpReady then exit;
  if HaveFfprobe() then exit;

  // A failed download must not stop the install. Plenty of things get in the
  // way - no connection, a corporate proxy, or an antivirus that blocks the
  // file outright, which is not hypothetical: one refused it with HTTP 499
  // during testing. Everything else still installs, and configure.ps1 says
  // plainly what is missing and where to point it.
  DownloadPage.Show;
  try
    if TryDownload(FfmpegUrl1) or TryDownload(FfmpegUrl2) then
      FfmpegZip := ExpandConstant('{tmp}\ffmpeg.zip')
    else
    begin
      FfmpegZip := '';
      SuppressibleMsgBox(CustomMessage('FfmpegDownloadFailed'),
        mbInformation, MB_OK, IDOK);
    end;
  finally
    DownloadPage.Hide;
  end;
end;

// FfmpegZipArg hands the downloaded archive to configure.ps1, which pulls the
// single file we need out of it. Extracting the whole thing would put close to
// two hundred megabytes on disk for the sake of one executable.
function FfmpegZipArg(Param: String): String;
begin
  if FfmpegZip <> '' then
    Result := ' -FfmpegZip "' + FfmpegZip + '"'
  else
    Result := '';
end;

// stopOurs ends one of our programs by image name, if it is running.
//
// taskkill rather than Restart Manager: see the note on CloseApplications above.
// /T takes the children with it - the tray can have a mapper running under it,
// and that mapper holds the Plex database open.
procedure stopOurs(const ExeName: String);
var
  ResultCode: Integer;
begin
  Exec(ExpandConstant('{sys}\taskkill.exe'),
       '/F /T /IM "' + ExeName + '"', '', SW_HIDE, ewWaitUntilTerminated, ResultCode);
end;

// PrepareToInstall runs before a single file is copied, which is exactly when
// the running copy has to be out of the way: Windows will not let us overwrite
// an executable that is still running, and the failure surfaces as an
// unexplained "cannot create file" halfway through the install.
function PrepareToInstall(var NeedsRestart: Boolean): String;
begin
  Result := '';

  stopOurs('Plex External Audio Tray.exe');
  stopOurs('Plex External Audio Guard.exe');
  stopOurs('Plex External Audio Mapper.exe');
  // Older installs carried different names; an upgrade from one of those would
  // otherwise leave its tray icon running and holding the directory.
  stopOurs('pca-tray.exe');
  stopOurs('pca-guard.exe');

  // Ending a process is asynchronous - the handles it holds are released a
  // moment later, and copying over the file too early fails for no visible
  // reason.
  Sleep(800);
end;

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
