Imports System.Diagnostics

' The About page: what this build is, who wrote it, where to read more, and the one button that
' packs the local logs for the author.
'
' The card carries no heading of its own. The rail row says About, the page header above says
' About and what the page is for, and a third "About FileDO" inside the card said the same word a
' third time without adding a fact - so the card starts at the first thing a reader came for, the
' build stamp.
'
' It is a page of the shell rather than a dialog, because a dialog is something a person dismisses
' and a page is something they can read. The addresses live in Links.vb and the log flow in
' LogSender.vb, so every place that shows either one says the same thing.
Public Class AboutView
    Inherits UserControl

    Private ReadOnly dict As Dictionary(Of String, String)
    Private ReadOnly lang As String
    Private ReadOnly tips As New ToolTip()

    Private card As ShellCard
    Private buildLabel As Label
    Private cliLabel As Label
    Private authorLabel As Label
    Private linksHeader As Label
    Private licenseLabel As Label
    Private logsHintLabel As Label
    Private sendLogsBtn As Button
    Private ReadOnly linkLabels As New List(Of LinkLabel)

    Public Sub New()
        lang = ShellSettings.Language()
        dict = Localization.GetDict(lang)
        DoubleBuffered = True
        Dock = DockStyle.Fill
        AutoScroll = True
        BuildLayout()
    End Sub

    Private Function L(key As String) As String
        Dim v As String = Nothing
        If dict IsNot Nothing AndAlso dict.TryGetValue(key, v) Then Return v
        Return key
    End Function

    Private Function LText(key As String) As String
        Return Localization.Multiline(L(key))
    End Function

    Private Sub BuildLayout()
        SuspendLayout()

        Dim root As New TableLayoutPanel With {
            .Dock = DockStyle.Top,
            .AutoSize = True,
            .AutoSizeMode = AutoSizeMode.GrowAndShrink,
            .ColumnCount = 1,
            .RowCount = 1,
            .Padding = Ui.PxPad(Me, 0, 0, 0, 16),
            .Margin = New Padding(0)
        }
        root.ColumnStyles.Add(New ColumnStyle(SizeType.Percent, 100.0F))

        card = New ShellCard With {
            .Padding = Ui.PxPad(Me, 18, 16, 18, 16),
            .Margin = Ui.PxPad(Me, 0, 0, 0, 14)
        }

        Dim t As New TableLayoutPanel With {
            .Dock = DockStyle.Top,
            .AutoSize = True,
            .AutoSizeMode = AutoSizeMode.GrowAndShrink,
            .ColumnCount = 1,
            .RowCount = 8,
            .Margin = New Padding(0)
        }
        t.ColumnStyles.Add(New ColumnStyle(SizeType.Percent, 100.0F))

        buildLabel = New Label With {.Text = L("about_build") & " " & LogReport.BuildStamp(), .AutoSize = True, .Margin = Ui.PxPad(Me, 0, 0, 0, 2)}
        cliLabel = New Label With {.Text = L("about_cli") & " " & LogReport.CliVersion(L("about_cli_missing")), .AutoSize = True, .Margin = Ui.PxPad(Me, 0, 0, 0, 2)}
        authorLabel = New Label With {.Text = L("about_author") & " " & Links.AuthorName, .AutoSize = True, .Margin = Ui.PxPad(Me, 0, 0, 0, 10)}

        linksHeader = New Label With {.Text = L("about_links"), .AutoSize = True, .Margin = Ui.PxPad(Me, 0, 6, 0, 4)}

        Dim linkFlow As New FlowLayoutPanel With {
            .Dock = DockStyle.Top,
            .AutoSize = True,
            .AutoSizeMode = AutoSizeMode.GrowAndShrink,
            .FlowDirection = FlowDirection.TopDown,
            .WrapContents = False,
            .Margin = Ui.PxPad(Me, 0, 0, 0, 10)
        }
        linkFlow.Controls.Add(NewLink(L("about_site"), Links.Site))
        linkFlow.Controls.Add(NewLink(L("about_source"), Links.GitHub))
        linkFlow.Controls.Add(NewLink(L("about_issues"), Links.Issues))
        linkFlow.Controls.Add(NewLink(L("about_privacy"), Links.Privacy))
        linkFlow.Controls.Add(NewLink(L("about_portfolio"), Links.Portfolio))
        linkFlow.Controls.Add(NewLink(Links.AuthorEmail, "mailto:" & Links.AuthorEmail))

        licenseLabel = New Label With {.Text = L("about_license"), .AutoSize = True, .Margin = Ui.PxPad(Me, 0, 0, 0, 12)}
        logsHintLabel = New Label With {.Text = LText("about_logs_hint"), .AutoSize = True, .Margin = Ui.PxPad(Me, 0, 0, 0, 8)}

        sendLogsBtn = New Button With {
            .Text = L("ui_send_logs"),
            .AutoSize = True,
            .AutoSizeMode = AutoSizeMode.GrowAndShrink,
            .Padding = Ui.PxPad(Me, 12, 5, 12, 5)
        }
        AddHandler sendLogsBtn.Click, AddressOf SendLogs_Click
        tips.SetToolTip(sendLogsBtn, LText("logs_tip"))

        t.Controls.Add(buildLabel, 0, 0)
        t.Controls.Add(cliLabel, 0, 1)
        t.Controls.Add(authorLabel, 0, 2)
        t.Controls.Add(linksHeader, 0, 3)
        t.Controls.Add(linkFlow, 0, 4)
        t.Controls.Add(licenseLabel, 0, 5)
        t.Controls.Add(logsHintLabel, 0, 6)
        t.Controls.Add(sendLogsBtn, 0, 7)

        card.Controls.Add(t)
        root.Controls.Add(card, 0, 0)
        Controls.Add(root)

        Ui.Wrap(licenseLabel, card, Ui.Px(Me, 36))
        Ui.Wrap(logsHintLabel, card, Ui.Px(Me, 36))

        ResumeLayout(True)
    End Sub

    Private Function NewLink(text As String, url As String) As LinkLabel
        Dim lbl As New LinkLabel With {
            .Text = text,
            .AutoSize = True,
            .Margin = Ui.PxPad(Me, 0, 2, 0, 2),
            .Tag = url
        }
        lbl.AccessibleName = text & " - " & url
        tips.SetToolTip(lbl, url)
        AddHandler lbl.LinkClicked, Sub(s As Object, e As LinkLabelLinkClickedEventArgs)
                                      Links.Open(ShellDialog.OwnerOf(Me), CStr(DirectCast(s, LinkLabel).Tag))
                                  End Sub
        linkLabels.Add(lbl)
        Return lbl
    End Function

    ' Nothing leaves the machine here: the zip is built locally, the folder is opened so the user
    ' sees it first, and the mail is sent by the user or not at all. The flow is LogSender's, shared
    ' with the Send logs button of every problem dialog.
    Private Sub SendLogs_Click(sender As Object, e As EventArgs)
        LogSender.Run(ShellDialog.OwnerOf(Me))
    End Sub

    Public Sub ApplyTheme()
        Dim p = Theme.Current
        BackColor = p.Background
        ForeColor = p.Text

        card.BackColor = p.Surface
        card.BorderColour = p.Border

        For Each lbl As Label In New Label() {buildLabel, cliLabel, authorLabel}
            lbl.Font = Theme.FontBody()
            lbl.ForeColor = p.Text
        Next

        linksHeader.Font = Theme.FontSubtitle()
        linksHeader.ForeColor = p.Accent

        For Each lbl In linkLabels
            lbl.Font = Theme.FontBody()
            lbl.LinkColor = p.Link
            lbl.ActiveLinkColor = p.Text
            lbl.VisitedLinkColor = p.Link
            lbl.LinkBehavior = LinkBehavior.HoverUnderline
        Next

        licenseLabel.Font = Theme.FontCaption()
        licenseLabel.ForeColor = p.MutedText
        logsHintLabel.Font = Theme.FontBody()
        logsHintLabel.ForeColor = p.MutedText

        sendLogsBtn.Font = Theme.FontBody()
        Ui.StyleButton(sendLogsBtn, p.SurfaceAlt, p.Text, p.Border)

        Invalidate(True)
    End Sub

End Class
