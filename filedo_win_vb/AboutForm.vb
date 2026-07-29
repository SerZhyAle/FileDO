' The About window: what this build is, who wrote it, where to read more - and the one button
' that packs the local FileDO logs and hands them to the user's mail program.
'
' Nothing here runs by itself. The log archive is only ever built because the user pressed
' Send logs and then confirmed. See docs/spec-send-logs-to-author.md.

Imports System.Diagnostics

Public Class AboutForm

    Private ReadOnly lang As String
    Private ReadOnly dict As Dictionary(Of String, String)

    Private Const UrlSite As String = "https://serzhyale.github.io/FileDO/"
    Private Const UrlGitHub As String = "https://github.com/SerZhyAle/FileDO"
    Private Const UrlIssues As String = "https://github.com/SerZhyAle/FileDO/issues"
    Private Const UrlPrivacy As String = "https://serzhyale.github.io/FileDO/privacy.html"
    Private Const UrlPortfolio As String = "https://sza.od.ua"

    Public Sub New(language As String)
        InitializeComponent()
        AppIcon.Apply(Me)
        lang = language
        dict = Localization.GetDict(language)
    End Sub

    Private Function L(key As String) As String
        Dim v As String = Nothing
        If dict IsNot Nothing AndAlso dict.TryGetValue(key, v) Then Return v
        Return key
    End Function

    Private Function LText(key As String) As String
        Return Localization.Multiline(L(key))
    End Function

    Private Sub AboutForm_Load(sender As Object, e As EventArgs) Handles MyBase.Load
        Me.Text = L("about_title")
        lblTagline.Text = LText("about_tagline")
        lblBuild.Text = L("about_build") & " " & LogReport.BuildStamp()
        lblCli.Text = L("about_cli") & " " & LogReport.CliVersion()
        lblAuthor.Text = L("about_author") & " Serhii Zhyhunenko (sza)"
        lnkMail.Text = LogReport.AuthorEmail
        lblLinks.Text = L("about_links")
        lnkSite.Text = L("about_site")
        lnkGitHub.Text = L("about_source")
        lnkIssues.Text = L("about_issues")
        lnkPrivacy.Text = L("about_privacy")
        lnkPortfolio.Text = L("about_portfolio")
        lblLicense.Text = L("about_license")
        lblLogsHint.Text = LText("about_logs_hint")
        btnSendLogs.Text = L("ui_send_logs")
        btnClose.Text = L("ui_close")

        tipAbout.SetToolTip(btnSendLogs, LText("logs_tip"))
        tipAbout.SetToolTip(lnkSite, UrlSite)
        tipAbout.SetToolTip(lnkGitHub, UrlGitHub)
        tipAbout.SetToolTip(lnkIssues, UrlIssues)
        tipAbout.SetToolTip(lnkPrivacy, UrlPrivacy)
        tipAbout.SetToolTip(lnkPortfolio, UrlPortfolio)
        tipAbout.SetToolTip(lnkMail, "mailto:" & LogReport.AuthorEmail)
    End Sub

    ' ---- links ------------------------------------------------------------

    Private Sub Open(target As String)
        Try
            Process.Start(New ProcessStartInfo() With {.FileName = target, .UseShellExecute = True})
        Catch ex As Exception
            MessageBox.Show(target & Environment.NewLine & Environment.NewLine & ex.Message,
                            L("about_title"), MessageBoxButtons.OK, MessageBoxIcon.Warning)
        End Try
    End Sub

    Private Sub lnkSite_Click(sender As Object, e As LinkLabelLinkClickedEventArgs) Handles lnkSite.LinkClicked
        Open(UrlSite)
    End Sub

    Private Sub lnkGitHub_Click(sender As Object, e As LinkLabelLinkClickedEventArgs) Handles lnkGitHub.LinkClicked
        Open(UrlGitHub)
    End Sub

    Private Sub lnkIssues_Click(sender As Object, e As LinkLabelLinkClickedEventArgs) Handles lnkIssues.LinkClicked
        Open(UrlIssues)
    End Sub

    Private Sub lnkPrivacy_Click(sender As Object, e As LinkLabelLinkClickedEventArgs) Handles lnkPrivacy.LinkClicked
        Open(UrlPrivacy)
    End Sub

    Private Sub lnkPortfolio_Click(sender As Object, e As LinkLabelLinkClickedEventArgs) Handles lnkPortfolio.LinkClicked
        Open(UrlPortfolio)
    End Sub

    Private Sub lnkMail_Click(sender As Object, e As LinkLabelLinkClickedEventArgs) Handles lnkMail.LinkClicked
        Open("mailto:" & LogReport.AuthorEmail)
    End Sub

    Private Sub btnClose_Click(sender As Object, e As EventArgs) Handles btnClose.Click
        Me.Close()
    End Sub

    ' ---- send logs --------------------------------------------------------

    ' Collect the FileDO logs on this machine into one zip and hand it to the user's mail
    ' program. Reached only by the user pressing this button and confirming; nothing leaves the
    ' machine until the user sends the mail themselves.
    Private Sub btnSendLogs_Click(sender As Object, e As EventArgs) Handles btnSendLogs.Click
        If MessageBox.Show(LText("logs_confirm"), L("logs_title"),
                           MessageBoxButtons.YesNo, MessageBoxIcon.Question) <> DialogResult.Yes Then
            Return
        End If

        Dim count As Integer = 0
        Dim archive As String
        Try
            Me.Cursor = Cursors.WaitCursor
            archive = LogReport.BuildArchive(lang, count)
        Catch ex As Exception
            MessageBox.Show(String.Format(LText("logs_error"), ex.Message), L("logs_title"),
                            MessageBoxButtons.OK, MessageBoxIcon.Error)
            Return
        Finally
            Me.Cursor = Cursors.Default
        End Try

        If archive = "" Then
            MessageBox.Show(LText("logs_none"), L("logs_title"),
                            MessageBoxButtons.OK, MessageBoxIcon.Information)
            Return
        End If

        Dim problems As New List(Of String)
        LogReport.RevealInExplorer(archive, problems)
        LogReport.CopyPathToClipboard(archive, problems)
        LogReport.OpenMailClient(archive, problems)

        Dim msg As String = String.Format(LText("logs_ready"), archive, count)
        If problems.Count > 0 Then
            msg &= Environment.NewLine & Environment.NewLine & L("logs_partial") &
                   Environment.NewLine & String.Join(Environment.NewLine, problems.ToArray())
        End If
        MessageBox.Show(msg, L("logs_title"), MessageBoxButtons.OK, MessageBoxIcon.Information)
    End Sub

End Class
