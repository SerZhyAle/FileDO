' One system icon for the process. The menu delegates to the windows' existing action paths.
Friend Class TrayIcon
    Implements IDisposable

    Private ReadOnly host As AppHost
    Private ReadOnly icon As NotifyIcon
    Private ReadOnly menu As New ContextMenuStrip()
    Private ticks As Integer
    Private ReadOnly timer As New Windows.Forms.Timer With {.Interval = 1000}
    Friend Shared Function MenuKeys(packaged As Boolean, mounted As Integer, jobs As Integer) As String()
        Dim keys As New List(Of String) From {"tray_open", "tray_manager", "-"}
        If Not packaged AndAlso mounted > 0 Then keys.Add("tray_unmount")
        If jobs > 0 Then keys.Add("tray_stop")
        If keys.Last() <> "-" Then keys.Add("-")
        keys.Add("settings_startup")
        keys.Add("tray_exit")
        Return keys.ToArray()
    End Function

    Friend Shared Function ShouldShow(hiddenStart As Boolean, hiddenWindows As Integer) As Boolean
        Return hiddenStart OrElse hiddenWindows > 0
    End Function

    Friend Sub New(owner As AppHost)
        host = owner
        icon = New NotifyIcon With {.Icon = AppIcon.SystemIcon(), .Text = "FileDO", .ContextMenuStrip = menu}
        AddHandler icon.MouseClick, Sub(sender, e)
                                        If e.Button = MouseButtons.Left Then host.ShowShell()
                                    End Sub
        AddHandler menu.Opening, Sub() BuildMenu()
        AddHandler timer.Tick, Sub() UpdateState()
        timer.Start()
    End Sub

    Friend ReadOnly Property Visible As Boolean
        Get
            Return icon.Visible
        End Get
    End Property

    Friend Sub UpdateState()
        icon.Visible = host.TrayHeld
        ticks += 1
        If Visible AndAlso ticks Mod 5 = 0 Then host.RefreshTraySnapshot()
        Dim text = Localization.Format(Localization.T("tray_status"), host.RunningJobs, host.MountedDisks)
        icon.Text = If(text.Length > 63, text.Substring(0, 63), text)
    End Sub

    Private Sub BuildMenu()
        For Each item In menu.Items.Cast(Of ToolStripItem)().ToArray()
            menu.Items.Remove(item)
            item.Dispose()
        Next
        For Each key In MenuKeys(Packaging.IsPackaged(), host.MountedDisks, host.RunningJobs)
            If key = "-" Then
                menu.Items.Add(New ToolStripSeparator())
                Continue For
            End If
            Dim entry As New ToolStripMenuItem(Localization.T(key))
            If key = "settings_startup" Then entry.Checked = Not Packaging.IsPackaged() AndAlso ShellSettings.Autostart() <> "off"
            AddHandler entry.Click, Sub() Invoke(key)
            menu.Items.Add(entry)
        Next
    End Sub

    Private Sub Invoke(key As String)
        Select Case key
            Case "tray_open" : host.ShowShell()
            Case "tray_manager" : host.ShowManager()
            Case "tray_unmount" : host.UnmountAll()
            Case "tray_stop" : host.StopJobs()
            Case "settings_startup" : host.ShowSettings()
            Case "tray_exit" : host.RequestExit()
        End Select
    End Sub

    Friend Sub Finished()
        UpdateState()
        If Visible AndAlso ShellSettings.FinishNotification() Then
            icon.ShowBalloonTip(5000, "FileDO", Localization.T("tray_finished"), ToolTipIcon.Info)
        End If
    End Sub

    Public Sub Dispose() Implements IDisposable.Dispose
        timer.Stop()
        timer.Dispose()
        icon.Visible = False
        icon.Dispose()
        menu.Dispose()
    End Sub
End Class
