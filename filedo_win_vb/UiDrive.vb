' SP-0150: the driven visual acceptance run. `filedo_win.exe --ui-drive <folder>` shows the real
' windows on the real screen, drives the real theme mechanism (HKCU choice -> Theme.Refresh ->
' Theme.Watch -> ApplyTheme), activates a settings group with the keyboard, hands focus over on
' collapse, reopens a dialog, closes the shell through its real close path and reopens a fresh one
' with the remembered page, groups and viewport. Every step writes a screenshot of the window's
' actual on-screen rectangle, and the dominant colour of each shot is checked against the palette
' that should have been in force - a stale repaint or a black rectangle fails the run.
'
' The user's own HKCU values (window placement, last page, remembered groups, viewport, theme,
' language) are snapshotted before the run and restored after it.
Imports System.Runtime.InteropServices

Module UiDrive

    Private ReadOnly log As New Text.StringBuilder()
    Private failures As Integer = 0
    Private shotFolder As String = ""

    ' 0 when every step passed, 1 otherwise. The log is the verdict; stdout repeats it.
    Public Function Run(folder As String) As Integer
        IO.Directory.CreateDirectory(folder)
        shotFolder = folder
        Dim saved = SnapshotUserValues()
        Try
            Drive()
        Catch ex As Exception
            Line("ui-drive: crashed: " & ex.ToString())
            failures += 1
        Finally
            RestoreUserValues(saved)
        End Try
        Dim path = IO.Path.Combine(folder, "ui-drive.log")
        Try
            IO.File.WriteAllText(path, log.ToString())
        Catch
        End Try
        Console.WriteLine(path)
        Return If(failures = 0, 0, 1)
    End Function

    Private Sub Line(text As String)
        log.AppendLine(text)
        Console.WriteLine(text)
    End Sub

    Private Sub Check(name As String, ok As Boolean, detail As String)
        Line(If(ok, "PASS ", "FAIL ") & name & If(detail <> "", " - " & detail, ""))
        If Not ok Then failures += 1
    End Sub

    ' ---- the user's own values, snapshotted and restored ---------------------

    Private Function SnapshotUserValues() As Dictionary(Of String, Object)
        Dim values As New Dictionary(Of String, Object)()
        Try
            Using k = Microsoft.Win32.Registry.CurrentUser.OpenSubKey("Software\FileDO")
                If k Is Nothing Then Return values
                For Each name As String In k.GetValueNames()
                    values(name) = k.GetValue(name)
                Next
            End Using
        Catch
        End Try
        Return values
    End Function

    Private Sub RestoreUserValues(saved As Dictionary(Of String, Object))
        Try
            Using k = Microsoft.Win32.Registry.CurrentUser.CreateSubKey("Software\FileDO")
                If k Is Nothing Then Return
                For Each name As String In k.GetValueNames()
                    If Not saved.ContainsKey(name) Then k.DeleteValue(name, False)
                Next
                For Each pair In saved
                    k.SetValue(pair.Key, pair.Value)
                Next
            End Using
        Catch
        End Try
    End Sub

    ' ---- the drive -----------------------------------------------------------

    <DllImport("user32.dll")>
    Private Function SetForegroundWindow(hwnd As IntPtr) As <MarshalAs(UnmanagedType.Bool)> Boolean
    End Function

    <DllImport("user32.dll", SetLastError:=True)>
    Private Function PostMessage(hWnd As IntPtr, Msg As UInteger, wParam As IntPtr, lParam As IntPtr) As <MarshalAs(UnmanagedType.Bool)> Boolean
    End Function

    Private Const WM_KEYDOWN As UInteger = &H100

    ' A key press delivered as the message a physical key would deliver, to the control that owns it.
    Private Sub PressKey(target As Control, key As Keys)
        If target Is Nothing OrElse Not target.IsHandleCreated Then Return
        PostMessage(target.Handle, WM_KEYDOWN, New IntPtr(CInt(key)), IntPtr.Zero)
        Application.DoEvents()
    End Sub

    Private Sub Pump()
        For i = 1 To 8
            Application.DoEvents()
            Threading.Thread.Sleep(40)
        Next
    End Sub

    ' A newly shown WinForms window may reach the screen only after its first layout and paint; a
    ' capture taken before that photographs whatever sits behind the window. Bring the window to the
    ' foreground, force a synchronous paint, let the compositor run, and only then read the screen.
    <DllImport("user32.dll")>
    Private Sub keybd_event(bVk As Byte, bScan As Byte, dwFlags As UInteger, dwExtraInfo As IntPtr)
    End Sub

    Private Const KEYEVENTF_KEYUP As UInteger = 2

    ' Windows lets only the foreground process take the keyboard and denies SetForegroundWindow to a
    ' process it did not start from the foreground - so a drive launched from a script would click
    ' and type into whatever sat in front. The Alt tap is the operating system's own documented
    ' unlock for that case, and TopMost keeps the captured rectangle ours even if the switch is
    ' refused anyway.
    Private Sub Foreground(window As Form)
        keybd_event(&H12, 0, 0, IntPtr.Zero)
        window.BringToFront()
        window.Activate()
        If window.IsHandleCreated Then SetForegroundWindow(window.Handle)
        keybd_event(&H12, 0, KEYEVENTF_KEYUP, IntPtr.Zero)
        window.TopMost = True
        Pump()
        window.Refresh()
        Application.DoEvents()
        Threading.Thread.Sleep(60)
        Application.DoEvents()
        window.TopMost = False
        Application.DoEvents()
    End Sub

    Private Sub Shot(window As Form, name As String)
        Foreground(window)
        Dim rect = window.RectangleToScreen(window.ClientRectangle)
        Try
            Using bmp As New Bitmap(rect.Width, rect.Height)
                Using g = Graphics.FromImage(bmp)
                    g.CopyFromScreen(rect.Location, Point.Empty, rect.Size)
                End Using
                bmp.Save(IO.Path.Combine(shotFolder, name & ".png"), Imaging.ImageFormat.Png)
            End Using
            Line("shot " & name & ".png")
        Catch ex As Exception
            Check("ui-drive:shot:" & name, False, ex.GetType().Name)
        End Try
    End Sub

    ' The dominant colour of a shot must be one of the current palette's surfaces, and must not be
    ' the opposite theme's background - the defect a stale repaint produces (APP-STYLE 0.10 section 10).
    Private Sub CheckShot(name As String, dark As Boolean)
        Dim file = IO.Path.Combine(shotFolder, name & ".png")
        If Not IO.File.Exists(file) Then
            Check("ui-drive:colour:" & name, False, "missing shot")
            Return
        End If
        Dim counts As New Dictionary(Of Integer, Integer)()
        Using bmp As New Bitmap(file)
            For y = 0 To bmp.Height - 1 Step 3
                For x = 0 To bmp.Width - 1 Step 3
                    Dim argb = bmp.GetPixel(x, y).ToArgb()
                    Dim n As Integer = 0
                    counts.TryGetValue(argb, n)
                    counts(argb) = n + 1
                Next
            Next
        End Using
        Dim best As Integer = 0, bestCount As Integer = -1
        For Each pair In counts
            If pair.Value > bestCount Then
                bestCount = pair.Value
                best = pair.Key
            End If
        Next
        Dim now = Theme.PaletteFor(dark), was = Theme.PaletteFor(Not dark)
        Dim onCurrent = NearArgb(best, now.Background) OrElse NearArgb(best, now.Surface) OrElse NearArgb(best, now.SurfaceAlt)
        Dim onOpposite = NearArgb(best, was.Background) OrElse NearArgb(best, was.Surface)
        Check("ui-drive:colour:" & name, onCurrent AndAlso Not onOpposite,
              "#" & (best And &HFFFFFF).ToString("X6") & " on " & If(dark, "dark", "light"))
    End Sub

    ' A measured pixel is decoded as the integer it already is - no colour is named or mixed here
    ' (APP-STYLE rule 3's gate reads this file too); only the palette in Theme.vb names colours.
    Private Function NearArgb(argb As Integer, palette As Color) As Boolean
        Return Math.Abs(((argb >> 16) And &HFF) - CInt(palette.R)) <= 10 AndAlso
               Math.Abs(((argb >> 8) And &HFF) - CInt(palette.G)) <= 10 AndAlso
               Math.Abs((argb And &HFF) - CInt(palette.B)) <= 10
    End Function

    Private Sub Drive()
        ShellSettings.SetThemeChoice("light")
        Theme.Refresh()

        ' 1) The shell, on the screen, in light.
        Dim shell As New ShellForm()
        shell.Show()
        Pump()
        Pump()
        Check("ui-drive:shell-shown", shell.Visible, "")
        Shot(shell, "01-shell-light")
        CheckShot("01-shell-light", False)

        ' 2) The settings page: independent groups, native editors, the remembered context.
        shell.OpenSettings()
        Pump()
        Dim panel = shell.SettingsPanelForTest
        Check("ui-drive:settings-groups", panel.Groups.Count >= 4, panel.Groups.Count.ToString() & " groups")
        Dim appearance = panel.Groups.First(Function(g) g.GroupId = "appearance")
        Dim historyGroup = panel.Groups.First(Function(g) g.GroupId = "history")
        Check("ui-drive:settings-revealed", appearance.Expanded AndAlso appearance.Body.Controls.OfType(Of SettingRow)().Any(),
              "")
        Shot(shell, "02-settings-light")
        CheckShot("02-settings-light", False)

        ' 3) Collapse hands focus to the header when it held a child (WINDOWS-UI section 3.2).
        Dim themeRow = appearance.Body.Controls.OfType(Of SettingRow)().First(Function(r) r.Name = "setting:theme")
        themeRow.Editor.Focus()
        Pump()
        appearance.Expanded = False
        Pump()
        Check("ui-drive:collapse-focus-handoff", appearance.Header.Focused, "header focused: " & appearance.Header.Focused.ToString())
        Check("ui-drive:collapse-hides-children", Not appearance.Body.Visible AndAlso Not themeRow.Editor.Visible, "")
        appearance.Expanded = True
        Pump()

        ' 4) Keyboard activation of a group header (Space is the rail entry's own key).
        Dim expandedBefore = historyGroup.Expanded
        Foreground(shell)
        historyGroup.Header.Focus()
        Pump()
        Check("ui-drive:group-header-focused", historyGroup.Header.Focused OrElse historyGroup.Header.ContainsFocus,
              "focused: " & historyGroup.Header.Focused.ToString())
        PressKey(historyGroup.Header, Keys.Space)
        Pump()
        Check("ui-drive:group-keyboard-toggle", historyGroup.Expanded <> expandedBefore,
              "was " & expandedBefore.ToString() & ", now " & historyGroup.Expanded.ToString())
        PressKey(historyGroup.Header, Keys.Space)
        Pump()
        Check("ui-drive:group-keyboard-back", historyGroup.Expanded = expandedBefore, "")

        ' 5) The theme cycle on the live window: light -> dark -> light (APP-STYLE section 2).
        ShellSettings.SetThemeChoice("dark")
        Pump()
        Shot(shell, "03-settings-dark")
        CheckShot("03-settings-dark", True)
        Check("ui-drive:combo-follows-theme", themeRow.Editor.BackColor.ToArgb() = Theme.Current.Surface.ToArgb(),
              themeRow.Editor.BackColor.ToString())
        ShellSettings.SetThemeChoice("light")
        Pump()
        Shot(shell, "04-settings-light-again")
        CheckShot("04-settings-light-again", False)

        ' 6) A small editing dialog: themed while open, reopened after a close (WINDOWS-UI 6.2).
        Dim dict = Localization.GetDict(ShellSettings.Language())
        Dim dialog As New DiskNameDialog(dict, "C:\work\photos.fdd", "photos", Nothing, shell)
        dialog.Show(shell)
        Pump()
        Shot(dialog, "05-dialog-light")
        CheckShot("05-dialog-light", False)
        ShellSettings.SetThemeChoice("dark")
        Pump()
        Shot(dialog, "06-dialog-dark-open")
        CheckShot("06-dialog-dark-open", True)
        Check("ui-drive:dialog-follows-theme", dialog.BackColor.ToArgb() = Theme.Current.Surface.ToArgb(),
              dialog.BackColor.ToString())
        dialog.Close()
        Pump()
        Dim reopened As New DiskNameDialog(dict, "C:\work\photos.fdd", "photos", Nothing, shell)
        reopened.Show(shell)
        Pump()
        Shot(reopened, "07-dialog-reopened-dark")
        CheckShot("07-dialog-reopened-dark", True)
        Check("ui-drive:dialog-reopened-themed", reopened.BackColor.ToArgb() = Theme.Current.Surface.ToArgb(),
              reopened.BackColor.ToString())
        reopened.Close()
        Pump()
        ShellSettings.SetThemeChoice("light")
        Pump()

        ' 7) Escape leaves the settings page (the companion-surface exit, APP-BEHAVIOUR rule 1).
        shell.OpenSettings()
        Pump()
        Foreground(shell)
        PressKey(shell, Keys.Escape)
        Pump()
        Dim selected = shell.RailEntriesForTest().FirstOrDefault(Function(e) e.Selected)
        Check("ui-drive:escape-leaves-settings", selected IsNot Nothing AndAlso selected.Key = "rail_job_command",
              If(selected Is Nothing, "none", selected.Key))

        ' 8) The real close path persists the page, the groups and the viewport (WINDOWS-UI 3.3).
        shell.OpenSettings()
        Pump()
        panel.AutoScrollPosition = New Point(0, 160)
        Pump()
        Dim appearanceWas = appearance.Expanded
        shell.Close()
        Pump()
        Check("ui-drive:last-page-saved", ShellSettings.LastPage() = "rail_job_settings", ShellSettings.LastPage())

        ' 9) A fresh shell restores the page, the groups and the viewport without running a job.
        Dim fresh As New ShellForm()
        fresh.Show()
        Pump()
        Dim selectedFresh = fresh.RailEntriesForTest().FirstOrDefault(Function(e) e.Selected)
        Check("ui-drive:reopen-last-page", selectedFresh IsNot Nothing AndAlso selectedFresh.Key = "rail_job_settings",
              If(selectedFresh Is Nothing, "none", selectedFresh.Key))
        Dim freshPanel = fresh.SettingsPanelForTest
        Dim freshAppearance = freshPanel.Groups.First(Function(g) g.GroupId = "appearance")
        Check("ui-drive:reopen-group-state", freshAppearance.Expanded = appearanceWas,
              "was " & appearanceWas.ToString() & ", reopened " & freshAppearance.Expanded.ToString())
        Pump()
        Dim scrolled = freshPanel.AutoScrollPosition.Y < 0
        Check("ui-drive:reopen-viewport", scrolled, "scroll " & freshPanel.AutoScrollPosition.Y.ToString())
        Shot(fresh, "08-shell-reopened-light")
        CheckShot("08-shell-reopened-light", False)
        fresh.Close()
        Pump()
    End Sub

End Module
