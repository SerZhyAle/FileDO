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
        New String() {"rail_job_command", "command", ""}
    }

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
