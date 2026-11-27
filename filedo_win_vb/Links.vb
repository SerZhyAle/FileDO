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

    ' The guide page or anchored guide section of each function a published guide covers, by the rail
    ' key of the page where the person sets the function up and starts it (SITE-STRUCTURE 0.5 section
    ' 10 item A, FILEDO-0247). The shell's header links it while that page is on screen. A rail page
    ' that is a place rather than a function (History, Settings, About) or a function no guide covers
    ' (Recover a drive) has no row and shows no link. Every address here is the English page, written
    ' out in full so packaging/check-external-docs.ps1 (step addresses) finds it and checks it answers;
    ' GuideAddress below turns it into the page of the UI's language.
    Public ReadOnly GuideByRail As New Dictionary(Of String, String)(StringComparer.Ordinal) From {
        {"rail_job_capacity", "https://serzhyale.github.io/FileDO/guides/check-storage.html"},
        {"rail_job_speed", "https://serzhyale.github.io/FileDO/guides/check-storage.html"},
        {"rail_job_info", "https://serzhyale.github.io/FileDO/guides/check-storage.html"},
        {"rail_job_damaged", "https://serzhyale.github.io/FileDO/guides/check-storage.html"},
        {"rail_job_probe", "https://serzhyale.github.io/FileDO/guides/check-storage.html"},
        {"rail_job_duplicates", "https://serzhyale.github.io/FileDO/guides/files-and-copies.html"},
        {"rail_job_compare", "https://serzhyale.github.io/FileDO/guides/files-and-copies.html"},
        {"rail_job_copy", "https://serzhyale.github.io/FileDO/guides/files-and-copies.html"},
        {"rail_job_clean", "https://serzhyale.github.io/FileDO/guides/erase.html"},
        {"rail_job_fill", "https://serzhyale.github.io/FileDO/guides/erase.html"},
        {"rail_job_wipe", "https://serzhyale.github.io/FileDO/guides/erase.html"},
        {"rail_job_secure", "https://serzhyale.github.io/FileDO/guides/fd-sec-containers.html"},
        {"rail_job_unsecure", "https://serzhyale.github.io/FileDO/guides/fd-sec-containers.html"},
        {"rail_job_reveal", "https://serzhyale.github.io/FileDO/guides/fd-sec-containers.html"},
        {"rail_job_backup", "https://serzhyale.github.io/FileDO/guides/fd-sec-containers.html#backup"},
        {"rail_job_restore", "https://serzhyale.github.io/FileDO/guides/fd-sec-containers.html#backup"},
        {"rail_job_vd_list", "https://serzhyale.github.io/FileDO/guides/virtual-disks.html"},
        {"rail_job_vd_new", "https://serzhyale.github.io/FileDO/guides/virtual-disks.html#create"},
        {"rail_job_vd_mount", "https://serzhyale.github.io/FileDO/guides/virtual-disks.html#create"},
        {"rail_job_vd_unmount", "https://serzhyale.github.io/FileDO/guides/virtual-disks.html#create"},
        {"rail_job_vd_save", "https://serzhyale.github.io/FileDO/guides/virtual-disks.html#create"},
        {"rail_job_vd_info", "https://serzhyale.github.io/FileDO/guides/virtual-disks.html#read"},
        {"rail_job_vd_verify", "https://serzhyale.github.io/FileDO/guides/virtual-disks.html#read"},
        {"rail_job_vd_export", "https://serzhyale.github.io/FileDO/guides/virtual-disks.html#read"},
        {"rail_job_vd_compact", "https://serzhyale.github.io/FileDO/guides/virtual-disks.html#change"},
        {"rail_job_vd_grow", "https://serzhyale.github.io/FileDO/guides/virtual-disks.html#change"},
        {"rail_job_vd_format", "https://serzhyale.github.io/FileDO/guides/virtual-disks.html#change"},
        {"rail_job_vd_seal", "https://serzhyale.github.io/FileDO/guides/virtual-disks.html#change"},
        {"rail_job_vd_clone", "https://serzhyale.github.io/FileDO/guides/virtual-disks.html#change"},
        {"rail_job_vd_destroy", "https://serzhyale.github.io/FileDO/guides/virtual-disks.html#change"},
        {"rail_job_vd_pass", "https://serzhyale.github.io/FileDO/guides/virtual-disks.html#protection"},
        {"rail_job_vd_auto", "https://serzhyale.github.io/FileDO/guides/virtual-disks.html#autostart"},
        {"rail_job_vd_add", "https://serzhyale.github.io/FileDO/guides/virtual-disks.html#manager"},
        {"rail_job_vd_manager", "https://serzhyale.github.io/FileDO/guides/virtual-disks.html#manager"},
        {"rail_job_command", "https://serzhyale.github.io/FileDO/guides/gui-command-builder.html"}
    }

    ' The resolver of SITE-STRUCTURE rule 11 (section 8 item C). The site publishes every guide in
    ' English, Russian and Ukrainian on one page (the language picked by ?lang=en, ru or uk, the code
    ' the page writes itself) and in German and French as pages of their own under de/ and fr/. A UI
    ' language the site does not publish
    ' opens the English page - never the nearest translated one. The anchor is kept as it is: the
    ' five variants of a guide carry the same section ids. An address that is not a guide of this
    ' site is returned unchanged.
    Public Function GuideAddress(englishAddress As String, Optional language As String = Nothing) As String
        Dim guides = Site & "guides/"
        If englishAddress Is Nothing OrElse Not englishAddress.StartsWith(guides, StringComparison.Ordinal) Then Return englishAddress
        Dim lang = If(language, ShellSettings.Language())
        Dim hash = englishAddress.IndexOf("#"c)
        Dim page = If(hash < 0, englishAddress, englishAddress.Substring(0, hash))
        Dim anchor = If(hash < 0, "", englishAddress.Substring(hash))
        Select Case lang
            Case "de", "fr"
                Return Site & lang & "/guides/" & page.Substring(guides.Length) & anchor
            Case "ru"
                Return page & "?lang=ru" & anchor
            Case "uk"
                Return page & "?lang=uk" & anchor
            Case Else
                ' English, and every language the site lacks: the page would otherwise follow the
                ' visitor's stored site language, so the English one is asked for by name.
                Return page & "?lang=en" & anchor
        End Select
    End Function

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
