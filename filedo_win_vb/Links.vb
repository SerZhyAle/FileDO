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

    Public Const AuthorName As String = "Serhii Zhyhunenko (sza)"
    Public Const AuthorEmail As String = LogReport.AuthorEmail

    Public Const Site As String = "https://serzhyale.github.io/FileDO/"
    Public Const GitHub As String = "https://github.com/SerZhyAle/FileDO"
    Public Const Issues As String = "https://github.com/SerZhyAle/FileDO/issues"
    Public Const Privacy As String = "https://serzhyale.github.io/FileDO/privacy.html"
    Public Const Portfolio As String = "https://sza.od.ua"

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
                                   Localization.T("shell_btn_copy_address")) Then
                Ui.CopyText(owner, target)
            End If
        End Try
    End Sub

End Module
