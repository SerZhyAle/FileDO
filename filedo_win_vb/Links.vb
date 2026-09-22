' Every address this program can send a user to, in one place.
'
' The builder's About window and the shell's About page show the same links; two copies of a URL
' is one copy to get wrong, and a stale link in a program is indistinguishable from a broken
' product. The addresses are the published ones for this project and the author's portfolio hub.
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

    ' A link that cannot be opened says so with the address in front of the user, because an
    ' address they can read is an address they can copy into a browser themselves.
    Public Sub Open(target As String, title As String)
        Try
            Process.Start(New ProcessStartInfo() With {.FileName = target, .UseShellExecute = True})
        Catch ex As Exception
            MessageBox.Show(target & Environment.NewLine & Environment.NewLine & ex.Message,
                            title, MessageBoxButtons.OK, MessageBoxIcon.Warning)
        End Try
    End Sub

End Module
