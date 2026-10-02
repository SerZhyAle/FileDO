Public Class SettingsPanel
    Inherits UserControl

    Public Event ThemeChanged()

    Private ReadOnly dict As Dictionary(Of String, String)
    Private card As ShellCard
    Private themeHeader As Label
    Private themeCombo As ComboBox
    Private langHeader As Label
    Private langCombo As ComboBox
    Private langHint As Label
    Private historyHeader As Label
    Private historyCheck As CheckBox
    Private historyHint As Label
    Private suppress As Boolean = False
    Friend ReadOnly StartupCombo As New ComboBox With {.DropDownStyle = ComboBoxStyle.DropDownList}
    Private ReadOnly minimizeCheck As New CheckBox With {.AutoSize = True}
    Private ReadOnly notifyCheck As New CheckBox With {.AutoSize = True}
    Private ReadOnly resetButton As New Button With {.AutoSize = True}
    Private ReadOnly startupHeader As New Label With {.AutoSize = True}
    Private ReadOnly startupHint As New Label With {.AutoSize = True}
    Friend Shared ReadOnly NewKeys As String() = {"settings_startup", "settings_startup_off", "settings_startup_shell", "settings_startup_manager", "settings_startup_tray", "settings_startup_hint", "settings_startup_packaged", "settings_startup_failed", "settings_minimize_tray", "settings_notify", "settings_reset", "settings_reset_hint", "settings_close", "tray_open", "tray_manager", "tray_unmount", "tray_stop", "tray_exit", "tray_status", "tray_finished"}

    Public Sub New()
        dict = Localization.GetDict(ShellSettings.Language())
        DoubleBuffered = True
        Dock = DockStyle.Fill
        AutoScroll = True
        BuildLayout()
        AddHandler ShellSettings.ValuesChanged, AddressOf RefreshValues
    End Sub

    Friend Sub RefreshValues()
        suppress = True
        Try
            themeCombo.SelectedIndex = ThemeIndex(ShellSettings.ThemeChoice())
            langCombo.SelectedIndex = Math.Max(0, Array.IndexOf(Localization.Languages, ShellSettings.Language()))
            historyCheck.Checked = ShellSettings.HistoryEnabled()
            StartupCombo.SelectedIndex = Array.IndexOf(StartupLaunch.Choices, ShellSettings.Autostart())
            minimizeCheck.Checked = ShellSettings.MinimizeToTray()
            notifyCheck.Checked = ShellSettings.FinishNotification()
        Finally
            suppress = False
        End Try
    End Sub

    Protected Overrides Sub Dispose(disposing As Boolean)
        If disposing Then RemoveHandler ShellSettings.ValuesChanged, AddressOf RefreshValues
        MyBase.Dispose(disposing)
    End Sub

    Private Function L(key As String) As String
        Dim v As String = Nothing
        If dict IsNot Nothing AndAlso dict.TryGetValue(key, v) Then Return v
        Return key
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
            .RowCount = 9,
            .Margin = New Padding(0)
        }
        t.ColumnStyles.Add(New ColumnStyle(SizeType.Percent, 100.0F))

        themeHeader = New Label With {.Text = L("shell_settings_theme"), .AutoSize = True, .Margin = Ui.PxPad(Me, 0, 0, 0, 4)}
        themeCombo = New ComboBox With {
            .DropDownStyle = ComboBoxStyle.DropDownList,
            .Width = Ui.Px(Me, 260),
            .Margin = Ui.PxPad(Me, 0, 0, 0, 14)
        }
        themeCombo.AccessibleName = L("shell_settings_theme")
        themeCombo.Items.Add(L("shell_theme_auto"))
        themeCombo.Items.Add(L("shell_theme_light"))
        themeCombo.Items.Add(L("shell_theme_dark"))
        themeCombo.SelectedIndex = ThemeIndex(ShellSettings.ThemeChoice())
        AddHandler themeCombo.SelectedIndexChanged, AddressOf ThemeCombo_Changed

        langHeader = New Label With {.Text = L("shell_settings_lang"), .AutoSize = True, .Margin = Ui.PxPad(Me, 0, 0, 0, 4)}
        langCombo = New ComboBox With {
            .DropDownStyle = ComboBoxStyle.DropDownList,
            .Width = Ui.Px(Me, 260),
            .Margin = Ui.PxPad(Me, 0, 0, 0, 4)
        }
        langCombo.AccessibleName = L("shell_settings_lang")
        For Each langName In Localization.LanguageNames
            langCombo.Items.Add(langName)
        Next
        langCombo.SelectedIndex = Math.Max(0, Array.IndexOf(Localization.Languages, ShellSettings.Language()))
        AddHandler langCombo.SelectedIndexChanged, AddressOf LangCombo_Changed

        langHint = New Label With {.Text = L("shell_settings_lang_hint"), .AutoSize = True, .Margin = Ui.PxPad(Me, 0, 0, 0, 14)}

        historyHeader = New Label With {.Text = L("shell_settings_history_title"), .AutoSize = True, .Margin = Ui.PxPad(Me, 0, 0, 0, 4)}
        historyCheck = New CheckBox With {
            .Text = L("shell_settings_history"),
            .AutoSize = True,
            .Checked = ShellSettings.HistoryEnabled(),
            .Margin = Ui.PxPad(Me, 0, 0, 0, 4)
        }
        AddHandler historyCheck.CheckedChanged, Sub()
            If Not suppress Then ShellSettings.SetHistoryEnabled(historyCheck.Checked)
        End Sub

        historyHint = New Label With {.Text = L("shell_settings_history_hint"), .AutoSize = True, .Margin = Ui.PxPad(Me, 0, 0, 0, 0)}

        t.Controls.Add(themeHeader, 0, 0)
        t.Controls.Add(themeCombo, 0, 1)
        t.Controls.Add(langHeader, 0, 2)
        t.Controls.Add(langCombo, 0, 3)
        t.Controls.Add(langHint, 0, 4)
        t.Controls.Add(historyHeader, 0, 5)
        t.Controls.Add(historyCheck, 0, 6)
        t.Controls.Add(historyHint, 0, 7)

        startupHeader.Text = L("settings_startup")
        StartupCombo.AccessibleName = startupHeader.Text
        StartupCombo.Width = Ui.Px(Me, 300)
        For Each choice In StartupLaunch.Choices
            StartupCombo.Items.Add(L("settings_startup_" & choice))
        Next
        StartupCombo.SelectedIndex = Array.IndexOf(StartupLaunch.Choices, ShellSettings.Autostart())
        AddHandler StartupCombo.SelectedIndexChanged, Sub()
            If suppress OrElse StartupCombo.SelectedIndex < 0 Then Return
            If Not StartupLaunch.SetChoice(StartupLaunch.Choices(StartupCombo.SelectedIndex), FindForm()) Then
                suppress = True
                StartupCombo.SelectedIndex = Array.IndexOf(StartupLaunch.Choices, ShellSettings.Autostart())
                suppress = False
            End If
        End Sub
        Dim packaged = Packaging.IsPackaged()
        startupHeader.Visible = Not packaged
        StartupCombo.Visible = Not packaged
        startupHint.Text = L(If(packaged, "settings_startup_packaged", "settings_startup_hint"))
        minimizeCheck.Text = L("settings_minimize_tray")
        minimizeCheck.Checked = ShellSettings.MinimizeToTray()
        AddHandler minimizeCheck.CheckedChanged, Sub()
            If Not suppress Then ShellSettings.SetMinimizeToTray(minimizeCheck.Checked)
        End Sub
        notifyCheck.Text = L("settings_notify")
        notifyCheck.Checked = ShellSettings.FinishNotification()
        AddHandler notifyCheck.CheckedChanged, Sub()
            If Not suppress Then ShellSettings.SetFinishNotification(notifyCheck.Checked)
        End Sub
        resetButton.Text = L("settings_reset")
        AddHandler resetButton.Click, Sub() ShellSettings.ResetPlacements()
        Dim resetHint As New Label With {.Text = L("settings_reset_hint"), .AutoSize = True}
        Dim row = 8
        For Each c As Control In New Control() {startupHeader, StartupCombo, startupHint, minimizeCheck, notifyCheck, resetButton, resetHint}
            c.Margin = Ui.PxPad(Me, 0, 8, 0, 4)
            t.RowCount = row + 1
            t.Controls.Add(c, 0, row)
            row += 1
        Next
        Ui.Wrap(startupHint, card, Ui.Px(Me, 36))
        Ui.Wrap(resetHint, card, Ui.Px(Me, 36))
        card.Controls.Add(t)
        root.Controls.Add(card, 0, 0)
        Controls.Add(root)

        Ui.Wrap(langHint, card, Ui.Px(Me, 36))
        Ui.Wrap(historyHint, card, Ui.Px(Me, 36))

        ResumeLayout(True)
    End Sub

    Private Shared Function ThemeIndex(choice As String) As Integer
        Select Case choice
            Case "light" : Return 1
            Case "dark" : Return 2
            Case Else : Return 0
        End Select
    End Function

    Private Sub ThemeCombo_Changed(sender As Object, e As EventArgs)
        If suppress Then Return
        Dim choice = "auto"
        Select Case themeCombo.SelectedIndex
            Case 1 : choice = "light"
            Case 2 : choice = "dark"
        End Select
        ShellSettings.SetThemeChoice(choice)
        RaiseEvent ThemeChanged()
    End Sub

    ' The language is stored now and shown on the next opening: every view builds its dictionary
    ' once, at construction, so re-labelling a live window would leave half of it in the old
    ' language - which is worse than asking for a restart and saying so.
    Private Sub LangCombo_Changed(sender As Object, e As EventArgs)
        If suppress Then Return
        Dim i = langCombo.SelectedIndex
        If i >= 0 AndAlso i < Localization.Languages.Length Then
            ShellSettings.SetLanguage(Localization.Languages(i))
        End If
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

        For Each h As Label In New Label() {themeHeader, langHeader, historyHeader, startupHeader}
            h.Font = Theme.FontSubtitle()
            h.ForeColor = p.Accent
        Next

        For Each c As ComboBox In New ComboBox() {themeCombo, langCombo, StartupCombo}
            c.Font = Theme.FontBody()
            c.BackColor = p.SurfaceAlt
            c.ForeColor = p.Text
            c.FlatStyle = FlatStyle.Flat
        Next

        historyCheck.Font = Theme.FontBody()
        historyCheck.ForeColor = p.Text

        For Each lbl As Label In New Label() {langHint, historyHint, startupHint}
            lbl.Font = Theme.FontCaption()
            lbl.ForeColor = p.MutedText
        Next

        For Each c As Control In New Control() {minimizeCheck, notifyCheck, resetButton}
            c.Font = Theme.FontBody()
            c.ForeColor = p.Text
            c.BackColor = p.Surface
        Next
        suppress = True
        themeCombo.SelectedIndex = ThemeIndex(ShellSettings.ThemeChoice())
        suppress = False

        Ui.StyleButton(resetButton, p.SurfaceAlt, p.Text, p.Border)
        Invalidate(True)
    End Sub

End Class
