import { createHash } from "node:crypto";
import { mkdir, readFile, writeFile } from "node:fs/promises";
import { join } from "node:path";
import { root } from "./package-lib.mjs";

const source =
  "https://raw.githubusercontent.com/tauri-apps/tauri/tauri-cli-v2.12.4/crates/tauri-bundler/src/bundle/windows/nsis/installer.nsi";
const sha256 = "d00089d3b1a2c4a30d7b6e098079100cb8f25e980f84d4515aca4597b95e967d";
export const installerTemplate = { version: "2.12.4", source, sha256, license: "MIT" };

// NSIS expressions use @{...} here so JavaScript does not interpolate them.
const nsis = (value) => value.replaceAll("@{", "${");
const hooks = nsis(String.raw`
Var DbmcpOldDirectory
Var DbmcpMigration
Var DbmcpLog
Var DbmcpMarker

Function DbmcpLogMigration
  Push $R0
  Push $R1
  Push $R2
  Push $R3
  Push $R4
  Push $R5
  Push $R6
  Push $R7
  Push $R8
  Push $R9
  ClearErrors
  CreateDirectory "$LOCALAPPDATA\@{BUNDLEID}\logs"
  FileOpen $DbmcpLog "$LOCALAPPDATA\@{BUNDLEID}\logs\installer-migration.log" a
  IfErrors dbmcp_log_done
  FileSeek $DbmcpLog 0 END $R6
  @{If} $R6 > 1048576
    FileClose $DbmcpLog
    FileOpen $DbmcpLog "$LOCALAPPDATA\@{BUNDLEID}\logs\installer-migration.log" w
    IfErrors dbmcp_log_done
  @{EndIf}
  ; FileWrite uses the active ANSI code page. Write UTF-8 through Win32 so
  ; diagnostic exports can also read paths containing non-ASCII characters.
  nsis_tauri_utils::StrReplace $DbmcpMarker '\' '\\'
  Pop $R8
  nsis_tauri_utils::StrReplace $R8 '$\"' '\$\"'
  Pop $R8
  @{GetTime} "" "LS" $R0 $R1 $R2 $R3 $R4 $R5 $R6
  StrCpy $R8 '{$\"component$\":$\"installer$\",$\"version$\":$\"@{VERSION}$\",$\"time_utc$\":$\"$R2-$R1-$R0T$R4:$R5:$R6Z$\",$\"msg$\":$\"$R8$\"}$\r$\n'
  System::Call 'kernel32::WideCharToMultiByte(i 65001, i 0, w R8, i -1, p 0, i 0, p 0, p 0) i.R9'
  System::Alloc $R9
  Pop $R7
  System::Call 'kernel32::WideCharToMultiByte(i 65001, i 0, w R8, i -1, p R7, i R9, p 0, p 0)'
  IntOp $R9 $R9 - 1
  System::Call 'kernel32::WriteFile(p $DbmcpLog, p R7, i R9, *i.R6, p 0)'
  System::Free $R7
  FileClose $DbmcpLog
  dbmcp_log_done:
  DetailPrint "$DbmcpMarker"
  Pop $R9
  Pop $R8
  Pop $R7
  Pop $R6
  Pop $R5
  Pop $R4
  Pop $R3
  Pop $R2
  Pop $R1
  Pop $R0
FunctionEnd

Function DbmcpMigrationError
  Call DbmcpLogMigration
  SetErrorLevel 2
  Abort "$DbmcpMarker"
FunctionEnd

Function DbmcpInitializeDirectory
  ReadRegStr $DbmcpOldDirectory SHCTX "@{MANUPRODUCTKEY}" ""
  @{GetFileName} $INSTDIR $0
  @{If} $0 == "DB MCP Tools"
  @{AndIf} $INSTDIR == $DbmcpOldDirectory
    @{GetParent} $INSTDIR $0
    StrCpy $INSTDIR "$0\db-mcp-tools"
  @{EndIf}
FunctionEnd

!macro NSIS_HOOK_PREINSTALL
  ReadRegStr $DbmcpOldDirectory SHCTX "@{MANUPRODUCTKEY}" ""
  GetFullPathName $INSTDIR $INSTDIR
  ReadRegStr $0 SHCTX "@{UNINSTKEY}" "UninstallString"
  @{If} $0 == ""
  @{AndIfNot} @{FileExists} "$DbmcpOldDirectory\@{MAINBINARYNAME}.exe"
    StrCpy $DbmcpOldDirectory ""
  @{EndIf}
  @{If} $DbmcpOldDirectory != ""
  @{AndIf} $DbmcpOldDirectory != $INSTDIR
    ; Only a registered NSIS installation is eligible. Never delete a tree.
    ReadRegStr $0 SHCTX "@{UNINSTKEY}" "UninstallString"
    @{If} $0 != '$\"$DbmcpOldDirectory\uninstall.exe$\"'
    @{OrIfNot} @{FileExists} "$DbmcpOldDirectory\uninstall.exe"
      StrCpy $DbmcpMarker "@{VERSION}: migration rejected; missing or invalid registered uninstaller; $DbmcpOldDirectory -> $INSTDIR"
      Call DbmcpMigrationError
    @{EndIf}
    ; Source and destination must not contain each other.
    StrLen $0 "$DbmcpOldDirectory\"
    StrCpy $1 "$INSTDIR\" $0
    @{If} $1 == "$DbmcpOldDirectory\"
      StrCpy $DbmcpMarker "@{VERSION}: migration rejected; destination inside source; $DbmcpOldDirectory -> $INSTDIR"
      Call DbmcpMigrationError
    @{EndIf}
    StrLen $0 "$INSTDIR\"
    StrCpy $1 "$DbmcpOldDirectory\" $0
    @{If} $1 == "$INSTDIR\"
      StrCpy $DbmcpMarker "@{VERSION}: migration rejected; source inside destination; $DbmcpOldDirectory -> $INSTDIR"
      Call DbmcpMigrationError
    @{EndIf}
    ; Retry an interrupted copy only when this installer owns the staging marker.
    StrCpy $1 ""
    ClearErrors
    FileOpen $0 "$INSTDIR\.dbmcp-install-migration" r
    FileReadUTF16LE $0 $1
    FileClose $0
    @{If} $1 != $DbmcpOldDirectory
      FindFirst $0 $1 "$INSTDIR\*"
      dbmcp_check_directory:
        @{If} $1 != ""
        @{AndIf} $1 != "."
        @{AndIf} $1 != ".."
          FindClose $0
          StrCpy $DbmcpMarker "@{VERSION}: migration rejected; destination is not empty; $DbmcpOldDirectory -> $INSTDIR"
          Call DbmcpMigrationError
        @{EndIf}
        ClearErrors
        FindNext $0 $1
        IfErrors dbmcp_directory_empty
        Goto dbmcp_check_directory
      dbmcp_directory_empty:
      FindClose $0
    @{EndIf}
    ClearErrors
    FileOpen $0 "$INSTDIR\.dbmcp-install-migration" w
    FileWriteUTF16LE $0 $DbmcpOldDirectory
    FileClose $0
    @{If} @{Errors}
      StrCpy $DbmcpMarker "@{VERSION}: cannot write migration marker; $INSTDIR"
      Call DbmcpMigrationError
    @{EndIf}
    StrCpy $DbmcpMigration 1
    StrCpy $DbmcpMarker "@{VERSION}: preparing $DbmcpOldDirectory -> $INSTDIR"
    Call DbmcpLogMigration
    !insertmacro CheckIfAppIsRunning "$DbmcpOldDirectory\@{MAINBINARYNAME}.exe" "@{PRODUCTNAME}"
  @{EndIf}
!macroend

Function DbmcpRemovePreviousInstallation
  @{If} $DbmcpMigration != 1
    Return
  @{EndIf}
  ; Run the original file manifest after the new payload has been extracted.
  ; /UPDATE plus silent mode preserves profiles, WebView data, Run and shortcuts.
  ClearErrors
  ExecWait '$\"$DbmcpOldDirectory\uninstall.exe$\" /S /UPDATE _?=$DbmcpOldDirectory' $0
  @{If} @{Errors}
    StrCpy $0 2
  @{EndIf}
  StrCpy $DbmcpMarker "@{VERSION}: previous uninstaller exit=$0; $DbmcpOldDirectory -> $INSTDIR"
  Call DbmcpLogMigration
  @{If} $0 != 0
  @{OrIf} @{FileExists} "$DbmcpOldDirectory\@{MAINBINARYNAME}.exe"
    StrCpy $DbmcpMarker "@{VERSION}: previous installation removal failed; data retained; retry installer; $DbmcpOldDirectory -> $INSTDIR"
    Call DbmcpMigrationError
  @{EndIf}
  Delete "$DbmcpOldDirectory\uninstall.exe"
  RMDir "$DbmcpOldDirectory"
FunctionEnd

!macro DbmcpRetargetShortcut shortcut
  !insertmacro IsShortcutTarget "@{shortcut}" "$DbmcpOldDirectory\@{MAINBINARYNAME}.exe"
  Pop $0
  @{If} $0 = 1
    !insertmacro SetShortcutTarget "@{shortcut}" "$INSTDIR\@{MAINBINARYNAME}.exe"
  @{EndIf}
!macroend

!macro NSIS_HOOK_POSTINSTALL
  @{If} $DbmcpMigration = 1
    !insertmacro DbmcpRetargetShortcut "$SMPROGRAMS\$AppStartMenuFolder\@{PRODUCTNAME}.lnk"
    !insertmacro DbmcpRetargetShortcut "$SMPROGRAMS\@{PRODUCTNAME}.lnk"
    !insertmacro DbmcpRetargetShortcut "$DESKTOP\@{PRODUCTNAME}.lnk"
    ; The plugin's app name has used both product and package names. Change only
    ; values which actually launch this installation, retaining all arguments.
    !insertmacro DbmcpRetargetAutostart "@{PRODUCTNAME}"
    !insertmacro DbmcpRetargetAutostart "@{MAINBINARYNAME}"
    Delete "$INSTDIR\.dbmcp-install-migration"
    StrCpy $DbmcpMarker "@{VERSION}: migration completed; $INSTDIR"
    Call DbmcpLogMigration
  @{EndIf}
  Push $UpdateMode
  StrCpy $UpdateMode 0
  !insertmacro MUI_STARTMENU_WRITE_BEGIN Application
    Call CreateOrUpdateStartMenuShortcut
  !insertmacro MUI_STARTMENU_WRITE_END
  Pop $UpdateMode
!macroend

!macro DbmcpRetargetAutostart name
  ReadRegStr $0 HKCU "Software\Microsoft\Windows\CurrentVersion\Run" "@{name}"
  StrLen $1 '$\"$DbmcpOldDirectory\@{MAINBINARYNAME}.exe$\"'
  StrCpy $2 $0 $1
  @{If} $2 == '$\"$DbmcpOldDirectory\@{MAINBINARYNAME}.exe$\"'
    StrCpy $2 $0 "" $1
    WriteRegStr HKCU "Software\Microsoft\Windows\CurrentVersion\Run" "@{name}" '$\"$INSTDIR\@{MAINBINARYNAME}.exe$\"$2'
  @{Else}
    StrLen $1 "$DbmcpOldDirectory\@{MAINBINARYNAME}.exe"
    StrCpy $2 $0 $1
    StrCpy $3 $0 1 $1
    @{If} $2 == "$DbmcpOldDirectory\@{MAINBINARYNAME}.exe"
    @{AndIf} $3 == " "
    @{OrIf} $0 == "$DbmcpOldDirectory\@{MAINBINARYNAME}.exe"
      StrCpy $2 $0 "" $1
      WriteRegStr HKCU "Software\Microsoft\Windows\CurrentVersion\Run" "@{name}" '$\"$INSTDIR\@{MAINBINARYNAME}.exe$\"$2'
    @{EndIf}
  @{EndIf}
!macroend
`);

