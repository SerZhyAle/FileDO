Public Class SettingsPanel
    Inherits UserControl
    Public Event ThemeChanged()
    Friend ReadOnly StartupCombo As New ComboBox With {.DropDownStyle = ComboBoxStyle.DropDownList}
    Friend ReadOnly AutoPlayButton As New Button With {.AutoSize = True}
    Friend ReadOnly Groups As New List(Of SettingsGroup)
    Private ReadOnly themeCombo As New ComboBox With {.DropDownStyle = ComboBoxStyle.DropDownList}
    Private ReadOnly langCombo As New ComboBox With {.DropDownStyle = ComboBoxStyle.DropDownList}
    Private ReadOnly historyCheck As New CheckBox With {.AutoSize = True}
    Private ReadOnly minimizeCheck As New CheckBox With {.AutoSize = True}
    Private ReadOnly notifyCheck As New CheckBox With {.AutoSize = True}
    Private ReadOnly root As New TableLayoutPanel With {.Dock = DockStyle.Top, .AutoSize = True, .ColumnCount = 1}
    Private ReadOnly autoPlayStatus As New Label With {.AutoSize = True}
    Private suppress As Boolean
    Private restoring As Boolean
    Private restorePending As Boolean = True
    Friend Shared ReadOnly NewKeys As String() = {"settings_startup", "settings_startup_off", "settings_startup_shell", "settings_startup_manager", "settings_startup_tray", "settings_startup_hint", "settings_startup_packaged", "settings_startup_failed", "settings_minimize_tray", "settings_notify", "settings_reset", "settings_reset_hint", "settings_close", "tray_open", "tray_manager", "tray_unmount", "tray_stop", "tray_exit", "tray_status", "tray_finished", "settings_autoplay_disable", "settings_autoplay_hint", "settings_autoplay_done", "settings_autoplay_open", "settings_autoplay_failed", "settings_appearance", "settings_windows", "settings_theme_hint", "settings_minimize_hint", "settings_notify_hint", "settings_startup_apply"}

    Public Sub New()
        Dock = DockStyle.Fill
        DoubleBuffered = True
        AutoScroll = True
        root.ColumnStyles.Add(New ColumnStyle(SizeType.Percent, 100))
        Controls.Add(root)
        restoring = True
        BuildLayout()
        For Each group In Groups
            group.Expanded = ShellSettings.SettingsGroupExpanded(group.GroupId)
            AddHandler group.ExpansionChanged, AddressOf SaveContext
        Next
        restoring = False
        RefreshValues()
        AddHandler ShellSettings.ValuesChanged, AddressOf RefreshValues
        AddHandler VisibleChanged, Sub()
                                      If Visible Then
                                          ApplyTheme()
                                          RestoreViewport()
                                      ElseIf IsHandleCreated Then
                                          SaveContext()
                                      End If
                                  End Sub
        ApplyTheme()
    End Sub

    Private Function NewGroup(id As String, key As String) As SettingsGroup
        Dim value As New SettingsGroup(id, Localization.T(key))
        root.RowCount += 1
        root.RowStyles.Add(New RowStyle(SizeType.AutoSize))
        root.Controls.Add(value, 0, root.RowStyles.Count - 1)
        Groups.Add(value)
        Return value
    End Function

    Private Sub AddRow(group As SettingsGroup, id As String, caption As String, hint As String, editor As Control)
        group.AddRow(New SettingRow(id, Localization.T(caption), Localization.T(hint), editor))
    End Sub

    Private Sub BuildLayout()
        Dim appearance = NewGroup("appearance", "settings_appearance")
        For Each key In New String() {"shell_theme_auto", "shell_theme_light", "shell_theme_dark"}
            themeCombo.Items.Add(Localization.T(key))
        Next
        langCombo.Items.AddRange(Localization.LanguageNames)
        AddRow(appearance, "theme", "shell_settings_theme", "settings_theme_hint", themeCombo)
        AddRow(appearance, "language", "shell_settings_lang", "shell_settings_lang_hint", langCombo)
        AddHandler themeCombo.SelectedIndexChanged, Sub()
                                                      If suppress OrElse themeCombo.SelectedIndex < 0 Then Return
                                                      ShellSettings.SetThemeChoice(New String() {"auto", "light", "dark"}(themeCombo.SelectedIndex))
                                                      RaiseEvent ThemeChanged()
                                                  End Sub
        AddHandler langCombo.SelectedIndexChanged, Sub()
                                                     If suppress OrElse langCombo.SelectedIndex < 0 Then Return
                                                     ShellSettings.SetLanguage(Localization.Languages(langCombo.SelectedIndex))
                                                 End Sub
        Dim history = NewGroup("history", "shell_settings_history_title")
        AddRow(history, "history", "shell_settings_history", "shell_settings_history_hint", historyCheck)
        AddHandler historyCheck.CheckedChanged, Sub()
                                                  If Not suppress Then ShellSettings.SetHistoryEnabled(historyCheck.Checked)
                                              End Sub
        Dim startup = NewGroup("startup", "settings_startup")
        For Each choice In StartupLaunch.Choices
            StartupCombo.Items.Add(Localization.T("settings_startup_" & choice))
        Next
        Dim packaged = Packaging.IsPackaged()
        If Not packaged Then
            AddRow(startup, "startup", "settings_startup", "settings_startup_hint", StartupCombo)
            Dim apply As New Button With {.Text = Localization.T("settings_startup_apply"), .AutoSize = True}
            AddRow(startup, "startup-apply", "settings_startup_apply", "settings_startup_hint", apply)
            AddHandler apply.Click, Sub()
                                       If StartupCombo.SelectedIndex >= 0 Then StartupLaunch.SetChoice(StartupLaunch.Choices(StartupCombo.SelectedIndex), FindForm())
                                       RefreshValues()
                                   End Sub
        Else
            StartupCombo.Visible = False
            Dim openStartup As New Button With {.Text = Localization.T("settings_startup"), .AutoSize = True}
            AddRow(startup, "startup-system", "settings_startup", "settings_startup_packaged", openStartup)
            AddHandler openStartup.Click, Sub() Links.Open(FindForm(), "ms-settings:startupapps")
        End If
        AddRow(startup, "minimize", "settings_minimize_tray", "settings_minimize_hint", minimizeCheck)
        AddRow(startup, "notification", "settings_notify", "settings_notify_hint", notifyCheck)
        AddHandler minimizeCheck.CheckedChanged, Sub()
                                                   If Not suppress Then ShellSettings.SetMinimizeToTray(minimizeCheck.Checked)
                                               End Sub
        AddHandler notifyCheck.CheckedChanged, Sub()
                                                 If Not suppress Then ShellSettings.SetFinishNotification(notifyCheck.Checked)
                                             End Sub
        Dim windows = NewGroup("windows", "settings_windows")
        AutoPlayButton.Text = Localization.T("settings_autoplay_disable")
        AutoPlayButton.Visible = Not packaged
        If Not packaged Then
            AddRow(windows, "autoplay", "settings_autoplay_disable", "settings_autoplay_hint", AutoPlayButton)
            AddHandler AutoPlayButton.Click, Sub()
                                                autoPlayStatus.Text = ""
                                                If WindowsAutoPlay.Disable(FindForm()) Then autoPlayStatus.Text = Localization.T("settings_autoplay_done")
                                            End Sub
        End If
        Dim autoplayOpen As New Button With {.Text = Localization.T("settings_autoplay_open"), .AutoSize = True}
        AddRow(windows, "autoplay-system", "settings_autoplay_open", "settings_autoplay_hint", autoplayOpen)
        AddHandler autoplayOpen.Click, Sub() Links.Open(FindForm(), WindowsAutoPlay.SettingsUri)
        Dim reset As New Button With {.Text = Localization.T("settings_reset"), .AutoSize = True}
        AddRow(windows, "reset", "settings_reset", "settings_reset_hint", reset)
        AddHandler reset.Click, Sub() ShellSettings.ResetPlacements()
        windows.Body.RowCount += 1
        windows.Body.Controls.Add(autoPlayStatus)
        Ui.Wrap(autoPlayStatus, windows.Body)
    End Sub

    Friend Sub RefreshValues()
        If IsDisposed Then Return
        suppress = True
        Try
            themeCombo.SelectedIndex = Array.IndexOf(New String() {"auto", "light", "dark"}, ShellSettings.ThemeChoice())
            langCombo.SelectedIndex = Math.Max(0, Array.IndexOf(Localization.Languages, ShellSettings.Language()))
            historyCheck.Checked = ShellSettings.HistoryEnabled()
            StartupCombo.SelectedIndex = Array.IndexOf(StartupLaunch.Choices, ShellSettings.Autostart())
            minimizeCheck.Checked = ShellSettings.MinimizeToTray()
            notifyCheck.Checked = ShellSettings.FinishNotification()
        Finally
            suppress = False
        End Try
    End Sub

    Friend Sub SaveContext()
        If restoring OrElse IsDisposed Then Return
        For Each group In Groups
            ShellSettings.SetSettingsGroupExpanded(group.GroupId, group.Expanded)
        Next
        If Not IsHandleCreated Then Return
        Dim top = -AutoScrollPosition.Y
        Dim anchor = Groups.LastOrDefault(Function(g) g.Top + root.Top - AutoScrollPosition.Y <= top)
        If anchor Is Nothing Then anchor = Groups.First()
        Dim contentTop = anchor.Top + root.Top - AutoScrollPosition.Y
        ShellSettings.SaveSettingsViewport(anchor.GroupId, CInt(Math.Round((top - contentTop) * 96.0 / Ui.DpiFor(Me))))
        restorePending = True
    End Sub

    Friend Sub RestoreViewport()
        If Not restorePending OrElse Not IsHandleCreated Then Return
        restorePending = False
        PerformLayout()
        Dim offset As Integer
        Dim anchorId = ShellSettings.SettingsViewport(offset)
        Dim anchor = Groups.FirstOrDefault(Function(g) g.GroupId = anchorId)
        If anchor Is Nothing Then Return
        Dim y = anchor.Top + root.Top - AutoScrollPosition.Y + Ui.Px(Me, offset)
        AutoScrollPosition = New Point(0, Math.Max(0, y))
    End Sub

    Friend Sub Reveal(id As String)
        Dim row = Groups.SelectMany(Function(g) g.Body.Controls.OfType(Of SettingRow)()).FirstOrDefault(Function(r) r.Name = "setting:" & id)
        If row Is Nothing Then Return
        Dim group = Groups.First(Function(g) g.Body.Controls.Contains(row))
        group.Expanded = True
        ScrollControlIntoView(row)
        row.Editor.Focus()
    End Sub

    Public Sub ApplyTheme()
        Ui.SuspendTree(Me)
        Try
            BackColor = Theme.Current.Background
            ForeColor = Theme.Current.Text
            root.BackColor = BackColor
            For Each group In Groups
                group.ApplyTheme()
                group.UpdateAccessibility()
            Next
            autoPlayStatus.Font = Theme.FontBody()
            autoPlayStatus.ForeColor = Theme.Current.MutedText
        Finally
            Ui.ResumeTree(Me)
        End Try
    End Sub

    Protected Overrides Sub Dispose(disposing As Boolean)
        If disposing Then
            SaveContext()
            RemoveHandler ShellSettings.ValuesChanged, AddressOf RefreshValues
        End If
        MyBase.Dispose(disposing)
    End Sub
End Class
