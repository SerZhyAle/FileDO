' Both settings surfaces host the same reversible value controls. Close promises no rollback.
Friend Class DiskSettingsDialog
    Inherits Form

    Friend ReadOnly Panel As SettingsPanel
    Private ReadOnly closeButton As Button

    Friend Sub New()
        Text = Localization.T("rail_job_settings")
        Font = Theme.FontBody()
        FormBorderStyle = FormBorderStyle.Sizable
        MinimizeBox = False
        MaximizeBox = False
        ShowInTaskbar = False
        StartPosition = FormStartPosition.CenterParent
        AutoScaleMode = AutoScaleMode.Font
        Size = Ui.PxSize(Me, 620, 610)
        MinimumSize = Ui.PxSize(Me, 420, 360)
        AppIcon.Apply(Me)
        Dim root As New TableLayoutPanel With {.Dock = DockStyle.Fill, .ColumnCount = 1, .RowCount = 2, .Padding = Ui.PxPad(Me, 12, 12, 12, 12)}
        root.ColumnStyles.Add(New ColumnStyle(SizeType.Percent, 100))
        root.RowStyles.Add(New RowStyle(SizeType.Percent, 100))
        root.RowStyles.Add(New RowStyle(SizeType.AutoSize))
        Panel = New SettingsPanel()
        closeButton = New Button With {.Text = Localization.T("settings_close"), .AutoSize = True, .Anchor = AnchorStyles.Right, .DialogResult = DialogResult.Cancel}
        CancelButton = closeButton
        root.Controls.Add(Panel, 0, 0)
        root.Controls.Add(closeButton, 0, 1)
        Controls.Add(root)
        AddHandler Panel.ThemeChanged, AddressOf RepaintTheme
        RepaintTheme()
        Theme.Watch(Me, AddressOf RepaintTheme)
    End Sub

    Friend Sub DrawClientForCapture(bitmap As Bitmap)
        Controls(0).DrawToBitmap(bitmap, New Rectangle(0, 0, bitmap.Width, bitmap.Height))
    End Sub

    Protected Overrides Sub OnHandleCreated(e As EventArgs)
        MyBase.OnHandleCreated(e)
        Chrome.Apply(Me)
    End Sub

    Protected Overrides Sub OnLoad(e As EventArgs)
        MyBase.OnLoad(e)
        Dim work = Screen.FromHandle(Handle).WorkingArea
        Size = New Size(Math.Min(Width, work.Width), Math.Min(Height, work.Height))
    End Sub

    Protected Overrides Sub WndProc(ByRef m As Message)
        MyBase.WndProc(m)
        If m.Msg = &H1A AndAlso ShellSettings.ThemeChoice() = "auto" AndAlso Panel IsNot Nothing Then
            Theme.Refresh()
            RepaintTheme()
        End If
    End Sub

    Friend Sub RepaintTheme()
        BackColor = Theme.Current.Background
        ForeColor = Theme.Current.Text
        Panel.ApplyTheme()
        Ui.StyleButton(closeButton, Theme.Current.SurfaceAlt, Theme.Current.Text, Theme.Current.Border)
        Chrome.Apply(Me)
        AppHost.SettingsThemeChanged()
    End Sub
End Class
