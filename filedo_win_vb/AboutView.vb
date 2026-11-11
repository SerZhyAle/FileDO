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
    Private descriptionLabel As Label
    Private buildLabel As Label
    Private cliLabel As Label
    Private authorLabel As Label
    Private linksHeader As Label
    Private relatedHeader As Label
    Private licenseLabel As Label
    Private logsHintLabel As Label
    Private sendLogsBtn As Button
    Private ReadOnly linkLabels As New List(Of LinkLabel)
    Private ReadOnly relatedNotes As New List(Of Label)

    Public Sub New()
        lang = ShellSettings.Language()
        dict = Localization.GetDict(lang)
        DoubleBuffered = True
        Dock = DockStyle.Fill
        AutoScroll = True
        BuildLayout()
        ' The parts the help explains (HelpContent, page about).
        buildLabel.Name = "help:build"
        authorLabel.Name = "help:author"
        licenseLabel.Name = "help:license"
        sendLogsBtn.Name = "help:logs"
    End Sub

    ' The page is built again each time the Disk manager's settings window opens it, so the tooltip
    ' component (not a child control) goes with the page.
    Protected Overrides Sub Dispose(disposing As Boolean)
        If disposing Then tips.Dispose()
        MyBase.Dispose(disposing)
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
            .RowCount = 11,
            .Margin = New Padding(0)
        }
        t.ColumnStyles.Add(New ColumnStyle(SizeType.Percent, 100.0F))

        ' What the program is for comes first (SETTINGS-NAVIGATION 2.0 section 3.3, the complete set):
        ' the same sentence the store listing leads with, in every language.
        descriptionLabel = New Label With {.Text = L("about_description"), .AutoSize = True, .Margin = Ui.PxPad(Me, 0, 0, 0, 10)}
        buildLabel =New Label With {.Text = L("about_build") & " " & LogReport.BuildStamp(), .AutoSize = True, .Margin = Ui.PxPad(Me, 0, 0, 0, 2)}
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
        linkFlow.Name = "help:links"
        ' The help of this program comes first among the links: it is on this machine and needs no
        ' network. It opens the index of the help pages (CONTEXT-HELP, the About page links to it).
        linkFlow.Controls.Add(NewLink(L("vd_mgr_name_help"), "", Sub() Help.OpenIndex(ShellDialog.OwnerOf(Me))))
        linkFlow.Controls.Add(NewLink(L("about_topics"), Links.Topics))
        linkFlow.Controls.Add(NewLink(L("about_site"), Links.Site))
        linkFlow.Controls.Add(NewLink(L("about_releases"), Links.Releases))
        linkFlow.Controls.Add(NewLink(L("about_source"), Links.GitHub))
        linkFlow.Controls.Add(NewLink(L("about_support"), Links.Support))
        linkFlow.Controls.Add(NewLink(L("about_issues"), Links.Issues))
        linkFlow.Controls.Add(NewLink(L("about_privacy"), Links.Privacy))
        linkFlow.Controls.Add(NewLink(L("about_license_text"), Links.License))
        linkFlow.Controls.Add(NewLink(L("about_portfolio"), Links.Portfolio))
        linkFlow.Controls.Add(NewLink(Links.AuthorEmail, "mailto:" & Links.AuthorEmail))

        ' The related programs, from the one inventory in Links.vb (section 3.4), each with its purpose.
        relatedHeader = New Label With {.Text = L("about_related"), .AutoSize = True, .Margin = Ui.PxPad(Me, 0, 6, 0, 4)}
        Dim relatedFlow As New FlowLayoutPanel With {
            .Dock = DockStyle.Top,
            .AutoSize = True,
            .AutoSizeMode = AutoSizeMode.GrowAndShrink,
            .FlowDirection = FlowDirection.TopDown,
            .WrapContents = False,
            .Margin = Ui.PxPad(Me, 0, 0, 0, 10),
            .Name = "about:related"
        }
        ' The name and the purpose line are two controls, each one key, so a live language switch
        ' (LiveLanguage) translates both.
        For Each product In Links.Related
            relatedFlow.Controls.Add(NewLink(L(product(1)), product(0)))
            Dim note As New Label With {.Text = L(product(2)), .AutoSize = True, .Margin = Ui.PxPad(Me, 0, 0, 0, 4)}
            relatedNotes.Add(note)
            relatedFlow.Controls.Add(note)
        Next

        licenseLabel = New Label With {.Text = L("about_license"), .AutoSize = True, .Margin = Ui.PxPad(Me, 0, 0, 0, 12)}
        logsHintLabel = New Label With {.Text = LText("about_logs_hint"), .AutoSize = True, .Margin = Ui.PxPad(Me, 0, 0, 0, 8)}

        sendLogsBtn = Ui.GlyphCommandButton(L("ui_send_logs"), GlyphRef.Vocabulary("action.email"), New Padding(0))
        AddHandler sendLogsBtn.Click, AddressOf SendLogs_Click
        tips.SetToolTip(sendLogsBtn, LText("logs_tip"))

        t.Controls.Add(descriptionLabel, 0, 0)
        t.Controls.Add(buildLabel, 0, 1)
        t.Controls.Add(cliLabel, 0, 2)
        t.Controls.Add(authorLabel, 0, 3)
        t.Controls.Add(linksHeader, 0, 4)
        t.Controls.Add(linkFlow, 0, 5)
        t.Controls.Add(relatedHeader, 0, 6)
        t.Controls.Add(relatedFlow, 0, 7)
        t.Controls.Add(licenseLabel, 0, 8)
        t.Controls.Add(logsHintLabel, 0, 9)
        t.Controls.Add(sendLogsBtn, 0, 10)

        card.Controls.Add(t)
        root.Controls.Add(card, 0, 0)
        Controls.Add(root)

        Ui.Wrap(descriptionLabel, card, Ui.Px(Me, 36))
        For Each note In relatedNotes
            Ui.Wrap(note, card, Ui.Px(Me, 36))
        Next
        Ui.Wrap(licenseLabel, card, Ui.Px(Me, 36))
        Ui.Wrap(logsHintLabel, card, Ui.Px(Me, 36))

        ResumeLayout(True)
    End Sub

    ' A link that opens an address, or - given onClick - does something of the program's own (the help).
    Private Function NewLink(text As String, url As String, Optional onClick As Action = Nothing) As LinkLabel
        Dim lbl As New LinkLabel With {
            .Text = text,
            .AutoSize = True,
            .Margin = Ui.PxPad(Me, 0, 2, 0, 2),
            .Tag = url
        }
        lbl.AccessibleName = If(url = "", text, text & " - " & url)
        If url <> "" Then tips.SetToolTip(lbl, url)
        AddHandler lbl.LinkClicked, Sub(s As Object, e As LinkLabelLinkClickedEventArgs)
                                      If onClick IsNot Nothing Then
                                          onClick()
                                      Else
                                          Links.Open(ShellDialog.OwnerOf(Me), CStr(DirectCast(s, LinkLabel).Tag))
                                      End If
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
        Ui.SuspendTree(Me)
        Try
            ApplyThemeCore()
        Finally
            Ui.ResumeTree(Me)
        End Try
    End Sub

    Private Sub ApplyThemeCore()
        Dim p = Theme.Current
        BackColor = p.Background
        ForeColor = p.Text

        card.BackColor = p.Surface
        card.BorderColour = p.Border

        For Each lbl As Label In New Label() {descriptionLabel, buildLabel, cliLabel, authorLabel}
            lbl.Font = Theme.FontBody()
            lbl.ForeColor = p.Text
        Next

        For Each lbl As Label In New Label() {linksHeader, relatedHeader}
            lbl.Font = Theme.FontSubtitle()
            lbl.ForeColor = p.Accent
        Next

        For Each lbl In linkLabels
            lbl.Font = Theme.FontBody()
            lbl.LinkColor = p.Link
            lbl.ActiveLinkColor = p.Text
            lbl.VisitedLinkColor = p.Link
            lbl.LinkBehavior = LinkBehavior.HoverUnderline
        Next

        For Each note In relatedNotes
            note.Font = Theme.FontCaption()
            note.ForeColor = p.MutedText
        Next

        licenseLabel.Font = Theme.FontCaption()
        licenseLabel.ForeColor = p.MutedText
        logsHintLabel.Font = Theme.FontBody()
        logsHintLabel.ForeColor = p.MutedText

        sendLogsBtn.Font = Theme.FontBody()


        Invalidate(True)
    End Sub

End Class
