' Every address this program can send a user to, in one place.
'
' Two copies of a URL is one copy to get wrong, and a stale link in a program is indistinguishable
' from a broken product. The addresses are the published ones for this project and the author's
' portfolio hub.
'
' Nothing here is opened by the program on its own: a link is followed because a person clicked it
' (SP-0006 section 14, "no network connection, no phoning home").
Imports System.Diagnostics

Module Links
    Public Const FMSInstall As String = "https://serzhyale.github.io/FastMediaSorter_Lite/"

    Public Const AuthorName As String = "Serhii Zhyhunenko (sza)"
    Public Const AuthorEmail As String = LogReport.AuthorEmail

    Public Const Site As String = "https://serzhyale.github.io/FileDO/"
    Public Const GitHub As String = "https://github.com/SerZhyAle/FileDO"
    Public Const Issues As String = "https://github.com/SerZhyAle/FileDO/issues"
    Public Const Privacy As String = "https://serzhyale.github.io/FileDO/privacy.html"
    ' The guides of the site, and the one about virtual disks - what the Disk manager's help sends a
    ' user to (SP-0063). The site chooses its own language from the browser's.
    Public Const Guides As String = "https://serzhyale.github.io/FileDO/guides/"
    Public Const DiskGuide As String = "https://serzhyale.github.io/FileDO/guides/virtual-disks.html"
    ' The page about the Fast Media Sorter & Sharing integration - what the Disk manager's help
    ' sends a user to for setup, every dialog and recovery (the DISK-SHARE surfaces).
    Public Const ShareGuide As String = "https://serzhyale.github.io/FileDO/guides/fms-sharing.html"
    Public Const Readme As String = "https://github.com/SerZhyAle/FileDO#readme"
    Public Const Portfolio As String = "https://sza.od.ua"

    ' The rest of the About set (SETTINGS-NAVIGATION 2.0 section 3.3, SP-0169 S3): the licence's own
    ' text, where every version is downloaded and what changed in it, the site's subject index of
    ' every topic and resource, and the page that says how to get help.
    Public Const License As String = "https://github.com/SerZhyAle/FileDO/blob/main/LICENSE"
    Public Const Releases As String = "https://github.com/SerZhyAle/FileDO/releases"
    Public Const Topics As String = "https://serzhyale.github.io/FileDO/guides/topics.html"
    Public Const Support As String = "https://serzhyale.github.io/FileDO/guides/support.html"

    ' The related programs, one inventory (section 3.4): the address, the localized name and the
    ' localized purpose line. The help pages' "Related" list names the same program by the same keys.
    Public ReadOnly Related As String()() = {
        New String() {FMSInstall, "help_rel_fms_title", "help_rel_fms_note"}
    }

    ' A link that cannot be opened says so with the address in front of the user and a button that
    ' copies it, because an address they can paste into a browser themselves is the way round a
    ' machine with no default browser or mail program (APP-BEHAVIOUR rule 6: a cause and an action,
    ' never the exception's text).
    Public Sub Open(owner As IWin32Window, target As String)
        Try
            Process.Start(New ProcessStartInfo() With {.FileName = target, .UseShellExecute = True})
        Catch ex As Exception
            ShellLog.Write("open link " & target, ex)
            If ShellDialog.Problem(owner, Localization.Format(Localization.T("shell_link_failed"), target, Problems.Cause(ex)),
                                   Localization.T("shell_btn_copy_address"), offerLogs:=True,
                                   actionGlyph:=GlyphRef.Vocabulary("action.copy")) Then
                Ui.CopyText(owner, target)
            End If
        End Try
    End Sub

End Module
