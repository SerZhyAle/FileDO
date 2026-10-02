' `--capture-screens <folder>` - the guide screenshots, rendered from the window itself (SP-0062 T6).
'
' DOC-EXTERNAL-QUALITY rule 6 asks a multi-step guide for screenshots of the current interface, in
' the reader's language or language-neutral. A capture taken by hand goes stale on the first label
' change and nobody notices; a capture the program renders of itself is re-taken by one command,
' so the pictures on the site are regenerated rather than remembered.
'
' Each page is built in each authored site language, drawn off-screen and saved as a PNG named
' gui-<page>-<site language>.png. The language and the theme are forced in process only: nothing
' is written to HKCU - not GuiLang, not the theme, not the window placement - because this runs on
' the maintainer's own machine and must leave their settings as it found them. The window is
' disposed without closing, which is what keeps OnFormClosing from saving its placement.
Module Capture

    ' The site authors ru/en/ua; the window calls Ukrainian "uk".
    Private ReadOnly SiteLanguages As String()() = {
        New String() {"en", "en"},
        New String() {"ru", "ru"},
        New String() {"uk", "ua"}
    }

    ' The pages the guides show, and the target a job page is opened on so that its check step
    ' has something to say.
    Private ReadOnly Pages As String()() = {
        New String() {"rail_job_info", "target-info", "C:\"},
        New String() {"rail_job_command", "command", ""},
        New String() {"rail_job_settings", "settings", ""}
    }

    ' The Disk manager's pictures (SP-0063, SP-0080): the window with its disks, the help, the first
    ' steps, the Autostart dialog. Built from a sample `vd status json` document, so a capture needs no
    ' disk, no task and no state.
    Private ReadOnly DiskPages As String() = {"disk-manager", "disk-help", "disk-welcome", "disk-autostart", "disk-settings"}

    ' Design pixels, so a capture is the same picture at any display scale.
    Private Const DesignWidth As Integer = 1180
    Private Const DesignHeight As Integer = 760

    ' 0 when every file was written, 1 otherwise. Each written path goes to stdout.
    Public Function Run(folder As String) As Integer
        Dim failed = False
        Try
            IO.Directory.CreateDirectory(folder)
            Theme.UsePaletteForTest(True)
            For Each lang In SiteLanguages
                ShellSettings.LanguageOverride = lang(0)
                Localization.ResetShellDict()
                For Each page In Pages
                    Dim file = IO.Path.Combine(folder, "gui-" & page(1) & "-" & lang(1) & ".png")
                    Try
                        CapturePage(page(0), page(2), file)
                        Console.WriteLine(file)
                    Catch ex As Exception
                        failed = True
                        ShellLog.Write("capture " & file, ex)
                        Console.Error.WriteLine("capture failed: " & file & " (" & ex.GetType().Name & ", see the shell log)")
                    End Try
                Next
                For Each page In DiskPages
                    Dim file = IO.Path.Combine(folder, "gui-" & page & "-" & lang(1) & ".png")
                    Try
                        CaptureDisk(page, file)
                        Console.WriteLine(file)
                    Catch ex As Exception
                        failed = True
                        ShellLog.Write("capture " & file, ex)
                        Console.Error.WriteLine("capture failed: " & file & " (" & ex.GetType().Name & ", see the shell log)")
                    End Try
                Next
            Next
        Catch ex As Exception
            failed = True
            ShellLog.Write("capture-screens", ex)
            Console.Error.WriteLine("capture failed (" & ex.GetType().Name & ", see the shell log)")
        Finally
            ShellSettings.LanguageOverride = Nothing
            Localization.ResetShellDict()
            Theme.UsePaletteForTest(Nothing)
        End Try
        Return If(failed, 1, 0)
    End Function

    ' One of the Disk manager's pictures. The window never writes the user's settings here: it is opened
    ' without its first run, and disposed without being closed, so its placement is not saved either.
    Private Sub CaptureDisk(kind As String, file As String)
        Dim dict = Localization.GetDict(ShellSettings.Language())
        DiskManagerForm.SuppressWelcome = True
        DiskManagerForm.SuppressReads = True
        Select Case kind
            Case "disk-manager"
                Dim m As New DiskManagerForm()
                Try
                    m.StartPosition = FormStartPosition.Manual
                    m.ShowInTaskbar = False
                    m.Bounds = New Rectangle(-32000, -32000, Ui.Px(m, 1100), Ui.Px(m, 640))
                    m.Show()
                    Dim problem As String = ""
                    Dim snap = DiskSnapshot.Parse(SampleSnapshotLine(), problem)
                    m.ApplySnapshotForTest(snap, "")
                    m.SelectForTest(snap.Disks.First(Function(d) d.Name = "secrets").Key)
                    For i = 1 To 6
                        Application.DoEvents()
                        Threading.Thread.Sleep(40)
                    Next
                    Using bmp As New Bitmap(m.ClientRectangle.Width, m.ClientRectangle.Height)
                        m.DrawClientForCapture(bmp)
                        ' A native list view does not print: the rows are drawn by the painters the list uses.
                        Using g = Graphics.FromImage(bmp)
                            m.PaintListForCapture(g, m.ListOriginForCapture())
                        End Using
                        bmp.Save(file, Imaging.ImageFormat.Png)
                    End Using
                Finally
                    m.Dispose()
                End Try
            Case "disk-settings"
                Using dlg As New DiskSettingsDialog()
                    dlg.ShowInTaskbar = False
                    dlg.Show()
                    For i = 1 To 5
                        Application.DoEvents()
                    Next
                    Using bmp As New Bitmap(dlg.ClientSize.Width, dlg.ClientSize.Height)
                        dlg.DrawClientForCapture(bmp)
                        bmp.Save(file, Imaging.ImageFormat.Png)
                    End Using
                End Using
            Case "disk-help"
                Using dlg As New DiskHelpDialog(dict, Nothing, False)
                    SaveDialog(dlg, file)
                End Using
            Case "disk-autostart"
                ' The sample machine's registered disks, and a guard that is on, running, and whose last
                ' run closed everything. The delegates that would act are inert: nothing runs here.
                Dim problem As String = ""
                Dim snap = DiskSnapshot.Parse(SampleSnapshotLine(), problem)
                ' Four rows say every shape a row takes - off, on, and refused with its reason (the vault).
                Dim shown As String() = {"scratch", "work", "secrets", "archive"}
                Dim records = snap.Disks.Where(Function(d) d.Registered AndAlso Not d.IsImage AndAlso shown.Contains(d.Name)).ToList()
                Dim guard As New DiskGuardState With {.Installed = True, .Running = True, .Ended = "session",
                                                      .LastRun = New DateTimeOffset(2026, 9, 29, 22, 41, 0, TimeSpan.FromHours(2))}
                guard.Containers.Add(New DiskGuardRunRow With {.Name = "scratch", .Action = "save", .Outcome = "saved", .BytesSaved = 188743680L})
                guard.Containers.Add(New DiskGuardRunRow With {.Name = "scratch", .Action = "unmount", .Outcome = "unmounted"})
                guard.Containers.Add(New DiskGuardRunRow With {.Name = "work", .Action = "unmount", .Outcome = "unmounted"})
                Using dlg As New DiskAutostartDialog(dict, Nothing,
                                                     Function() records, Function() guard,
                                                     Sub(a, rows)
                                                     End Sub,
                                                     Sub(turnOn)
                                                     End Sub)
                    SaveDialog(dlg, file)
                End Using
            Case Else
                Using dlg As New DiskWelcomeDialog(dict, Nothing, False)
                    SaveDialog(dlg, file)
                End Using
        End Select
    End Sub

    ' What the guide's picture of the window shows: a plausible machine, not the test fixture (whose paths
    ' are placeholders and whose mounts have no time). One line, the way `vd status json` prints it.
    Private Function SampleSnapshotLine() As String
        Const t As String = "2026-09-30T09:12:40+02:00"
        
        Dim disks As New List(Of String)
        disks.Add("{""kind"":""image"",""letter"":""I:"",""mounted_at"":""" & t & """,""path"":""D:\\ISO\\install.iso""}")
        disks.Add(SampleDisk("scratch", "D:\\Disks\\scratch.fdd", "ram", "obfuscated", 4294967296L, "R:", False, True, t, False, 188743680L))
        disks.Add(SampleDisk("old", "D:\\Disks\\old.fdd", "plain", "obfuscated", 10737418240L, "S:", False, False, t, False, -1L))
        disks.Add(SampleDisk("work", "D:\\Disks\\work.fdd", "plain", "obfuscated", 21474836480L, "W:", False, True, t, False, -1L))
        disks.Add(SampleDisk("secrets", "D:\\Vaults\\secrets.fdd", "vault", "encrypted", 5368709120L, "X:", True, True, "2026-09-30T09:15:02+02:00", False, -1L))
        disks.Add(SampleDisk("archive", "D:\\Disks\\archive.fdd", "fast", "obfuscated", 2147483648L, "", False, False, "", True, -1L))
        disks.Add("{""kind"":""container"",""name"":""backup"",""path"":""E:\\Backup\\backup.fdd"",""container_id"":""6b0a"",""registered"":true," &
                  """file"":""missing"",""profile"":""plain"",""protection"":"""",""logical_size"":53687091200,""clean"":null,""last_good_save"":null,""auto"":false,""mount"":null}")
        Return "{""schema"":""filedo.vd-status"",""version"":1,""at"":""" & t & """,""packaged"":false," &
               """transport"":{""ready"":true,""initiator_service"":""running"",""reason"":""""},""disks"":[" & String.Join(",", disks.ToArray()) & "]}"
    End Function

    Private Function SampleDisk(name As String, path As String, profile As String, protection As String, size As Long, letter As String,
                                readOnly_ As Boolean, alive As Boolean, mountedAt As String, auto As Boolean, dirty As Long) As String
        Dim mount = "null"
        If letter <> "" Then
            Dim ram = If(dirty >= 0, "{""dirty_bytes"":" & dirty.ToString() & ",""saving"":false,""last_good_save"":""2026-09-30T09:40:00+02:00""}", "null")
            mount = "{""letter"":""" & letter & """,""read_only"":" & readOnly_.ToString().ToLowerInvariant() & ",""mounted_at"":""" & mountedAt &
                    """,""server_alive"":" & alive.ToString().ToLowerInvariant() & ",""ram"":" & ram & "}"
        End If
        Return "{""kind"":""container"",""name"":""" & name & """,""path"":""" & path & """,""container_id"":""" & name & "-id"",""registered"":true," &
               """file"":""ok"",""profile"":""" & profile & """,""protection"":""" & protection & """,""logical_size"":" & size.ToString() &
               ",""clean"":true,""last_good_save"":""2026-09-29T21:10:00+02:00"",""auto"":" & auto.ToString().ToLowerInvariant() & ",""mount"":" & mount & "}"
    End Function

    Private Sub SaveDialog(dlg As DiskPageDialog, file As String)
        dlg.StartPosition = FormStartPosition.Manual
        dlg.ShowInTaskbar = False
        dlg.Location = New Point(-32000, -32000)
        dlg.Show()
        For i = 1 To 5
            Application.DoEvents()
            Threading.Thread.Sleep(40)
        Next
        Using bmp As New Bitmap(dlg.ClientRectangle.Width, dlg.ClientRectangle.Height)
            dlg.DrawClientForCapture(bmp)
            bmp.Save(file, Imaging.ImageFormat.Png)
        End Using
    End Sub

    Private Sub CapturePage(key As String, target As String, file As String)
        Dim shell As New ShellForm()
        Try
            shell.StartPosition = FormStartPosition.Manual
            shell.WindowState = FormWindowState.Normal
            shell.ShowInTaskbar = False
            shell.Bounds = New Rectangle(-32000, -32000, Ui.Px(shell, DesignWidth), Ui.Px(shell, DesignHeight))
            shell.Show()
            shell.ShowPageForCapture(key, target)
            For i = 1 To 5
                Application.DoEvents()
                Threading.Thread.Sleep(40)
            Next
            Dim client = shell.ClientRectangle
            Using bmp As New Bitmap(client.Width, client.Height)
                shell.DrawClientForCapture(bmp)
                bmp.Save(file, Imaging.ImageFormat.Png)
            End Using
        Finally
            shell.Dispose()
        End Try
    End Sub

End Module