export async function windowsInstaller(dir) {
  const cache = join(root, ".cache/build-tools/tauri-cli-2.12.4-installer.nsi");
  await mkdir(join(root, ".cache/build-tools"), { recursive: true });
  let template = await readFile(cache).catch(() => null);
  const digest = (bytes) => createHash("sha256").update(bytes).digest("hex");
  if (!template || digest(template) !== sha256) {
    const response = await fetch(source, { signal: AbortSignal.timeout(60000) });
    if (!response.ok) throw new Error(`Pinned NSIS template download failed: ${response.status}`);
    template = Buffer.from(await response.arrayBuffer());
    if (digest(template) !== sha256) throw new Error("Pinned NSIS template checksum mismatch");
    await writeFile(cache, template);
  }
  let content = template.toString("utf8");
  const replace = (before, after) => {
    if (content.split(before).length !== 2) throw new Error("Upstream NSIS template changed");
    content = content.replace(before, after);
  };
  // Our hook declares functions, so load it after product definitions, plugin
  // paths and shared variables, rather than the upstream macro-only position.
  const include = '{{#if installer_hooks}}\n!include "{{installer_hooks}}"\n{{/if}}\n';
  replace(include, "");
  replace("Function .onInit\n", `${include}\nFunction .onInit\n`);
  replace('"placeholder\\${PRODUCTNAME}"', '"placeholder\\db-mcp-tools"');
  replace('"$LOCALAPPDATA\\${PRODUCTNAME}"', '"$LOCALAPPDATA\\db-mcp-tools"');
  replace(
    "    Call RestorePreviousInstallLocation",
    "    Call RestorePreviousInstallLocation\n    Call DbmcpInitializeDirectory",
  );
  // Migration uses the old uninstaller once, with data preservation enforced.
  replace(
    "Function PageReinstall\n",
    nsis(String.raw`Function PageReinstall
  ReadRegStr $0 SHCTX "@{MANUPRODUCTKEY}" ""
  @{If} $0 != ""
  @{AndIf} $0 != $INSTDIR
    Abort
  @{EndIf}
`),
  );
  replace(
    "Function PageLeaveReinstall\n",
    nsis(String.raw`Function PageLeaveReinstall
  ReadRegStr $0 SHCTX "@{MANUPRODUCTKEY}" ""
  @{If} $0 != ""
  @{AndIf} $0 != $INSTDIR
    Return
  @{EndIf}
`),
  );
  replace(
    "  ; Create uninstaller\n",
    "  Call DbmcpRemovePreviousInstallation\n\n  ; Create uninstaller\n",
  );
  const path = join(dir, "installer.nsi");
  const hookPath = join(dir, "installer-hooks.nsh");
  await writeFile(path, content);
  await writeFile(hookPath, hooks);
  return { template: path, installerHooks: hookPath };
}
