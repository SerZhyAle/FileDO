Imports System.IO
Imports System.Text

' `filedo_win.exe --selftest` - the shell's own gate.
'
' The window has no console and no test runner, so until now the only way to find out that a page
' threw on its way up was to click it. That is a poor gate for a page whose whole job is to write
' a command line correctly, and it is why ArgQuotingTests sat in the project unreferenced.
'
' The run builds every job page the catalogue knows, reads back the command each one would run,
' and checks the shape against what the CLI parses (cmd\filedo\main.go, command_handlers.go).
' It writes a line per check to filedo_win_selftest.log next to the exe, ends the log with a
' `selftest: PASS (n)` or `selftest: FAIL (n): names` line, and exits 0 or 1 - or 2 when the log
' cannot be written, because a verdict nobody can read proves nothing.
Partial Public Module SelfTest

    Private ReadOnly report As New StringBuilder()
    Private failures As Integer = 0

    Public Function Run() As Integer
        report.Clear()
        failures = 0

        ' The failures some rows provoke on purpose are logged; they go to a file of the self-test's
        ' own, never to the user's filedo_win.log.
        Dim testLog = Path.Combine(Path.GetTempPath(), "filedo_selftest_shell_" & Guid.NewGuid().ToString("N") & ".log")
        ShellLog.PathForTest = testLog
        ShellSettings.ValuesForTest = New Dictionary(Of String, Object)()
        ' A Disks page reads its container through filedo.exe; the self-test never starts filedo.exe.
        DiskProbe.Enabled = False
        Try
            RunAll()
        Finally
            DiskProbe.Enabled = True
            ShellSettings.ValuesForTest = Nothing
            Packaging.OverrideForTest = Nothing
            ShellLog.PathForTest = Nothing
            Try
                File.Delete(testLog)
            Catch
            End Try
        End Try
        Return WriteLog()
    End Function

    Private Sub CheckSettingsTray()
        Dim exe = "C:\Program Files\FileDO\filedo_win.exe"
        For Each pair In New String()() {New String() {"shell", " --startup"}, New String() {"manager", " --startup --disks"}, New String() {"tray", " --startup --tray"}}
            Check("settings:startup:" & pair(0), StartupLaunch.Command(exe, pair(0)) = """" & exe & """" & pair(1), "")
        Next
        Check("settings:startup:off", StartupLaunch.Command(exe, "off") Is Nothing AndAlso StartupLaunch.Command(exe, "invalid") Is Nothing, "")
        Check("tray:manual-visible", Not StartupLaunch.HiddenStart({"filedo_win.exe", "--tray"}), "")
        Check("tray:startup-hidden", StartupLaunch.HiddenStart({"filedo_win.exe", "--STARTUP", "--TRAY"}), "")
        For Each packaged In New Boolean() {False, True}
            For Each mounted In New Integer() {0, 2}
                For Each jobs In New Integer() {0, 1}
                    Dim keys = TrayIcon.MenuKeys(packaged, mounted, jobs)
                    Check("tray:unmount:" & packaged & ":" & mounted & ":" & jobs, keys.Contains("tray_unmount") = (Not packaged AndAlso mounted > 0), "")
                    Check("tray:stop:" & packaged & ":" & mounted & ":" & jobs, keys.Contains("tray_stop") = (jobs > 0), "")
                    Check("tray:exit-last:" & packaged & ":" & mounted & ":" & jobs, keys.Last() = "tray_exit" AndAlso keys(0) = "tray_open" AndAlso keys(1) = "tray_manager", "")
                    Check("tray:no-destructive:" & packaged & ":" & mounted & ":" & jobs, Not keys.Any(Function(k) k.Contains("wipe") OrElse k.Contains("destroy") OrElse k.Contains("format")), "")
                Next
            Next
        Next
        Check("tray:menu-order", String.Join(",", TrayIcon.MenuKeys(False, 1, 1)) = "tray_open,tray_manager,-,tray_unmount,tray_stop,-,settings_startup,tray_exit", "")
        Check("tray:exit-with-job-asks", DiskManagerForm.DecisionOnClose(True, False, False) = DiskManagerForm.CloseDecision.Ask, "")
        ShellSettings.SavePlacementOf(ShellSettings.ShellPrefix, 10, 10, 900, 600, False, 96)
        ShellSettings.ResetPlacements()
        ShellSettings.SavePlacementOf(ShellSettings.ShellPrefix, 10, 10, 900, 600, False, 96)
        Check("settings:reset-survives-close", Not ShellSettings.LoadPlacement().HasValue, "")
        ShellSettings.SavePlacementOf(ShellSettings.ShellPrefix, 10, 10, 900, 600, False, 96)
        Check("settings:placement-after-reset", ShellSettings.LoadPlacement().HasValue, "")
        Check("tray:presence", Not TrayIcon.ShouldShow(False, 0) AndAlso TrayIcon.ShouldShow(True, 0) AndAlso TrayIcon.ShouldShow(False, 1), "")
        For Each lang In Localization.Languages
            Check("settings:locale:" & lang, SettingsPanel.NewKeys.All(Function(k) Localization.OwnKeysForTest(lang).Contains(k)), "")
        Next
        For Each packaged In New Boolean() {False, True}
            Packaging.OverrideForTest = packaged
            Using page As New SettingsView(), dialog As New DiskSettingsDialog()
                Check("settings:shared-panel:" & packaged, page.Panel.GetType() Is dialog.Panel.GetType(), "")
                Check("settings:autoplay-button:" & packaged,
                      page.Panel.AutoPlayButton.Text = Localization.T("settings_autoplay_disable") AndAlso
                      dialog.Panel.AutoPlayButton.Text = Localization.T("settings_autoplay_disable"), "")
                Check("settings:autoplay-packaged:" & packaged, page.Panel.AutoPlayButton.Visible = Not packaged, "")
                Check("settings:packaged-control:" & packaged, Not packaged = page.Panel.StartupCombo.Visible, "")
                ShellSettings.SetMinimizeToTray(True)
                ShellSettings.SetFinishNotification(False)
                ShellSettings.SetAutostart("manager")
                Check("settings:shared-values:" & packaged, page.Panel.StartupCombo.SelectedIndex = 2 AndAlso dialog.Panel.StartupCombo.SelectedIndex = 2, "")
                Check("settings:notification", Not ShellSettings.FinishNotification(), "")
                Check("settings:rule12", dialog.CancelButton IsNot Nothing AndAlso DirectCast(dialog.CancelButton, Button).Text = Localization.T("settings_close"), "")
                For Each dark In New Boolean() {False, True}
                    Theme.UsePaletteForTest(dark)
                    dialog.RepaintTheme()
                    Check("theme:settings-dialog:" & packaged & ":" & dark, dialog.BackColor.ToArgb() = Theme.Current.Background.ToArgb(), "")
                Next
            End Using
        Next
        Packaging.OverrideForTest = Nothing
        Theme.UsePaletteForTest(Nothing)
        ShellSettings.LoadPlacementOf(ShellSettings.DiskManagerPrefix)
        ShellSettings.ValuesForTest.Clear()
        CheckTrayUnmountAll()
    End Sub

    ' AUD-88-F3 and AUD-85-F3: the tray's Unmount all, and the count beside it, cover the containers the
    ' Disk Manager owns and that Unmount accepts now (SP-0081 5) - never an ISO or VHD the user mounted
    ' in Explorer, and never a row with an operation running or queued, which every other gesture
    ' leaves alone.
    Private Sub CheckTrayUnmountAll()
        Dim m As DiskManagerForm = Nothing
        Try
            Dim snap = GoldenSnapshot()
            Dim ctx As New DiskContext()
            Dim none As New Dictionary(Of String, String)(StringComparer.Ordinal)
            Dim containers = snap.Disks.Where(Function(d) d.IsMounted AndAlso Not d.IsImage).ToList()
            Dim images = snap.Disks.Where(Function(d) d.IsMounted AndAlso d.IsImage).ToList()
            Check("tray:unmount-all:fixture", containers.Count = 5 AndAlso images.Count = 1, containers.Count.ToString() & " containers, " & images.Count.ToString() & " images")

            Dim targets = DiskManagerForm.UnmountAllTargets(snap, none, ctx)
            Check("tray:unmount-all-skips-images", targets.Count = containers.Count AndAlso Not targets.Any(Function(r) r.IsImage),
                  String.Join(",", targets.Select(Function(r) r.Letter).ToArray()))

            ' Only an image is mounted: nothing to unmount and no count.
            Dim imageOnly As New DiskSnapshot()
            imageOnly.Disks.Add(New DiskRecord With {.Kind = "image", .Path = "C:\sample\disc.iso", .Letter = "I:", .ServerAlive = True})
            Check("tray:unmount-all-image-only", DiskManagerForm.UnmountAllTargets(imageOnly, none, ctx).Count = 0, "")

            ' A row with an unmount running or queued takes no second one.
            Dim busyNow As New Dictionary(Of String, String)(StringComparer.Ordinal)
            busyNow(containers(0).Key) = "unmount"
            targets = DiskManagerForm.UnmountAllTargets(snap, busyNow, ctx)
            Check("tray:unmount-all-skips-busy", targets.Count = containers.Count - 1 AndAlso Not targets.Any(Function(r) r.Key = containers(0).Key),
                  targets.Count.ToString())

            ' The Store build cannot unmount at all.
            Check("tray:unmount-all-packaged", DiskManagerForm.UnmountAllTargets(snap, none, New DiskContext With {.Packaged = True}).Count = 0, "")

            ' The window's own answers, which the tray's tooltip and its menu entry read.
            m = New DiskManagerForm()
            m.ApplySnapshotForTest(snap, "")
            Check("tray:count-containers-only", m.MountedCount = containers.Count, m.MountedCount.ToString())
            Check("tray:window-targets", m.UnmountAllTargetsForTest().Count = containers.Count, m.UnmountAllTargetsForTest().Count.ToString())
            m.SetBusyForTest(containers(0).Key, "unmount")
            Check("tray:window-skips-busy", m.UnmountAllTargetsForTest().Count = containers.Count - 1, m.UnmountAllTargetsForTest().Count.ToString())
            m.SetBusyForTest(containers(0).Key, "")
            m.ApplySnapshotForTest(imageOnly, "")
            Check("tray:count-image-only", m.MountedCount = 0 AndAlso m.UnmountAllTargetsForTest().Count = 0 AndAlso
                                           Not TrayIcon.MenuKeys(False, m.MountedCount, 0).Contains("tray_unmount"), m.MountedCount.ToString())
        Catch ex As Exception
            Check("tray:unmount-all", False, ex.GetType().Name & ": " & ex.Message)
        Finally
            If m IsNot Nothing Then m.Dispose()
        End Try
    End Sub

    Private Sub RunAll()
        ' SHELL-15: every group of rows runs inside Guard, so a check that throws is a FAIL row with
        ' the exception's type - and the rows after it still run - instead of a process that dies
        ' before the log is written.
        Guard("ArgQuoting", Sub() Check("ArgQuoting", ArgQuotingTests.RunTests()))
        Guard("argquoting", AddressOf CheckArgQuotingCases)
        Guard("shortcut", AddressOf CheckShortcut)
        Guard("settings-tray", AddressOf CheckSettingsTray)
        Guard("catalogue", AddressOf CheckCatalogue)
        Guard("page", AddressOf CheckJobPages)
        Guard("expert", AddressOf CheckExpertPage)
        Guard("rail", AddressOf CheckRailTargets)
        Guard("dpi", AddressOf CheckDpiFonts)
        Guard("verdict", AddressOf CheckVerdictTable)
        Guard("glyph", AddressOf CheckVerdictGlyphs)

        ' SP-0016: the iconography contracts - the vendored drawings, the rail's glyph map, the shared
        ' state tones, and the contrast of every glyph against the surface it is drawn on.
        Guard("icons", AddressOf CheckGlyphProvenance)
        Guard("rail-glyph", AddressOf CheckRailGlyphs)
        Guard("rail-accordion", AddressOf CheckRailAccordion)
        Guard("menu-icon", AddressOf CheckMenuIcons)
        Guard("state-tone", AddressOf CheckStateTones)
        Guard("contrast", AddressOf CheckGlyphContrast)

        Guard("tailer", AddressOf CheckEventStreamTailer)
        Guard("tailer-long-line", AddressOf CheckEventStreamLongLine)
        Guard("sample", AddressOf CheckEventStreamSample)
        Guard("fdsec", AddressOf CheckFdsecReportRedaction)

        ' SP-0014: the shared desktop contracts APP-BEHAVIOUR and APP-STYLE, rung by rung.
        Guard("palette", AddressOf CheckPaletteCompleteness)
        Guard("theme", AddressOf CheckThemeRoundTrip)
        Guard("disabled-caption", AddressOf CheckDisabledCaptions)
        Guard("rail-paint", AddressOf CheckRailPaint)
        Guard("rail-label", AddressOf CheckRailLabels)
        Guard("progress", AddressOf CheckProgressRule)
        Guard("format", AddressOf CheckLocalizedFormat)
        Guard("a11y", AddressOf CheckAccessibleNames)
        Guard("placement", AddressOf CheckPlacement)
        Guard("fit", AddressOf CheckWindowFit)
        Guard("wipe", AddressOf CheckWipeRules)
        Guard("dialog", AddressOf CheckDialogEscape)
        Guard("dialog-destructive", AddressOf CheckDestructiveDialogDefaults)

        ' SP-0029: the shell's robustness remediation, ticket by ticket.
        Guard("target", AddressOf CheckTargetRules)
        Guard("dup", AddressOf CheckDuplicatesPage)
        Guard("cmp", AddressOf CheckComparePage)
        Guard("command-cred", AddressOf CheckCommandCredential)
        Guard("command-copy", AddressOf CheckCommandCopy)
        Guard("redirect", AddressOf CheckRedirectNotice)
        Guard("runner", AddressOf CheckRunnerChild)
        Guard("runner-start", AddressOf CheckStartFailure)
        Guard("report", AddressOf CheckReportRedaction)
        Guard("history", AddressOf CheckHistory)
        Guard("output", AddressOf CheckOutputBounds)
        Guard("params", AddressOf CheckParameters)
        Guard("check-options", AddressOf CheckCheckOptions)
        Guard("clean", AddressOf CheckCleanCarriesTarget)
        Guard("cause", AddressOf CheckCauses)
        Guard("duration", AddressOf CheckDurationFormat)
        Guard("about", AddressOf CheckAboutStamp)
        Guard("logs", AddressOf CheckLogArchives)
        Guard("diagnostic", AddressOf CheckDiagnosticExport)
        Guard("diagnostic-lists", AddressOf CheckNoFileListInArchive)

        ' SP-0004 P6: the Disks jobs - the command of every page, the credential kept out of every
        ' line, the elevation split, the double-click routes, a mount outliving the window, and the
        ' five locales holding the same keys.
        Guard("disk-cmd", AddressOf CheckDiskCommands)
        Guard("disk-run", AddressOf CheckDiskRunRules)
        Guard("disk-facts", AddressOf CheckDiskFacts)
        Guard("disk-result", AddressOf CheckDiskResults)
        Guard("disk-route", AddressOf CheckDiskRoutes)
        Guard("disk-outlives", AddressOf CheckMountOutlivesWindow)

        ' SP-0063: the Disk Manager - the snapshot's reader over the golden document, the states and
        ' their precedence, the action matrix over every combination, the quick actions' lines, the
        ' window itself, and the pieces around it.
        Guard("disk-snap", AddressOf CheckDiskSnapshot)
        Guard("disk-state", AddressOf CheckDiskStates)
        Guard("disk-matrix", AddressOf CheckDiskMatrix)
        Guard("disk-quick", AddressOf CheckDiskQuick)
        ' SP-0080: the Autostart dialog - the guard's words, the state matrix, the last-run
        ' rendering, the packaged build's hidden entry.
        Guard("disk-auto", AddressOf CheckDiskAutostart)
        Guard("disk-auto-open", AddressOf CheckDiskAutostartOpens)
        ' SP-0121: the shared-disk words, the holder's tokens and the refusal that follows from them.
        Guard("disk-share", AddressOf CheckDiskShare)
        Guard("disk-share-manager", AddressOf CheckDiskShareManager)
        ' SP-0148: partition disks - the disk list's reader, the partition lines, the refusals, the
        ' disk map at every scale, the new-partition and delete dialogs, the Store build's absence.
        Guard("disk-part", AddressOf CheckDiskPartitions)
        Guard("disk-mgr", AddressOf CheckDiskManagerWindow)
        Guard("disk-host", AddressOf CheckDiskHost)
        Guard("disk-ui", AddressOf CheckDiskUi)
        Guard("locale-keys", AddressOf CheckLocaleKeySets)
    End Sub

    ' FILEDO_SELFTEST_ONLY=page,expert runs just the groups whose names start with one of the words
    ' (a developer's loop; the full run takes minutes). Such a run is never a pass: the verdict says
    ' PARTIAL and the exit code is 2, "could not verify", so a gate cannot mistake it for one.
    Private ReadOnly onlyGroups As String() =
        If(Environment.GetEnvironmentVariable("FILEDO_SELFTEST_ONLY"), "").
            Split(New Char() {","c, ";"c, " "c}, StringSplitOptions.RemoveEmptyEntries)

    Private Sub Guard(name As String, body As Action)
        If onlyGroups.Length > 0 AndAlso Not onlyGroups.Any(Function(o) name.StartsWith(o, StringComparison.OrdinalIgnoreCase)) Then Return
        ' A TIME line per group says where the run's minutes go; it is no check and is not counted.
        Dim clock = Diagnostics.Stopwatch.StartNew()
        Try
            body()
        Catch ex As Exception
            Check(name, False, "threw " & ex.GetType().Name & ": " & ex.Message)
        End Try
        report.AppendLine("TIME " & name & " - " & clock.ElapsedMilliseconds.ToString() & " ms")
    End Sub

    Private Sub CheckShortcut()
        Dim args As List(Of String) = Nothing
        Check("shortcut:empty", ShortcutWriter.ParseLine("filedo.exe", args) = "shortcut_empty")
        Check("shortcut:quote", ShortcutWriter.ParseLine("filedo.exe ""unfinished", args) = "shortcut_parse")
        Check("shortcut:password", ShortcutWriter.ParseLine("filedo.exe secure p:private-value", args) = "shortcut_password")
        Check("shortcut:shell-credential", ShortcutWriter.ParseLine("filedo.exe secure pe:FILEDO_SHELL_CRED", args) = "shortcut_shell_cred")
        Check("shortcut:external-variable", ShortcutWriter.ParseLine("filedo.exe secure pe:SOMEVAR", args) = "")
        Check("shortcut:pause", Not ShortcutWriter.HasPause(args))
        Check("shortcut:name", ShortcutWriter.DefaultName(New String() {"C:\a folder", "info"}).StartsWith("FileDO - info "))
        Check("shortcut:existing-pause", ShortcutWriter.HasPause(New String() {"info", "--pause"}))
        Check("shortcut:add-pause", ShortcutWriter.SavedArgs(New String() {"info"}, True).SequenceEqual(New String() {"info", "--pause"}))
        Check("shortcut:single-pause", ShortcutWriter.SavedArgs(New String() {"info", "--pause"}, True).Count = 2)
        Check("shortcut:leave-closed", ShortcutWriter.SavedArgs(New String() {"info"}, False).SequenceEqual(New String() {"info"}))
        Check("shortcut:sanitize", ShortcutWriter.CleanName("a<>:b. ") = "a---b")
        Dim folder = Path.Combine(Path.GetTempPath(), "filedo-shortcut-test-" & Guid.NewGuid().ToString("N"))
        Directory.CreateDirectory(folder)
        Try
            File.WriteAllText(Path.Combine(folder, "FileDO - check.lnk"), "keep")
            Check("shortcut:collision", Path.GetFileName(ShortcutWriter.AvailablePath(folder, "FileDO - check")) = "FileDO - check (2).lnk")
            Dim exe = Path.Combine(folder, "filedo.exe")
            Dim icon = Path.Combine(folder, "FileDO.ico")
            File.WriteAllBytes(exe, New Byte() {0})
            File.WriteAllBytes(icon, New Byte() {0})
            Dim line = "filedo.exe ""C:\a folder\file.txt"" info --pause"
            Check("shortcut:parse", ShortcutWriter.ParseLine(line, args) = "" AndAlso args.Count = 3)
            Dim encoded = ArgQuoting.JoinArgs(args)
            Dim working = Runner.GetAppDataDir()
            Dim link = ShortcutWriter.Create(folder, "FileDO - check", exe, encoded, working, icon)
            Dim read = ShortcutWriter.ReadBack(link)
            Check("shortcut:round-trip", String.Equals(read(0), exe, StringComparison.OrdinalIgnoreCase) AndAlso
                  read(1) = encoded AndAlso String.Equals(read(2), working, StringComparison.OrdinalIgnoreCase) AndAlso
                  String.Equals(read(3), icon, StringComparison.OrdinalIgnoreCase))
            Dim bytes = File.ReadAllBytes(link)
            Check("shortcut:no-credential", Not Encoding.Default.GetString(bytes).Contains("private-value") AndAlso
                  Not Encoding.Unicode.GetString(bytes).Contains("private-value"))
            Check("shortcut:preserve", File.ReadAllText(Path.Combine(folder, "FileDO - check.lnk")) = "keep")
        Finally
            Directory.Delete(folder, True)
        End Try
    End Sub

    Private Function LogFilePath() As String
        Return Path.Combine(AppDomain.CurrentDomain.BaseDirectory, "filedo_win_selftest.log")
    End Function

    Private Function WriteLog() As Integer
        Dim logFile = LogFilePath()
        Dim verdict As String = If(failures = 0,
                                   If(onlyGroups.Length > 0,
                                      "selftest: PARTIAL (" & CountChecks().ToString() & ", only " & String.Join(",", onlyGroups) & ")",
                                      "selftest: PASS (" & CountChecks().ToString() & ")"),
                                   "selftest: FAIL (" & failures.ToString() & "): " & FailureNames())
        report.AppendLine(verdict)
        Try
            File.WriteAllText(logFile, report.ToString())
        Catch ex As Exception
            Console.Error.WriteLine("selftest: COULD NOT VERIFY (cannot write " & logFile & ": " & ex.Message & ")")
            Return 2
        End Try

        If failures <> 0 Then Return 1
        Return If(onlyGroups.Length > 0, 2, 0)
    End Function

    ' The last resort of Program.Main: something outside every Guard threw. The rows written so far
    ' and the crash still reach the log, and the gate fails rather than going silent.
    Public Function WriteCrash(ex As Exception) As Integer
        Try
            Check("selftest-crash", False, If(ex Is Nothing, "unknown", ex.GetType().Name & ": " & ex.Message))
            Return WriteLog()
        Catch
            Return 2
        End Try
    End Function

    Private Sub Check(name As String, ok As Boolean, Optional detail As String = "")
        If Not ok Then failures += 1
        report.AppendLine(If(ok, "PASS ", "FAIL ") & name & If(detail = "", "", " - " & detail))
    End Sub

    Private Function CountChecks() As Integer
        Return report.ToString().Split(New String() {vbCrLf, vbLf}, StringSplitOptions.RemoveEmptyEntries).
            Count(Function(line) line.StartsWith("PASS ") OrElse line.StartsWith("FAIL "))
    End Function

    Private Function FailureNames() As String
        Dim names As New List(Of String)()
        For Each line In report.ToString().Split(New String() {vbCrLf, vbLf}, StringSplitOptions.RemoveEmptyEntries)
            If line.StartsWith("FAIL ") Then names.Add(line.Substring(5).Split(" "c)(0))
        Next
        Return String.Join(", ", names.ToArray())
    End Function

    ' Every rail row that leads to a job page must have a catalogue entry, and every catalogue
    ' entry must have a label, a purpose and a verb. A missing key shows up as the key itself on
    ' screen, in one locale, on one page - exactly the kind of thing nobody clicks through to.
    Private Sub CheckCatalogue()
        Dim dict = Localization.GetDict("en")
        For Each job In JobCatalogue.GetAllJobs()
            Check("catalogue:" & job.Id & ":verb", Not String.IsNullOrEmpty(job.DefaultVerb))
            Check("catalogue:" & job.Id & ":label", dict.ContainsKey(job.LabelKey))
            Check("catalogue:" & job.Id & ":purpose", dict.ContainsKey(job.PurposeKey))
        Next
    End Sub

    ' Each page is built, given a target, and asked what it would run. The command has to start
    ' with filedo.exe, name the job's verb, and carry the target - the three things that are
    ' wrong when a page has been given a control the command builder does not know about.
    ' One JobView serves every job, as the window's does: SetJob resets it (ResetView), and building a
    ' fresh page per job was two thirds of this group's time.
    Private Sub CheckJobPages()
        Dim view As New JobView()
        Try
            CheckJobPagesWith(view)
        Finally
            view.Dispose()
        End Try
    End Sub

    Private Sub CheckJobPagesWith(view As JobView)
        For Each job In JobCatalogue.GetAllJobs()
            Try
                view.SetJob(job)
                view.SetTarget(SampleTargetFor(job))

                Dim cmd = view.CurrentCommand()
                Check("page:" & job.Id & ":builds", cmd.StartsWith("filedo.exe "), cmd)
                Check("page:" & job.Id & ":verb", CommandNames(cmd, job.DefaultVerb), cmd)
                ' A job with no target (the Disks list) shows no step 2 and carries none.
                If job.TargetKind = JobDefinition.TargetType.None Then
                    Check("page:" & job.Id & ":no-target", Not view.TargetCardShownForTest AndAlso Not cmd.Contains(SampleTargetFor(job)), cmd)
                Else
                    Check("page:" & job.Id & ":target", cmd.Contains(SampleTargetFor(job)), cmd)
                End If

                ' SP-0025 FDSEC-19: filedo.exe refuses a pe: variable that is empty, so an
                ' empty password - the page's documented obfuscation-only choice - travels as
                ' the visible p:, and a real one by variable name.
                If job.DefaultVerb = "secure" OrElse job.DefaultVerb = "unsecure" OrElse job.DefaultVerb = "reveal" Then
                    view.SetCredentialForTest("")
                    Dim emptyCmd = view.CurrentCommand()
                    Check("page:" & job.Id & ":empty-cred-is-p", emptyCmd.EndsWith(" p:") AndAlso Not emptyCmd.Contains("pe:"), emptyCmd)
                    view.SetCredentialForTest("selftest-pw")
                    Dim credCmd = view.CurrentCommand()
                    Check("page:" & job.Id & ":cred-by-variable", credCmd.Contains("pe:FILEDO_SHELL_CRED") AndAlso Not credCmd.Contains("selftest-pw"), credCmd)
                End If
            Catch ex As Exception
                Check("page:" & job.Id, False, ex.GetType().Name & ": " & ex.Message)
            End Try
        Next
    End Sub

    ' The expert page: every operation in its dropdown has to write a command and explain itself.
    ' An explanation that comes back as its own key - "op_probe" rather than a sentence - is the
    ' failure this catches, and it is the one that used to reach users: the page shipped with ten
    ' operations while the other thirteen sat translated and unreachable.
    Private Sub CheckExpertPage()
        Dim view As CommandView = Nothing
        Try
            view = New CommandView()
            Check("expert:operations", view.OperationCount() >= 20, view.OperationCount().ToString())

            ' A password no user would type, so that finding it in a command line is unambiguous.
            Const secret As String = "selftest-pa55phrase-not-a-real-one"
            view.SetCredentialForTest(secret)

            For i = 0 To view.OperationCount() - 1
                Dim op = view.SelectOperation(i)
                view.SetCredentialForTest(secret)
                Dim cmd = view.CurrentCommandLine()
                Dim hint = view.CurrentHint()
                Check("expert:" & op & ":builds", cmd.StartsWith("filedo.exe "), cmd)
                Check("expert:" & op & ":names", cmd.Contains(op.Split(" "c)(0)), cmd)
                Check("expert:" & op & ":explained", Not hint.StartsWith("op_"), hint)

                ' The rule this page lives by: a password is never on the line, and the five
                ' verbs that need one name the variable it travels in instead.
                Check("expert:" & op & ":no-secret", Not cmd.Contains(secret), cmd)
                If IsCredentialOp(op) Then
                    Check("expert:" & op & ":by-variable", cmd.Contains("pe:FILEDO_SHELL_CRED"), cmd)
                End If
            Next
        Catch ex As Exception
            Check("expert", False, ex.GetType().Name & ": " & ex.Message)
        Finally
            If view IsNot Nothing Then view.Dispose()
        End Try
    End Sub

    ' SP-0016 T4, the mixed-DPI defect: a font is made for the window it is shown in, so the same
    ' nominal size is larger on a display whose scaling is larger than the system's and smaller on
    ' one that is smaller, and a control with no window yet is sized for the window in front.
    Private Sub CheckDpiFonts()
        Dim sys = Theme.SystemDpi()
        Check("dpi:same-as-system-is-nominal", Math.Abs(Theme.ScaledPoints(10.0F, sys) - 10.0F) < 0.001F, sys.ToString())
        Check("dpi:double-is-double", Math.Abs(Theme.ScaledPoints(10.0F, sys * 2) - 20.0F) < 0.001F)
        Check("dpi:half-is-half", Math.Abs(Theme.ScaledPoints(10.0F, sys \ 2) - 5.0F) < 0.06F)
        Check("dpi:garbage-falls-back-to-system", Math.Abs(Theme.ScaledPoints(10.0F, 0) - 10.0F) < 0.001F)

        Dim was = Theme.CurrentDpi
        Try
            Using small = Theme.FontBody(96), large = Theme.FontBody(192)
                Check("dpi:font-follows-dpi", Math.Abs(large.SizeInPoints / small.SizeInPoints - 2.0F) < 0.01F,
                      small.SizeInPoints.ToString() & " / " & large.SizeInPoints.ToString())
            End Using
            Theme.CurrentDpi = 192
            Using c As New Label()
                Check("dpi:px-of-unparented-control-uses-current", Ui.Px(c, 10) = 20, Ui.Px(c, 10).ToString())
            End Using
            Theme.CurrentDpi = 96
            Using c As New Label()
                Check("dpi:px-at-96", Ui.Px(c, 10) = 10, Ui.Px(c, 10).ToString())
            End Using
            Check("dpi:px-of-nothing", Ui.Px(Nothing, 10) = 10)
        Finally
            Theme.CurrentDpi = was
        End Try
    End Sub

    ' SP-0016 T4: both the group headers and the selectable rows use the Windows 44 px target.
    ' ShellForm applies this design-pixel value through Ui.Px, so it scales together with the form.
    Private Sub CheckRailTargets()
        Check("rail:target-height", ShellForm.RailTargetHeight >= 44, ShellForm.RailTargetHeight.ToString())
    End Sub

    ' SP-0005 section 12: a reveal's sealed true name is allowed on screen,
    ' but never in the shell's retained report.  Exercise every output shape
    ' that names a copy - reveal's "Revealed" line and indented fallback,
    ' unsecure start's "OK" line with the name repeated in parentheses - in
    ' both line endings, and leave ordinary output and the sandbox intact.
    Private Sub CheckFdsecReportRedaction()
        Const sealedName As String = "fdsec report (name) token.txt"
        Const box As String = "C:\Users\user\AppData\Local\FileDO\reveal\"
        Dim lines = {
            "Revealed C:\box.fd-sec -> " & box & "rv-test\" & sealedName & " (12 B)",
            "  " & box & "rv-test\" & sealedName,
            "OK C:\box.fd-sec -> " & box & "us-test\" & sealedName & " (" & sealedName & ", 12 B)",
            "ordinary output"}
        For Each eol In {vbLf, vbCrLf}
            Dim cleaned = Runner.RedactReportOutput(String.Join(eol, lines) & eol)
            Dim ok = Not cleaned.Contains("report (name)") AndAlso
                     cleaned.Contains(box & "rv-test\<protected reveal copy>" & eol) AndAlso
                     cleaned.Contains(box & "us-test\<protected reveal copy>" & eol) AndAlso
                     cleaned.Contains(eol & "ordinary output" & eol)
            Check("fdsec:report-redaction" & If(eol = vbLf, "", "-crlf"), ok, cleaned)
        Next
    End Sub

    ' Rung 2 of the CLI-EVENT-STREAM conformance ladder: every (verdict, exit
    ' code, stop-file, channel version) combination the shell can meet, and the
    ' verdict it must produce. Both ways of reaching *Not proven* are here,
    ' because inferring success from a process that merely exited is the one
    ' failure this channel exists to prevent.
    Private Sub CheckVerdictTable()
        Dim words = New String() {"Passed", "Failed", "Done", "Stopped", "Not proven"}
        For Each word In words
            For Each code In New Integer() {0, 1, 2}
                For Each stopped In New Boolean() {False, True}
                    CheckVerdictCase(word & "/" & code.ToString() & "/stop=" & stopped.ToString(), code, word, stopped, 0)
                Next
            Next
        Next

        ' The values outside the 30 normal triples name the degradation cases
        ' in the rule text: no result, unknown token, foreign code and refusal.
        CheckVerdictCase("no-result", 0, Nothing, False, 0)
        CheckVerdictCase("unknown-word", 0, "Quarantined", False, 0)
        CheckVerdictCase("wrong-case", 0, "passed", False, 0)
        CheckVerdictCase("foreign-code-3", 3, "Failed", False, 0)
        CheckVerdictCase("foreign-code-minus-1", -1, "Passed", False, 0)
        CheckVerdictCase("refused-version", 0, "Passed", False, EventStream.KnownSchemaVersion + 1)

        ' AUD-66-F1: the (Failed, 1, stop file present) triple, pinned by its
        ' literal answer rather than by the oracle above, so the oracle and
        ' Judge cannot drift together back to Stopped.
        Dim stopReason As String = ""
        Dim stopGot = Runner.Judge(1, New EventStream.ResultInfo With {.Verdict = "Failed"}, True, 0, stopReason)
        Check("verdict:defect-before-stop", stopGot = "Failed", stopGot & " (want Failed, reason " & stopReason & ")")
    End Sub

    ' The expected verdict is the contract text in executable form, used for
    ' every row rather than a hand-picked subset of the possible triples.
    Private Function ExpectedVerdict(word As String, code As Integer, stopped As Boolean, refusedVersion As Integer) As String
        If refusedVersion > EventStream.KnownSchemaVersion Then Return "Not proven"
        If String.IsNullOrEmpty(word) Then Return If(stopped, "Stopped", "Not proven")
        ' Rule 15 (0.11): the stop file overrides every result but Failed - a
        ' defect recorded before the stop outranks it.
        If stopped AndAlso word <> "Failed" Then Return "Stopped"
        Dim expected As Integer
        Select Case word
            Case "Passed", "Done" : expected = 0
            Case "Failed" : expected = 1
            Case "Not proven" : expected = 2
            Case "Stopped" : Return "Stopped"
            Case Else : Return "Not proven"
        End Select
        If code < 0 OrElse code > 2 Then Return word
        Return If(code = expected, word, "Not proven")
    End Function

    Private Sub CheckVerdictCase(label As String, code As Integer, word As String, stopped As Boolean, refusedVersion As Integer)
        Dim info As EventStream.ResultInfo = Nothing
        If word IsNot Nothing Then info = New EventStream.ResultInfo With {.Verdict = word}
        Dim reason As String = ""
        Dim got = Runner.Judge(code, info, stopped, refusedVersion, reason)
        Dim want = ExpectedVerdict(word, code, stopped, refusedVersion)
        Check("verdict:" & label, got = want, got & " (want " & want & ", reason " & reason & ")")
    End Sub

    ' ICON-SET / ICON-RENDER (SP-0016 T2): each verdict of CLI-EVENT-STREAM, plus one this build
    ' does not know, shows its state glyph in its state colour. Passed, Done and Failed draw the
    ' vocabulary's status.ok and status.error, Stopped and Not proven status.stopped and
    ' status.not-proven (ICON-SET 0.14) - every verdict a vocabulary drawing. The plain check (the
    ' vocabulary's action.confirm, Segoe E73E) and the plain cross (nav.close, E711) are the two
    ' pictures listed as distinct from status.ok and status.error, so neither may come back.
    Private Sub CheckVerdictGlyphs()
        Dim p = Theme.Current
        Dim cases = New Object()() {
            New Object() {"Passed", "status.ok", p.StateOk},
            New Object() {"Done", "status.ok", p.StateOk},
            New Object() {"Failed", "status.error", p.StateError},
            New Object() {"Stopped", "status.stopped", p.StateWarning},
            New Object() {"Not proven", "status.not-proven", p.MutedText},
            New Object() {"Quarantined", "status.not-proven", p.MutedText}
        }
        For Each c In cases
            Dim verdict = DirectCast(c(0), String)
            Dim glyph = Theme.VerdictGlyph(verdict)
            Check("glyph:" & verdict & ":meaning", glyph.Meaning = DirectCast(c(1), String), glyph.ToString())
            Dim code = AscW(glyph.Interim) And &HFFFF
            Check("glyph:" & verdict & ":not-confirm-or-close", code <> &HE73E AndAlso code <> &HE711, glyph.ToString())
            Check("glyph:" & verdict & ":drawn", glyph.IsVocabulary AndAlso Glyphs.IsDrawable(glyph.Id), glyph.ToString())
            Dim want = DirectCast(c(2), Color)
            Dim got = Theme.VerdictGlyphColor(verdict, p)
            Check("glyph:" & verdict & ":colour", got.ToArgb() = want.ToArgb(), got.ToString())
        Next
    End Sub

    ' ---- SP-0016: the vocabulary's drawings -----------------------------------

    ' Every vocabulary id the shell draws: the rail's glyph map, the group chevrons, the verdicts,
    ' and the Explorer icons it writes (MenuIcons.vb).
    Private Function MappedVocabularyIds() As HashSet(Of String)
        Dim ids As New HashSet(Of String)(MenuIcons.Ids, StringComparer.Ordinal)
        Dim refs As New List(Of GlyphRef)()
        For Each row In RailRow.All
            If row.Glyph IsNot Nothing Then refs.Add(row.Glyph)
        Next
        refs.Add(Theme.ChevronGlyph(True))
        refs.Add(Theme.ChevronGlyph(False))
        ' The Disk Manager's state glyphs (SP-0063 5.4).
        For Each state As DiskRowState In [Enum].GetValues(GetType(DiskRowState))
            Dim g = DiskStates.GlyphOf(state)
            If g IsNot Nothing Then refs.Add(g)
        Next
        For Each verdict In New String() {"Passed", "Done", "Failed", "Stopped", "Not proven"}
            refs.Add(Theme.VerdictGlyph(verdict))
        Next
        ' The Disk Manager's toolbar, menus and detail buttons (DiskGlyphs: one table for the rail and the
        ' window), and the window's own controls.
        For Each a As DiskAction In [Enum].GetValues(GetType(DiskAction))
            Dim g = DiskGlyphs.For(a)
            If g IsNot Nothing Then refs.Add(g)
        Next
        refs.AddRange(New GlyphRef() {DiskGlyphs.Help, DiskGlyphs.More, DiskGlyphs.CloseIt, DiskGlyphs.ClearInput,
                                      DiskGlyphs.OpenExternal, DiskGlyphs.ShowDetails, DiskGlyphs.StopRun, DiskGlyphs.ClearQueue})
        For Each r In refs
            If r.IsVocabulary Then ids.Add(r.Id)
        Next
        Return ids
    End Function

    ' T10, ICON-EXTERNAL rule 5 and the compatibility law's "a higher MAJOR is refused cleanly":
    ' every vendored file is embedded and matches its SHA-256 line in PROVENANCE.txt, which was
    ' mapped against this build's ICON-SET MAJOR; every vocabulary id the shell draws is among
    ' them and every one of them is drawn by something; each lands on its 24 grid and actually
    ' paints - the pixel test, because a reader that misses part of the SVG subset draws another
    ' picture rather than failing.
    Private Sub CheckGlyphProvenance()
        Dim problems = Glyphs.Problems()
        Check("icons:provenance", problems.Count = 0,
              If(problems.Count = 0, Glyphs.RecordedFileCount().ToString() & " files verified, " & Glyphs.CatalogVersions(),
                 String.Join("; ", problems.ToArray())))
        Check("icons:icon-set-major", Glyphs.CatalogVersions().StartsWith("ICON-SET " & Glyphs.MappedIconSetMajor.ToString() & ".",
                                                                          StringComparison.Ordinal),
              Glyphs.CatalogVersions())

        Dim mapped = MappedVocabularyIds()
        For Each id In mapped.OrderBy(Function(s) s)
            Check("icons:mapped:" & id, Glyphs.IsDrawable(id),
                  If(Glyphs.IsDrawable(id), "vendored, verified", "not vendored or not verified - run assets\sync-icon-glyphs.ps1"))
        Next

        Dim p = Theme.PaletteFor(False)
        For Each id In Glyphs.DrawableIds().OrderBy(Function(s) s)
            Check("icons:used:" & id, mapped.Contains(id), If(mapped.Contains(id), "drawn by the shell", "vendored but drawn by nothing"))
            Dim box = Glyphs.InkBounds(id)
            Check("icons:grid:" & id, box.Width > 0 AndAlso box.Height > 0 AndAlso box.Left >= 0 AndAlso box.Top >= 0 AndAlso
                                      box.Right <= 24 AndAlso box.Bottom <= 24, box.ToString())
            Dim inked = 0
            Using bmp As New Bitmap(24, 24)
                Using g = Graphics.FromImage(bmp)
                    Using back As New SolidBrush(p.Surface)
                        g.FillRectangle(back, 0, 0, 24, 24)
                    End Using
                    Glyphs.Draw(g, GlyphRef.Vocabulary(id), New Rectangle(0, 0, 24, 24), p.Text)
                End Using
                For y = 0 To 23
                    For x = 0 To 23
                        If bmp.GetPixel(x, y).ToArgb() <> p.Surface.ToArgb() Then inked += 1
                    Next
                Next
            End Using
            Check("icons:paints:" & id, inked >= 24, inked.ToString() & " of 576 px inked at 24 px")
        Next
    End Sub

    ' The rows that draw a stand-in. Zero since ICON-SET 0.14 (2026-09-25) took the twelve meanings
    ' FileDO proposed; a new row never joins them - a new control starts from the vocabulary (ICON-SET
    ' rule 5) - so raising this is an amendment with a reason, not a fix.
    '
    ' Amended 2026-09-27 (SP-0004 P6): ten Disks rows - create, mount, unmount, compact, grow,
    ' format, seal, change password, auto-mount, remember - show meanings the
    ' vocabulary has no record for (Rail.vb says which). They draw stand-ins under proposed ids until
    ' the records land; the number goes back down as each one is vendored, never up.
    '
    ' Amended 2026-10-02 (SP-0016 / SP-0122, ICON-SET 0.17): seven of those records landed and were
    ' vendored; three remain waiting (create, compact, auto-mount).
    Private Const WaitingRailRowsBaseline As Integer = 3

    ' T1, ICON-SET rules 1, 4 and 5 on the rail: every job row shows a glyph; a vocabulary one is
    ' drawable; a waiting one names the id proposed for it and a Segoe stand-in in the private-use
    ' area with that font's own name for it (ICON-EXTERNAL rule 5); and no two rows that show
    ' different meanings share a picture - neither one vocabulary drawing nor one stand-in.
    Private Sub CheckRailGlyphs()
        Dim meaningOf As New Dictionary(Of String, String)(StringComparer.Ordinal)
        Dim waiting As New List(Of String)()
        Dim hues As New HashSet(Of String)(Theme.GroupHueKeys, StringComparer.Ordinal)
        Dim rowsOfGroup As New Dictionary(Of String, Integer)(StringComparer.Ordinal)
        Dim currentGroup As String = Nothing
        For Each row In RailRow.All
            Dim glyph = row.Glyph
            ' A header and a lone job show the colour look, so each names a hue Theme knows; a row
            ' inside a group is mono and names none.
            If row.IsGroup OrElse row.IsAlone Then
                Check("rail-hue:" & row.Key, hues.Contains(row.Hue), row.Hue)
            Else
                Check("rail-hue:" & row.Key, row.Hue = "", "a row inside a group draws in the text colour")
            End If
            ' A group of one job is no group: the job stands by itself (RailRow.Alone).
            If row.IsGroup Then
                currentGroup = row.Key
                rowsOfGroup(currentGroup) = 0
            ElseIf row.IsAlone Then
                currentGroup = Nothing
            ElseIf currentGroup IsNot Nothing Then
                rowsOfGroup(currentGroup) += 1
            End If
            If glyph Is Nothing Then
                Check("rail-glyph:" & row.Key, False, "no glyph")
                Continue For
            End If
            Dim code = AscW(glyph.Interim) And &HFFFF
            If glyph.IsVocabulary Then
                Check("rail-glyph:" & row.Key, Glyphs.IsDrawable(glyph.Id), glyph.ToString())
            Else
                waiting.Add(row.Key)
                Check("rail-glyph:" & row.Key, glyph.Pending.Contains(".") AndAlso code >= &HE000 AndAlso code <= &HF8FF AndAlso
                                               glyph.FontName <> "", glyph.ToString())
            End If
            Dim picture = If(glyph.IsVocabulary, glyph.Id, "U+" & code.ToString("X4"))
            Dim other As String = Nothing
            If meaningOf.TryGetValue(picture, other) Then
                Check("rail-glyph:shared:" & row.Key, other = glyph.Meaning, picture & " already shows " & other)
            Else
                meaningOf(picture) = glyph.Meaning
            End If
        Next
        For Each kv In rowsOfGroup
            Check("rail-group:" & kv.Key & ":size", kv.Value >= 2,
                  kv.Value.ToString() & " job rows - a group of one job is no group, it stands by itself")
        Next
        Check("rail-glyph:waiting", waiting.Count <= WaitingRailRowsBaseline,
              waiting.Count.ToString() & " rows draw a stand-in (baseline " & WaitingRailRowsBaseline.ToString() &
              ", PROPOSAL-2026-09-23-filedo-meanings.md): " & String.Join(", ", waiting.ToArray()))
    End Sub

    ' The rail as built (owner, 2026-09-30): one group open at a time, opening one shuts the others,
    ' and neighbouring blocks - a header with its rows, or a lone job - alternate their bands. The
    ' rail is built by a real ShellForm; the click goes through ToggleGroupForTest, which does not
    ' write the user's remembered groups.
    Private Sub CheckRailAccordion()
        Using shell As New ShellForm()
            Dim all = shell.RailEntriesForTest()
            Dim headers = all.Where(Function(r) r.IsGroupHeader).ToList()
            Check("rail-accordion:headers", headers.Count >= 2, headers.Count.ToString() & " headers")

            Dim openNow = Function() headers.Where(Function(h) Not h.Collapsed).Select(Function(h) h.Key).ToList()
            Check("rail-accordion:start", openNow().Count <= 1, String.Join(",", openNow().ToArray()))

            ' The saved preference may start with the first group open. Close it so each
            ' iteration below tests opening a closed group, whatever that preference was.
            If openNow().Count = 1 Then shell.ToggleGroupForTest(openNow()(0))

            For Each head In headers
                shell.ToggleGroupForTest(head.Key)
                Dim open = openNow()
                Check("rail-accordion:open:" & head.Key, open.Count = 1 AndAlso open(0) = head.Key, String.Join(",", open.ToArray()))
            Next

            Dim last = headers(headers.Count - 1)
            shell.ToggleGroupForTest(last.Key)
            Check("rail-accordion:shut", openNow().Count = 0, String.Join(",", openNow().ToArray()))

            ' The checker: a header or a lone job starts a block on the band the previous block did not
            ' have, and a row inside a group keeps its header's band.
            Dim rowsByKey = RailRow.All.ToDictionary(Function(r) r.Key)
            Dim prevBand = -1, band = -1
            For Each entry In all
                Dim def = rowsByKey(entry.Key)
                If def.IsGroup OrElse def.IsAlone Then
                    band = If(prevBand = 0, 1, 0)
                    prevBand = band
                End If
                Check("rail-band:" & entry.Key, entry.Band = band, "band " & entry.Band.ToString() & ", block band " & band.ToString())
            Next
        End Using
    End Sub

    ' T8, ICON-SET rule 7 and ICON-RENDER 0.12 rule 9 on the Explorer surfaces: every icon the
    ' writers name is embedded (it came from assets\menu-icons\), carries every size, paints in the
    ' one menu tone, and still is the drawing of its glyph - each size is drawn afresh and compared
    ' with the copy. The tolerance is for anti-aliasing that may differ by a step between Windows
    ' builds; a changed drawing moves whole pixels and fails. The fix is to re-run
    ' `filedo_win.exe --write-menu-icons assets\menu-icons` and rebuild.
    ' The plated icon of the Disk Manager (owner decision 2026-10-02) is held to the same freshness
    ' and to its own rules (CheckPlatedIcon): a plate in the accent, the glyph in the on-plate colour.
    Private Sub CheckMenuIcons()
        Const tolerance As Integer = 24
        For Each id In MenuIcons.AllIds
            Dim ico = MenuIcons.EmbeddedIco(id)
            If ico Is Nothing Then
                Check("menu-icon:" & id, False, "not embedded - assets\menu-icons\" & id & ".ico is missing")
                Continue For
            End If
            Dim images = MenuIcons.ReadIco(ico)
            Try
                For Each size In MenuIcons.Sizes
                    Dim stored As Bitmap = Nothing
                    If Not images.TryGetValue(size, stored) Then
                        Check("menu-icon:" & id & ":" & size.ToString(), False, "no " & size.ToString() & " px image")
                        Continue For
                    End If
                    If MenuIcons.IsPlated(id) Then
                        CheckPlatedIcon(id, size, stored, tolerance)
                        Continue For
                    End If
                    Dim worst = 0, inked = 0, offTone = 0
                    Using fresh = MenuIcons.Render(id, size)
                        For y = 0 To size - 1
                            For x = 0 To size - 1
                                Dim a = fresh.GetPixel(x, y), b = stored.GetPixel(x, y)
                                worst = Math.Max(worst, Math.Abs(CInt(a.A) - b.A))
                                If b.A > 0 Then
                                    inked += 1
                                    If b.A >= 32 AndAlso (Math.Abs(CInt(b.R) - MenuIcons.Tone.R) > 3 OrElse Math.Abs(CInt(b.G) - MenuIcons.Tone.G) > 3 OrElse
                                       Math.Abs(CInt(b.B) - MenuIcons.Tone.B) > 3) Then offTone += 1
                                End If
                            Next
                        Next
                    End Using
                    Check("menu-icon:" & id & ":" & size.ToString(), worst <= tolerance AndAlso offTone = 0 AndAlso inked * 16 >= size * size,
                          "alpha differs by " & worst.ToString() & " at most, " & inked.ToString() & " px inked, " &
                          offTone.ToString() & " off the menu tone")
                Next
            Finally
                For Each bmp In images.Values
                    bmp.Dispose()
                Next
            End Try
        Next
        ' The product's picture is not the generic file-type glyph: the plated icon and the mono icon of
        ' the same glyph are different files, and the plate's glyph holds 3:1 on its plate.
        For Each id In MenuIcons.PlatedIds
            Dim plated = MenuIcons.EmbeddedIco(id)
            Dim generic = MenuIcons.EmbeddedIco(MenuIcons.PlatedGlyphId(id))
            Check("menu-icon:" & id & ":not-generic", plated IsNot Nothing AndAlso generic IsNot Nothing AndAlso Not plated.SequenceEqual(generic),
                  "plated vs " & MenuIcons.PlatedGlyphId(id) & ".ico: " & If(plated IsNot Nothing AndAlso generic IsNot Nothing AndAlso plated.SequenceEqual(generic), "byte-identical", "different files"))
            Dim ratio = Theme.ContrastRatio(Theme.AppIconInk, Theme.AppIconPlate)
            Check("menu-icon:" & id & ":contrast", ratio >= 3.0, "glyph on plate is " & ratio.ToString("0.00", Globalization.CultureInfo.InvariantCulture) & ":1, want 3:1")
        Next
    End Sub

    ' One size of a plated icon: the stored image is the fresh drawing (within the anti-aliasing
    ' tolerance, every channel), the centre is the opaque plate and the corner is transparent, the
    ' plate holds its colour and the glyph its on-plate colour, and the glyph's 24 grid spans 0.6 of
    ' the plate within 0.05 (ICON-RENDER section 10 item E).
    Private Sub CheckPlatedIcon(id As String, size As Integer, stored As Bitmap, tolerance As Integer)
        Dim worst = 0, platePx = 0, inkPx = 0
        Dim plateCol = Theme.AppIconPlate, inkCol = Theme.AppIconInk
        Using fresh = MenuIcons.Render(id, size)
            For y = 0 To size - 1
                For x = 0 To size - 1
                    Dim a = fresh.GetPixel(x, y), b = stored.GetPixel(x, y)
                    worst = Math.Max(worst, Math.Max(Math.Abs(CInt(a.A) - b.A), If(a.A = 0 AndAlso b.A = 0, 0,
                                     Math.Max(Math.Abs(CInt(a.R) - b.R), Math.Max(Math.Abs(CInt(a.G) - b.G), Math.Abs(CInt(a.B) - b.B))))))
                    If b.A = 255 Then
                        If Math.Abs(CInt(b.R) - plateCol.R) <= 3 AndAlso Math.Abs(CInt(b.G) - plateCol.G) <= 3 AndAlso Math.Abs(CInt(b.B) - plateCol.B) <= 3 Then platePx += 1
                        If Math.Abs(CInt(b.R) - inkCol.R) <= 3 AndAlso Math.Abs(CInt(b.G) - inkCol.G) <= 3 AndAlso Math.Abs(CInt(b.B) - inkCol.B) <= 3 Then inkPx += 1
                    End If
                Next
            Next
        End Using
        Dim plate = MenuIcons.PlateRect(size)
        Dim ratio = MenuIcons.PlateGlyphSquare(size).Width / CDbl(plate.Width)
        Dim centre = stored.GetPixel(size \ 2, size \ 2), corner = stored.GetPixel(0, 0)
        Dim problems As New List(Of String)()
        If worst > tolerance Then problems.Add("differs from a fresh drawing by " & worst.ToString())
        If centre.A <> 255 Then problems.Add("the centre is not opaque")
        If corner.A <> 0 Then problems.Add("the corner is not transparent")
        If platePx * 4 < size * size Then problems.Add("only " & platePx.ToString() & " px in the plate colour")
        If inkPx * 50 < size * size Then problems.Add("only " & inkPx.ToString() & " px in the glyph colour")
        If ratio < 0.55 OrElse ratio > 0.65 Then problems.Add("the glyph spans " & ratio.ToString("0.000", Globalization.CultureInfo.InvariantCulture) & " of the plate, want 0.6 +- 0.05")
        Check("menu-icon:" & id & ":" & size.ToString(), problems.Count = 0, String.Join("; ", problems.ToArray()))
    End Sub

    ' T11, ICON-RENDER section 10 item D taken as exact tones (SP-0016 D2): each state role of both
    ' palettes is the vendored palette.json's day or night tone. The light warning joined them when
    ' ICON-RENDER 0.14 moved its day tone to #EF6C00 and the shell's own exception (the old tone
    ' failed 3:1) was retired.
    Private Sub CheckStateTones()
        Dim json = Glyphs.VerifiedData("palette.json")
        If json Is Nothing Then
            Check("state-tone:palette", False, "palette.json is not vendored or did not verify")
            Return
        End If
        Dim hues As Dictionary(Of String, Object) = Nothing
        Try
            Dim root = New Web.Script.Serialization.JavaScriptSerializer().Deserialize(Of Dictionary(Of String, Object))(json)
            hues = DirectCast(root("hues"), Dictionary(Of String, Object))
        Catch ex As Exception
            Check("state-tone:palette", False, ex.GetType().Name & ": " & ex.Message)
            Return
        End Try

        Dim light = Theme.PaletteFor(False)
        Dim dark = Theme.PaletteFor(True)
        Dim rows = New Object()() {
            New Object() {"state.ok", "day", light.StateOk},
            New Object() {"state.ok", "night", dark.StateOk},
            New Object() {"state.error", "day", light.StateError},
            New Object() {"state.error", "night", dark.StateError},
            New Object() {"state.warning", "day", light.StateWarning},
            New Object() {"state.warning", "night", dark.StateWarning}
        }
        For Each r In rows
            Dim key = DirectCast(r(0), String)
            Dim tone = DirectCast(r(1), String)
            Dim got = DirectCast(r(2), Color)
            Dim want = PaletteTone(hues, key, tone)
            Check("state-tone:" & key & ":" & tone, Not want.IsEmpty AndAlso want.ToArgb() = got.ToArgb(),
                  HexOf(got) & " (palette.json " & HexOf(want) & ")")
        Next
    End Sub

    Private Function PaletteTone(hues As Dictionary(Of String, Object), key As String, tone As String) As Color
        Dim hue As Object = Nothing
        If hues Is Nothing OrElse Not hues.TryGetValue(key, hue) Then Return Nothing
        Dim fields = TryCast(hue, Dictionary(Of String, Object))
        Dim value As Object = Nothing
        If fields Is Nothing OrElse Not fields.TryGetValue(tone, value) Then Return Nothing
        Return Theme.FromHex(TryCast(value, String))
    End Function

    Private Function HexOf(c As Color) As String
        If c.IsEmpty Then Return "(none)"
        Return "#" & c.R.ToString("X2") & c.G.ToString("X2") & c.B.ToString("X2")
    End Function

    ' ICON-RENDER rule 3 and conformance rung 4, measured rather than claimed: every glyph the
    ' shell draws against every surface it is drawn on, in both palettes - 3:1 for a glyph, and
    ' 4.5:1 for the verdict word the Command page paints in the glyph's colour and for the verdict
    ' badge's word on its fill.
    Private Sub CheckGlyphContrast()
        For Each dark In New Boolean() {False, True}
            Dim p = Theme.PaletteFor(dark)
            Dim t = If(dark, "dark", "light")
            ContrastPair("contrast:" & t & ":rail-glyph:plain", p.Text, p.SurfaceAlt, 3.0)
            ContrastPair("contrast:" & t & ":rail-glyph:hover", p.Text, p.ControlHover, 3.0)
            ContrastPair("contrast:" & t & ":rail-glyph:selected", p.Text, p.SurfaceSelected, 3.0)
            ContrastPair("contrast:" & t & ":chevron:plain", p.MutedText, p.SurfaceAlt, 3.0)
            ContrastPair("contrast:" & t & ":chevron:hover", p.MutedText, p.ControlHover, 3.0)
            ' The rail's second band and what is drawn on it: the text at 4.5:1, the glyphs and the
            ' chevron at 3:1, and every group hue on every surface a row can be on.
            ContrastPair("contrast:" & t & ":rail-band:text", p.Text, p.SurfaceBand, 4.5)
            ContrastPair("contrast:" & t & ":rail-band:text-hover", p.Text, p.BandHover, 4.5)
            ContrastPair("contrast:" & t & ":rail-band:chevron", p.MutedText, p.SurfaceBand, 3.0)
            ContrastPair("contrast:" & t & ":rail-band:chevron-hover", p.MutedText, p.BandHover, 3.0)
            For Each hue In Theme.GroupHueKeys
                Dim tone = Theme.GroupTone(hue, p)
                ContrastPair("contrast:" & t & ":" & hue & ":plain", tone, p.SurfaceAlt, 3.0)
                ContrastPair("contrast:" & t & ":" & hue & ":band", tone, p.SurfaceBand, 3.0)
                ContrastPair("contrast:" & t & ":" & hue & ":hover", tone, p.ControlHover, 3.0)
                ContrastPair("contrast:" & t & ":" & hue & ":band-hover", tone, p.BandHover, 3.0)
                ContrastPair("contrast:" & t & ":" & hue & ":selected", tone, p.SurfaceSelected, 3.0)
            Next
            For Each verdict In New String() {"Passed", "Done", "Failed", "Stopped", "Not proven"}
                ContrastPair("contrast:" & t & ":verdict:" & verdict, Theme.VerdictColor(verdict, p), p.Surface, 4.5)
                ContrastPair("contrast:" & t & ":verdict-glyph:" & verdict, Theme.VerdictGlyphColor(verdict, p), p.Surface, 3.0)
                ContrastPair("contrast:" & t & ":badge:" & verdict, Theme.VerdictFore(verdict, p), Theme.VerdictBack(verdict, p), 4.5)
            Next
        Next
    End Sub

    Private Sub ContrastPair(name As String, fore As Color, back As Color, need As Double)
        Dim ratio = Theme.ContrastRatio(fore, back)
        Dim inv = Globalization.CultureInfo.InvariantCulture
        Check(name, ratio >= need, ratio.ToString("0.00", inv) & ":1, needs " & need.ToString("0.0", inv) & " - " &
              HexOf(fore) & " on " & HexOf(back))
    End Sub

    ' Rung 3 of the same ladder, plus the two ways a line can be lost. A reader
    ' that consumes an incomplete final line drops exactly one event, at
    ' random, under load - and the event most likely to be half-written is the
    ' last one, which is the one carrying the verdict.
    Private Sub CheckEventStreamTailer()
        Dim eventsPath = Path.Combine(Path.GetTempPath(), "filedo_selftest_events_" & Guid.NewGuid().ToString("N") & ".jsonl")
        Try
            File.WriteAllText(eventsPath, "")

            ' Half a line, then a poll: nothing may be dispatched and nothing
            ' may be consumed.
            Dim results As New List(Of String)()
            Dim received As New List(Of String)()
            Dim refused As Integer = 0
            Using stream As New EventStream(eventsPath)
                AddHandler stream.EventReceived, Sub(e) received.Add(e.Kind)
                AddHandler stream.ResultReceived, Sub(r) results.Add(r.Verdict)
                AddHandler stream.UnsupportedVersion, Sub(v) refused = v

                Const head As String = "{""schemaVersion"":1,""kind"":""result"",""timestamp"":""2026-09-22T14:31:07.418+02:00"",""data"":{""verd"
                Const tail As String = "ict"":""Passed""}}" & vbLf
                AppendText(eventsPath, head)
                stream.Poll()
                Check("tailer:partial-line-not-consumed", results.Count = 0, results.Count.ToString())

                ' The other half arrives: the event must now be read whole.
                AppendText(eventsPath, tail)
                stream.Poll()
                Check("tailer:line-completed", results.Count = 1 AndAlso results(0) = "Passed",
                      String.Join(",", results.ToArray()))

                ' A timestamp nobody can parse costs the field, never the
                ' event that carried it.
                AppendText(eventsPath, "{""schemaVersion"":1,""kind"":""result"",""timestamp"":""not-a-date"",""data"":{""verdict"":""Failed""}}" & vbLf)
                stream.Poll()
                Check("tailer:bad-timestamp-keeps-event", results.Count = 2 AndAlso results(1) = "Failed",
                      String.Join(",", results.ToArray()))

                ' Omitted carrier defaults to MAJOR 1 for this document.
                AppendText(eventsPath, "{""kind"":""result"",""data"":{""verdict"":""Done""}}" & vbLf)
                stream.Poll()
                Check("tailer:absent-carrier-is-v1", results.Count = 3 AndAlso results(2) = "Done",
                      String.Join(",", results.ToArray()))

                ' Case variants and unknown kinds are delivered as raw events
                ' only; neither is dispatched as a result (rule 8).
                AppendText(eventsPath, "{""schemaVersion"":1,""kind"":""RESULT"",""data"":{""verdict"":""Passed""}}" & vbLf)
                stream.Poll()
                Check("tailer:kind-is-case-sensitive", results.Count = 3 AndAlso received.Contains("RESULT"),
                      String.Join(",", received.ToArray()))

                ' A newer MAJOR stops interpretation and does not resume.
                AppendText(eventsPath, "{""schemaVersion"":2,""kind"":""result"",""data"":{""verdict"":""Passed""}}" & vbLf)
                stream.Poll()
                Check("tailer:newer-major-refused", refused = 2, refused.ToString())
                Check("tailer:newer-major-not-interpreted", results.Count = 3, results.Count.ToString())

                AppendText(eventsPath, "{""schemaVersion"":1,""kind"":""result"",""data"":{""verdict"":""Passed""}}" & vbLf)
                stream.Poll()
                Check("tailer:refusal-is-final", results.Count = 3, results.Count.ToString())
            End Using

            ' A present carrier of the wrong JSON type is a refusal, not an
            ' implicit v1. It needs a fresh tailer because refusal is final.
            Dim garbledPath = Path.Combine(Path.GetTempPath(), "filedo_selftest_garbled_" & Guid.NewGuid().ToString("N") & ".jsonl")
            Try
                File.WriteAllText(garbledPath, "{""schemaVersion"":""two"",""kind"":""result"",""data"":{""verdict"":""Passed""}}" & vbLf)
                Dim garbledRefused As Integer = 0
                Dim garbledResults As Integer = 0
                Using stream As New EventStream(garbledPath)
                    AddHandler stream.UnsupportedVersion, Sub(v) garbledRefused = v
                    AddHandler stream.ResultReceived, Sub(r) garbledResults += 1
                    stream.Poll()
                End Using
                Check("tailer:garbled-carrier-refused", garbledRefused > EventStream.KnownSchemaVersion AndAlso garbledResults = 0,
                      garbledRefused.ToString() & "/" & garbledResults.ToString())
            Finally
                Try
                    File.Delete(garbledPath)
                Catch
                End Try
            End Try
        Catch ex As Exception
            Check("tailer", False, ex.GetType().Name & ": " & ex.Message)
        Finally
            Try
                File.Delete(eventsPath)
            Catch
            End Try
        End Try
    End Sub

    ' Rung 1: the checked-in producer-generated sample is linked as a resource
    ' so the consumer proves it can read exactly the bytes another implementation
    ' would receive, not a hand-written approximation of them.
    Private Sub CheckEventStreamSample()
        Dim tempPath = Path.Combine(Path.GetTempPath(), "filedo_selftest_sample_" & Guid.NewGuid().ToString("N") & ".jsonl")
        Try
            Using source = GetType(SelfTest).Assembly.GetManifestResourceStream("FileDOGUI.events.sample-v1.jsonl")
                If source Is Nothing Then
                    Check("sample:resource", False, "missing FileDOGUI.events.sample-v1.jsonl")
                    Return
                End If
                Using destination As New FileStream(tempPath, FileMode.Create, FileAccess.Write)
                    source.CopyTo(destination)
                End Using
            End Using
            Dim kinds As New List(Of String)()
            Dim stepName As String = ""
            Dim progressItems As Long = 0
            Dim findingMessage As String = ""
            Dim noteMessage As String = ""
            Dim verdict As String = ""
            Using stream As New EventStream(tempPath)
                AddHandler stream.EventReceived, Sub(e) kinds.Add(e.Kind)
                AddHandler stream.StepChanged, Sub(n, d) stepName = n
                AddHandler stream.ProgressReported, Sub(p) progressItems = p.DoneItems
                AddHandler stream.FindingReported, Sub(t, m, d) findingMessage = m
                AddHandler stream.NoteReported, Sub(m) noteMessage = m
                AddHandler stream.ResultReceived, Sub(r) verdict = r.Verdict
                stream.Poll()
            End Using
            Dim want = New String() {"run", "step", "progress", "finding", "note", "result"}
            Check("sample:all-kinds", kinds.SequenceEqual(want), String.Join(",", kinds.ToArray()))
            Check("sample:fields", stepName = "write" AndAlso progressItems = 1 AndAlso findingMessage = "An example finding" AndAlso noteMessage = "An example note" AndAlso verdict = "Passed")
        Catch ex As Exception
            Check("sample", False, ex.GetType().Name & ": " & ex.Message)
        Finally
            Try
                File.Delete(tempPath)
            Catch
            End Try
        End Try
    End Sub

    ' ---- SP-0014: APP-STYLE --------------------------------------------------

    ' APP-STYLE rung 2: both palettes give every role a colour. A role left empty paints as black
    ' or transparent in exactly one theme, which is the kind of thing nobody finds by looking.
    Private Sub CheckPaletteCompleteness()
        For Each dark In New Boolean() {False, True}
            Dim p = Theme.PaletteFor(dark)
            Dim name = If(dark, "dark", "light")
            For Each prop In GetType(Theme.Palette).GetProperties()
                If prop.PropertyType IsNot GetType(Color) Then Continue For
                Dim c = DirectCast(prop.GetValue(p), Color)
                Check("palette:" & name & ":" & prop.Name, Not c.IsEmpty AndAlso c.A = 255, c.ToString())
            Next
        Next
    End Sub

    ' APP-STYLE section 3, the defect class "a reference resolved once at load": a Failed result on
    ' screen under one palette, the other palette applied, and the badge and both verdict glyphs
    ' must carry the new palette's StateError. The palette is switched through Theme's test seam,
    ' never through HKCU.
    Private Sub CheckThemeRoundTrip()
        Dim jv As JobView = Nothing
        Dim cv As CommandView = Nothing
        Try
            Theme.UsePaletteForTest(False)
            jv = New JobView()
            jv.SetJob(JobCatalogue.GetJob("rail_job_capacity"))
            jv.ShowResultForTest("Failed")
            cv = New CommandView()
            cv.ShowVerdictForTest("Failed")
            Dim light = Theme.PaletteFor(False)
            Check("theme:job-badge-light", jv.VerdictBadgeForTest.BackColor.ToArgb() = light.StateError.ToArgb(),
                  jv.VerdictBadgeForTest.BackColor.ToString())

            Theme.UsePaletteForTest(True)
            jv.ApplyTheme()
            cv.ApplyTheme()
            Dim dark = Theme.PaletteFor(True)
            Check("theme:job-badge-follows", jv.VerdictBadgeForTest.BackColor.ToArgb() = dark.StateError.ToArgb(),
                  jv.VerdictBadgeForTest.BackColor.ToString())
            Check("theme:job-glyph-follows", jv.VerdictGlyphForTest.ForeColor.ToArgb() = dark.StateError.ToArgb() AndAlso
                  jv.VerdictGlyphForTest.Glyph IsNot Nothing AndAlso jv.VerdictGlyphForTest.Glyph.Meaning = "status.error",
                  jv.VerdictGlyphForTest.ForeColor.ToString())
            Check("theme:command-verdict-follows", cv.VerdictLabelForTest.ForeColor.ToArgb() = dark.StateError.ToArgb(),
                  cv.VerdictLabelForTest.ForeColor.ToString())
            Check("theme:command-glyph-follows", cv.VerdictGlyphForTest.ForeColor.ToArgb() = dark.StateError.ToArgb() AndAlso
                  cv.VerdictGlyphForTest.Glyph IsNot Nothing AndAlso cv.VerdictGlyphForTest.Glyph.Meaning = "status.error",
                  cv.VerdictGlyphForTest.ForeColor.ToString())
            Check("theme:command-line-has-no-glyph", Not cv.VerdictLabelForTest.Text.Any(Function(ch) AscW(ch) >= &HE000 AndAlso AscW(ch) <= &HF8FF),
                  cv.VerdictLabelForTest.Text)
        Catch ex As Exception
            Check("theme", False, ex.GetType().Name & ": " & ex.Message)
        Finally
            Theme.UsePaletteForTest(Nothing)
            If jv IsNot Nothing Then jv.Dispose()
            If cv IsNot Nothing Then cv.Dispose()
        End Try
    End Sub

    ' A disabled control's caption is readable and stays where it was. WinForms draws it in a darker
    ' shade of the back colour, which on the dark theme left the Run button of a page waiting for its
    ' target near-black on near-black (Ui.KeepCaptionsReadable). Each kind the shell disables is drawn
    ' twice on the palette's own colours: disabled, and enabled with TextDisabled as its fore colour -
    ' the caption WinForms itself draws in that colour. Both must put their ink - pixels at 3:1 or more
    ' against the control's back colour - in the same place, and there must be some.
    Private Sub CheckDisabledCaptions()
        For Each dark In New Boolean() {False, True}
            Theme.UsePaletteForTest(dark)
            Dim themeName = If(dark, "dark", "light")
            Try
                Dim p = Theme.Current
                For Each kind In New String() {"button", "check", "radio"}
                    Using host As New Panel With {.BackColor = p.Surface, .Size = New Size(900, 200)}
                        Dim twin = CaptionControl(kind, p)
                        Dim off = CaptionControl(kind, p)
                        off.Enabled = False
                        host.Controls.Add(twin)
                        host.Controls.Add(off)
                        Ui.KeepCaptionsReadable(host)
                        Dim want = CaptionInk(twin)
                        Dim got = CaptionInk(off)
                        Check("disabled-caption:" & themeName & ":" & kind, Not got.IsEmpty AndAlso got = want,
                              "enabled " & want.ToString() & ", disabled " & got.ToString())
                    End Using
                Next
            Catch ex As Exception
                Check("disabled-caption:" & themeName, False, ex.GetType().Name & ": " & ex.Message)
            Finally
                Theme.UsePaletteForTest(Nothing)
            End Try
        Next

        ' The window hooks every button, check box and radio button it has, not only the ones above.
        Dim shell As ShellForm = Nothing
        Try
            shell = New ShellForm()
            ' The job page is built when a job is first chosen: build it, or its buttons are not walked.
            Check("disabled-caption:job-page-built", shell.JobViewForTest IsNot Nothing, "")
            Dim missed As New List(Of String)
            For Each c In AllControls(shell)
                Dim b = TryCast(c, ButtonBase)
                If b IsNot Nothing AndAlso Not Ui.CaptionKeptReadable(b) Then missed.Add(b.GetType().Name & " '" & b.Text & "'")
            Next
            Check("disabled-caption:window", missed.Count = 0, String.Join("; ", missed.Take(8).ToArray()))
        Finally
            If shell IsNot Nothing Then shell.Dispose()
        End Try
    End Sub

    ' A control of one kind as the shell builds it: the Run button's style, or a body-text option.
    Private Function CaptionControl(kind As String, p As Theme.Palette) As ButtonBase
        Dim c As ButtonBase
        Select Case kind
            Case "button"
                c = New Button With {.Font = Theme.FontBodyStrong(), .Padding = New Padding(18, 7, 18, 7)}
                Ui.StyleButton(DirectCast(c, Button), p.SurfaceAlt, p.TextDisabled, p.Border)
            Case "check"
                c = New CheckBox With {.Font = Theme.FontBody(), .ForeColor = p.TextDisabled}
            Case Else
                c = New RadioButton With {.Font = Theme.FontBody(), .ForeColor = p.TextDisabled}
        End Select
        c.Text = "Run: check for damage"
        c.Size = c.GetPreferredSize(Size.Empty)
        Return c
    End Function

    ' The bounds of a control's caption ink, right of a check box's or radio button's glyph.
    Private Function CaptionInk(c As ButtonBase) As Rectangle
        Using bmp As New Bitmap(c.Width, c.Height)
            c.DrawToBitmap(bmp, New Rectangle(0, 0, c.Width, c.Height))
            Dim fromX = 0
            If Not TypeOf c Is Button Then
                Using g = Graphics.FromImage(bmp)
                    Dim glyph = If(TypeOf c Is CheckBox,
                                   CheckBoxRenderer.GetGlyphSize(g, VisualStyles.CheckBoxState.UncheckedNormal).Width,
                                   RadioButtonRenderer.GetGlyphSize(g, VisualStyles.RadioButtonState.UncheckedNormal).Width)
                    fromX = c.Padding.Left + glyph + 1
                End Using
            End If
            Dim back = c.BackColor
            Dim minX = Integer.MaxValue, minY = Integer.MaxValue, maxX = -1, maxY = -1
            For y = 0 To bmp.Height - 1
                For x = fromX To bmp.Width - 1
                    If Theme.ContrastRatio(bmp.GetPixel(x, y), back) < 3.0 Then Continue For
                    minX = Math.Min(minX, x) : minY = Math.Min(minY, y)
                    maxX = Math.Max(maxX, x) : maxY = Math.Max(maxY, y)
                Next
            Next
            If maxX < 0 Then Return Rectangle.Empty
            Return Rectangle.FromLTRB(minX, minY, maxX + 1, maxY + 1)
        End Using
    End Function

    ' ---- SP-0014: the rail ----------------------------------------------------

    ' T1: every kind of rail row, in every state it can be drawn in, painted under both palettes.
    ' A paint that throws is what WinForms turns into the red-cross placeholder; a pure-red pixel
    ' in the bitmap is that placeholder, since no palette role is pure red.
    Private Sub CheckRailPaint()
        Dim dict = Localization.GetDict("en")
        For Each dark In New Boolean() {False, True}
            Theme.UsePaletteForTest(dark)
            Dim themeName = If(dark, "dark", "light")
            Try
                For Each row In RailRow.All
                    Dim text = RailText(dict, row, "rail-paint:" & themeName & ":" & row.Key)
                    If text Is Nothing Then Continue For
                    For Each stateName In New String() {"plain", "hover", "selected", "collapsed-holding"}
                        If stateName = "collapsed-holding" AndAlso Not row.IsGroup Then Continue For
                        If stateName = "selected" AndAlso row.IsGroup Then Continue For
                        For band = 0 To 1
                            Using e As New RailEntry With {
                                .Key = row.Key,
                                .Glyph = row.Glyph,
                                .Hue = row.Hue,
                                .Band = band,
                                .IsGroupHeader = row.IsGroup,
                                .Text = text,
                                .RowUnit = 44,
                                .Size = New Size(250, 44)
                            }
                                If stateName = "selected" Then e.Selected = True
                                If stateName = "collapsed-holding" Then
                                    e.Collapsed = True
                                    e.HasSelectedChild = True
                                End If
                                e.Height = e.PreferredRowHeight(e.Width)
                                Dim label = "rail-paint:" & themeName & ":" & row.Key & ":" & stateName & ":band" & band.ToString()
                                Try
                                    Using bmp As New Bitmap(e.Width, e.Height)
                                        Using g = Graphics.FromImage(bmp)
                                            e.PaintForTest(g, stateName = "hover")
                                        End Using
                                        Check(label, CountPureRed(bmp) = 0, "pure red in the bitmap")
                                    End Using
                                Catch ex As Exception
                                    Check(label, False, ex.GetType().Name & ": " & ex.Message)
                                End Try
                            End Using
                        Next
                    Next
                Next
            Finally
                Theme.UsePaletteForTest(Nothing)
            End Try
        Next
    End Sub

    ' A rail row's label as the window draws it, or Nothing - with a FAIL row under the check's name -
    ' when the table has no such key (SHELL-15: a missing key used to throw KeyNotFoundException out
    ' of the whole run).
    Private Function RailText(dict As Dictionary(Of String, String), row As RailRow, name As String) As String
        Dim v As String = Nothing
        If dict Is Nothing OrElse Not dict.TryGetValue(row.Key, v) Then
            Check(name, False, "no text for " & row.Key)
            Return Nothing
        End If
        Return If(row.IsGroup, v.ToUpperInvariant(), v)
    End Function

    Private Function CountPureRed(bmp As Bitmap) As Integer
        Dim n = 0
        For y = 0 To bmp.Height - 1
            For x = 0 To bmp.Width - 1
                Dim c = bmp.GetPixel(x, y)
                If c.R = 255 AndAlso c.G = 0 AndAlso c.B = 0 Then n += 1
            Next
        Next
        Return n
    End Function

    ' T4, APP-BEHAVIOUR rule 2: every rail label, in all five languages, fits the rectangle its row
    ' draws it into - wrapped where it has to be, and with no single word wider than the label. The
    ' width is the narrowest the rail gives a row (its column less the margins and a scroll bar),
    ' at this machine's DPI, because the text is measured at this machine's DPI too.
    Private Sub CheckRailLabels()
        Dim scale As Single = 1.0F
        Try
            Using g = Graphics.FromHwnd(IntPtr.Zero)
                scale = g.DpiX / 96.0F
            End Using
        Catch
        End Try
        Dim rowWidth = CInt(ShellForm.RailWidth * scale) - SystemInformation.VerticalScrollBarWidth
        Dim unit = CInt(ShellForm.RailTargetHeight * scale)

        For Each lang In Localization.Languages
            Dim dict = Localization.GetDict(lang)
            For Each row In RailRow.All
                Dim text = RailText(dict, row, "rail-label:" & lang & ":" & row.Key)
                If text Is Nothing Then Continue For
                Using e As New RailEntry With {
                    .Key = row.Key,
                    .IsGroupHeader = row.IsGroup,
                    .Glyph = row.Glyph,
                    .Hue = row.Hue,
                    .RowUnit = unit,
                    .Text = text
                }
                    Dim detail As String = ""
                    Dim fits = e.LabelFits(rowWidth, detail)
                    Check("rail-label:" & lang & ":" & row.Key, fits, detail)
                End Using
            Next
        Next
    End Sub

    ' ---- SP-0014: APP-BEHAVIOUR ----------------------------------------------

    ' T6, rule 3: both run pages draw progress by one rule - a filling bar when the run said how
    ' much there is, a marquee when it did not.
    Private Sub CheckProgressRule()
        Dim withTotals As New EventStream.ProgressInfo With {.DoneBytes = 50, .TotalBytes = 200}
        Dim itemsOnly As New EventStream.ProgressInfo With {.DoneItems = 3, .TotalItems = 4}
        Dim noTotals As New EventStream.ProgressInfo With {.DoneItems = 7}
        Dim jv As JobView = Nothing
        Dim cv As CommandView = Nothing
        Try
            jv = New JobView()
            jv.SetJob(JobCatalogue.GetJob("rail_job_fill"))
            cv = New CommandView()
            For Each pair In New Object()() {
                New Object() {"job", CType(Sub(p As EventStream.ProgressInfo) jv.FeedProgressForTest(p), Action(Of EventStream.ProgressInfo)), jv.ProgressBarForTest},
                New Object() {"command", CType(Sub(p As EventStream.ProgressInfo) cv.FeedProgressForTest(p), Action(Of EventStream.ProgressInfo)), cv.ProgressBarForTest}}
                Dim name = DirectCast(pair(0), String)
                Dim feed = DirectCast(pair(1), Action(Of EventStream.ProgressInfo))
                Dim bar = DirectCast(pair(2), ProgressBar)
                feed(withTotals)
                Check("progress:" & name & ":bytes", bar.Style = ProgressBarStyle.Continuous AndAlso bar.Value = 25,
                      bar.Style.ToString() & " " & bar.Value.ToString())
                feed(itemsOnly)
                Check("progress:" & name & ":items", bar.Style = ProgressBarStyle.Continuous AndAlso bar.Value = 75,
                      bar.Style.ToString() & " " & bar.Value.ToString())
                feed(noTotals)
                Check("progress:" & name & ":marquee", bar.Style = ProgressBarStyle.Marquee, bar.Style.ToString())
            Next
        Catch ex As Exception
            Check("progress", False, ex.GetType().Name & ": " & ex.Message)
        Finally
            If jv IsNot Nothing Then jv.Dispose()
            If cv IsNot Nothing Then cv.Dispose()
        End Try
    End Sub

    ' T8, rule 7: rendering a translated template cannot throw. The cases are the ways a template
    ' goes wrong in a translation edit - a brace dropped, an index that does not exist.
    Private Sub CheckLocalizedFormat()
        Dim cases = New Object()() {
            New Object() {"plain", "a {0} b", New Object() {"x"}, "a x b"},
            New Object() {"two", "{1}-{0}", New Object() {"x", "y"}, "y-x"},
            New Object() {"missing-arg", "a {1} b", New Object() {"x"}, "a  b"},
            New Object() {"no-args", "a {0}", New Object() {}, "a "},
            New Object() {"unclosed", "bad {0", New Object() {"x"}, "bad {0"},
            New Object() {"stray-close", "bad } here", New Object() {"x"}, "bad } here"},
            New Object() {"not-a-number", "bad {x} here", New Object() {"x"}, "bad {x} here"},
            New Object() {"escaped", "{{0}} {0}", New Object() {"x"}, "{0} x"},
            New Object() {"align", "[{0,3}]", New Object() {"x"}, "[  x]"},
            New Object() {"format", "{0:F1}", New Object() {1.25}, (1.25).ToString("F1")},
            New Object() {"bad-format", "{0:Q}", New Object() {5}, "5"}
        }
        For Each c In cases
            Dim got As String
            Try
                got = Localization.Format(DirectCast(c(1), String), DirectCast(c(2), Object()))
            Catch ex As Exception
                got = "threw " & ex.GetType().Name
            End Try
            Check("format:" & DirectCast(c(0), String), got = DirectCast(c(3), String), got)
        Next
    End Sub

    ' T12, rule 9, and B2 of the ladder: every control a user can operate has a name - its text or
    ' its accessible name - and every rail row has a default action, which is what UI Automation's
    ' Invoke reaches. The whole window is built and walked, every job page included.
    Private Sub CheckAccessibleNames()
        Dim shell As ShellForm = Nothing
        Try
            shell = New ShellForm()
            ' The window builds its job page the first time a job is chosen; the walk needs it now.
            Check("a11y:job-page-built", shell.JobViewForTest IsNot Nothing, "")
            Dim problems As New List(Of String)
            WalkAccessible(shell, problems)
            Check("a11y:window", problems.Count = 0, String.Join("; ", problems.Take(8).ToArray()))

            Dim jv As JobView = Nothing
            For Each c In AllControls(shell)
                jv = TryCast(c, JobView)
                If jv IsNot Nothing Then Exit For
            Next
            If jv IsNot Nothing Then
                For Each job In JobCatalogue.GetAllJobs()
                    jv.SetJob(job)
                    Dim jobProblems As New List(Of String)
                    WalkAccessible(jv, jobProblems)
                    Check("a11y:" & job.Id, jobProblems.Count = 0, String.Join("; ", jobProblems.Take(8).ToArray()))
                Next
            Else
                Check("a11y:job-page", False, "the window has no job page")
            End If
        Catch ex As Exception
            Check("a11y", False, ex.GetType().Name & ": " & ex.Message)
        Finally
            If shell IsNot Nothing Then shell.Dispose()
        End Try
    End Sub

    Private Sub WalkAccessible(root As Control, problems As List(Of String))
        For Each c In AllControls(root)
            Dim interactive = TypeOf c Is ButtonBase OrElse TypeOf c Is ComboBox OrElse TypeOf c Is TextBoxBase OrElse
                              TypeOf c Is LinkLabel OrElse TypeOf c Is ListBox OrElse TypeOf c Is RailEntry
            If Not interactive Then Continue For
            Dim named = Not String.IsNullOrWhiteSpace(c.Text) OrElse Not String.IsNullOrWhiteSpace(c.AccessibleName)
            ' A text box's Text is what the user typed, not its name: it must carry one of its own.
            If TypeOf c Is TextBoxBase OrElse TypeOf c Is ComboBox OrElse TypeOf c Is ListBox Then
                named = Not String.IsNullOrWhiteSpace(c.AccessibleName)
            End If
            If Not named Then problems.Add(c.GetType().Name & " " & If(c.Name, "") & " has no name")
            Dim row = TryCast(c, RailEntry)
            If row IsNot Nothing AndAlso String.IsNullOrEmpty(row.AccessibilityObject.DefaultAction) Then
                problems.Add("rail row " & row.Key & " has no default action")
            End If
        Next
    End Sub

    Private Iterator Function AllControls(root As Control) As IEnumerable(Of Control)
        For Each c As Control In root.Controls
            Yield c
            For Each inner In AllControls(c)
                Yield inner
            Next
        Next
    End Function

    ' T13, rule 10: where a saved window is put back, for screens that are invented here.
    Private Sub CheckPlacement()
        Dim primary As New WindowPlacement.ScreenArea(New Rectangle(0, 0, 1920, 1040), 96)
        Dim second As New WindowPlacement.ScreenArea(New Rectangle(1920, 0, 2560, 1400), 144)
        Dim one = New List(Of WindowPlacement.ScreenArea) From {primary}
        Dim both = New List(Of WindowPlacement.ScreenArea) From {primary, second}
        Const caption As Integer = 30

        Dim r = WindowPlacement.Place(New Rectangle(100, 100, 1200, 800), 96, both, caption)
        Check("placement:kept", r = New Rectangle(100, 100, 1200, 800), r.ToString())

        ' The second monitor is gone: moved onto the nearest screen, at its saved size.
        r = WindowPlacement.Place(New Rectangle(2200, 200, 1200, 800), 96, one, caption)
        Check("placement:vanished-monitor", primary.Area.Contains(r) AndAlso r.Size = New Size(1200, 800), r.ToString())

        ' The title strip is above the top of the screen: pulled down until it is on it.
        r = WindowPlacement.Place(New Rectangle(100, -200, 1200, 800), 96, one, caption)
        Check("placement:title-strip", r.Top >= 0 AndAlso r.Top + caption <= primary.Area.Bottom, r.ToString())

        ' Hanging off the bottom with only a sliver showing: pulled up so the strip is reachable.
        r = WindowPlacement.Place(New Rectangle(100, 1030, 1200, 800), 96, one, caption)
        Check("placement:off-bottom", r.Top + caption <= primary.Area.Bottom, r.ToString())

        ' Saved at 144 DPI, restored on a 96 DPI screen: two thirds of the size.
        r = WindowPlacement.Place(New Rectangle(100, 100, 1500, 900), 144, one, caption)
        Check("placement:dpi-scaled", r.Size = New Size(1000, 600), r.ToString())

        ' Larger than the screen: no larger than its working area.
        r = WindowPlacement.Place(New Rectangle(0, 0, 4000, 3000), 96, one, caption)
        Check("placement:clamped", r.Width <= 1920 AndAlso r.Height <= 1040, r.ToString())

        r = WindowPlacement.Place(New Rectangle(0, 0, 800, 600), 96, New List(Of WindowPlacement.ScreenArea)(), caption)
        Check("placement:no-screens", r.IsEmpty, r.ToString())

        ' SHELL-08: a window spread over both monitors, its title strip on the first, stays spread.
        r = WindowPlacement.Place(New Rectangle(1500, 100, 1200, 800), 144, both, caption)
        Check("placement:spanning", r = New Rectangle(1500, 100, 1200, 800), r.ToString())

        ' SHELL-09: values no real window has are not restored at all - they used to overflow the
        ' scaling and keep the window from opening.
        Check("placement:huge-x", Not ShellSettings.PlacementIsSane(2147483000, 100, 1200, 800, 96), "")
        Check("placement:tiny-dpi", Not ShellSettings.PlacementIsSane(100, 100, 1200, 800, 1), "")
        Check("placement:zero-width", Not ShellSettings.PlacementIsSane(100, 100, 0, 800, 96), "")
        Check("placement:sane", ShellSettings.PlacementIsSane(-1920, 0, 1200, 800, 144) AndAlso
                                ShellSettings.PlacementIsSane(100, 100, 1200, 800, 0), "")

        ' SHELL-07: maximized, then minimized, then closed - it reopens maximized.
        Check("placement:min-after-max", ShellForm.SavesMaximized(FormWindowState.Minimized, FormWindowState.Maximized) AndAlso
                                         Not ShellForm.SavesMaximized(FormWindowState.Minimized, FormWindowState.Normal) AndAlso
                                         ShellForm.SavesMaximized(FormWindowState.Maximized, FormWindowState.Maximized), "")
    End Sub

    ' Compact use of the screen (UI_UX): no top-level window may need more room than the screen it
    ' opens on has. The matrix is the screens people have - 1366x768, 1536x864, 1920x1080 - at the
    ' scalings Windows offers, with the taskbar taken off the height (48 design pixels, Windows 11's
    ' tall one, so the answer holds for the 40 of Windows 10 too).
    Private Sub CheckWindowFit()
        Const taskbar As Integer = 48
        Const caption As Integer = 30
        Dim designs As New List(Of Object()) From {
            New Object() {"shell", ShellForm.MinDesignWidth, ShellForm.MinDesignHeight},
            New Object() {"disks", DiskManagerForm.MinWidth, DiskManagerForm.MinHeight}
        }
        Dim screens = New Integer()() {New Integer() {1366, 768}, New Integer() {1536, 864}, New Integer() {1920, 1080}}

        For Each sc In screens
            For Each pct In New Integer() {100, 125, 150, 175, 200}
                Dim scale = pct / 100.0
                Dim work As New Rectangle(0, 0, sc(0), sc(1) - CInt(Math.Round(taskbar * scale)))
                Dim tag = sc(0).ToString() & "x" & sc(1).ToString() & "@" & pct.ToString()

                ' The minimum, stated in this screen's pixels, is never more than its working area - and is
                ' the whole design minimum wherever that fits, so a roomy screen loses nothing.
                For Each d In designs
                    Dim wanted As New Size(CInt(Math.Round(CInt(d(1)) * scale)), CInt(Math.Round(CInt(d(2)) * scale)))
                    Dim got = WindowPlacement.FitMinimum(wanted, work.Size)
                    Dim fits = wanted.Width <= work.Width AndAlso wanted.Height <= work.Height
                    Check("placement:min:" & CStr(d(0)) & ":" & tag,
                          got.Width <= work.Width AndAlso got.Height <= work.Height AndAlso (Not fits OrElse got = wanted) AndAlso
                          got.Width > 0 AndAlso got.Height > 0, got.ToString() & " in " & work.Size.ToString())
                Next

                ' A rectangle saved on a larger screen - the old 940x640 minimum, a maximised window's
                ' restore size, a 4K window's 1760x1120 - comes back wholly inside this one, whichever
                ' corner it was saved at.
                For Each saved In New Rectangle() {
                    New Rectangle(300, 200, CInt(Math.Round(1760 * scale)), CInt(Math.Round(1120 * scale))),
                    New Rectangle(sc(0) - 400, sc(1) - 300, CInt(Math.Round(1760 * scale)), CInt(Math.Round(1120 * scale))),
                    New Rectangle(-1200, -900, 4000, 3000)}
                    Dim r = WindowPlacement.Place(saved, CInt(Math.Round(96 * scale)), New List(Of WindowPlacement.ScreenArea) From {
                                                      New WindowPlacement.ScreenArea(work, CInt(Math.Round(96 * scale)))}, caption)
                    Check("placement:fit:" & tag & ":" & saved.X.ToString() & "," & saved.Y.ToString() & " " & saved.Width.ToString() & "x" & saved.Height.ToString(),
                          Not r.IsEmpty AndAlso work.Contains(r), r.ToString() & " in " & work.ToString())
                Next
            Next
        Next

        ' The same oversize rectangle, saved on a big second monitor that is no longer there.
        Dim laptop As New WindowPlacement.ScreenArea(New Rectangle(0, 0, 1366, 720), 96)
        Dim lone = New List(Of WindowPlacement.ScreenArea) From {laptop}
        Dim away = WindowPlacement.Place(New Rectangle(3000, 400, 2000, 1300), 96, lone, caption)
        Check("placement:fit:monitor-gone", Not away.IsEmpty AndAlso laptop.Area.Contains(away), away.ToString())

        ' A window that is not too big keeps its size and, wholly on a screen, its place.
        Dim kept = WindowPlacement.Place(New Rectangle(100, 60, 900, 600), 96, lone, caption)
        Check("placement:fit:small-kept", kept = New Rectangle(100, 60, 900, 600), kept.ToString())

        ' What the real windows ask for on this machine: no more than its primary screen's working area.
        Dim shell As ShellForm = Nothing
        Dim mgr As DiskManagerForm = Nothing
        Dim wa = Screen.PrimaryScreen.WorkingArea
        Dim wasReads = DiskManagerForm.SuppressReads
        Dim wasWelcome = DiskManagerForm.SuppressWelcome
        Dim wasLang = ShellSettings.LanguageOverride
        Try
            DiskManagerForm.SuppressReads = True
            DiskManagerForm.SuppressWelcome = True
            shell = New ShellForm()
            Check("placement:min-real:shell", shell.MinimumSize.Width <= wa.Width AndAlso shell.MinimumSize.Height <= wa.Height AndAlso
                                              shell.Width <= wa.Width AndAlso shell.Height <= wa.Height,
                  shell.MinimumSize.ToString() & " / " & shell.Size.ToString() & " in " & wa.Size.ToString())
            mgr = New DiskManagerForm()
            Check("placement:min-real:disks", mgr.MinimumSize.Width <= wa.Width AndAlso mgr.MinimumSize.Height <= wa.Height AndAlso
                                              mgr.Width <= wa.Width AndAlso mgr.Height <= wa.Height,
                  mgr.MinimumSize.ToString() & " / " & mgr.Size.ToString() & " in " & wa.Size.ToString())
            mgr.Dispose()
            mgr = Nothing
            shell.Dispose()
            shell = Nothing

            ' At the smallest size the windows can be made, what is on them still reads: the shell's
            ' title and subtitle wrap inside their column (the longest titles are German and Russian),
            ' the expert page keeps a run card, the Disk manager keeps a list.
            For Each lang In New String() {"de", "ru"}
                ShellSettings.LanguageOverride = lang
                Localization.ResetShellDict()
                shell = New ShellForm()
                shell.StartPosition = FormStartPosition.Manual
                shell.ShowInTaskbar = False
                shell.Show()
                shell.Bounds = New Rectangle(-32000, -32000, shell.MinimumSize.Width, shell.MinimumSize.Height)
                shell.ShowPageForCapture("rail_job_vd_new", "")
                Settle()
                Check("placement:min-layout:shell-title:" & lang, shell.HeaderTextFitsForTest(), shell.ClientSize.ToString())
                shell.ShowPageForCapture("rail_job_command", "")
                Settle()
                Dim runFloor = Ui.Px(shell, 300)
                Check("placement:min-layout:command-run-card:" & lang, shell.CommandViewForTest.RunCardHeightForTest >= runFloor - Ui.Px(shell, 4),
                      shell.CommandViewForTest.RunCardHeightForTest.ToString() & " of " & runFloor.ToString())
                shell.Dispose()
                shell = Nothing

                mgr = New DiskManagerForm()
                mgr.StartPosition = FormStartPosition.Manual
                mgr.ShowInTaskbar = False
                mgr.Show()
                mgr.Bounds = New Rectangle(-32000, -32000, mgr.MinimumSize.Width, mgr.MinimumSize.Height)
                Dim snap = GoldenSnapshot()
                mgr.ApplySnapshotForTest(snap, "")
                mgr.SelectForTest(snap.Disks.First(Function(x) x.Name = "secrets").Key)
                Settle()
                Dim least = Ui.Px(mgr, 150)
                Check("placement:min-layout:disks-list:" & lang, mgr.ListHeightForTest >= least - Ui.Px(mgr, 6),
                      mgr.ListHeightForTest.ToString() & " of " & least.ToString() & " (" & mgr.HeightReportForTest & ")")
                mgr.Dispose()
                mgr = Nothing
            Next
        Finally
            ShellSettings.LanguageOverride = wasLang
            Localization.ResetShellDict()
            DiskManagerForm.SuppressReads = wasReads
            DiskManagerForm.SuppressWelcome = wasWelcome
            If shell IsNot Nothing Then shell.Dispose()
            If mgr IsNot Nothing Then mgr.Dispose()
        End Try
    End Sub

    Private Sub Settle()
        For i = 1 To 5
            Application.DoEvents()
            Threading.Thread.Sleep(40)
        Next
    End Sub

    ' T10 and T11, rule 5: the Wipe page confirms what is really there, and only what it can run.
    Private Sub CheckWipeRules()
        Dim jv As JobView = Nothing
        Dim cv As CommandView = Nothing
        Dim reason As String = ""
        Try
            Dim wipe = JobCatalogue.GetJob("rail_job_wipe")
            jv = New JobView()
            jv.SetJob(wipe)
            jv.SetTarget("C:\sample-that-is-not-there")

            jv.SetWipeInputsForTest("WIPE", False)
            Dim runs = jv.RunStateForTest(reason)
            Check("wipe:needs-y", Not runs AndAlso reason <> "", runs.ToString() & " / " & reason)

            jv.SetWipeInputsForTest("", True)
            runs = jv.RunStateForTest(reason)
            Check("wipe:needs-typed-word", Not runs, runs.ToString())

            jv.SetWipeInputsForTest("WIPE", True)
            runs = jv.RunStateForTest(reason)
            Check("wipe:typed-and-y-runs", runs, runs.ToString() & " / " & reason)

            jv.SetCountForTest(0, 0)
            runs = jv.RunStateForTest(reason)
            Check("wipe:empty-is-nothing", Not runs AndAlso reason <> "", runs.ToString() & " / " & reason)

            jv.SetCountForTest(0, 3)
            runs = jv.RunStateForTest(reason)
            Check("wipe:empty-folders-still-confirm", runs, runs.ToString() & " / " & reason)

            jv.SetTarget("C:\")
            jv.SetWipeInputsForTest("WIPE", True)
            runs = jv.RunStateForTest(reason)
            Check("wipe:drive-root-needs-console", Not runs AndAlso reason <> "", runs.ToString() & " / " & reason)

            Check("wipe-safety:root", WipeSafety.DangerKey("D:\") = "shell_wipe_danger_root", WipeSafety.DangerKey("D:\"))
            Check("wipe-safety:share", WipeSafety.DangerKey("\\server\share") = "shell_wipe_danger_share",
                  WipeSafety.DangerKey("\\server\share"))
            Check("wipe-safety:temp", WipeSafety.DangerKey(IO.Path.GetTempPath()) = "shell_wipe_danger_temp",
                  WipeSafety.DangerKey(IO.Path.GetTempPath()))
            Check("wipe-safety:folder", WipeSafety.DangerKey("C:\sample\folder") = "", WipeSafety.DangerKey("C:\sample\folder"))
            CheckWipeSafetyParity()

            cv = New CommandView()
            Check("command:wipe-asks-word", Not cv.RunEnabledForTest("filedo.exe C:\sample wipe -y", ""), "")
            Check("command:wipe-typed-runs", cv.RunEnabledForTest("filedo.exe C:\sample wipe -y", "WIPE"), "")
            Check("command:wipe-without-y-asks-word", Not cv.RunEnabledForTest("filedo.exe C:\sample wipe", ""), "")
            Check("command:wipe-root-refused", Not cv.RunEnabledForTest("filedo.exe D:\ wipe -y", "WIPE"), "")
            Check("command:no-wipe-no-word", cv.RunEnabledForTest("filedo.exe E: info", ""), "")
            Check("command:duplicate-delete-asks-word", Not cv.RunEnabledForTest("filedo.exe E: cd del", ""), "")
            Check("command:duplicate-delete-confirmed", cv.RunEnabledForTest("filedo.exe E: cd del", "DELETE"), "")
            Check("command:compare-delete-asks-word", Not cv.RunEnabledForTest("filedo.exe compare C:\a D:\b del source --yes", ""), "")
            Check("command:recover-asks-word", Not cv.RunEnabledForTest("filedo.exe E: recover yes", ""), "")
            Check("command:recover-confirmed", cv.RunEnabledForTest("filedo.exe E: recover yes", "RECOVER"), "")
            Check("command:repair-alias-asks-word", Not cv.RunEnabledForTest("filedo.exe E: repair yes", ""), "")
            Check("command:probe-read-only", cv.RunEnabledForTest("filedo.exe E: probe", ""), "")
            Check("command:probe-fix-asks-word", Not cv.RunEnabledForTest("filedo.exe E: probe fix yes", ""), "")
            Check("command:probe-fix-confirmed", cv.RunEnabledForTest("filedo.exe E: probe fix yes", "FIX"), "")
            Check("command:probe-repair-alias-confirmed", cv.RunEnabledForTest("filedo.exe E: probe repair yes", "FIX"), "")
            Check("command:probe-format-asks-word", Not cv.RunEnabledForTest("filedo.exe E: probe format yes", ""), "")
            Check("command:probe-format-confirmed", cv.RunEnabledForTest("filedo.exe E: probe format yes", "FORMAT"), "")
            Check("command:disk-format-asks-word", Not cv.RunEnabledForTest("filedo.exe C:\disk.fdd format fs ntfs force", ""), "")
            Check("command:disk-format-confirmed", cv.RunEnabledForTest("filedo.exe C:\disk.fdd format fs ntfs force", "FORMAT"), "")
            Check("command:disk-destroy-asks-word", Not cv.RunEnabledForTest("filedo.exe C:\disk.fdd destroy force", ""), "")
            Check("command:disk-destroy-confirmed", cv.RunEnabledForTest("filedo.exe C:\disk.fdd destroy force", "DESTROY"), "")
            Check("command:disk-discard-asks-word", Not cv.RunEnabledForTest("filedo.exe C:\disk.fdd unmount force nosave", ""), "")
            Check("command:disk-discard-confirmed", cv.RunEnabledForTest("filedo.exe C:\disk.fdd unmount force nosave", "DISCARD"), "")
            ' SP-0097 F5: a hand-over of the very line whose word is already typed still asks again -
            ' only SetCommand's own reset can clear it, as the line itself does not change.
            Dim destroyLine = "filedo.exe C:\disk.fdd destroy force"
            Dim destroyReady = cv.RunEnabledForTest(destroyLine, "DESTROY")
            cv.SetCommand(destroyLine)
            Check("command:disk-handoff-clears-old-word", destroyReady AndAlso Not cv.RunEnabledNowForTest, destroyReady.ToString())
            ' AUD-15-F2: the same word typed for one line does not carry to the next one.
            Dim wipeReady = cv.RunEnabledForTest("filedo.exe C:\sample-a wipe -y", "WIPE")
            cv.SetLineForTest("filedo.exe C:\sample-b wipe -y")
            Check("command:wipe-word-not-carried", wipeReady AndAlso Not cv.RunEnabledNowForTest, wipeReady.ToString())

            ' AUD-15-F3: a probe that carries yes writes raw sectors; a duplicate move moves files.
            Check("command:probe-yes-asks-word", Not cv.RunEnabledForTest("filedo.exe E: probe yes", ""), "")
            Check("command:probe-yes-confirmed", cv.RunEnabledForTest("filedo.exe E: probe yes", "PROBE"), "")
            Check("command:dup-move-asks-word", Not cv.RunEnabledForTest("filedo.exe D:\x cd move D:\dups -y", ""), "")
            Check("command:dup-move-confirmed", cv.RunEnabledForTest("filedo.exe D:\x cd move D:\dups -y", "MOVE"), "")

            ' AUD-13-F2: the lines the job pages hand over after asking their own question are asked
            ' again here - unmount nosave by drive letter, clean --yes, probe yes.
            Check("command:unmount-letter-nosave-asks-discard", Not cv.RunEnabledForTest("filedo.exe X: unmount force nosave", ""), "")
            Check("command:unmount-letter-nosave-confirmed", cv.RunEnabledForTest("filedo.exe X: unmount force nosave", "DISCARD"), "")
            Check("command:clean-yes-asks-word", Not cv.RunEnabledForTest("filedo.exe E: clean --yes", ""), "")
            Check("command:clean-yes-confirmed", cv.RunEnabledForTest("filedo.exe E: clean --yes", "CLEAN"), "")
            Check("command:clean-without-yes-no-word", cv.RunEnabledForTest("filedo.exe E: clean", ""), "")
            Dim hv As JobView = Nothing
            Try
                hv = New JobView()
                For Each pair In New String()() {
                    New String() {"rail_job_clean", "E:", "clean"},
                    New String() {"rail_job_probe", "E:", "probe"},
                    New String() {"rail_job_vd_unmount", "X:", "unmount"}}
                    hv.SetJob(JobCatalogue.GetJob(pair(0)))
                    hv.SetTarget(pair(1))
                    If pair(2) = "unmount" Then hv.SetDiskForTest(New DiskOptions With {.NoSave = True}, "", "", DiskOptionsPanel.DiscardWord)
                    Dim handed = hv.CurrentCommand()
                    cv.SetCommand(handed)
                    Check("command:handover-" & pair(2) & "-asks-word", handed.Contains(pair(2)) AndAlso Not cv.RunEnabledNowForTest, handed)
                Next
            Finally
                If hv IsNot Nothing Then hv.Dispose()
            End Try

            ' T2-F2: every spelling the CLI accepts for a vd verb that loses data - the vd namespace
            ' form, a registered name, and the destroy alias erase.
            For Each c In New String()() {
                New String() {"vd-destroy", "filedo.exe vd destroy C:\disk.fdd force", "DESTROY"},
                New String() {"vd-destroy-name", "filedo.exe vd erase work -y", "DESTROY"},
                New String() {"vdisk-format", "filedo.exe vdisk format C:\disk.fdd force", "FORMAT"},
                New String() {"fdd-erase", "filedo.exe C:\disk.fdd erase force", "DESTROY"},
                New String() {"vd-unmount-nosave", "filedo.exe vd detach work nosave", "DISCARD"}}
                Check("command:" & c(0) & "-asks-word",
                      Not cv.RunEnabledForTest(c(1), "") AndAlso cv.RunEnabledForTest(c(1), c(2)), c(1))
            Next
            Check("command:vd-info-no-word", cv.RunEnabledForTest("filedo.exe vd info work", ""), "")
            CheckVdWordsMatchCli()

            ' GUI-11: the CLI's other word for wipe asks for the typed word too, and so does a batch
            ' whose list wipes.
            Check("command:wipe-alias-asks-word", Not cv.RunEnabledForTest("filedo.exe C:\sample w -y", ""), "")
            Check("command:wipe-alias-typed-runs", cv.RunEnabledForTest("filedo.exe C:\sample w -y", "WIPE"), "")
            Dim listDir = Path.Combine(Path.GetTempPath(), "filedo_selftest_lists_" & Guid.NewGuid().ToString("N"))
            Directory.CreateDirectory(listDir)
            Try
                Dim wipingList = Path.Combine(listDir, "wipes.lst")
                File.WriteAllText(wipingList, "# a batch" & vbLf & "C:\sample-x info" & vbLf & "C:\sample-x W -y" & vbLf)
                Dim plainList = Path.Combine(listDir, "plain.lst")
                File.WriteAllText(plainList, "C:\sample-x info" & vbLf)
                Dim deletingList = Path.Combine(listDir, "deletes.lst")
                File.WriteAllText(deletingList, "C:\sample-x cd del" & vbLf)
                Dim nestedList = Path.Combine(listDir, "nested.lst")
                File.WriteAllText(nestedList, "from " & ArgQuoting.EscapeArg(wipingList) & vbLf)
                Check("command:from-wipe-asks-word", Not cv.RunEnabledForTest("filedo.exe from " & ArgQuoting.EscapeArg(wipingList), ""), "")
                Check("command:nested-from-wipe-asks-word", Not cv.RunEnabledForTest("filedo.exe from " & ArgQuoting.EscapeArg(nestedList), ""), "")
                Check("command:from-plain-no-word", cv.RunEnabledForTest("filedo.exe from " & ArgQuoting.EscapeArg(plainList), ""), "")
                Check("command:from-duplicate-delete-asks-word", Not cv.RunEnabledForTest("filedo.exe from " & ArgQuoting.EscapeArg(deletingList), ""), "")
                ' SP-0097 F5: Run is offered for the plain list, then the list turns into a wipe before
                ' the click - only the launch re-check can refuse it.
                Dim plainReady = cv.RunEnabledForTest("filedo.exe from " & ArgQuoting.EscapeArg(plainList), "")
                File.WriteAllText(plainList, "from " & ArgQuoting.EscapeArg(wipingList) & vbLf)
                Check("command:from-changed-at-launch", plainReady AndAlso Not cv.LaunchAllowedForTest(), plainReady.ToString())
                ' AUD-58-F1: the strongest word over the whole tree, not the first one found.
                Dim delThenWipe = Path.Combine(listDir, "del-then-wipe.lst")
                File.WriteAllText(delThenWipe, "C:\sample-x cd del -y" & vbLf & "C:\sample-x w -y" & vbLf)
                Dim delThenWipeLine = "filedo.exe from " & ArgQuoting.EscapeArg(delThenWipe)
                Check("command:from-delete-then-wipe-asks-wipe",
                      Not cv.RunEnabledForTest(delThenWipeLine, "DELETE") AndAlso cv.RunEnabledForTest(delThenWipeLine, "WIPE"), "")
                Check("command:from-delete-then-wipe-line-wipes", CommandView.LineWipesForTest(delThenWipeLine), "")
                Dim recoverList = Path.Combine(listDir, "recover.lst")
                File.WriteAllText(recoverList, "E: recover yes" & vbLf)
                Dim nestedRecoverWipe = Path.Combine(listDir, "nested-recover-wipe.lst")
                File.WriteAllText(nestedRecoverWipe, "from " & ArgQuoting.EscapeArg(recoverList) & vbLf & "from " & ArgQuoting.EscapeArg(wipingList) & vbLf)
                Dim nestedLine = "filedo.exe from " & ArgQuoting.EscapeArg(nestedRecoverWipe)
                Check("command:nested-recover-then-wipe-asks-wipe",
                      Not cv.RunEnabledForTest(nestedLine, "RECOVER") AndAlso cv.RunEnabledForTest(nestedLine, "WIPE"), "")
                Dim moveThenDel = Path.Combine(listDir, "move-then-del.lst")
                File.WriteAllText(moveThenDel, "D:\x cd move D:\dups -y" & vbLf & "E: cd del -y" & vbLf)
                Dim moveThenDelLine = "filedo.exe from " & ArgQuoting.EscapeArg(moveThenDel)
                Check("command:from-move-then-delete-asks-delete",
                      Not cv.RunEnabledForTest(moveThenDelLine, "MOVE") AndAlso cv.RunEnabledForTest(moveThenDelLine, "DELETE"), "")
                File.WriteAllText(nestedList, "from " & ArgQuoting.EscapeArg(nestedList) & vbLf)
                Check("command:from-cycle-asks-word", Not cv.RunEnabledForTest("filedo.exe from " & ArgQuoting.EscapeArg(nestedList), ""), "")
                Check("command:from-missing-asks-word",
                      Not cv.RunEnabledForTest("filedo.exe from " & ArgQuoting.EscapeArg(Path.Combine(listDir, "none.lst")), ""), "")
            Finally
                Try
                    Directory.Delete(listDir, True)
                Catch
                End Try
            End Try

            ' SHELL-04 on the Command page: a relative target is read where filedo.exe runs, and
            ' ".." there is LocalAppData - which holds FileDO's own data.
            Check("command:relative-wipe-refused", Not cv.RunEnabledForTest("filedo.exe .. wipe -y", "WIPE"), "")
        Catch ex As Exception
            Check("wipe", False, ex.GetType().Name & ": " & ex.Message)
        Finally
            If jv IsNot Nothing Then jv.Dispose()
            If cv IsNot Nothing Then cv.Dispose()
        End Try
    End Sub

    ' T2-F2: the Command page's destructive vd words are the CLI's (cmd\filedo\vdisk_verbs.go, the
    ' copy embedded in this exe). A word added on one side only fails here.
    Private Sub CheckVdWordsMatchCli()
        Dim src As String = Nothing
        Using s = GetType(SelfTest).Assembly.GetManifestResourceStream("FileDOGUI.vdisk_verbs.go")
            If s IsNot Nothing Then
                Using r As New StreamReader(s, Encoding.UTF8)
                    src = r.ReadToEnd()
                End Using
            End If
        End Using
        If src Is Nothing Then
            Check("command:vd-words-match-cli", False, "missing FileDOGUI.vdisk_verbs.go")
            Return
        End If
        Dim problems As New List(Of String)
        For Each pair In New Object()() {
            New Object() {"list_of_flags_for_vd", DiskCommands.VdNamespaceWords},
            New Object() {"vdFormatWords", DiskCommands.VdFormatWords},
            New Object() {"vdDestroyWords", DiskCommands.VdDestroyWords},
            New Object() {"vdUnmountWords", DiskCommands.VdUnmountWords}}
            Dim name = DirectCast(pair(0), String)
            Dim mine = DirectCast(pair(1), String())
            Dim m = System.Text.RegularExpressions.Regex.Match(src, "\b" & name & "\s*=\s*\[\]string\{([^}]*)\}")
            If Not m.Success Then
                problems.Add(name & " not found")
                Continue For
            End If
            Dim theirs = System.Text.RegularExpressions.Regex.Matches(m.Groups(1).Value, """([^""]*)""").
                         Cast(Of System.Text.RegularExpressions.Match)().Select(Function(x) x.Groups(1).Value).OrderBy(Function(x) x).ToArray()
            Dim ours = mine.Select(Function(x) x.ToLowerInvariant()).OrderBy(Function(x) x).ToArray()
            If Not theirs.SequenceEqual(ours) Then problems.Add(name & ": CLI {" & String.Join(",", theirs) & "} GUI {" & String.Join(",", ours) & "}")
        Next
        Check("command:vd-words-match-cli", problems.Count = 0, String.Join("; ", problems))
    End Sub

    ' AUD-28-F1: the page calls dangerous every target the CLI's classifyWipeTarget does
    ' (cmd\filedo\wipe_handler.go) - anything below Windows, Program Files or FileDO's data folder,
    ' the profiles folder, a root under a device-namespace spelling, and a protected folder reached
    ' through a junction - so the page refuses up front what the CLI would cancel on a console.
    Private Sub CheckWipeSafetyParity()
        Try
            Dim sysRoot = Environment.GetEnvironmentVariable("SystemRoot")
            Dim lad = Environment.GetFolderPath(Environment.SpecialFolder.LocalApplicationData)
            Dim pf = Environment.GetEnvironmentVariable("ProgramFiles")
            Dim sysDrive = Environment.GetEnvironmentVariable("SystemDrive")
            For Each row In New String()() {
                New String() {"inside-windows", Path.Combine(sysRoot, "System32"), ""},
                New String() {"inside-filedo-data", Path.Combine(lad, "FileDO", "state"), ""},
                New String() {"inside-program-files", Path.Combine(pf, "Common Files"), ""},
                New String() {"users-folder", sysDrive & "\Users", ""},
                New String() {"dot-root", "\\?\C:\.", "shell_wipe_danger_root"},
                New String() {"device-drive-root", "\\.\C:\", "shell_wipe_danger_root"},
                New String() {"volume-guid-root", "\\?\Volume{00000000-0000-0000-0000-000000000000}\", "shell_wipe_danger_volume"},
                New String() {"globalroot-root", "\\?\GLOBALROOT\Device\HarddiskVolume3", "shell_wipe_danger_volume"},
                New String() {"device-unc-share-root", "\\?\UNC\server\share\", "shell_wipe_danger_share"},
                New String() {"trailing-dot-root", "D:\.", "shell_wipe_danger_root"}}
                Dim k = WipeSafety.DangerKey(row(1))
                Check("wipe-safety:" & row(0), If(row(2) = "", k <> "", k = row(2)), row(1) & " -> " & k)
            Next
            ' A sibling that only shares a prefix with a guarded folder is not inside it.
            Dim sibling = Path.Combine(lad, "FileDOX-selftest")
            Check("wipe-safety:prefix-sibling-not-inside", WipeSafety.DangerKey(sibling) = "", WipeSafety.DangerKey(sibling))

            Dim jdir = Path.Combine(Path.GetTempPath(), "filedo_selftest_junction_" & Guid.NewGuid().ToString("N"))
            Dim link = Path.Combine(jdir, "win")
            Try
                Directory.CreateDirectory(jdir)
                Dim psi As New Diagnostics.ProcessStartInfo("cmd.exe", "/c mklink /J """ & link & """ """ & sysRoot & """") With {
                    .UseShellExecute = False, .CreateNoWindow = True}
                Using p = Diagnostics.Process.Start(psi)
                    p.WaitForExit(15000)
                End Using
                If Directory.Exists(link) Then
                    Dim k = WipeSafety.DangerKey(Path.Combine(link, "System32"))
                    Check("wipe-safety:junction-into-windows", k <> "", k)
                Else
                    Check("wipe-safety:junction-into-windows", False, "could not create the junction")
                End If
            Finally
                ' Non-recursive on purpose: RemoveDirectory drops the junction and never follows it.
                Try
                    If Directory.Exists(link) Then Directory.Delete(link, False)
                Catch
                End Try
                Try
                    If Directory.Exists(jdir) Then Directory.Delete(jdir, False)
                Catch
                End Try
            End Try
        Catch ex As Exception
            Check("wipe-safety:parity", False, ex.GetType().Name & ": " & ex.Message)
        End Try
    End Sub

    ' T9, rule 1: a question's Escape presses its no-action answer.
    Private Sub CheckDialogEscape()
        Try
            Dim cancel = ShellDialog.CancelOf(New String() {"Stop and close", "Keep running"}, 1)
            Check("dialog:escape-is-no-action", cancel = "Keep running", cancel)
        Catch ex As Exception
            Check("dialog", False, ex.GetType().Name & ": " & ex.Message)
        End Try
    End Sub

    ' APP-BEHAVIOUR rule 5 (0.12), APP-STYLE section 4: in a confirmation of a destructive action the
    ' safe answer is the default - the button Enter presses and the one that holds the focus - and
    ' Escape gives it too; the acting answer is a different button, painted danger and never the
    ' default. Every definition in DestructiveDialogs, built (not shown) in every language.
    Private Sub CheckDestructiveDialogDefaults()
        Try
            Dim ids As New HashSet(Of String)()
            For Each lang In Localization.Languages
                For Each entry In DestructiveDialogs.AllForTest(Localization.GetDict(lang))
                    Dim spec = entry.Value
                    ids.Add(entry.Key)
                    Dim inRange = spec.DangerAt >= 0 AndAlso spec.DangerAt < spec.Choices.Length AndAlso
                                  spec.DefaultAt >= 0 AndAlso spec.DefaultAt < spec.Choices.Length
                    Dim shown = If(inRange, ShellDialog.DefaultOf(spec), "")
                    Dim escape = If(inRange, ShellDialog.CancelOf(spec.Choices, spec.CancelAt), "")
                    Dim safe = inRange AndAlso shown <> "" AndAlso shown = spec.Choices(spec.DefaultAt) AndAlso
                               escape = shown AndAlso spec.DefaultAt <> spec.DangerAt AndAlso
                               shown <> spec.Choices(spec.DangerAt)
                    Check("dialog:destructive-default-safe:" & entry.Key & ":" & lang, safe,
                          "default=" & shown & ", escape=" & escape & ", danger=" & spec.DangerAt.ToString())
                Next
            Next
            Check("dialog:destructive-default-safe:covered", ids.Count = 9, ids.Count.ToString() & " definitions")
        Catch ex As Exception
            Check("dialog-destructive", False, ex.GetType().Name & ": " & ex.Message)
        End Try
    End Sub

    ' ---- SP-0029: the shell's robustness remediation --------------------------

    ' GUI-20, GUI-21: the argument rules, one row per case.
    Private Sub CheckArgQuotingCases()
        For Each c In ArgQuotingTests.NamedCases()
            Check("argquoting:" & c.Key, c.Value)
        Next
    End Sub

    ' GUI-01 and SHELL-04: what a page does with the text of step 2.
    Private Sub CheckTargetRules()
        Dim en = Localization.GetDict(ShellSettings.Language())
        Dim notAbsolute = en("shell_target_not_absolute")
        Dim jv As JobView = Nothing
        Dim reason As String = ""
        Try
            jv = New JobView()

            ' GUI-01: a folder with "(" in its name is a folder, not a drive row.
            jv.SetJob(JobCatalogue.GetJob("rail_job_duplicates"))
            jv.SetTarget("D:\Photos (2019)")
            Dim cmd = jv.CurrentCommand()
            Check("page:cd:paren", cmd.Contains("""D:\Photos (2019)""") AndAlso Not cmd.Contains(" D: "), cmd)

            jv.SetJob(JobCatalogue.GetJob("rail_job_reveal"))
            jv.SetTarget("C:\taxes (1).pdf.fd-sec")
            cmd = jv.CurrentCommand()
            Check("page:reveal:paren", cmd.Contains("""C:\taxes (1).pdf.fd-sec"""), cmd)

            ' A drive row the page listed itself is still read as its letter.
            jv.SetJob(JobCatalogue.GetJob("rail_job_capacity"))
            Dim row = jv.TargetTextForTest
            cmd = jv.CurrentCommand()
            Check("page:drive-row-is-a-letter", row = "" OrElse Not row.Contains("(") OrElse
                                                (Not cmd.Contains("(") AndAlso cmd.Contains(" " & row.Substring(0, 2) & " ")),
                  row & " -> " & cmd)

            ' SHELL-04: relative and drive-relative targets are refused with the reason.
            jv.SetJob(JobCatalogue.GetJob("rail_job_wipe"))
            jv.SetTarget("..")
            jv.SetWipeInputsForTest("WIPE", True)
            Dim runs = jv.RunStateForTest(reason)
            Check("wipe:relative-refused", Not runs AndAlso reason = notAbsolute, runs.ToString() & " / " & reason)
            jv.SetTarget("D:x")
            jv.SetWipeInputsForTest("WIPE", True)
            runs = jv.RunStateForTest(reason)
            Check("wipe:drive-relative-refused", Not runs AndAlso reason = notAbsolute, runs.ToString() & " / " & reason)
            jv.SetTarget("C:\sample-that-is-not-there\sub\..")
            jv.SetWipeInputsForTest("WIPE", True)
            runs = jv.RunStateForTest(reason)
            cmd = jv.CurrentCommand()
            Check("wipe:absolute-folded", runs AndAlso cmd.Contains("C:\sample-that-is-not-there wipe") AndAlso Not cmd.Contains(".."),
                  cmd & " / " & reason)

            jv.SetJob(JobCatalogue.GetJob("rail_job_compare"))
            jv.SetTarget("C:\sample-a")
            runs = jv.RunStateForTest(reason)
            Check("page:compare-needs-dest", Not runs, runs.ToString())
        Finally
            If jv IsNot Nothing Then jv.Dispose()
        End Try

        Check("target:resolve-folds-dots", TargetPath.Resolve("C:\a\..\b") = "C:\b", TargetPath.Resolve("C:\a\..\b"))
        Check("target:drive-token-kept", TargetPath.Resolve("E:") = "E:", TargetPath.Resolve("E:"))
        Check("target:relative-refused", TargetPath.Resolve("reports") Is Nothing AndAlso TargetPath.Resolve("D:x") Is Nothing AndAlso
                                         TargetPath.Resolve("\x") Is Nothing, "")
        Check("target:unc-kept", TargetPath.Resolve("\\server\share\x") = "\\server\share\x", TargetPath.Resolve("\\server\share\x"))

        ' SHELL-04: the Explorer start - a bare name is the file in the folder it was typed in.
        Dim dir = Path.Combine(Path.GetTempPath(), "filedo_selftest_start_" & Guid.NewGuid().ToString("N"))
        Dim oldCwd = Environment.CurrentDirectory
        Try
            Directory.CreateDirectory(dir)
            File.WriteAllText(Path.Combine(dir, "notes.txt"), "x")
            Environment.CurrentDirectory = dir
            Dim got = Program.StartupTargetFrom(New String() {"filedo_win.exe", "notes.txt"})
            Check("startup:relative-target", String.Equals(got, Path.Combine(dir, "notes.txt"), StringComparison.OrdinalIgnoreCase), got)
            Check("startup:switch-skipped", Program.StartupTargetFrom(New String() {"filedo_win.exe", "-debug"}) Is Nothing, "")
            ' SP-0004 spec 7.2 item 2: a credential-shaped token never reaches the -debug start line;
            ' a double-clicked container and the window's switches are kept as they are.
            Dim startLine = Program.StartLogLine(New String() {"--mount-ro", "C:\disks\v.fdd", "p:hunter2", "-debug"})
            Check("startup:debug-line-screens-credential", Not startLine.Contains("hunter2") AndAlso
                  startLine.Contains("C:\disks\v.fdd") AndAlso startLine.Contains("--mount-ro"), startLine)
            Dim vdLine = Program.StartLogLine(New String() {"C:\disks\v.fdd", "mount", "hunter2"})
            Check("startup:debug-line-screens-vd-line", Not vdLine.Contains("hunter2"), vdLine)
        Finally
            Environment.CurrentDirectory = oldCwd
            Try
                Directory.Delete(dir, True)
            Catch
            End Try
        End Try

        ' SP-0027 WIPE-02, mirrored: FileDO's data, LocalAppData above it and the profile are asked
        ' about twice on a console, so the Wipe page refuses them up front.
        Dim localApp = Environment.GetFolderPath(Environment.SpecialFolder.LocalApplicationData)
        For Each pair In New String()() {
            New String() {"wipe-safety:filedo-data", Path.Combine(localApp, "FileDO")},
            New String() {"wipe-safety:localappdata", localApp},
            New String() {"wipe-safety:profile", Environment.GetFolderPath(Environment.SpecialFolder.UserProfile)},
            New String() {"wipe-safety:windows", Environment.GetEnvironmentVariable("SystemRoot")}}
            Dim key = WipeSafety.DangerKey(pair(1))
            Check(pair(0), key = "shell_wipe_danger_system", pair(1) & " -> " & key)
        Next
        Check("wipe-safety:drive-token-is-root", WipeSafety.DangerKey("E:") = "shell_wipe_danger_root", WipeSafety.DangerKey("E:"))
    End Sub

    ' GUI-03: Delete and Move are destructive on the page, and pass -y only behind a typed word.
    Private Sub CheckDuplicatesPage()
        Dim dict = Localization.GetDict(ShellSettings.Language())
        Dim jv As JobView = Nothing
        Dim reason As String = ""
        Try
            jv = New JobView()
            jv.SetJob(JobCatalogue.GetJob("rail_job_duplicates"))
            jv.SetTarget("C:\sample-dups")

            Dim runs = jv.RunStateForTest(reason)
            Dim cmd = jv.CurrentCommand()
            Check("dup:report-is-reversible", runs AndAlso Not jv.DestructiveBadgeShownForTest AndAlso
                  jv.ReversibilityForTest = dict("shell_rev_reversible") AndAlso Not cmd.Contains("-y"), cmd)
            Check("count:dup-report-does-not-count", Not jv.CountRowShownForTest, "")

            jv.SetDupActionForTest("delete")
            runs = jv.RunStateForTest(reason)
            cmd = jv.CurrentCommand()
            Check("dup:delete-is-destructive", Not runs AndAlso jv.DestructiveBadgeShownForTest AndAlso
                  jv.ReversibilityForTest = dict("shell_rev_permanent"), cmd)
            Check("count:dup-delete-counts", jv.CountRowShownForTest, "")
            jv.SetConfirmWordForTest("delete")
            Check("dup:delete-word-is-exact", Not jv.RunStateForTest(reason), "")
            jv.SetConfirmWordForTest("DELETE")
            runs = jv.RunStateForTest(reason)
            Check("dup:delete-typed-runs", runs AndAlso cmd.Contains(" del -y"), cmd)

            jv.SetDupActionForTest("move", "C:\sample-dups-moved")
            runs = jv.RunStateForTest(reason)
            cmd = jv.CurrentCommand()
            Check("dup:move-is-destructive", Not runs AndAlso jv.DestructiveBadgeShownForTest AndAlso
                  jv.ReversibilityForTest = dict("shell_rev_partly_reversible"), cmd)
            jv.SetConfirmWordForTest("MOVE")
            runs = jv.RunStateForTest(reason)
            Check("dup:move-typed-runs", runs AndAlso cmd.Contains("move C:\sample-dups-moved -y"), cmd)

            jv.SetDupActionForTest("report")
            runs = jv.RunStateForTest(reason)
            Check("dup:back-to-report", runs AndAlso Not jv.DestructiveBadgeShownForTest AndAlso Not jv.CurrentCommand().Contains("-y"),
                  jv.CurrentCommand())

            ' GUI-17: pages that delete nothing do not walk the target.
            jv.SetJob(JobCatalogue.GetJob("rail_job_fill"))
            jv.SetTarget("E:")
            Check("count:fill-does-not-count", Not jv.CountRowShownForTest, "")
        Finally
            If jv IsNot Nothing Then jv.Dispose()
        End Try
    End Sub

    ' AUD-16-F1: a Compare delete rule is destructive and permanent on the page, and passes --yes
    ' only behind the typed DELETE; "none" stays reversible and passes nothing.
    Private Sub CheckComparePage()
        Dim dict = Localization.GetDict(ShellSettings.Language())
        Dim jv As JobView = Nothing
        Dim reason As String = ""
        Try
            jv = New JobView()
            jv.SetJob(JobCatalogue.GetJob("rail_job_compare"))
            jv.SetTarget("C:\sample-cmp-a")
            jv.SetCompareForTest("C:\sample-cmp-b", "")

            Dim runs = jv.RunStateForTest(reason)
            Dim cmd = jv.CurrentCommand()
            Check("cmp:none-is-reversible", runs AndAlso Not jv.DestructiveBadgeShownForTest AndAlso
                  jv.ReversibilityForTest = dict("shell_rev_reversible") AndAlso Not cmd.Contains("--yes") AndAlso
                  Not cmd.Contains(" del"), cmd)

            jv.SetCompareForTest("C:\sample-cmp-b", "del source")
            runs = jv.RunStateForTest(reason)
            cmd = jv.CurrentCommand()
            Dim destructive = Not runs AndAlso jv.DestructiveBadgeShownForTest AndAlso
                              jv.ReversibilityForTest = dict("shell_rev_permanent")
            jv.SetConfirmWordForTest("delete")
            Dim exact = Not jv.RunStateForTest(reason)
            jv.SetConfirmWordForTest("DELETE")
            runs = jv.RunStateForTest(reason)
            cmd = jv.CurrentCommand()
            Check("cmp:delete-is-destructive", destructive AndAlso exact AndAlso runs AndAlso
                  cmd.Contains(" del source --yes"), cmd)

            ' Back to "none": the badge goes, the word is cleared, and no --yes is passed.
            jv.SetCompareForTest("C:\sample-cmp-b", "")
            runs = jv.RunStateForTest(reason)
            cmd = jv.CurrentCommand()
            Check("cmp:back-to-none", runs AndAlso Not jv.DestructiveBadgeShownForTest AndAlso
                  jv.ReversibilityForTest = dict("shell_rev_reversible") AndAlso Not cmd.Contains("--yes"), cmd)
            jv.SetCompareForTest("C:\sample-cmp-b", "del old target")
            Check("cmp:word-asked-again", Not jv.RunStateForTest(reason), jv.CurrentCommand())
        Finally
            If jv IsNot Nothing Then jv.Dispose()
        End Try
    End Sub

    ' GUI-02: a line handed over from a Protect page carries its password, and one handed over
    ' without it is not run with an empty one.
    Private Sub CheckCommandCredential()
        Dim cv As CommandView = Nothing
        Try
            cv = New CommandView()
            cv.SelectOperation(0)
            cv.SetCommand("filedo.exe C:\a.txt secure pe:FILEDO_SHELL_CRED")
            Check("command:open-secure-without-cred", Not cv.RunEnabledNowForTest AndAlso cv.CredentialBlockShownForTest,
                  "run " & cv.RunEnabledNowForTest.ToString() & ", block " & cv.CredentialBlockShownForTest.ToString())

            cv.SetCommand("filedo.exe C:\a.txt secure pe:FILEDO_SHELL_CRED", "selftest-handed-over")
            Check("command:open-secure-with-cred", cv.RunEnabledNowForTest AndAlso cv.CredentialBlockShownForTest,
                  "run " & cv.RunEnabledNowForTest.ToString() & ", block " & cv.CredentialBlockShownForTest.ToString())

            ' Handing over a line with no password clears the one an earlier line left.
            cv.SetCommand("filedo.exe C:\b.txt unsecure pe:FILEDO_SHELL_CRED")
            Check("command:stale-cred-not-reused", Not cv.RunEnabledNowForTest, cv.RunEnabledNowForTest.ToString())

            cv.SetCommand("filedo.exe E: info")
            Check("command:no-cred-block-for-info", Not cv.CredentialBlockShownForTest AndAlso cv.RunEnabledNowForTest, "")
        Finally
            If cv IsNot Nothing Then cv.Dispose()
        End Try
    End Sub

    ' AUD-91-F1 (GUI-20 reopened): the Command page's Copy command is written for cmd.exe, where its
    ' own wipe refusal tells the user to run it - as the job page's copy already is. The box keeps the
    ' line as the runner reads it; only the clipboard gets the cmd-safe form.
    Private Sub CheckCommandCopy()
        Dim cv As CommandView = Nothing
        Dim q = """"
        Try
            cv = New CommandView()
            cv.SelectOperation(0)
            Dim cases As New List(Of String()) From {
                New String() {"ampersand", "D:\Work&Play", q & "D:\Work&Play" & q},
                New String() {"caret", "D:\Work^Play", q & "D:\Work^Play" & q},
                New String() {"percent", "D:\100%", q & "D:\100" & q & "^%" & q & q},
                New String() {"pipe", "D:\x|y", q & "D:\x|y" & q},
                New String() {"space", "D:\My Work", q & "D:\My Work" & q},
                New String() {"plain", "D:\Work", "D:\Work"}}
            For Each c In cases
                Dim line = "filedo.exe " & ArgQuoting.JoinArgs(New String() {c(1), "wipe"})
                cv.SetLineForTest(line)
                Check("cmd-copy:command-page:" & c(0), cv.CopyPayload() = "filedo.exe " & c(2) & " wipe", cv.CopyPayload())
                Check("cmd-copy:command-page:box-unchanged:" & c(0), cv.CurrentCommandLine() = line, cv.CurrentCommandLine())
            Next

            ' A line that holds no argument is copied as it stands, never as a bare "filedo.exe ".
            cv.SetLineForTest("")
            Check("cmd-copy:command-page:empty", cv.CopyPayload() = "", cv.CopyPayload())

            ' The program name alone is no argument either: it is copied once, not "filedo.exe filedo.exe".
            cv.SetLineForTest("filedo.exe")
            Check("cmd-copy:command-page:bare-program", cv.CopyPayload() = "filedo.exe", cv.CopyPayload())
        Finally
            If cv IsNot Nothing Then cv.Dispose()
        End Try
    End Sub

    ' GUI-18: the redirect notice is shown for the root of the system volume in its spellings, and
    ' for nothing below it.
    Private Sub CheckRedirectNotice()
        Dim sys = Environment.GetEnvironmentVariable("SystemRoot")
        Dim letter = If(String.IsNullOrEmpty(sys), "C", sys.Substring(0, 1))
        Dim jv As JobView = Nothing
        Try
            jv = New JobView()
            jv.SetJob(JobCatalogue.GetJob("rail_job_fill"))
            For Each shown In New String() {letter & ":", letter & ":\", letter.ToLowerInvariant() & ":/", "\\?\" & letter & ":\"}
                jv.SetTarget(shown)
                Check("redirect:shown:" & shown, jv.RedirectNoticeShownForTest, jv.CurrentCommand())
            Next
            For Each hidden In New String() {letter & ":\x", letter & ":\Users"}
                jv.SetTarget(hidden)
                Check("redirect:hidden:" & hidden, Not jv.RedirectNoticeShownForTest, jv.CurrentCommand())
            Next
            jv.SetJob(JobCatalogue.GetJob("rail_job_clean"))
            jv.SetTarget(letter & ":\")
            Check("redirect:clean-shown", jv.RedirectNoticeShownForTest, jv.CurrentCommand())
            jv.SetJob(JobCatalogue.GetJob("rail_job_info"))
            jv.SetTarget(letter & ":\")
            Check("redirect:info-hidden", Not jv.RedirectNoticeShownForTest, jv.CurrentCommand())
        Finally
            If jv IsNot Nothing Then jv.Dispose()
        End Try
    End Sub

    ' GUI-04 and GUI-06: a child that reads stdin until it ends is started the way a run is. It ends
    ' by itself because its stdin is closed, and it is in the window's job while it runs.
    Private Sub CheckRunnerChild()
        Dim sortExe = Path.Combine(Environment.SystemDirectory, "sort.exe")
        If Not File.Exists(sortExe) Then
            Check("runner:stdin-closed", False, "no " & sortExe)
            Return
        End If
        Dim inJob = False
        Dim exited = False
        Dim sw = Diagnostics.Stopwatch.StartNew()
        Runner.RunChildForTest(sortExe, "", 5000, inJob, exited)
        Check("runner:stdin-closed", exited, "sort.exe ended after " & sw.ElapsedMilliseconds.ToString() & " ms")
        Check("runner:child-in-job", inJob, "")
    End Sub

    ' SHELL-13: a data folder that cannot be written ends the run Not proven, with the page idle.
    Private Sub CheckStartFailure()
        Dim dict = Localization.GetDict(ShellSettings.Language())
        Dim jv As JobView = Nothing
        Runner.RunsDirForTest = Function() As String
                                    Throw New UnauthorizedAccessException("selftest: the runs folder is unavailable")
                                End Function
        Try
            jv = New JobView()
            jv.SetJob(JobCatalogue.GetJob("rail_job_info"))
            jv.SetTarget(Path.GetTempPath())
            jv.PressRunForTest()
            Check("runner:start-failure-not-proven", jv.VerdictTextForTest = dict("shell_verdict_not_proven") AndAlso Not jv.IsRunning,
                  jv.VerdictTextForTest & " / running " & jv.IsRunning.ToString())
        Finally
            Runner.RunsDirForTest = Nothing
            If jv IsNot Nothing Then jv.Dispose()
        End Try
    End Sub

    ' SHELL-01: the report's Command line is redacted like history.json, and a run that can print a
    ' sealed name keeps no output at all.
    Private Sub CheckReportRedaction()
        ' The vectors the CLI's redactCredentialArgs is tested against, from the copy embedded here.
        Dim vectors As String = Nothing
        Using source = GetType(SelfTest).Assembly.GetManifestResourceStream("FileDOGUI.redaction.vectors.tsv")
            If source IsNot Nothing Then
                Using r As New StreamReader(source, Encoding.UTF8)
                    vectors = r.ReadToEnd()
                End Using
            End If
        End Using
        If vectors Is Nothing Then
            Check("report:vectors", False, "missing FileDOGUI.redaction.vectors.tsv")
        Else
            Dim n = 0
            For Each raw In vectors.Replace(vbCr, "").Split(ControlChars.Lf)
                If raw.Trim() = "" OrElse raw.StartsWith("#") Then Continue For
                n += 1
                Dim sep = raw.IndexOf(vbTab & "=>" & vbTab, StringComparison.Ordinal)
                If sep < 0 Then
                    Check("report:vector:" & n.ToString(), False, "no => in " & raw)
                    Continue For
                End If
                Dim input = raw.Substring(0, sep).Split(ControlChars.Tab)
                Dim want = String.Join(vbTab, raw.Substring(sep + 4).Split(ControlChars.Tab))
                Dim got = String.Join(vbTab, Runner.RedactCredentialArgs(input))
                Check("report:vector:" & n.ToString(), got = want, got.Replace(vbTab, " "))
            Next
            Check("report:vectors", n > 0, n.ToString() & " vectors")
        End If

        ' SP-0025 FDSEC-18, which this port follows: a sub-verb is one only right after fdsec. In
        ' the target-first form the parser takes it as the password, so it is redacted like one;
        ' "start" is an option word of unsecure and is kept.
        Dim redacted = Runner.RedactCredentialArgs(New String() {"a.txt", "secure", "verify", "hunter2"})
        Check("report:target-first-subverb-redacted", String.Join(" ", redacted) = "a.txt secure *** ***", String.Join(" ", redacted))
        redacted = Runner.RedactCredentialArgs(New String() {"a.fd-sec", "unsecure", "start", "pe:FILEDO_SHELL_CRED"})
        Check("report:start-kept", String.Join(" ", redacted) = "a.fd-sec unsecure start pe:FILEDO_SHELL_CRED", String.Join(" ", redacted))

        Check("report:sensitive-verbs",
              Runner.IsSensitiveRun(New String() {"C:\x.fd-sec", "rev"}) AndAlso
              Runner.IsSensitiveRun(New String() {"fdsec", "info", "C:\x.fd-sec"}) AndAlso
              Runner.IsSensitiveRun(New String() {"C:\x.fd-sec", "unsecure", "start"}) AndAlso
              Not Runner.IsSensitiveRun(New String() {"C:\x", "secure", "p:secret"}) AndAlso
              Not Runner.IsSensitiveRun(New String() {"E:", "info"}), "")

        Dim dir = Path.Combine(Path.GetTempPath(), "filedo_selftest_report_" & Guid.NewGuid().ToString("N"))
        Try
            Directory.CreateDirectory(dir)
            Dim spool = Path.Combine(dir, "run.out")
            File.WriteAllText(spool, "Revealed C:\box.fd-sec -> C:\Users\u\AppData\Local\FileDO\reveal\rv-1\secret-name.docx (12 B)" & vbLf &
                                     "true name: secret-name.docx" & vbLf)
            Dim report1 = Path.Combine(dir, "report_1.log")
            Runner.WriteReport(report1, "1", New String() {"--events", "e.jsonl", "C:\box.fd-sec", "reveal", "pe:FILEDO_SHELL_CRED"},
                               "Done", 0, TimeSpan.FromSeconds(3), spool, Nothing)
            Dim text1 = File.ReadAllText(report1)
            Check("report:fdsec-redaction:reveal", Not text1.Contains("secret-name") AndAlso text1.Contains("Verdict: Done") AndAlso
                                                   text1.Contains("Exit Code: 0"), text1.Replace(vbCrLf, " | "))

            File.WriteAllText(spool, "Secured C:\x.txt" & vbLf)
            Dim report2 = Path.Combine(dir, "report_2.log")
            Runner.WriteReport(report2, "2", New String() {"C:\x.txt", "secure", "p:secret"}, "Done", 0, TimeSpan.FromSeconds(3), spool, Nothing)
            Dim text2 = File.ReadAllText(report2)
            Check("report:fdsec-redaction:typed-password", Not text2.Contains("secret") AndAlso text2.Contains("p:***") AndAlso
                                                           text2.Contains("Secured C:\x.txt"), text2.Replace(vbCrLf, " | "))

            ' SP-0064 T1-F2: the console quotes the word an error failed on, and the report keeps the
            ' console - a p: password there is p:*** too; a path on drive P: stays a path.
            File.WriteAllText(spool, "Error: Unknown command ""p:hunter2""" & vbLf &
                                     "GetFileAttributesEx p:hunter2: x" & vbLf &
                                     "read p:\dir\file (p:hunter2)" & vbLf)
            Dim report3 = Path.Combine(dir, "report_3.log")
            Runner.WriteReport(report3, "3", New String() {"C:\a.txt", "p:hunter2", "secure"}, "Failed", 2, TimeSpan.FromSeconds(1), spool, Nothing)
            Dim text3 = File.ReadAllText(report3)
            Check("report:error-text-redacted", Not text3.Contains("hunter2") AndAlso text3.Contains("Unknown command ""p:***""") AndAlso
                                                text3.Contains("GetFileAttributesEx p:*** x") AndAlso text3.Contains("read p:\dir\file (p:***"),
                  text3.Replace(vbCrLf, " | "))

            ' SP-0064 R-F1: a password that holds spaces is one argument, and the word rule alone
            ' keeps everything after its first space. The run's own p: values - on its command
            ' line and on each line of a batch list it runs - are removed whole first, also as Go's
            ' %q spells them inside an error's quotes.
            Dim spaced = "Error: Unknown command ""p:horse battery staple"""
            File.WriteAllText(spool, spaced & vbLf & "wrong password: horse battery staple" & vbLf &
                                     "Error: Unknown command ""p:say \""hi\"" now""" & vbLf)
            Dim report4 = Path.Combine(dir, "report_4.log")
            Runner.WriteReport(report4, "4", New String() {"C:\a.txt", "p:horse battery staple", "p:say ""hi"" now", "secure"},
                               "Failed", 2, TimeSpan.FromSeconds(1), spool, Nothing)
            Dim text4 = File.ReadAllText(report4)
            Check("report:credential-with-spaces-removed-whole",
                  Runner.RedactCredentialText(spaced).Contains("battery staple") AndAlso
                  Not text4.Contains("horse") AndAlso Not text4.Contains("battery") AndAlso Not text4.Contains("staple") AndAlso
                  Not text4.Contains("say ") AndAlso text4.Contains("Unknown command ""p:***""") AndAlso
                  text4.Contains("wrong password: ***"), text4.Replace(vbCrLf, " | "))

            Dim list = Path.Combine(dir, "list.lst")
            File.WriteAllText(list, "# a batch" & vbLf & "filedo C:\b.txt secure ""p:correct horse battery""" & vbLf)
            File.WriteAllText(spool, "Error: Unknown command ""p:correct horse battery""" & vbLf)
            Dim report5 = Path.Combine(dir, "report_5.log")
            Runner.WriteReport(report5, "5", New String() {"from", list}, "Failed", 2, TimeSpan.FromSeconds(1), spool, Nothing)
            Dim text5 = File.ReadAllText(report5)
            Check("report:batch-credential-with-spaces-removed-whole",
                  Not text5.Contains("horse") AndAlso Not text5.Contains("battery") AndAlso text5.Contains("Unknown command ""p:***"""),
                  text5.Replace(vbCrLf, " | "))
        Finally
            Try
                Directory.Delete(dir, True)
            Catch
            End Try
        End Try
    End Sub

    ' SHELL-02 and SHELL-03: the sweep keeps what is young and removes what is old, and a report too
    ' big to show whole is shown by its head and its end.
    Private Sub CheckHistory()
        Dim dir = Path.Combine(Path.GetTempPath(), "filedo_selftest_sweep_" & Guid.NewGuid().ToString("N"))
        Try
            Directory.CreateDirectory(dir)
            Dim old = Path.Combine(dir, "report_old.log")
            Dim young = Path.Combine(dir, "report_young.log")
            File.WriteAllText(old, "old")
            File.WriteAllText(young, "young")
            File.SetLastWriteTime(old, DateTime.Now.AddDays(-40))
            File.SetLastWriteTime(young, DateTime.Now.AddDays(-5))
            Dim removed = Runner.SweepDirectory(dir, 30)
            Check("history:sweep", removed = 1 AndAlso Not File.Exists(old) AndAlso File.Exists(young), removed.ToString())

            Dim big = Path.Combine(dir, "report_big.log")
            Using w As New StreamWriter(big, False, New UTF8Encoding(False))
                w.WriteLine("FileDO Run Report")
                w.WriteLine("Verdict: Failed")
                For i = 1 To 40000
                    w.WriteLine("line " & i.ToString() & " of a long run's output - Фото")
                Next
                w.WriteLine("the last line")
            End Using
            Dim view = HistoryView.ReadReportView(big, "TRUNCATED")
            Check("history:report-tail", view.StartsWith("FileDO Run Report") AndAlso view.Contains("Verdict: Failed") AndAlso
                                         view.Contains("TRUNCATED") AndAlso view.TrimEnd().EndsWith("the last line") AndAlso
                                         view.Length < HistoryView.ReportHeadBytes + HistoryView.ReportTailBytes + 200,
                  view.Length.ToString() & " chars")
            Dim smallView = HistoryView.ReadReportView(young, "TRUNCATED")
            Check("history:report-small-whole", smallView = "young", smallView)
        Finally
            Try
                Directory.Delete(dir, True)
            Catch
            End Try
        End Try
    End Sub

    ' GUI-10: a result line past the old 2 MB limit is read, and its verdict received.
    Private Sub CheckEventStreamLongLine()
        Dim eventsPath = Path.Combine(Path.GetTempPath(), "filedo_selftest_long_" & Guid.NewGuid().ToString("N") & ".jsonl")
        Try
            Dim sb As New StringBuilder()
            sb.Append("{""schemaVersion"":1,""kind"":""result"",""data"":{""verdict"":""Failed"",""filesLeft"":[")
            Dim n = 0
            While sb.Length < 3 * 1024 * 1024
                If n > 0 Then sb.Append(","c)
                sb.Append("""E:\\FILL_").Append(n.ToString("D8")).Append(".tmp""")
                n += 1
            End While
            sb.Append("]}}").Append(vbLf)
            File.WriteAllText(eventsPath, sb.ToString())
            Dim verdict = ""
            Dim left = 0
            Using stream As New EventStream(eventsPath)
                AddHandler stream.ResultReceived, Sub(r)
                                                      verdict = r.Verdict
                                                      left = r.FilesLeft.Count
                                                  End Sub
                stream.Poll()
            End Using
            Check("tailer:3mb-result", verdict = "Failed" AndAlso left = n, verdict & " / " & left.ToString() & " of " & n.ToString())
        Finally
            Try
                File.Delete(eventsPath)
            Catch
            End Try
        End Try
    End Sub

    ' GUI-14: the output box and the runner's memory both stay bounded, and both keep the end.
    Private Sub CheckOutputBounds()
        Using box As New TextBox With {.Multiline = True}
            Dim h = box.Handle
            Dim pane As New OutputPane(box)
            For i = 1 To 200000
                pane.Add("output line " & i.ToString("D6") & " - some text to make it longer")
                If i Mod 5000 = 0 Then pane.Flush()
            Next
            pane.Flush()
            Check("output:pane-bounded", box.TextLength <= OutputPane.MaxChars AndAlso box.Text.TrimEnd().EndsWith("line 200000 - some text to make it longer"),
                  box.TextLength.ToString() & " chars")
        End Using

        Dim bounded As New BoundedText(64 * 1024, 1024 * 1024)
        For i = 1 To 500000
            bounded.AppendLine("line " & i.ToString("D6") & " of a long run")
        Next
        Dim kept = bounded.ToString()
        Check("output:runner-bounded", kept.Length < 64 * 1024 + 1024 * 1024 + 200 AndAlso kept.StartsWith("line 000001") AndAlso
                                       kept.TrimEnd().EndsWith("line 500000 of a long run") AndAlso kept.Contains("lines not kept"),
              kept.Length.ToString() & " chars")
    End Sub

    ' GUI-15: the size box takes what Atoi takes.
    Private Sub CheckParameters()
        Dim jv As JobView = Nothing
        Dim reason As String = ""
        Try
            jv = New JobView()
            jv.SetJob(JobCatalogue.GetJob("rail_job_capacity"))
            jv.SetTarget("E:")
            For Each bad In New String() {"1,000", "1e3", "2.5", "-5"}
                jv.SetSizeForTest(bad)
                Check("params:size-refused:" & bad, Not jv.RunStateForTest(reason), jv.CurrentCommand())
            Next
            jv.SetSizeForTest("1000")
            Check("params:size-accepted:1000", jv.RunStateForTest(reason) AndAlso jv.CurrentCommand().Contains(" test 1000"), jv.CurrentCommand())

            ' AUD-26-F1: `test` and `fill` take `del`; `speed` takes only `nodel` and `short`, and
            ' any other word is a usage error (CLI-25), so the Speed page never offers or writes `del`.
            jv.SetDeleteOptionsForTest(True, False)
            Check("params:test-keeps-del", Array.IndexOf(jv.CurrentCommand().Split(" "c), "del") >= 0, jv.CurrentCommand())
            jv.SetDeleteOptionsForTest(False, False)
            jv.SetJob(JobCatalogue.GetJob("rail_job_speed"))
            jv.SetTarget("E:")
            jv.SetDeleteOptionsForTest(True, False)
            Check("params:speed-no-del", Not jv.AutoDelOfferedForTest AndAlso
                  Array.IndexOf(jv.CurrentCommand().Split(" "c), "del") < 0, jv.CurrentCommand())
            jv.SetDeleteOptionsForTest(True, True)
            Check("params:speed-nodel-stays-free", jv.NoDelEnabledForTest AndAlso
                  Array.IndexOf(jv.CurrentCommand().Split(" "c), "nodel") >= 0, jv.CurrentCommand())
        Finally
            If jv IsNot Nothing Then jv.Dispose()
        End Try
        Check("params:decimal-invariant", Ui.IsDecimalNumber("2.5") AndAlso Not Ui.IsDecimalNumber("2,5") AndAlso Not Ui.IsWholeNumber("2.5"), "")
    End Sub

    ' AUD-28-F3: the check verb reads its Int options with Go's flag package, which parses base 0:
    ' "09" is a parse error after the run has started and "010" is octal 8. A value with a leading
    ' zero is therefore not a number to the panel - it is flagged and left off the line - while "0",
    ' "8" and "10" still go through, and a decimal option keeps its base-10 reading.
    Private Sub CheckCheckOptions()
        Using panel As New CheckOptionsPanel()
            Dim workers As TextBox = Nothing
            Dim minMb As TextBox = Nothing
            For Each c In AllControls(panel)
                If TypeOf c Is TextBox AndAlso c.AccessibleName = "--workers" Then workers = CType(c, TextBox)
                If TypeOf c Is TextBox AndAlso c.AccessibleName = "--min-mb" Then minMb = CType(c, TextBox)
            Next
            Check("check-options:fields-found", workers IsNot Nothing AndAlso minMb IsNot Nothing, "")
            If workers Is Nothing OrElse minMb Is Nothing Then Return
            For Each bad In New String() {"09", "010", "00"}
                workers.Text = bad
                Check("check-options:leading-zero:" & bad, panel.HasInvalidNumber() AndAlso
                      panel.ToArgs().IndexOf("--workers") < 0, String.Join(" ", panel.ToArgs()))
            Next
            For Each good In New String() {"0", "8", "10"}
                workers.Text = good
                Dim args = panel.ToArgs()
                Dim i = args.IndexOf("--workers")
                Check("check-options:whole-number:" & good, Not panel.HasInvalidNumber() AndAlso
                      i >= 0 AndAlso i + 1 < args.Count AndAlso args(i + 1) = good, String.Join(" ", args))
            Next
            workers.Text = ""
            minMb.Text = "010.5"
            Check("check-options:decimal-leading-zero-kept", Not panel.HasInvalidNumber() AndAlso
                  panel.ToArgs().IndexOf("--min-mb") >= 0, String.Join(" ", panel.ToArgs()))
        End Using
    End Sub

    ' GUI-12: "Clean files" opens Clean on the target that was tested.
    Private Sub CheckCleanCarriesTarget()
        Dim jv As JobView = Nothing
        Try
            jv = New JobView()
            jv.SetJob(JobCatalogue.GetJob("rail_job_capacity"))
            jv.CleanAfterRunForTest("Q:")
            Check("clean:carries-target", jv.CurrentJobId = "rail_job_clean" AndAlso jv.TargetTextForTest = "Q:" AndAlso
                                          jv.CurrentCommand().Contains("Q: clean"), jv.CurrentJobId & " / " & jv.CurrentCommand())
        Finally
            If jv IsNot Nothing Then jv.Dispose()
        End Try
    End Sub

    ' SHELL-10: only the clipboard's own code is "another program is using it".
    Private Sub CheckCauses()
        Dim gdi As New Runtime.InteropServices.ExternalException("A generic error occurred in GDI+.", &H80004005)
        Check("cause:gdi", Problems.CauseKey(gdi) = "shell_cause_unexpected", Problems.CauseKey(gdi))
        Dim clip As New Runtime.InteropServices.ExternalException("Requested Clipboard operation did not succeed.", CInt(Problems.ClipboardCantOpen - &H100000000L))
        Check("cause:clipboard", Problems.CauseKey(clip) = "shell_cause_busy", Problems.CauseKey(clip))
        Dim seh As New Runtime.InteropServices.SEHException()
        Check("cause:seh", Problems.CauseKey(seh) = "shell_cause_unexpected", Problems.CauseKey(seh))
    End Sub

    ' SHELL-11: one duration format, with hours from one hour up.
    Private Sub CheckDurationFormat()
        Check("format:duration", Ui.FormatDuration(TimeSpan.FromMinutes(203)) = "3:23:00", Ui.FormatDuration(TimeSpan.FromMinutes(203)))
        Check("format:duration-short", Ui.FormatDuration(New TimeSpan(0, 5, 7)) = "05:07", Ui.FormatDuration(New TimeSpan(0, 5, 7)))
        Check("format:duration-days", Ui.FormatDuration(TimeSpan.FromHours(26.5)) = "26:30:00", Ui.FormatDuration(TimeSpan.FromHours(26.5)))
    End Sub

    ' SHELL-14: About and Send logs show the release stamp, or say the build is a local one.
    Private Sub CheckAboutStamp()
        Dim stamp = LogReport.BuildStamp()
        Check("about:stamp", System.Text.RegularExpressions.Regex.IsMatch(stamp, "^\d{10}$") OrElse stamp.StartsWith("dev"), stamp)
    End Sub

    ' SHELL-05: an archive older than a week is gone at the next Send logs; a new one stays.
    Private Sub CheckLogArchives()
        Dim dir = Path.Combine(Path.GetTempPath(), "filedo_selftest_logs_" & Guid.NewGuid().ToString("N"))
        Try
            Directory.CreateDirectory(dir)
            Dim old = Path.Combine(dir, "filedo-logs-20260101-000000.zip")
            Dim young = Path.Combine(dir, "filedo-logs-20260920-000000.zip")
            Dim other = Path.Combine(dir, "someone-else.zip")
            For Each f In New String() {old, young, other}
                File.WriteAllText(f, "x")
                File.SetLastWriteTime(f, DateTime.Now.AddDays(If(f = young, -2, -30)))
            Next
            Dim removed = LogReport.SweepOldArchives(dir)
            Check("logs:old-archives-swept", removed = 1 AndAlso Not File.Exists(old) AndAlso File.Exists(young) AndAlso File.Exists(other),
                  removed.ToString())
        Finally
            Try
                Directory.Delete(dir, True)
            Catch
            End Try
        End Try
    End Sub

    Private Sub CheckDiagnosticExport()
        Dim dir = Path.Combine(Path.GetTempPath(), "filedo_selftest_diagnostic_" & Guid.NewGuid().ToString("N"))
        Try
            Directory.CreateDirectory(dir)
            Dim source = Path.Combine(dir, "report_C-account-canary.log")
            ' Synthetic PEM delimiters, not signing material. Build them for the export fixture.
            Dim pemBorder = New String("-"c, 5)
            Dim lines As New List(Of String) From {"startup-canary", "phase-canary completed", "password=credential-canary", "p:quoted-credential-canary with spaces", "C:\Users\profile-canary\invoice.txt", "C:/Users/Profile Space Canary/notes.txt", "\\host-canary\share\private", "/data/user/0/package-canary/state", pemBorder & "BEGIN PRIVATE KEY" & pemBorder, "key-body-canary", pemBorder & "END PRIVATE KEY" & pemBorder, "tail-canary"}
            For Each key In New String() {"token", "X-Amz-Signature", "X-Amz-Credential", "X-Amz-Security-Token", "wmsAuthSign", "hdnts", "hdnea", "Policy", "Key-Pair-Id", "api_key", "client_secret", "refresh_token"}
                lines.Add("https://host/path?a=1&amp;" & key & "=query-canary")
            Next
            lines.Add("http://userinfo-canary:pass/with?punctuation#@host/live/id")
            lines.Add("https://host/live/account-canary/password-canary/123.ts")
            lines.Add("p:""multiline-start-canary")
            lines.Add("multiline-body-canary")
            lines.Add("multiline-end-canary""")
            lines.Add("access_token=access-canary")
            lines.Add("pass=short-pass-canary")
            File.WriteAllLines(source, lines, Encoding.UTF8)
            Dim archive = LogReport.ArchiveForTest(source, dir)
            Dim packed As New StringBuilder()
            Using fs As New FileStream(archive, FileMode.Open, FileAccess.Read)
                Using zip As New System.IO.Compression.ZipArchive(fs, System.IO.Compression.ZipArchiveMode.Read)
                    Check("diagnostic:environment", zip.GetEntry("environment.txt") IsNot Nothing)
                    For Each entry In zip.Entries
                        packed.AppendLine(entry.FullName)
                        Using reader As New StreamReader(entry.Open(), Encoding.UTF8)
                            packed.AppendLine(reader.ReadToEnd())
                        End Using
                    Next
                End Using
            End Using
            Dim output = packed.ToString()
            For Each value In New String() {"credential-canary", "profile-canary", "Profile Space Canary", "host-canary", "package-canary", "key-body-canary", "query-canary", "userinfo-canary", "account-canary", "password-canary", "multiline-body-canary", "multiline-end-canary", "access-canary", "short-pass-canary", source, "report_C-account"}
                Check("diagnostic:no-leak:" & value.Replace(source, "source-path"), Not output.Contains(value))
            Next
            Check("diagnostic:useful-context", output.Contains("startup-canary") AndAlso output.Contains("tail-canary") AndAlso output.Contains("phase-canary"))
            Check("diagnostic:count-only", output.Contains("volume_metrics_capability=unavailable") AndAlso Not output.Contains("history.json"))
            Check("diagnostic:oversize-line", DiagnosticText.Sanitize(New String("x"c, 65537)).Contains("LINE OMITTED"))
            ' A log held by its writer must still be exportable.
            Using writer As New FileStream(source, FileMode.Open, FileAccess.Write, FileShare.ReadWrite Or FileShare.Delete)
                Check("diagnostic:live-log", File.Exists(LogReport.ArchiveForTest(source, dir)))
            End Using
            Dim compacted = Path.Combine(dir, "compact.log")
            File.WriteAllText(compacted, "head-canary" & New String("x"c, 5000) & "tail-canary", Encoding.ASCII)
            ShellLog.CompactLog(compacted, 4096, 32, 32)
            Dim compactText = File.ReadAllText(compacted)
            Check("diagnostic:head-tail", compactText.StartsWith("head-canary") AndAlso compactText.EndsWith("tail-canary") AndAlso compactText.Contains("LOG COMPACTED") AndAlso compactText.Length < 4096)
            Dim truncated = "[Diag] LOG COMPACTED | dropped_middle_bytes=999" & vbLf & "opaque-key-tail-canary" & vbLf & "2026-10-01 12:00:00.000 INFO continued-canary"
            Dim sanitized = DiagnosticText.Sanitize(truncated)
            Check("diagnostic:compacted-fragment", Not sanitized.Contains("opaque-key-tail-canary") AndAlso sanitized.Contains("continued-canary"))
            For n = 1 To 12
                File.WriteAllText(Path.Combine(dir, "filedo_win.session-" & n.ToString("D2") & ".log"), "session")
            Next
            Dim active = Path.Combine(dir, "filedo_win.session-01.log")
            Using lease As New FileStream(active, FileMode.Open, FileAccess.Read, FileShare.ReadWrite)
                ShellLog.PruneSessions(dir)
                Check("diagnostic:active-session-kept", File.Exists(active))
            End Using
            ShellLog.PruneSessions(dir)
            Check("diagnostic:ten-sessions", Directory.GetFiles(dir, "filedo_win.session-*.log").Length = 10 AndAlso Not File.Exists(active))
        Finally
            If Directory.Exists(dir) Then Directory.Delete(dir, True)
        End Try
    End Sub

    ' AUD-90-F1: Send logs tells the user that operation history and file lists are excluded. The
    ' compare, delete and check reports and damaged_files.log are file lists - lines of relative paths
    ' that the sanitizer cannot tell from prose - so they are neither counted nor packed, wherever they
    ' sit; the logs beside them still are.
    Private Sub CheckNoFileListInArchive()
        Dim root = Path.Combine(Path.GetTempPath(), "filedo_selftest_filelists_" & Guid.NewGuid().ToString("N"))
        Dim appDir = Path.Combine(root, "app")
        Dim dataDir = Path.Combine(root, "data")
        Dim zipDir = Path.Combine(root, "zip")
        Try
            For Each d In New String() {appDir, Path.Combine(dataDir, "state"), zipDir}
                Directory.CreateDirectory(d)
            Next
            LogReport.RootsForTest = New String() {appDir, dataDir}
            Dim listLine = "Tax\2025\return final.pdf | src=3 MB | dst=2 MB"
            For Each folder In New String() {appDir, Path.Combine(dataDir, "state"), dataDir}
                For Each name In New String() {"compare_report_x.log", "delete_report_x.log", "check_report_x.log", "damaged_files.log"}
                    File.WriteAllText(Path.Combine(folder, name), "header" & vbLf & listLine & vbLf)
                Next
            Next
            Dim listed = LogReport.CountAvailable()
            Check("diagnostic:no-file-list:count", listed = 0, listed.ToString() & " collected")

            File.WriteAllText(Path.Combine(appDir, "filedo_win_debug.log"), "debug-canary" & vbLf)
            Dim count = 0
            Dim archive = LogReport.ArchiveCollectedForTest(zipDir, count)
            Dim packed As New StringBuilder()
            Using fs As New FileStream(archive, FileMode.Open, FileAccess.Read)
                Using zip As New System.IO.Compression.ZipArchive(fs, System.IO.Compression.ZipArchiveMode.Read)
                    For Each entry In zip.Entries
                        Using reader As New StreamReader(entry.Open(), Encoding.UTF8)
                            packed.AppendLine(reader.ReadToEnd())
                        End Using
                    Next
                End Using
            End Using
            Dim output = packed.ToString()
            Check("diagnostic:no-file-list", Not output.Contains("return final"), "a file list reached the archive")
            Check("diagnostic:no-file-list:logs-kept", count = 1 AndAlso output.Contains("debug-canary") AndAlso LogReport.CountAvailable() = 1, count.ToString() & " files")
        Catch ex As Exception
            Check("diagnostic:no-file-list", False, ex.GetType().Name & ": " & ex.Message)
        Finally
            LogReport.RootsForTest = Nothing
            Try
                Directory.Delete(root, True)
            Catch
            End Try
        End Try
    End Sub

    Private Sub AppendText(path As String, text As String)
        Using fs As New FileStream(path, FileMode.Append, FileAccess.Write, FileShare.ReadWrite)
            Dim bytes = Encoding.UTF8.GetBytes(text)
            fs.Write(bytes, 0, bytes.Length)
        End Using
    End Sub

    Private Function IsCredentialOp(op As String) As Boolean
        Return Array.IndexOf(New String() {"secure", "unsecure", "reveal",
                                           "fdsec info", "fdsec verify"}, op) >= 0
    End Function

    ' `info` is written as `short` when the brief report is asked for, and copy is written as
    ' whichever strategy is chosen - so the check is "names the verb or one of its faces".
    Private Function CommandNames(cmd As String, verb As String) As Boolean
        If cmd.Contains(" " & verb) Then Return True
        ' The Disks list reads the machine-readable snapshot since SP-0063 (M1).
        If verb = "list" AndAlso cmd.EndsWith(" vd status json") Then Return True
        If verb = "info" AndAlso cmd.Contains(" short") Then Return True
        If verb = "copy" Then
            For Each v In CliRules.CopyVerbs
                If cmd.Contains(" " & v & " ") Then Return True
            Next
        End If
        Return False
    End Function

    ' The paths hold a "(" on purpose (GUI-01): a page that read any text with a colon and a "(" as
    ' a drive row cut them to "C:", and the target row fails the moment that comes back.
    Private Function SampleTargetFor(job As JobDefinition) As String
        If job.GroupKey = DiskCommands.GroupKey Then Return DiskSample
        Select Case job.TargetKind
            Case JobDefinition.TargetType.File : Return "C:\sample (1)\one.fd-sec"
            Case JobDefinition.TargetType.Drive : Return "E:"
            Case Else : Return "C:\sample (1)\x"
        End Select
    End Function

    ' ---- SP-0004 P6: the Disks jobs -------------------------------------------

    ' A container path with a space and a "(" in it, so a page that loses the quoting or cuts the
    ' path at the parenthesis fails here.
    Private Const DiskSample As String = "C:\sample (1)\one.fdd"
    Private Const DiskSampleQuoted As String = """C:\sample (1)\one.fdd"""

    ' Passwords no user would type, so finding one in a line is unambiguous.
    Private Const DiskSecret As String = "selftest-vd-secret-9f3"
    Private Const DiskNewSecret As String = "selftest-vd-new-secret-7c1"

    Private Function DiskFacts(protection As DiskProtection, clean As Boolean, letter As String) As ContainerFacts
        Return New ContainerFacts With {.Path = DiskSample, .Read = True, .Protection = protection, .Clean = clean,
                                        .MountedLetter = letter, .Profile = "plain"}
    End Function

    ' T6.20/T6.21: every Disks page writes the console grammar of the P6 brief, word for word, and
    ' no password it was given appears in the line, in the copy for cmd.exe or in the redacted
    ' report line - it is in the child's environment under FILEDO_SHELL_CRED (and, for `pass`, the
    ' new one under FILEDO_SHELL_CRED_NEW), and nowhere else. No Disks run is started elevated
    ' by the window (T6.22).
    Private Sub CheckDiskCommands()
        Dim Q = DiskSampleQuoted
        Dim cases As New List(Of Object())()
        ' name, job, options, password, new password, typed word, facts (Nothing = not read), expected line
        cases.Add(New Object() {"list", "list", New DiskOptions(), "", "", "", Nothing, "filedo.exe vd status json"})
        cases.Add(New Object() {"status", "list", New DiskOptions With {.ShowMounted = True}, "", "", "", Nothing, "filedo.exe vd status json"})
        cases.Add(New Object() {"new-vault", "new", New DiskOptions With {.Size = "20G", .Profile = "vault", .Label = "Work"}, DiskSecret, "", "", Nothing,
                                "filedo.exe vd new " & Q & " 20G vault label Work pe:FILEDO_SHELL_CRED"})
        cases.Add(New Object() {"new-obfuscated", "new", New DiskOptions With {.Size = "512M", .Profile = "plain"}, "", "", "", Nothing,
                                "filedo.exe vd new " & Q & " 512M plain p:"})
        cases.Add(New Object() {"new-ram", "new", New DiskOptions With {.Size = "1.5T", .Profile = "ram"}, DiskSecret, "", "", Nothing,
                                "filedo.exe vd new " & Q & " 1.5T ram pe:FILEDO_SHELL_CRED"})
        cases.Add(New Object() {"mount", "mount", New DiskOptions With {.ReadOnly = True, .Letter = "X:"}, DiskSecret, "", "", Nothing,
                                "filedo.exe " & Q & " mount ro as X: pe:FILEDO_SHELL_CRED"})
        cases.Add(New Object() {"mount-obfuscated", "mount", New DiskOptions With {.NoScan = True}, DiskSecret, "", "", DiskFacts(DiskProtection.Obfuscated, True, ""),
                                "filedo.exe " & Q & " mount noscan"})
        cases.Add(New Object() {"mount-encrypted", "mount", New DiskOptions(), DiskSecret, "", "", DiskFacts(DiskProtection.Encrypted, True, ""),
                                "filedo.exe " & Q & " mount pe:FILEDO_SHELL_CRED"})
        cases.Add(New Object() {"unmount", "unmount", New DiskOptions(), "", "", "", Nothing, "filedo.exe " & Q & " unmount"})
        cases.Add(New Object() {"unmount-force", "unmount", New DiskOptions With {.Force = True}, "", "", "", Nothing, "filedo.exe " & Q & " unmount force"})
        cases.Add(New Object() {"unmount-nosave", "unmount", New DiskOptions With {.NoSave = True}, "", "", DiskOptionsPanel.DiscardWord, Nothing,
                                "filedo.exe " & Q & " unmount force nosave"})
        cases.Add(New Object() {"info", "info", New DiskOptions(), "", "", "", Nothing, "filedo.exe " & Q & " info"})
        cases.Add(New Object() {"verify", "verify", New DiskOptions(), DiskSecret, "", "", Nothing, "filedo.exe " & Q & " verify pe:FILEDO_SHELL_CRED"})
        cases.Add(New Object() {"export-vhd", "export", New DiskOptions With {.ExportForm = "vhd", .Dest = "C:\out\disk.vhd"}, DiskSecret, "", "", Nothing,
                                "filedo.exe " & Q & " export C:\out\disk.vhd vhd pe:FILEDO_SHELL_CRED"})
        cases.Add(New Object() {"export-raw", "export", New DiskOptions With {.Dest = "C:\out\disk.img"}, "", "", "", DiskFacts(DiskProtection.Obfuscated, True, ""),
                                "filedo.exe " & Q & " export C:\out\disk.img raw"})
        cases.Add(New Object() {"save", "save", New DiskOptions(), "", "", "", Nothing, "filedo.exe " & Q & " save"})
        cases.Add(New Object() {"compact", "compact", New DiskOptions(), DiskSecret, "", "", Nothing, "filedo.exe " & Q & " compact pe:FILEDO_SHELL_CRED"})
        cases.Add(New Object() {"grow", "grow", New DiskOptions With {.Size = "40G"}, DiskSecret, "", "", Nothing, "filedo.exe " & Q & " grow 40G pe:FILEDO_SHELL_CRED"})
        cases.Add(New Object() {"format", "format", New DiskOptions With {.FileSystem = "exfat", .Label = "Data"}, DiskSecret, "", DiskOptionsPanel.FormatWord, Nothing,
                                "filedo.exe " & Q & " format fs exfat label Data force pe:FILEDO_SHELL_CRED"})
        cases.Add(New Object() {"seal-nopass", "seal", New DiskOptions With {.Dest = "C:\out\sealed.fdd", .NoPass = True}, DiskSecret, "", "", Nothing,
                                "filedo.exe " & Q & " seal C:\out\sealed.fdd nopass pe:FILEDO_SHELL_CRED"})
        cases.Add(New Object() {"clone", "clone", New DiskOptions With {.Dest = "C:\out\copy.fdd"}, DiskSecret, "", "", Nothing,
                                "filedo.exe " & Q & " clone C:\out\copy.fdd pe:FILEDO_SHELL_CRED"})
        cases.Add(New Object() {"pass", "pass", New DiskOptions(), DiskSecret, DiskNewSecret, "", Nothing,
                                "filedo.exe " & Q & " pass pe:FILEDO_SHELL_CRED new pe:FILEDO_SHELL_CRED_NEW"})
        cases.Add(New Object() {"destroy-wipe", "destroy", New DiskOptions With {.Wipe = True}, "", "", DiskOptionsPanel.DestroyWord, Nothing,
                                "filedo.exe " & Q & " destroy wipe force"})
        cases.Add(New Object() {"auto-on", "auto", New DiskOptions With {.AutoOn = True}, "", "", "", Nothing, "filedo.exe vd auto " & Q & " logon"})
        cases.Add(New Object() {"auto-off", "auto", New DiskOptions With {.AutoOn = False}, "", "", "", Nothing, "filedo.exe vd auto off " & Q})
        cases.Add(New Object() {"add", "add", New DiskOptions With {.Name = "work"}, "", "", "", Nothing, "filedo.exe vd add " & Q & " as work"})
        cases.Add(New Object() {"forget", "add", New DiskOptions With {.Remember = False}, "", "", "", Nothing, "filedo.exe vd forget " & Q})

        Dim jv As JobView = Nothing
        Try
            jv = New JobView()
            For Each c In cases
                Dim name = "disk-cmd:" & DirectCast(c(0), String)
                Dim job = JobCatalogue.GetJob("rail_job_vd_" & DirectCast(c(1), String))
                If job Is Nothing Then
                    Check(name, False, "no job rail_job_vd_" & DirectCast(c(1), String))
                    Continue For
                End If
                jv.SetJob(job)
                If job.TargetKind <> JobDefinition.TargetType.None Then jv.SetTarget(DiskSample)
                Dim facts = TryCast(c(6), ContainerFacts)
                If facts IsNot Nothing Then jv.SetDiskFactsForTest(facts)
                jv.SetDiskForTest(DirectCast(c(2), DiskOptions), DirectCast(c(3), String), DirectCast(c(4), String), DirectCast(c(5), String))

                Dim cmd = jv.CurrentCommand()
                Dim expected = DirectCast(c(7), String)
                Check(name, cmd = expected, cmd & " (want " & expected & ")")

                ' Nothing a password was typed as reaches a line of any kind.
                Dim forCmd = jv.CommandForCmd()
                Dim redacted = ArgQuoting.JoinArgs(Runner.RedactCredentialArgs(ArgQuoting.SplitArgs(cmd.Substring("filedo.exe ".Length))))
                Dim leaks = cmd.Contains(DiskSecret) OrElse cmd.Contains(DiskNewSecret) OrElse
                            forCmd.Contains(DiskSecret) OrElse forCmd.Contains(DiskNewSecret) OrElse
                            redacted.Contains(DiskSecret) OrElse redacted.Contains(DiskNewSecret)
                Check(name & ":no-secret", Not leaks, cmd)

                ' The environment carries exactly the passwords the line names.
                Dim env = jv.RunEnvironmentForTest()
                Dim credOk As Boolean
                If cmd.Replace("pe:FILEDO_SHELL_CRED_NEW", "").Contains("pe:FILEDO_SHELL_CRED") Then
                    credOk = env IsNot Nothing AndAlso env.ContainsKey(DiskCommands.CredentialEnvName) AndAlso env(DiskCommands.CredentialEnvName) = DiskSecret
                Else
                    credOk = env Is Nothing OrElse Not env.ContainsKey(DiskCommands.CredentialEnvName)
                End If
                If cmd.Contains("pe:FILEDO_SHELL_CRED_NEW") Then
                    credOk = credOk AndAlso env.ContainsKey(DiskCommands.NewCredentialEnvName) AndAlso env(DiskCommands.NewCredentialEnvName) = DiskNewSecret
                End If
                Check(name & ":env", credOk, If(env Is Nothing, "no environment", String.Join(",", env.Keys.ToArray())))

                ' T6.22: the window never elevates a Disks run itself.
                Check(name & ":not-elevated", Not jv.ElevatesRunForTest, job.NeedsElevation.ToString())
            Next

            ' `pass` needs two passwords, and "Open in Command" can hand over one: it is not offered.
            jv.SetJob(JobCatalogue.GetJob("rail_job_vd_pass"))
            Check("disk-cmd:pass-no-open-in-command", Not jv.OpenInCommandOfferedForTest, "")
            jv.SetJob(JobCatalogue.GetJob("rail_job_vd_mount"))
            Check("disk-cmd:mount-open-in-command", jv.OpenInCommandOfferedForTest, "")
        Finally
            If jv IsNot Nothing Then jv.Dispose()
        End Try
    End Sub

    ' Opens a Disks page on the sample, with facts, answers and passwords, and returns whether Run
    ' is offered and the reason line.
    Private Function DiskRunState(jv As JobView, verb As String, o As DiskOptions, password As String, newPassword As String,
                                  typed As String, facts As ContainerFacts, ByRef reason As String) As Boolean
        jv.SetJob(JobCatalogue.GetJob("rail_job_vd_" & verb))
        jv.SetTarget(DiskSample)
        If facts IsNot Nothing Then jv.SetDiskFactsForTest(facts)
        jv.SetDiskForTest(o, password, newPassword, typed)
        Return jv.RunStateForTest(reason)
    End Function

    ' T6.24, T6.25 and spec 7.4: what the page refuses before a run, each with its own sentence -
    ' and the profile consequences printed beside the choice.
    Private Sub CheckDiskRunRules()
        Dim en = Localization.GetDict(ShellSettings.Language())
        Dim jv As JobView = Nothing
        Dim reason As String = ""
        Try
            jv = New JobView()

            ' `pass`: an empty new password is refused with the way to an obfuscated copy.
            Dim ok = DiskRunState(jv, "pass", New DiskOptions(), DiskSecret, "", "", Nothing, reason)
            Check("disk-run:pass-empty-new-refused", Not ok AndAlso reason = en("vd_pass_empty_new") AndAlso reason.Contains("clone"), reason)
            ok = DiskRunState(jv, "pass", New DiskOptions(), DiskSecret, DiskNewSecret, "", Nothing, reason)
            Check("disk-run:pass-offered", ok, reason)
            jv.DiskPanelForTest.SetNewConfirmForTest("different")
            ok = jv.RunStateForTest(reason)
            Check("disk-run:pass-new-asked-twice", Not ok AndAlso reason = en("shell_cred_mismatch"), reason)
            ok = DiskRunState(jv, "pass", New DiskOptions(), DiskSecret, DiskNewSecret, "", DiskFacts(DiskProtection.Obfuscated, True, ""), reason)
            Check("disk-run:pass-obfuscated-refused", Not ok AndAlso reason = en("vd_block_pass_obfuscated"), reason)

            ' Create: a vault needs a password; an empty one elsewhere is called obfuscation.
            ok = DiskRunState(jv, "new", New DiskOptions With {.Size = "20G", .Profile = "vault"}, "", "", "", Nothing, reason)
            Check("disk-run:vault-needs-password", Not ok AndAlso reason = en("vd_cred_vault_needs"), reason)
            ok = DiskRunState(jv, "new", New DiskOptions With {.Size = "", .Profile = "plain"}, "", "", "", Nothing, reason)
            Check("disk-run:new-needs-size", Not ok AndAlso reason = en("vd_need_size"), reason)
            ok = DiskRunState(jv, "new", New DiskOptions With {.Size = "20G", .Profile = "plain"}, "", "", "", Nothing, reason)
            Check("disk-run:new-obfuscated-offered", ok, reason)

            ' T6.24: each profile's consequence, beside the choice.
            For Each pair In New String()() {
                New String() {"plain", "vd_profile_plain_note"}, New String() {"fast", "vd_profile_fast_note"},
                New String() {"ram", "vd_profile_ram_note"}, New String() {"vault", "vd_profile_vault_note"}}
                DiskRunState(jv, "new", New DiskOptions With {.Size = "20G", .Profile = pair(0)}, DiskSecret, "", "", Nothing, reason)
                Check("disk-run:profile-note:" & pair(0), jv.DiskPanelForTest.ProfileNoticeForTest = en(pair(1)), jv.DiskPanelForTest.ProfileNoticeForTest)
            Next

            ' Format and Destroy: the typed word, and never while mounted.
            ok = DiskRunState(jv, "format", New DiskOptions(), "", "", "", Nothing, reason)
            Check("disk-run:format-needs-word", Not ok AndAlso reason = en("vd_confirm_format"), reason)
            ok = DiskRunState(jv, "format", New DiskOptions(), "", "", DiskOptionsPanel.FormatWord, Nothing, reason)
            Check("disk-run:format-offered", ok, reason)
            ' T2-F3: the typed word approved one container; naming another one (even on the way
            ' back to the first) asks for it again.
            jv.SetTarget(Path.Combine(Path.GetDirectoryName(DiskSample), "selftest-other.fdd"))
            jv.SetTarget(DiskSample)
            ok = jv.RunStateForTest(reason)
            Check("disk-run:word-cleared-on-target-change", Not ok AndAlso reason = en("vd_confirm_format"), reason)
            ok = DiskRunState(jv, "format", New DiskOptions(), "", "", DiskOptionsPanel.FormatWord, DiskFacts(DiskProtection.Obfuscated, False, "X:"), reason)
            Check("disk-run:format-refused-while-mounted", Not ok AndAlso reason = Localization.Format(en("vd_block_mounted_fmt"), "X:"), reason)
            ok = DiskRunState(jv, "destroy", New DiskOptions(), "", "", DiskOptionsPanel.DestroyWord, DiskFacts(DiskProtection.Obfuscated, False, "X:"), reason)
            Check("disk-run:destroy-refused-while-mounted", Not ok AndAlso reason = Localization.Format(en("vd_block_mounted_fmt"), "X:"), reason)
            Check("disk-run:destroy-destructive", jv.DestructiveBadgeShownForTest, "")

            ' Unmount without the final save: destructive and permanent while it is chosen.
            ok = DiskRunState(jv, "unmount", New DiskOptions With {.NoSave = True}, "", "", "", Nothing, reason)
            Check("disk-run:nosave-needs-word", Not ok AndAlso reason = en("vd_confirm_nosave"), reason)
            Check("disk-run:nosave-destructive", jv.DestructiveBadgeShownForTest AndAlso jv.ReversibilityForTest = en("shell_rev_permanent"),
                  jv.ReversibilityForTest)
            DiskRunState(jv, "unmount", New DiskOptions(), "", "", "", Nothing, reason)
            Check("disk-run:unmount-not-destructive", Not jv.DestructiveBadgeShownForTest, "")

            ' Mount: an encrypted container waits for its password; an obfuscated one asks none; a
            ' mounted one is not mounted twice.
            ok = DiskRunState(jv, "mount", New DiskOptions(), "", "", "", DiskFacts(DiskProtection.Encrypted, True, ""), reason)
            Check("disk-run:mount-encrypted-needs-password", Not ok AndAlso reason = en("vd_cred_needed"), reason)
            ok = DiskRunState(jv, "mount", New DiskOptions(), "", "", "", DiskFacts(DiskProtection.Obfuscated, True, ""), reason)
            Check("disk-run:mount-obfuscated-no-password", ok AndAlso Not jv.DiskPanelForTest.CredentialShownForTest, reason)
            ok = DiskRunState(jv, "mount", New DiskOptions(), "", "", "", DiskFacts(DiskProtection.Obfuscated, False, "E:"), reason)
            Check("disk-run:mount-already-mounted", Not ok AndAlso reason = Localization.Format(en("vd_block_already_mounted_fmt"), "E:"), reason)

            ' T6.25 before the run: a packaged build is told what is missing instead of offered a
            ' mount; the read path (Export) stays offered.
            Packaging.OverrideForTest = True
            ok = DiskRunState(jv, "mount", New DiskOptions(), "", "", "", DiskFacts(DiskProtection.Obfuscated, True, ""), reason)
            Check("disk-run:packaged-no-mount", Not ok AndAlso reason = en("vd_packaged"), reason)
            ok = DiskRunState(jv, "export", New DiskOptions With {.Dest = "C:\selftest-no-such-dir\out.img"}, "", "", "", DiskFacts(DiskProtection.Obfuscated, True, ""), reason)
            Check("disk-run:packaged-export-offered", ok, reason)
            Packaging.OverrideForTest = False

            ' A path that is not a container is not offered to a container verb.
            jv.SetJob(JobCatalogue.GetJob("rail_job_vd_info"))
            jv.SetTarget("C:\sample (1)\one.fd-sec")
            ok = jv.RunStateForTest(reason)
            Check("disk-run:needs-fdd", Not ok AndAlso reason = en("vd_need_fdd"), reason)
            ' Unmount takes a drive letter as well.
            jv.SetJob(JobCatalogue.GetJob("rail_job_vd_unmount"))
            jv.SetTarget("X:")
            ok = jv.RunStateForTest(reason)
            Check("disk-run:unmount-by-letter", ok AndAlso jv.CurrentCommand() = "filedo.exe X: unmount", jv.CurrentCommand() & " / " & reason)
        Finally
            Packaging.OverrideForTest = Nothing
            If jv IsNot Nothing Then jv.Dispose()
        End Try
    End Sub

    ' What `info` prints, read by the lines the console writes - and "not encrypted" in the
    ' obfuscated note is never read as encrypted.
    Private Sub CheckDiskFacts()
        Dim obf = String.Join(vbLf, New String() {
            "Container:     C:\d\work.fdd", "Format:        FDD 1.0", "Profile:       ram",
            "Protection:    obfuscated (obfuscated, not encrypted: anyone holding the file and the published format can read it)",
            "Volume size:   20 GiB", "Closed clean:  NO - it was not closed cleanly, or it is mounted now",
            "Last good save: 2026-09-27 10:00:00", "Container id:  abc", "Mounted now:   x:"})
        Dim f = ContainerFacts.Parse("C:\d\work.fdd", obf, 0)
        Check("disk-facts:obfuscated", f.Read AndAlso f.Protection = DiskProtection.Obfuscated AndAlso f.Profile = "ram" AndAlso
                                       f.Clean.HasValue AndAlso Not f.Clean.Value AndAlso f.MountedLetter = "X:" AndAlso
                                       f.LastGoodSave = "2026-09-27 10:00:00", f.Protection.ToString() & " " & f.MountedLetter)
        Dim enc = "Protection:    encrypted (encrypted)" & vbCrLf & "Closed clean:  yes" & vbCrLf
        f = ContainerFacts.Parse("C:\d\s.fdd", enc, 0)
        Check("disk-facts:encrypted", f.Read AndAlso f.Protection = DiskProtection.Encrypted AndAlso f.Clean.Value AndAlso Not f.IsMounted,
              f.Protection.ToString())
        f = ContainerFacts.Parse("C:\d\s.fdd", "vd: the file is damaged", 4)
        Check("disk-facts:unread", Not f.Read AndAlso f.Protection = DiskProtection.Unknown, f.Protection.ToString())
    End Sub

    ' T6.25 after the run, and T6.20's table: each exit class says what happened in a sentence, the
    ' list comes back as rows, and a mount offers its drive.
    Private Sub CheckDiskResults()
        Dim en = Localization.GetDict(ShellSettings.Language())
        Dim jv As JobView = Nothing
        Try
            jv = New JobView()
            jv.SetJob(JobCatalogue.GetJob("rail_job_vd_mount"))
            jv.SetTarget(DiskSample)
            For Each code In New Integer() {2, 3, 4, 5, 6, 7, 8}
                jv.ShowRunResultForTest(New Runner.RunResult With {.ExitCode = code, .Verdict = "Failed", .Output = "", .Duration = TimeSpan.Zero}, False)
                Check("disk-result:exit-" & code.ToString(), jv.VerdictReasonForTest.Contains(en("vd_exit_" & code.ToString())), jv.VerdictReasonForTest)
            Next
            ' AUD-36-F1: grow and compact refused as "not closed cleanly" (class 2) say what to do; any
            ' other class 2, and the same words from another verb, keep the generic sentence.
            Dim refused = "Error: vd: not changed: C:\d\work.fdd " & DiskCommands.OfflineWriterRefusal & " (run grow from a console)" & vbLf
            For Each verbKey In New String() {"rail_job_vd_grow", "rail_job_vd_compact"}
                jv.SetJob(JobCatalogue.GetJob(verbKey))
                jv.SetTarget(DiskSample)
                jv.ShowRunResultForTest(New Runner.RunResult With {.ExitCode = 2, .Verdict = "Failed", .Output = refused, .Duration = TimeSpan.Zero}, False)
                Check("disk-result:exit-2-writer:" & verbKey, jv.VerdictReasonForTest.Contains(en("vd_exit_2_writer")), jv.VerdictReasonForTest)
                jv.ShowRunResultForTest(New Runner.RunResult With {.ExitCode = 2, .Verdict = "Failed", .Output = "Error: vd: unknown word 3 for grow", .Duration = TimeSpan.Zero}, False)
                Check("disk-result:exit-2-generic:" & verbKey, jv.VerdictReasonForTest.Contains(en("vd_exit_2")) AndAlso Not jv.VerdictReasonForTest.Contains(en("vd_exit_2_writer")), jv.VerdictReasonForTest)
            Next
            jv.SetJob(JobCatalogue.GetJob("rail_job_vd_mount"))
            jv.SetTarget(DiskSample)
            jv.ShowRunResultForTest(New Runner.RunResult With {.ExitCode = 2, .Verdict = "Failed", .Output = refused, .Duration = TimeSpan.Zero}, False)
            Check("disk-result:exit-2-writer-only-grow-compact", Not jv.VerdictReasonForTest.Contains(en("vd_exit_2_writer")), jv.VerdictReasonForTest)
            jv.ShowRunResultForTest(New Runner.RunResult With {.ExitCode = 0, .Verdict = "Done", .Duration = TimeSpan.Zero,
                                    .Output = "Container C:\d\work.fdd: plain, 20 GiB, encrypted." & vbLf & "Mounted read-only at X:. Unmount with: filedo X: unmount" & vbLf}, False)
            Check("disk-result:mount-opens-drive", jv.DiskResultForTest.OpenDriveTextForTest = Localization.Format(en("vd_btn_open_drive_fmt"), "X:"),
                  jv.DiskResultForTest.OpenDriveTextForTest)

            ' SP-0063 M1: the list is the snapshot of `vd status json`, two views of one document.
            jv.SetJob(JobCatalogue.GetJob("rail_job_vd_list"))
            Dim snapOut = GoldenSnapshotLine()
            jv.ShowRunResultForTest(New Runner.RunResult With {.ExitCode = 0, .Verdict = "Passed", .Output = snapOut, .Duration = TimeSpan.Zero}, False)
            Check("disk-result:list-registered", jv.DiskResultForTest.RowCountForTest = 8, jv.DiskResultForTest.RowCountForTest.ToString())
            jv.ShowRunResultForTest(New Runner.RunResult With {.ExitCode = 0, .Verdict = "Passed", .Output = snapOut, .Duration = TimeSpan.Zero}, True)
            Check("disk-result:list-mounted", jv.DiskResultForTest.RowCountForTest = 6, jv.DiskResultForTest.RowCountForTest.ToString())
            jv.ShowRunResultForTest(New Runner.RunResult With {.ExitCode = 0, .Verdict = "Passed", .Duration = TimeSpan.Zero,
                                    .Output = "No containers are registered. Register one with: filedo vd add <file.fdd> [as <name>]" & vbLf}, False)
            Check("disk-result:list-not-a-snapshot", jv.DiskResultForTest.RowCountForTest = 0 AndAlso
                                                      jv.DiskResultForTest.MessageForTest = en("vd_mgr_stale_failed"), jv.DiskResultForTest.MessageForTest)
            Dim empty = "{""schema"":""filedo.vd-status"",""version"":1,""at"":null,""packaged"":false,""transport"":{""ready"":true},""disks"":[]}"
            jv.ShowRunResultForTest(New Runner.RunResult With {.ExitCode = 0, .Verdict = "Passed", .Output = empty, .Duration = TimeSpan.Zero}, True)
            Check("disk-result:list-none-mounted", jv.DiskResultForTest.MessageForTest = en("vd_list_none_mounted"), jv.DiskResultForTest.MessageForTest)
        Finally
            If jv IsNot Nothing Then jv.Dispose()
        End Try
    End Sub

    ' T6.18: the three double-click routes, decided from what `info` said - and a `.fdd` opened by
    ' the window never lands on the secure page.
    Private Sub CheckDiskRoutes()
        Dim d = DiskRoute.Decide(DiskSample, DiskRoute.StartMode.Mount, DiskFacts(DiskProtection.Obfuscated, False, "E:"), False)
        Check("disk-route:mounted-opens-drive", d.Action = DiskRoute.RouteAction.OpenDrive AndAlso d.Letter = "E:", d.Action.ToString())
        d = DiskRoute.Decide(DiskSample, DiskRoute.StartMode.Mount, DiskFacts(DiskProtection.Obfuscated, True, ""), False)
        Check("disk-route:clean-obfuscated-mounts", d.Action = DiskRoute.RouteAction.OpenPage AndAlso d.JobKey = "rail_job_vd_mount" AndAlso d.AutoRun,
              d.JobKey & " " & d.AutoRun.ToString())
        d = DiskRoute.Decide(DiskSample, DiskRoute.StartMode.Mount, DiskFacts(DiskProtection.Obfuscated, False, ""), False)
        Check("disk-route:unclean-waits", d.JobKey = "rail_job_vd_mount" AndAlso Not d.AutoRun, d.AutoRun.ToString())
        d = DiskRoute.Decide(DiskSample, DiskRoute.StartMode.Mount, DiskFacts(DiskProtection.Encrypted, True, ""), False)
        Check("disk-route:encrypted-waits-for-password", d.JobKey = "rail_job_vd_mount" AndAlso Not d.AutoRun, d.AutoRun.ToString())
        d = DiskRoute.Decide(DiskSample, DiskRoute.StartMode.Mount, DiskFacts(DiskProtection.Obfuscated, True, ""), True)
        Check("disk-route:packaged-waits", Not d.AutoRun, d.AutoRun.ToString())
        d = DiskRoute.Decide(DiskSample, DiskRoute.StartMode.Mount, ContainerFacts.Unknown(DiskSample), False)
        Check("disk-route:unread-waits", d.JobKey = "rail_job_vd_mount" AndAlso Not d.AutoRun, d.AutoRun.ToString())
        d = DiskRoute.Decide(DiskSample, DiskRoute.StartMode.MountReadOnly, DiskFacts(DiskProtection.Obfuscated, True, ""), False)
        Check("disk-route:mount-ro", d.JobKey = "rail_job_vd_mount" AndAlso d.ReadOnly, d.ReadOnly.ToString())
        d = DiskRoute.Decide(DiskSample, DiskRoute.StartMode.Unmount, DiskFacts(DiskProtection.Obfuscated, False, "E:"), False)
        Check("disk-route:unmount-mounted", d.Action = DiskRoute.RouteAction.OpenPage AndAlso d.JobKey = "rail_job_vd_unmount" AndAlso d.AutoRun,
              d.JobKey & " " & d.AutoRun.ToString())
        d = DiskRoute.Decide(DiskSample, DiskRoute.StartMode.Unmount, DiskFacts(DiskProtection.Obfuscated, True, ""), False)
        Check("disk-route:unmount-not-mounted-waits", d.JobKey = "rail_job_vd_unmount" AndAlso Not d.AutoRun, d.AutoRun.ToString())

        Check("disk-route:switch-mount-ro", DiskRoute.ModeFrom(New String() {"filedo_win.exe", "--mount-ro", "C:\a.fdd"}) = DiskRoute.StartMode.MountReadOnly, "")
        Check("disk-route:switch-unmount", DiskRoute.ModeFrom(New String() {"filedo_win.exe", "--unmount", "C:\a.fdd"}) = DiskRoute.StartMode.Unmount, "")
        Check("disk-route:switch-none", DiskRoute.ModeFrom(New String() {"filedo_win.exe", "C:\a.fdd"}) = DiskRoute.StartMode.None, "")

        ' The window itself, started on a real (empty) .fdd file: the switch's path is the target,
        ' and each start lands on its Disks page - never on Secure.
        Dim dir = Path.Combine(Path.GetTempPath(), "filedo_selftest_vd_" & Guid.NewGuid().ToString("N"))
        Directory.CreateDirectory(dir)
        Dim fdd = Path.Combine(dir, "double click.fdd")
        File.WriteAllBytes(fdd, New Byte() {})
        Try
            Check("disk-route:switch-target", String.Equals(Program.StartupTargetFrom(New String() {"filedo_win.exe", "--mount-ro", fdd}), fdd,
                                                            StringComparison.OrdinalIgnoreCase), fdd)
            For Each start In New Object()() {
                New Object() {"plain", Nothing, "rail_job_vd_mount", False},
                New Object() {"mount-ro", DiskRoute.Decide(fdd, DiskRoute.StartMode.MountReadOnly, ContainerFacts.Unknown(fdd), False), "rail_job_vd_mount", True},
                New Object() {"unmount", DiskRoute.Decide(fdd, DiskRoute.StartMode.Unmount, ContainerFacts.Unknown(fdd), False), "rail_job_vd_unmount", False}}
                Dim shell As ShellForm = Nothing
                Try
                    shell = New ShellForm(fdd, TryCast(start(1), DiskRoute.Decision))
                    Dim page = shell.CurrentJobIdForTest
                    Dim cmd = shell.JobViewForTest.CurrentCommand()
                    Dim want = DirectCast(start(2), String)
                    Dim ro = DirectCast(start(3), Boolean)
                    Check("disk-route:window-" & DirectCast(start(0), String), page = want AndAlso page <> "rail_job_secure" AndAlso cmd.Contains(fdd) AndAlso
                                                                                (Not ro OrElse cmd.Contains(" mount ro")), page & ": " & cmd)
                Finally
                    If shell IsNot Nothing Then shell.Dispose()
                End Try
            Next
        Finally
            Try
                Directory.Delete(dir, True)
            Catch
            End Try
        End Try
    End Sub

    ' T6.25a, as far as it can be automated: the window's job ends filedo.exe with the window
    ' (kill-on-close) and lets everything filedo.exe starts go (silent breakaway) - checked on the
    ' job's flags as Windows reports them, and on a real grandchild, which is found outside the job.
    ' The block server additionally asks for CREATE_BREAKAWAY_FROM_JOB itself (vdStartServer). The
    ' end-to-end proof - close the window with a volume mounted, the volume and the server stay,
    ' and `vd status` from a fresh window lists them - needs a real mount and is the manual check.
    Private Sub CheckMountOutlivesWindow()
        Dim childInJob = False, found = False, grandInJob = True
        Runner.RunGrandchildForTest(childInJob, found, grandInJob)
        Check("disk-outlives:job-flags", ChildJob.BreakawayAndKillFlagsSet(), "0x" & ChildJob.LimitFlagsForTest().ToString("X"))
        Check("disk-outlives:child-in-job", childInJob, "")
        Check("disk-outlives:grandchild-found", found, "")
        Check("disk-outlives:grandchild-outside-job", found AndAlso Not grandInJob, "in job " & grandInJob.ToString())
    End Sub

    ' SP-0004 7.3, T6.23: every table holds exactly the keys English holds. The merged dictionary
    ' falls back to English in silence, so the tables are compared as they are written.
    Private Sub CheckLocaleKeySets()
        Dim en = Localization.OwnKeysForTest("en")
        For Each lang In Localization.Languages
            If lang = "en" Then Continue For
            Dim own = Localization.OwnKeysForTest(lang)
            Dim missing = en.Where(Function(k) Not own.Contains(k)).OrderBy(Function(k) k).ToList()
            Dim extra = own.Where(Function(k) Not en.Contains(k)).OrderBy(Function(k) k).ToList()
            Check("locale-keys:" & lang, missing.Count = 0 AndAlso extra.Count = 0,
                  en.Count.ToString() & " keys; missing " & String.Join(",", missing.Take(8).ToArray()) &
                  "; extra " & String.Join(",", extra.Take(8).ToArray()))
        Next
        ' Every Disks job has its label and purpose in every table.
        For Each lang In Localization.Languages
            Dim own = Localization.OwnKeysForTest(lang)
            Dim gaps As New List(Of String)()
            For Each job In JobCatalogue.GetAllJobs()
                If job.GroupKey <> DiskCommands.GroupKey Then Continue For
                If Not own.Contains(job.LabelKey) Then gaps.Add(job.LabelKey)
                If Not own.Contains(job.PurposeKey) Then gaps.Add(job.PurposeKey)
            Next
            If Not own.Contains(DiskCommands.GroupKey) Then gaps.Add(DiskCommands.GroupKey)
            ' The Disk Manager's row (SP-0063) is no job, and needs its label and purpose all the same.
            For Each key In New String() {RailRow.DiskManagerKey, "purpose_job_vd_manager"}
                If Not own.Contains(key) Then gaps.Add(key)
            Next
            Check("locale-keys:disks:" & lang, gaps.Count = 0, String.Join(",", gaps.ToArray()))
        Next
        ' Every key the Disk Manager names in code is in the English table (a key missing there shows
        ' as the key itself on screen, in every language).
        Dim enDict = Localization.GetDict("en")
        Dim missingKeys As New List(Of String)
        For Each key In DiskManagerKeys()
            If Not enDict.ContainsKey(key) Then missingKeys.Add(key)
        Next
        Check("locale-keys:disk-manager", missingKeys.Count = 0, String.Join(",", missingKeys.Take(12).ToArray()))
    End Sub

    ' The keys the manager's decisions can return: every state word, every action label, every
    ' reason an action is not offered, and the transport's.
    Private Function DiskManagerKeys() As HashSet(Of String)
        Dim keys As New HashSet(Of String)(StringComparer.Ordinal)
        For Each verb In New String() {"mount", "unmount", "verify", "save", "queued", "info"}
            keys.Add(DiskStates.BusyKey(verb))
        Next
        Dim sample = MgrRecord()
        For Each a As DiskAction In [Enum].GetValues(GetType(DiskAction))
            keys.Add(DiskStates.LabelKey(a, Nothing))
            For Each state As DiskRowState In [Enum].GetValues(GetType(DiskRowState))
                For Each packaged In New Boolean() {False, True}
                    For Each reason In New String() {"", "initiator_missing", "initiator_disabled"}
                        For Each r In MgrVariants()
                            Dim why = DiskStates.WhyNot(a, r, state, New DiskContext With {.Packaged = packaged, .TransportReady = (reason = ""), .TransportReason = reason})
                            If why <> "" Then keys.Add(why)
                        Next
                    Next
                Next
            Next
        Next
        keys.Add(DiskStates.WhyNotAll(DiskAction.Mount, New List(Of DiskRecord) From {sample, sample}, New List(Of DiskRowState) From {DiskRowState.NotMounted, DiskRowState.Mounted}, New DiskContext()))
        keys.Add(DiskStates.WhyNotAll(DiskAction.MountAs, New List(Of DiskRecord) From {sample, sample}, New List(Of DiskRowState) From {DiskRowState.NotMounted, DiskRowState.NotMounted}, New DiskContext()))
        For Each k In New String() {"vd_mgr_state_server_gone", "vd_mgr_state_unsaved_fmt", "vd_mgr_state_save_failing_fmt", "vd_mgr_detail_save_failing_fmt", "vd_mgr_state_mounted_ro", "vd_mgr_state_mounted",
                                   "vd_mgr_state_image", "vd_mgr_state_missing", "vd_mgr_state_different", "vd_mgr_state_unreadable",
                                   "vd_mgr_state_unclean", "vd_mgr_state_not_mounted", "vd_mgr_prot_obfuscated", "vd_mgr_prot_encrypted",
                                   "vd_mgr_stale_failed", "vd_mgr_stale_timeout", "vd_mgr_stale_format", "vd_mgr_stale_old_cli",
                                   "vd_mgr_name_help", "vd_mgr_name_close", "vd_mgr_name_clear", "vd_tip_new", "vd_tip_mount", "vd_tip_unmount",
                                   "vd_tip_open", "vd_tip_save", "vd_tip_refresh", "vd_tip_help", "vd_tip_filter", "vd_tip_clear_filter",
                                   "vd_tip_detail_close", "vd_tip_detail_show", "vd_mgr_learn_more", "vd_help_menu_help", "vd_help_menu_first",
                                   "vd_help_menu_guide", "vd_help_menu_guides", "vd_help_menu_site", "vd_help_menu_docs", "vd_help_menu_issues",
                                   "vd_help_title", "vd_help_heading", "vd_help_intro", "vd_help_states_title", "vd_help_keys_title",
                                   "vd_help_notes_title", "vd_help_note_uac", "vd_help_note_autostart", "vd_help_links_title", "vd_welcome_title", "vd_welcome_what",
                                   "vd_welcome_packaged", "vd_welcome_hint", "vd_welcome_create", "vd_welcome_add", "vd_welcome_guide",
                                   "vd_welcome_later", "vd_facts_obfuscated", "vd_facts_encrypted", "vd_mgr_close_mounted",
                                   "vd_mgr_detail_open_while_mounted", "shell_btn_close", "vd_mgr_btn_main", "vd_tip_main"}
        keys.Add(k)
        Next
        For Each k In New String() {
            "vd_mgr_act_autostart", "vd_mgr_strip_guard_running", "vd_mgr_strip_guard_stale", "vd_auto_name_guard",
            "vd_auto_title", "vd_auto_intro", "vd_auto_logon_title", "vd_auto_logon_count_fmt", "vd_auto_logon_none",
            "vd_auto_logon_intro", "vd_auto_row_on", "vd_auto_row_off", "vd_auto_guard_title",
            "vd_auto_guard_on_running", "vd_auto_guard_on_stale", "vd_auto_guard_off",
            "vd_auto_guard_run_never", "vd_auto_guard_run_when_fmt", "vd_auto_guard_run_empty",
            "vd_auto_guard_run_ok_fmt", "vd_auto_guard_run_left_fmt", "vd_auto_guard_outcome_skipped",
            "vd_auto_guard_outcome_unfinished", "vd_auto_switch_on_title", "vd_auto_switch_on_text",
            "vd_auto_switch_off_title", "vd_auto_switch_off_text", "vd_auto_note_consent", "vd_auto_note_signout",
            "vd_auto_note_encrypted", "vd_auto_note_uninstall", "vd_auto_notes_title", "vd_mgr_btn_turn_on", "vd_mgr_btn_turn_off",
            "vd_exit_2_writer", "vd_facts_unclean_writer_fmt"}
            keys.Add(k)
        Next
        For Each s As DiskRowState In DiskHelpDialog.Legend
            keys.Add(DiskHelpDialog.LegendKey(s))
            keys.Add(DiskHelpDialog.StateWordKey(s))
        Next
        For Each s In DiskShortcuts.All
            keys.Add(s.LabelKey)
        Next
        keys.Remove("")
        Return keys
    End Function

    ' ---- SP-0063: the Disk Manager ---------------------------------------------

    ' The golden `vd status json` document the CLI's test holds its output to, from the copy
    ' embedded here, on one line as filedo.exe prints it.
    Private Function GoldenSnapshotLine() As String
        Using source = GetType(SelfTest).Assembly.GetManifestResourceStream("FileDOGUI.vdstatus.golden.json")
            If source Is Nothing Then Return ""
            Using r As New StreamReader(source, Encoding.UTF8)
                Return String.Join("", r.ReadToEnd().Replace(vbCr, "").Split(ControlChars.Lf).Select(Function(l) l.Trim()).ToArray())
            End Using
        End Using
    End Function

    Friend Function GoldenSnapshot() As DiskSnapshot
        Dim problem As String = ""
        Return DiskSnapshot.Parse(GoldenSnapshotLine(), problem)
    End Function

    ' A container at rest, the base the rows below vary.
    ' SP-0121 (AUD-82-F2, F4, AUD-90-F2, F3): the holder arrives as a stable token, never as the CLI's
    ' display wording; every token has a localized word in all five tables; a disk the FMS service holds
    ' is refused for Mount, and an unknown holder is neither held nor read as free; the detail sentences
    ' are localized and say "key" only when a key is stored.
    Private Sub CheckDiskShare()
        Dim tokens = New String() {"none", "file-do", "fms-service", "fms-session", "unknown"}
        Dim words = New String() {"vd_mgr_detail_shared_fmt", "vd_mgr_detail_shared_noroot_fmt", "vd_mgr_detail_autostart_on", "vd_mgr_detail_autostart_key_on"}
        For Each lang In Localization.Languages
            Dim own = Localization.OwnKeysForTest(lang)
            Dim gaps As New List(Of String)
            For Each tok In tokens.Concat(New String() {"something-new", "", "FMS Service"})
                Dim key = DiskStates.HolderKey(tok)
                If Not own.Contains(key) Then gaps.Add(key)
            Next
            For Each key In words
                If Not own.Contains(key) Then gaps.Add(key)
            Next
            Check("locale-keys:holder:" & lang, gaps.Count = 0, String.Join(",", gaps.Distinct().ToArray()))
        Next
        Check("disk-share:unknown-token", DiskStates.HolderKey("something-new") = "vd_mgr_holder_unknown" AndAlso
                                          DiskStates.HolderKey("") = "vd_mgr_holder_unknown" AndAlso
                                          DiskStates.HolderKey("FMS Service") = "vd_mgr_holder_unknown", "")

        ' The reader: five tokens, a legacy display word, a missing holder on a shared and on an unshared disk.
        Dim doc = "{""schema"":""filedo.vd-status"",""version"":1,""transport"":{""ready"":true},""disks"":[" &
                  String.Join(",", tokens.Select(Function(t, i) "{""kind"":""container"",""name"":""h" & i & """,""path"":""C:\\h" & i & ".fdd"",""registered"":true,""file"":""ok"",""shared"":true,""root_name"":""R" & i & """,""holder"":""" & t & """}").ToArray()) &
                  ",{""kind"":""container"",""name"":""legacy"",""path"":""C:\\l.fdd"",""registered"":true,""file"":""ok"",""shared"":true,""root_name"":""L"",""holder"":""FMS Service""}" &
                  ",{""kind"":""container"",""name"":""blank"",""path"":""C:\\b.fdd"",""registered"":true,""file"":""ok"",""shared"":true,""root_name"":""B""}" &
                  ",{""kind"":""container"",""name"":""plain"",""path"":""C:\\p.fdd"",""registered"":true,""file"":""ok""}]}"
        Dim problem As String = ""
        Dim snap = DiskSnapshot.Parse(doc, problem)
        Check("disk-share:parse", snap IsNot Nothing AndAlso snap.Disks.Count = 8, If(snap Is Nothing, problem, snap.Disks.Count.ToString()))
        If snap Is Nothing Then Return
        Dim byName = Function(n As String) snap.Disks.First(Function(d) d.Name = n)
        Dim ctx As New DiskContext With {.Packaged = False, .TransportReady = True}
        Dim why = Function(n As String) DiskStates.WhyNot(DiskAction.Mount, byName(n), DiskRowState.NotMounted, ctx)
        Check("disk-share:held-by-service", byName("h2").Holder = "fms-service" AndAlso byName("h2").IsHeldByFMS AndAlso why("h2") = "vd_mgr_why_held_by_fms", why("h2"))
        Check("disk-share:held-by-session", byName("h3").IsHeldByFMS AndAlso why("h3") = "vd_mgr_why_held_by_fms", why("h3"))
        Check("disk-share:none-is-free", byName("h0").Holder = "none" AndAlso Not byName("h0").IsHeldByFMS AndAlso why("h0") = "", why("h0"))
        Check("disk-share:filedo-is-not-fms", byName("h1").Holder = "file-do" AndAlso Not byName("h1").IsHeldByFMS, byName("h1").Holder)
        Check("disk-share:unknown-is-not-free", byName("h4").Holder = "unknown" AndAlso byName("legacy").Holder = "unknown" AndAlso byName("blank").Holder = "unknown", byName("blank").Holder)
        Check("disk-share:unshared-blank-is-none", byName("plain").Holder = "none" AndAlso Not byName("plain").IsShared, byName("plain").Holder)

        ' The detail sentences: localized in every table, and "key" only with a stored key.
        For Each lang In Localization.Languages
            Dim d = Localization.GetDict(lang)
            Dim rec = byName("h2")
            rec.OpenHandles = 2
            Dim lines = DiskStates.SharedSentences(rec, d)
            Dim sentence = If(lines.Count > 0, lines(0), "")
            Check("disk-share:detail:" & lang, lines.Count = 3 AndAlso sentence <> "" AndAlso Not sentence.Contains("vd_mgr_") AndAlso Not sentence.Contains("fms-service") AndAlso
                                               sentence.Contains(d("vd_mgr_holder_fms_service")), sentence)
        Next
        Dim en = Localization.GetDict("en")
        Dim noKey = byName("h2") : noKey.Autostart = True : noKey.HasStoredKey = False
        Dim withKey = byName("h3") : withKey.Autostart = True : withKey.HasStoredKey = True
        Dim noKeyText = String.Join(" ", DiskStates.SharedSentences(noKey, en).ToArray())
        Dim withKeyText = String.Join(" ", DiskStates.SharedSentences(withKey, en).ToArray())
        Check("disk-detail:autostart-key", Not noKeyText.ToLowerInvariant().Contains("key") AndAlso withKeyText.ToLowerInvariant().Contains("key"), noKeyText & " | " & withKeyText)
        Dim noRoot = byName("h4") : noRoot.RootName = ""
        Check("disk-share:detail-without-root", Not String.Join(" ", DiskStates.SharedSentences(noRoot, en).ToArray()).Contains("root: )"), "")
    End Sub

    ' ---- SP-0148: partition disks ---------------------------------------------------

    ' What `filedo vd disks json` prints, banner and Finish line included: an MBR disk that is refused
    ' whole, and a GPT system disk with a too-small gap, a usable 40 GiB gap between C: and a FileDO
    ' partition this machine has not registered, and its recovery partition.
    Private Const PartSampleGuid As String = "9AC5A33F-1111-4222-8333-444455556666"
    Private Const PartSampleFdGuid As String = "0F0F0F0F-AAAA-4BBB-8CCC-DDDDEEEEFFFF"

    Private Function PartSampleOutput() As String
        Dim json = "{'schema':'filedo.vd-disks','version':1,'available':true,'reason':'','future_field':7,'disks':[" &
            "{'number':1,'guid':'','model':'Old USB HDD','bus':'sata','size':500107862016,'style':'mbr','logical_sector':512," &
            "'physical_sector':512,'online':true,'read_only':false,'removable':false,'serial_present':true,'usable':false,'reason':'mbr'," &
            "'partitions':[{'number':1,'guid':'','offset':1048576,'length':400000000000,'type':'07','kind':'mbr','fileDO':false,'letters':['E:']}]," &
            "'free':[{'offset':400001048576,'length':100000000000,'usable':false,'reason':'mbr'}]}," &
            "{'number':2,'guid':'{" & PartSampleGuid & "}','model':'Samsung SSD 9100 PRO 4TB','bus':'nvme','size':4000787030016,'style':'gpt'," &
            "'logical_sector':512,'physical_sector':4096,'online':true,'read_only':false,'removable':false,'serial_present':true,'system':true," &
            "'usable':true,'reason':'','partitions':[" &
            "{'number':1,'guid':'A1','offset':1048576,'length':104857600,'type':'C12A','kind':'efi','fileDO':false}," &
            "{'number':2,'guid':'A2','offset':105906176,'length':16777216,'type':'E3C9','kind':'msr','fileDO':false}," &
            "{'number':3,'guid':'A3','offset':122683392,'length':214748364800,'type':'EBD0','kind':'basic-data','fileDO':false,'letters':['C:']}," &
            "{'number':4,'guid':'{" & PartSampleFdGuid & "}','offset':300000000000,'length':1073741824,'type':'FD','kind':'fileDO','fileDO':true,'registered':''}," &
            "{'number':5,'guid':'A5','offset':3999000000000,'length':1000000000,'type':'DE94','kind':'recovery','fileDO':false}]," &
            "'free':[{'offset':214871048192,'length':10485760,'usable':false,'reason':'too-small'}," &
            "{'offset':214881533952,'length':42949672960,'usable':true,'reason':'','before':'basic data','after':'fileDO'}]}]}"
        Return "FileDO v3.2610 - vd disks" & vbCrLf & json.Replace("'", """") & vbCrLf & "Finish: 0.1 s" & vbCrLf
    End Function

    Private Sub CheckDiskPartitions()
        Dim ui = Localization.GetDict(ShellSettings.Language())
        Dim problem As String = ""

        ' The reader: the line among the banner and the Finish line, an unknown field ignored.
        Dim doc = VdDisksDoc.Parse(PartSampleOutput(), problem)
        Dim parsed = doc IsNot Nothing AndAlso doc.Available AndAlso doc.Disks.Count = 2
        Check("disk-part:parser-reads-sample", parsed, If(doc Is Nothing, problem, doc.Disks.Count.ToString()))
        If Not parsed Then Return
        Dim mbr = doc.Disks(0)
        Dim gpt = doc.Disks(1)
        Check("disk-part:parser-mbr-refused", Not mbr.Usable AndAlso mbr.Reason = "mbr" AndAlso Not mbr.HasUsableFree AndAlso
                                               mbr.Free.Count = 1 AndAlso Not mbr.Free(0).Usable AndAlso mbr.Partitions(0).Letters.Contains("E:"), mbr.Reason)
        Check("disk-part:parser-gpt-gap", gpt.Usable AndAlso gpt.Guid = PartSampleGuid AndAlso gpt.Partitions.Count = 5 AndAlso gpt.Free.Count = 2 AndAlso
                                           gpt.Free(1).Usable AndAlso gpt.Free(1).Length = 42949672960L AndAlso gpt.Free(0).Reason = "too-small" AndAlso
                                           gpt.Size = 4000787030016L AndAlso gpt.Bus = "nvme", gpt.Guid)
        Dim un = doc.Unregistered()
        Check("disk-part:parser-unregistered-filedo", un.Count = 1 AndAlso un(0).Value.Guid = PartSampleFdGuid AndAlso un(0).Key Is gpt, un.Count.ToString())
        Dim fd As VdDiskInfo = Nothing, fp As VdPartInfo = Nothing
        Check("disk-part:parser-find-by-locator", doc.FindPartition("{" & PartSampleFdGuid.ToLowerInvariant() & "}", fd, fp) AndAlso fd Is gpt AndAlso fp.Number = 4, "")
        Dim store = VdDisksDoc.Parse("{""schema"":""filedo.vd-disks"",""version"":1,""available"":false,""reason"":""store-build"",""disks"":[]}", problem)
        Check("disk-part:parser-store-build", store IsNot Nothing AndAlso Not store.Available AndAlso store.Reason = "store-build" AndAlso store.Disks.Count = 0, problem)
        Check("disk-part:parser-refuses-version", VdDisksDoc.Parse("{""schema"":""filedo.vd-disks"",""version"":2,""disks"":[]}", problem) Is Nothing AndAlso
                                                   VdDisksDoc.Parse("Finish: 0 s", problem) Is Nothing AndAlso
                                                   VdDisksDoc.Parse("{""schema"":""filedo.vd-status"",""version"":1,""disks"":[]}", problem) Is Nothing, problem)
        Check("disk-part:reason-unknown-token", PartitionCommands.ReasonKey("new-token") = "vd_part_reason_other" AndAlso
                                                 PartitionCommands.ReasonKey("not-initialized") = "vd_part_reason_not_initialized", "")

        ' Every string of the partition surfaces, in all five languages - a key in English alone
        ' would silently fall back.
        Dim keys = PartitionCommands.AllKeys()
        For Each lang In Localization.Languages
            Dim own = Localization.OwnKeysForTest(lang)
            Dim missing = keys.Where(Function(k) Not own.Contains(k)).ToList()
            Check("disk-part:keys:" & lang, missing.Count = 0, keys.Count.ToString() & " keys; missing " & String.Join(",", missing.Take(8).ToArray()))
        Next

        ' The lines (cli-surface.md): by GUID and offset, the size in MiB or max, the credential by name.
        Dim cases = New Object()() {
            New Object() {"new-max", PartitionCommands.NewPart(PartSampleGuid, 0, 214881533952L, "fast", "pdisk", "", False),
                          "vd new part disk:{" & PartSampleGuid & "} size max at 214881533952 fast as pdisk p: force"},
            New Object() {"new-size-label-cred", PartitionCommands.NewPart("{" & PartSampleGuid.ToLowerInvariant() & "}", 20480, 214881533952L, "vault", "safe", "My Data", True),
                          "vd new part disk:{" & PartSampleGuid & "} size 20480M at 214881533952 vault as safe label ""My Data"" pe:FILEDO_SHELL_CRED force"},
            New Object() {"image", PartitionCommands.Image("pdisk", "D:\copies\p disk.fdd"), "vd image pdisk to ""D:\copies\p disk.fdd"""},
            New Object() {"destroy", PartitionCommands.Destroy("pdisk", False), "vd destroy pdisk force"},
            New Object() {"destroy-wipe", PartitionCommands.Destroy("pdisk", True), "vd destroy pdisk wipe force"},
            New Object() {"adopt", PartitionCommands.Adopt(PartSampleFdGuid, "found"), "vd adopt fdpart:{" & PartSampleFdGuid & "} as found"},
            New Object() {"disks", New List(Of String)(PartitionCommands.DisksArguments), "--no-history vd disks json"}}
        For Each c In cases
            Dim args = DirectCast(c(1), List(Of String))
            Dim line = ArgQuoting.JoinArgs(args)
            Check("disk-part:line:" & CStr(c(0)), line = CStr(c(2)), line & " (want " & CStr(c(2)) & ")")
            ' The run report's line goes through Runner's redaction (whose word lists the CLI work
            ' owns): it keeps the verb, and the credential travels by variable name only, if at all.
            Dim redacted = ArgQuoting.JoinArgs(Runner.RedactCredentialArgs(args))
            Dim head = String.Join(" ", args.Take(If(args(0) = "--no-history", 3, 2)).ToArray())
            Check("disk-part:line:" & CStr(c(0)) & ":redaction-keeps-verb", redacted.StartsWith(head, StringComparison.Ordinal), redacted)
        Next

        ' A partition row of `vd status json`: no path, addressed by its name, its own refusals.
        Dim snapLine = ("{'schema':'filedo.vd-status','version':1,'at':'2026-10-03T10:00:00Z','packaged':false," &
                        "'transport':{'ready':true,'initiator_service':'running','reason':''},'disks':[" &
                        "{'kind':'container','name':'pdisk','path':'','container_id':'abcdef01-2222-4333-8444-555555555555','registered':true," &
                        "'file':'ok','profile':'fast','protection':'obfuscated','logical_size':42949672960,'clean':true,'last_good_save':''," &
                        "'auto':false,'mount':null,'carrier':'partition','locator':'fdpart:{" & PartSampleFdGuid & "}'}," &
                        "{'kind':'container','name':'gone','path':'','container_id':'abcdef01-3333-4333-8444-555555555555','registered':true," &
                        "'file':'missing','profile':'fast','protection':'obfuscated','logical_size':1073741824,'clean':null,'last_good_save':''," &
                        "'auto':false,'mount':null,'carrier':'partition','locator':'fdpart:{11111111-2222-4333-8444-555555555555}'}," &
                        "{'kind':'container','name':'work','path':'C:\\d\\work.fdd','container_id':'abcdef01-4444-4333-8444-555555555555','registered':true," &
                        "'file':'ok','profile':'plain','protection':'obfuscated','logical_size':1073741824,'clean':true,'last_good_save':''," &
                        "'auto':false,'mount':null}]}").Replace("'", """")
        Dim snap = DiskSnapshot.Parse(snapLine, problem)
        If snap Is Nothing OrElse snap.Disks.Count <> 3 Then
            Check("disk-part:snapshot-carrier", False, problem)
            Return
        End If
        Dim pr = snap.Disks(0)
        Dim gone = snap.Disks(1)
        Dim fileRow = snap.Disks(2)
        Check("disk-part:snapshot-carrier", pr.IsPartition AndAlso pr.Path = "" AndAlso pr.Target = "pdisk" AndAlso
                                             pr.Locator = "fdpart:{" & PartSampleFdGuid & "}" AndAlso Not fileRow.IsPartition AndAlso fileRow.Target = fileRow.Path,
              pr.Carrier & " " & pr.Target)
        Dim mountLine = ArgQuoting.JoinArgs(DiskStates.QuickCommand(DiskAction.MountAs, pr, New DiskOptions With {.NoScan = True, .Letter = "P:"}))
        Check("disk-part:mount-by-name-never-noscan", mountLine = "pdisk mount as P:", mountLine)
        Dim infoLine = ArgQuoting.JoinArgs(DiskStates.QuickCommand(DiskAction.Info, pr, Nothing))
        Check("disk-part:info-by-name", infoLine = "pdisk info", infoLine)
        Dim imgLine = ArgQuoting.JoinArgs(DiskStates.QuickCommand(DiskAction.ImageToFile, pr, New DiskOptions With {.Dest = "D:\p.fdd"}))
        Check("disk-part:image-by-name", imgLine = "vd image pdisk to D:\p.fdd", imgLine)
        Dim ctx As New DiskContext()
        Dim rest = DiskRowState.NotMounted
        Dim refusals = New Dictionary(Of DiskAction, String) From {
            {DiskAction.Compact, "vd_part_why_fixed_size"}, {DiskAction.Grow, "vd_part_why_fixed_size"},
            {DiskAction.Export, "vd_part_why_job_page"}, {DiskAction.Seal, "vd_part_why_job_page"}, {DiskAction.Clone, "vd_part_why_job_page"},
            {DiskAction.ChangePassword, "vd_part_why_job_page"}, {DiskAction.Format, "vd_part_why_job_page"},
            {DiskAction.ShowInFolder, "vd_part_why_no_file"}}
        For Each kv In refusals
            Dim why = DiskStates.WhyNot(kv.Key, pr, rest, ctx)
            Check("disk-part:refuses:" & kv.Key.ToString(), why = kv.Value AndAlso ui.ContainsKey(why), why)
        Next
        Check("disk-part:offers-at-rest", DiskStates.WhyNot(DiskAction.Mount, pr, rest, ctx) = "" AndAlso DiskStates.WhyNot(DiskAction.ImageToFile, pr, rest, ctx) = "" AndAlso
                                           DiskStates.WhyNot(DiskAction.Destroy, pr, rest, ctx) = "" AndAlso DiskStates.WhyNot(DiskAction.Info, pr, rest, ctx) = "", "")
        Check("disk-part:image-only-partition", DiskStates.WhyNot(DiskAction.ImageToFile, fileRow, rest, ctx) = "vd_part_why_not_partition", "")
        Dim goneState = DiskStates.StateOf(gone, "")
        Check("disk-part:missing-in-its-words", goneState = DiskRowState.Missing AndAlso
                                                 DiskStates.StateText(gone, goneState, "", ui) = ui("vd_part_state_missing") AndAlso
                                                 DiskStates.WhyNot(DiskAction.Mount, gone, goneState, ctx) = "vd_part_why_missing", "")
        Check("disk-part:adopt-store-refused", DiskStates.WhyNot(DiskAction.Adopt, Nothing, rest, New DiskContext With {.Packaged = True}) = "vd_part_store" AndAlso
                                               DiskStates.HiddenInBuild(DiskAction.Adopt, New DiskContext With {.Packaged = True}) AndAlso
                                               DiskStates.WhyNot(DiskAction.Adopt, Nothing, rest, ctx) = "", "")

        ' The disk map lays out from its client size alone: at every scale from 100 % to 225 % its
        ' segments are in disk order, touch, never overlap, end at the right edge and are each at least
        ' the floor wide - for the system disk with a 16 MiB partition beside a 200 GiB one.
        Dim was = Theme.CurrentDpi
        Try
            For Each pct In New Integer() {100, 125, 150, 175, 200, 225}
                Theme.CurrentDpi = 96 * pct \ 100
                Dim problems As New List(Of String)
                Using bar As New DiskMapBar(gpt, ui)
                    bar.Size = Global.FileDOGUI.Ui.PxSize(bar, 460, 34)
                    Dim segs = bar.SegmentsForTest
                    Dim floor = Math.Min(Global.FileDOGUI.Ui.Px(bar, DiskMapBar.MinSegmentDesign), bar.ClientSize.Width \ Math.Max(1, segs.Count))
                    If segs.Count <> gpt.Partitions.Count + gpt.Free.Count Then problems.Add("count " & segs.Count.ToString())
                    For i = 0 To segs.Count - 1
                        Dim b = segs(i).Bounds
                        If b.Width < floor Then problems.Add("segment " & i.ToString() & " is " & b.Width.ToString() & " px")
                        If b.Top <> 0 OrElse b.Height <> bar.ClientSize.Height Then problems.Add("segment " & i.ToString() & " height")
                        If i = 0 AndAlso b.Left <> 0 Then problems.Add("first starts at " & b.Left.ToString())
                        If i > 0 AndAlso b.Left <> segs(i - 1).Bounds.Right Then problems.Add("gap or overlap at " & i.ToString())
                    Next
                    If segs.Count > 0 AndAlso segs(segs.Count - 1).Bounds.Right <> bar.ClientSize.Width Then problems.Add("last ends at " & segs(segs.Count - 1).Bounds.Right.ToString())
                    Dim usableSeg = segs.FirstOrDefault(Function(s) s.IsFree AndAlso s.Usable)
                    If usableSeg Is Nothing OrElse Not bar.SelectFree(usableSeg.Index) OrElse bar.SelectedExtent IsNot gpt.Free(1) Then problems.Add("usable gap not selectable")
                    Dim tinySeg = segs.FirstOrDefault(Function(s) s.IsFree AndAlso Not s.Usable)
                    If tinySeg Is Nothing OrElse bar.SelectFree(tinySeg.Index) Then problems.Add("unusable gap selectable")
                    If tinySeg IsNot Nothing AndAlso Not bar.TipOf(tinySeg).Contains(ui("vd_part_reason_too_small")) Then problems.Add("unusable gap tip: " & bar.TipOf(tinySeg))
                End Using
                ' The dialog at the same scale: its maps are as wide as the scale says, and one map's
                ' segments never spill past it.
                Using dlg As New DiskNewPartitionDialog(ui, doc, New HashSet(Of String)(StringComparer.OrdinalIgnoreCase), Nothing)
                    For Each m In dlg.MapsForTest
                        If m.Width <> Global.FileDOGUI.Ui.Px(m, 460) Then problems.Add("dialog map width " & m.Width.ToString())
                        If m.SegmentsForTest.Any(Function(s) s.Bounds.Right > m.ClientSize.Width OrElse s.Bounds.Width < 0) Then problems.Add("dialog map spills")
                    Next
                End Using
                Check("disk-part:map-layout:" & pct.ToString() & "%", problems.Count = 0, String.Join("; ", problems.Take(6).ToArray()))
            Next
        Finally
            Theme.CurrentDpi = was
        End Try

        ' The new-partition dialog: the usable gap chosen at once, fast and all of it by default, its
        ' line; vault refuses an empty password; plain says what stays in the free space; Create asks
        ' with Cancel the default.
        Using dlg As New DiskNewPartitionDialog(ui, doc, New HashSet(Of String)(StringComparer.OrdinalIgnoreCase) From {"partition-disk"}, Nothing)
            Dim line = If(dlg.CommandLine() Is Nothing, "(none)", ArgQuoting.JoinArgs(dlg.CommandLine()))
            Check("disk-part:dialog-defaults", dlg.CreateEnabledForTest AndAlso dlg.ChosenExtent Is gpt.Free(1) AndAlso dlg.ChosenName = "partition-disk-2" AndAlso
                                                line = "vd new part disk:{" & PartSampleGuid & "} size max at 214881533952 fast as partition-disk-2 p: force", line)
            dlg.SetForTest("", True, "vault", "safe", "")
            Check("disk-part:dialog-vault-needs-password", Not dlg.CreateEnabledForTest AndAlso dlg.BlockReason() = ui("vd_cred_vault_needs"), dlg.BlockReason())
            dlg.SetForTest("20", False, "plain", "safe", "")
            line = ArgQuoting.JoinArgs(dlg.CommandLine())
            Check("disk-part:dialog-plain-residue", dlg.CreateEnabledForTest AndAlso dlg.ProfileNoteForTest = Localization.Multiline(ui("vd_part_residue")) AndAlso
                                                     line.Contains(" size 20480M at 214881533952 plain as safe p: force"), line)
            dlg.SetForTest("10", False, "fast", "safe", "", inMiB:=True)
            Dim tooSmall = dlg.BlockReason()
            dlg.SetForTest("50", False, "fast", "safe", "")
            Check("disk-part:dialog-size-bounds", tooSmall = ui("vd_part_too_small") AndAlso dlg.BlockReason().StartsWith(Localization.Format(ui("vd_part_too_big_fmt"), "").TrimEnd("."c)), dlg.BlockReason())
            dlg.SetForTest("", True, "fast", "safe", "")
            Dim spec = dlg.ConfirmSpec()
            Check("disk-part:dialog-confirm-cancel-default", ShellDialog.DefaultOf(spec) = ui("shell_btn_cancel") AndAlso spec.Text.Contains(PartSampleGuid) AndAlso
                                                              spec.Text.Contains("Samsung SSD 9100 PRO 4TB"), ShellDialog.DefaultOf(spec))
            Dim dp As New List(Of String)
            WalkAccessible(dlg, dp)
            Check("a11y:DiskNewPartitionDialog", dp.Count = 0 AndAlso dlg.CancelButton IsNot Nothing, String.Join("; ", dp.Take(8).ToArray()))
        End Using
        Using dlg As New DiskNewPartitionDialog(ui, New VdDisksDoc With {.Available = True}, Nothing, Nothing)
            Check("disk-part:dialog-no-disks", Not dlg.CreateEnabledForTest AndAlso dlg.BlockReason() = ui("vd_part_need_extent"), "")
        End Using

        ' Delete: the typed name, Cancel the default that Enter and Escape give, Delete only on an exact match.
        Using dlg As New DiskDestroyPartitionDialog(ui, "pdisk", "Samsung SSD, 40 GiB at 200.12 GiB", Nothing)
            Dim cancel = ui("shell_btn_cancel")
            Dim before = dlg.DefaultButtonForTest = cancel AndAlso dlg.EscapeButtonForTest = cancel AndAlso Not dlg.DeleteEnabledForTest
            dlg.TypeForTest("PDISK")
            Dim wrongCase = Not dlg.DeleteEnabledForTest
            dlg.TypeForTest("pdisk")
            Dim typed = dlg.DeleteEnabledForTest AndAlso dlg.DefaultButtonForTest = cancel
            Check("disk-part:delete-default-cancel", before AndAlso wrongCase AndAlso typed, dlg.DefaultButtonForTest)
            Dim dp As New List(Of String)
            WalkAccessible(dlg, dp)
            Check("a11y:DiskDestroyPartitionDialog", dp.Count = 0, String.Join("; ", dp.Take(8).ToArray()))
        End Using
        Using dlg As New DiskAdoptDialog(ui, un, Nothing, Nothing)
            Dim dp As New List(Of String)
            WalkAccessible(dlg, dp)
            Check("disk-part:adopt-dialog", dlg.ChosenGuid = PartSampleFdGuid AndAlso dlg.ChosenName = "partition-disk" AndAlso dp.Count = 0,
                  dlg.ChosenGuid & " " & String.Join("; ", dp.Take(4).ToArray()))
        End Using

        ' The window: the Kind column, the place where a file row has its path, the detail's facts,
        ' Image to file in the row's menu, and the Store build's choice without partitions.
        Dim m2 As DiskManagerForm = Nothing
        Try
            m2 = New DiskManagerForm()
            m2.ApplySnapshotForTest(snap, "")
            m2.ApplyDisksForTest(doc)
            Dim rowP = m2.RowTextsForTest.FirstOrDefault(Function(r) r(0) = "pdisk")
            Dim rowF = m2.RowTextsForTest.FirstOrDefault(Function(r) r(0) = "work")
            Check("disk-part:mgr-kind-column", rowP IsNot Nothing AndAlso rowF IsNot Nothing AndAlso rowP(9) = ui("vd_part_carrier_partition") AndAlso
                                               rowF(9) = ui("vd_part_carrier_file") AndAlso rowP(7).Contains("Samsung SSD 9100 PRO 4TB") AndAlso rowP(7).Contains("1 GiB"),
                  If(rowP Is Nothing, "no row", String.Join(" | ", rowP)))
            m2.SelectForTest(pr.Key)
            Check("disk-part:mgr-detail", m2.DetailForTest.Contains("Samsung SSD 9100 PRO 4TB") AndAlso m2.DetailForTest.Contains(Localization.Multiline(ui("vd_part_detail_consent"))),
                  m2.DetailForTest)
            Dim menu = m2.MenuForTest(True)
            Check("disk-part:mgr-menu", menu.Contains(ui("vd_mgr_act_image")) AndAlso menu.Any(Function(i) i.StartsWith(ui("vd_mgr_act_grow") & " [") AndAlso i.Contains(ui("vd_part_why_fixed_size"))) AndAlso
                                        Not menu.Any(Function(i) i.StartsWith(ui("vd_mgr_act_show_folder"))), String.Join(" | ", menu.ToArray()))
            Dim title = "", text = ""
            Dim choices = m2.NewDiskChoice(title, text)
            Dim setupOk = choices.Length = 3 AndAlso choices(1) = ui("vd_part_btn_partition")
            Packaging.OverrideForTest = True
            choices = m2.NewDiskChoice(title, text)
            Check("disk-part:store-has-no-partition-choice", setupOk AndAlso choices.Length = 2 AndAlso Not choices.Contains(ui("vd_part_btn_partition")) AndAlso
                                                            text.Contains(ui("vd_part_store")), String.Join(" | ", choices))
            Dim more = m2.MenuItemsForTest("more").Select(Function(i) i.Text).ToList()
            Check("disk-part:store-hides-adopt", Not more.Contains(ui("vd_mgr_act_adopt")), String.Join(" | ", more.ToArray()))
            Packaging.OverrideForTest = Nothing
            more = m2.MenuItemsForTest("more").Select(Function(i) i.Text).ToList()
            Check("disk-part:adopt-in-more", more.Contains(ui("vd_mgr_act_adopt")), String.Join(" | ", more.ToArray()))
        Finally
            Packaging.OverrideForTest = Nothing
            If m2 IsNot Nothing Then m2.Dispose()
        End Try
    End Sub

    Private Function MgrRecord() As DiskRecord
        Return New DiskRecord With {.Name = "work", .Path = DiskSample, .ContainerId = "11111111-2222-4333-8444-555555555555",
                                    .Registered = True, .FileState = "ok", .Profile = "plain", .Protection = DiskProtection.Obfuscated,
                                    .LogicalSize = 20L << 30, .Clean = True}
    End Function

    Private Function MgrVariant(change As Action(Of DiskRecord)) As DiskRecord
        Dim r = MgrRecord()
        change(r)
        Return r
    End Function

    Private Function MgrVariants() As List(Of DiskRecord)
        Return New List(Of DiskRecord) From {
            MgrRecord(),
            MgrVariant(Sub(r) r.Protection = DiskProtection.Encrypted),
            MgrVariant(Sub(r) r.Protection = DiskProtection.Unknown),
            MgrVariant(Sub(r)
                           r.Profile = "ram" : r.Letter = "R:" : r.ServerAlive = True : r.HasRam = True : r.RamDirty = 1 << 20
                       End Sub),
            MgrVariant(Sub(r)
                           r.Letter = "X:" : r.ReadOnly = True : r.ServerAlive = True
                       End Sub),
            MgrVariant(Sub(r)
                           r.Registered = False : r.Name = "" : r.Letter = "U:" : r.ServerAlive = True
                       End Sub),
            MgrVariant(Sub(r) r.AutoMount = True),
            MgrVariant(Sub(r)
                           r.Kind = "image" : r.Letter = "I:" : r.ServerAlive = True
                       End Sub)}
    End Function

    ' The record a state is made of, for the matrix below: a mount for S2-S4, an image for S5, the
    ' file's state for S6-S8, the clean marker for S9.
    Private Function RecordFor(state As DiskRowState, profile As String, protection As DiskProtection) As DiskRecord
        Dim r = MgrRecord()
        r.Profile = profile
        r.Protection = protection
        Select Case state
            Case DiskRowState.ServerGone
                r.Letter = "S:" : r.ServerAlive = False
            Case DiskRowState.Unsaved
                r.Letter = "R:" : r.ServerAlive = True : r.HasRam = True : r.RamDirty = 180L << 20
            Case DiskRowState.Mounted
                r.Letter = "W:" : r.ServerAlive = True
            Case DiskRowState.Image
                r.Kind = "image" : r.Letter = "I:" : r.ServerAlive = True : r.Protection = DiskProtection.Unknown
            Case DiskRowState.Missing
                r.FileState = "missing" : r.Clean = Nothing
            Case DiskRowState.Different
                r.FileState = "different" : r.Clean = Nothing
            Case DiskRowState.Unreadable
                r.FileState = "unreadable" : r.FileError = "damaged" : r.Clean = Nothing
            Case DiskRowState.Unclean
                r.Clean = False
        End Select
        Return r
    End Function

    ' Section 10: the reader of the snapshot over the golden document, an unknown field, a missing
    ' optional one, and the shapes it must refuse whole.
    Private Sub CheckDiskSnapshot()
        Dim line = GoldenSnapshotLine()
        Check("disk-snap:golden-embedded", line.StartsWith("{"), line.Length.ToString() & " chars")
        Dim problem As String = ""
        Dim s = DiskSnapshot.Parse(line, problem)
        Check("disk-snap:golden-reads", s IsNot Nothing AndAlso s.Version = 1 AndAlso s.Disks.Count = 10 AndAlso s.TransportReady AndAlso Not s.Packaged,
              If(s Is Nothing, problem, s.Disks.Count.ToString()))
        If s Is Nothing Then Return
        Dim byName = Function(n As String) s.Disks.FirstOrDefault(Function(d) d.Name = n)
        Dim expect = New Object()() {
            New Object() {"archive", DiskRowState.NotMounted}, New Object() {"backup", DiskRowState.Missing},
            New Object() {"junk", DiskRowState.Unreadable}, New Object() {"moved", DiskRowState.Different},
            New Object() {"old", DiskRowState.ServerGone}, New Object() {"scratch", DiskRowState.Unsaved},
            New Object() {"secrets", DiskRowState.Mounted}, New Object() {"work", DiskRowState.Mounted}}
        For Each e In expect
            Dim d = byName(DirectCast(e(0), String))
            Dim want = DirectCast(e(1), DiskRowState)
            Dim got = If(d Is Nothing, CType(-1, DiskRowState), DiskStates.StateOf(d, ""))
            Check("disk-snap:state:" & DirectCast(e(0), String), d IsNot Nothing AndAlso got = want, got.ToString())
        Next
        Dim archive = byName("archive")
        Check("disk-snap:archive", archive IsNot Nothing AndAlso archive.AutoMount AndAlso archive.Clean.HasValue AndAlso archive.Clean.Value AndAlso
                                   archive.Protection = DiskProtection.Obfuscated AndAlso archive.Registered AndAlso Not archive.IsMounted, "")
        Dim secrets = byName("secrets")
        Check("disk-snap:secrets", secrets IsNot Nothing AndAlso secrets.Protection = DiskProtection.Encrypted AndAlso secrets.ReadOnly AndAlso
                                   secrets.Letter = "X:" AndAlso secrets.Profile = "vault", "")
        Dim scratch = byName("scratch")
        Check("disk-snap:ram", scratch IsNot Nothing AndAlso scratch.HasRam AndAlso scratch.RamDirty = 180L << 20 AndAlso scratch.IsRam, "")
        Dim junk = byName("junk")
        Check("disk-snap:unreadable-says-why", junk IsNot Nothing AndAlso junk.FileError <> "" AndAlso Not junk.Clean.HasValue AndAlso
                                                junk.Protection = DiskProtection.Unknown, "")
        Dim loose = s.Disks.FirstOrDefault(Function(d) Not d.Registered AndAlso Not d.IsImage)
        Check("disk-snap:unregistered-mount", loose IsNot Nothing AndAlso loose.Letter = "U:" AndAlso loose.BaseName = "loose", "")
        Dim image = s.Disks.FirstOrDefault(Function(d) d.IsImage)
        Check("disk-snap:image", image IsNot Nothing AndAlso image.Letter = "I:" AndAlso image.ImageFormat = "iso" AndAlso
                                 DiskStates.StateOf(image, "") = DiskRowState.Image, "")

        ' An unknown field is ignored; a missing optional one takes its default.
        Dim future = "{""schema"":""filedo.vd-status"",""version"":1,""future"":{""x"":[1,2]},""transport"":{""ready"":true,""new"":1}," &
                     """disks"":[{""kind"":""container"",""name"":""a"",""path"":""C:\\a.fdd"",""registered"":true,""file"":""ok"",""future"":true}," &
                     "{""kind"":""container"",""name"":""b"",""path"":""C:\\b.fdd"",""registered"":true,""mount"":{""letter"":""B:"",""server_alive"":true}}]}"
        s = DiskSnapshot.Parse(future, problem)
        Check("disk-snap:unknown-field", s IsNot Nothing AndAlso s.Disks.Count = 2, problem)
        If s IsNot Nothing AndAlso s.Disks.Count = 2 Then
            Dim a = s.Disks(0)
            Check("disk-snap:missing-optional", a.Letter = "" AndAlso Not a.Clean.HasValue AndAlso Not a.LastGoodSave.HasValue AndAlso
                                                a.Protection = DiskProtection.Unknown AndAlso Not a.HasRam AndAlso s.Disks(1).Letter = "B:" AndAlso
                                                Not s.Disks(1).HasRam, a.Letter)
        End If
        ' A banner or a warning on the same stream does not stop the read.
        s = DiskSnapshot.Parse("Warning: x" & vbLf & line & vbLf & " Finish: y" & vbLf, problem)
        Check("disk-snap:among-lines", s IsNot Nothing AndAlso s.Disks.Count = 10, problem)
        ' Principle 3: a protection word other than the two is not taken for either.
        s = DiskSnapshot.Parse(future.Replace("""file"":""ok"",", """file"":""ok"",""protection"":""protected"","), problem)
        Check("disk-snap:only-two-words", s IsNot Nothing AndAlso s.Disks(0).Protection = DiskProtection.Unknown, "")
        ' AUD-35-F5: a ram mount whose saving fails carries save_error, an optional field of version 1;
        ' the row is a warning in words of its own, and a snapshot without the field reads as healthy.
        Dim failing = "{""schema"":""filedo.vd-status"",""version"":1,""transport"":{""ready"":true},""disks"":[{""kind"":""container"",""name"":""r"",""path"":""C:\\r.fdd""," &
                      """registered"":true,""file"":""ok"",""profile"":""ram"",""mount"":{""letter"":""R:"",""server_alive"":true,""ram"":{""dirty_bytes"":1048576," &
                      """saving"":false,""last_good_save"":null,""save_error"":""vdisk: I/O error: the device is not ready""}}}]}"
        s = DiskSnapshot.Parse(failing, problem)
        Dim fr = If(s IsNot Nothing AndAlso s.Disks.Count = 1, s.Disks(0), Nothing)
        Dim snapUi = Localization.GetDict(ShellSettings.Language())
        Check("disk-snap:save-error", fr IsNot Nothing AndAlso fr.HasRam AndAlso fr.RamSaveError = "vdisk: I/O error: the device is not ready" AndAlso
                                      DiskStates.StateOf(fr, "") = DiskRowState.Unsaved AndAlso
                                      DiskStates.StateText(fr, DiskRowState.Unsaved, "", snapUi) = Localization.Format(snapUi("vd_mgr_state_save_failing_fmt"), "1 MiB"),
              If(fr Is Nothing, problem, fr.RamSaveError))
        s = DiskSnapshot.Parse(failing.Replace(",""save_error"":""vdisk: I/O error: the device is not ready""", ""), problem)
        fr = If(s IsNot Nothing AndAlso s.Disks.Count = 1, s.Disks(0), Nothing)
        Check("disk-snap:save-error-absent", fr IsNot Nothing AndAlso fr.HasRam AndAlso fr.RamSaveError = "" AndAlso
                                             DiskStates.StateText(fr, DiskRowState.Unsaved, "", snapUi) = Localization.Format(snapUi("vd_mgr_state_unsaved_fmt"), "1 MiB"),
              If(fr Is Nothing, problem, fr.RamSaveError))

        ' Refused whole, each with the sentence of why.
        For Each c In New String()() {
            New String() {"newer-major", line.Replace("""version"": 1", """version"": 2").Replace("""version"":1", """version"":2"), "vd_mgr_stale_format"},
            New String() {"other-schema", line.Replace("filedo.vd-status", "filedo.other"), "vd_mgr_stale_format"},
            New String() {"no-disks", "{""schema"":""filedo.vd-status"",""version"":1}", "vd_mgr_stale_format"},
            New String() {"broken", "{""schema"":""filedo.vd-status"",""version"":1,""disks"":[", "vd_mgr_stale_format"},
            New String() {"no-document", "Nothing is mounted. No block server is running.", "vd_mgr_stale_failed"}}
            s = DiskSnapshot.Parse(c(1), problem)
            Check("disk-snap:refuses:" & c(0), s Is Nothing AndAlso problem = c(2), problem)
        Next
    End Sub

    ' Section 5.4: the precedence of the states, their words and their glyphs.
    Private Sub CheckDiskStates()
        Dim ui = Localization.GetDict(ShellSettings.Language())
        Dim gone = RecordFor(DiskRowState.ServerGone, "ram", DiskProtection.Obfuscated)
        gone.HasRam = True : gone.RamDirty = 5
        Check("disk-state:server-gone-beats-unsaved", DiskStates.StateOf(gone, "") = DiskRowState.ServerGone, "")
        Check("disk-state:busy-beats-all", DiskStates.StateOf(gone, "unmount") = DiskRowState.Busy AndAlso
                                          DiskStates.StateText(gone, DiskRowState.Busy, "unmount", ui) = ui("vd_mgr_state_busy_unmount"), "")
        Dim missing = RecordFor(DiskRowState.Missing, "plain", DiskProtection.Obfuscated)
        missing.Clean = False
        Check("disk-state:missing-beats-unclean", DiskStates.StateOf(missing, "") = DiskRowState.Missing, "")
        Dim mountedMissing = RecordFor(DiskRowState.Mounted, "plain", DiskProtection.Obfuscated)
        mountedMissing.FileState = "missing"
        Check("disk-state:mounted-beats-file", DiskStates.StateOf(mountedMissing, "") = DiskRowState.Mounted, "")
        Dim unknownClean = MgrRecord()
        unknownClean.Clean = Nothing
        Check("disk-state:no-marker-is-not-unclean", DiskStates.StateOf(unknownClean, "") = DiskRowState.NotMounted, "")
        Dim cleanRam = RecordFor(DiskRowState.Mounted, "ram", DiskProtection.Obfuscated)
        cleanRam.HasRam = True : cleanRam.RamDirty = 0
        Check("disk-state:saved-ram-is-mounted", DiskStates.StateOf(cleanRam, "") = DiskRowState.Mounted, "")

        Dim unsaved = RecordFor(DiskRowState.Unsaved, "ram", DiskProtection.Obfuscated)
        Dim text = DiskStates.StateText(unsaved, DiskRowState.Unsaved, "", ui)
        Check("disk-state:unsaved-says-how-much", text.Contains("180 MiB"), text)
        Dim ro = RecordFor(DiskRowState.Mounted, "plain", DiskProtection.Encrypted)
        ro.ReadOnly = True
        Check("disk-state:read-only-in-words", DiskStates.StateText(ro, DiskRowState.Mounted, "", ui) = ui("vd_mgr_state_mounted_ro"), "")

        ' Principle 1: every state has words, and they differ from one another.
        Dim words As New HashSet(Of String)
        For Each state As DiskRowState In [Enum].GetValues(GetType(DiskRowState))
            Dim w = DiskStates.StateText(RecordFor(state, "plain", DiskProtection.Obfuscated), state, "mount", ui)
            Check("disk-state:words:" & state.ToString(), w <> "" AndAlso Not w.StartsWith("vd_mgr_"), w)
            words.Add(w)
        Next
        Check("disk-state:words-differ", words.Count = [Enum].GetValues(GetType(DiskRowState)).Length, words.Count.ToString())

        ' The glyph of each state: ok, warning, error - and none for a disk at rest or at work.
        Dim glyphOf = Function(st As DiskRowState) If(DiskStates.GlyphOf(st) Is Nothing, "", DiskStates.GlyphOf(st).Id)
        Check("disk-state:glyphs", glyphOf(DiskRowState.Mounted) = "status.ok" AndAlso glyphOf(DiskRowState.Image) = "status.ok" AndAlso
                                   glyphOf(DiskRowState.Unsaved) = "status.warning" AndAlso glyphOf(DiskRowState.Unclean) = "status.warning" AndAlso
                                   glyphOf(DiskRowState.ServerGone) = "status.error" AndAlso glyphOf(DiskRowState.Unreadable) = "status.error" AndAlso
                                   glyphOf(DiskRowState.NotMounted) = "" AndAlso glyphOf(DiskRowState.Busy) = "", "")

        ' Principle 3 in every language: the two protection words differ, and the obfuscated one is
        ' never the word for encrypted.
        For Each lang In Localization.Languages
            Dim d = Localization.GetDict(lang)
            Dim obf = DiskStates.ProtectionText(MgrRecord(), d)
            Dim enc = DiskStates.ProtectionText(MgrVariant(Sub(r) r.Protection = DiskProtection.Encrypted), d)
            Dim unk = DiskStates.ProtectionText(MgrVariant(Sub(r) r.Protection = DiskProtection.Unknown), d)
            Check("disk-state:protection:" & lang, obf <> enc AndAlso Not obf.Contains(enc) AndAlso unk = "-" AndAlso Not obf.StartsWith("vd_mgr_"),
                  obf & " / " & enc)
        Next

        Check("disk-state:sizes", DiskStates.SizeText(20L << 30) = "20 GiB" AndAlso DiskStates.SizeText(180L << 20) = "180 MiB" AndAlso
                                  DiskStates.SizeText(2L << 40) = "2 TiB" AndAlso DiskStates.SizeText(3L << 29) = "1.5 GiB",
              DiskStates.SizeText(3L << 29))

        ' Spec 4.1: mounted first, by letter; then the rest by name.
        Dim rows = New List(Of DiskRecord) From {
            MgrVariant(Sub(r) r.Name = "b"), MgrVariant(Sub(r)
                                                            r.Name = "z" : r.Letter = "X:"
                                                        End Sub),
            MgrVariant(Sub(r) r.Name = "a"), MgrVariant(Sub(r)
                                                            r.Name = "y" : r.Letter = "E:"
                                                        End Sub)}
        rows.Sort(AddressOf DiskStates.DefaultOrder)
        Check("disk-state:default-order", String.Join(",", rows.Select(Function(r) r.Name).ToArray()) = "y,z,a,b",
              String.Join(",", rows.Select(Function(r) r.Name).ToArray()))
    End Sub

    ' Section 6.1 over every (state x profile x protection x packaged x transport) combination: each
    ' rule the console holds, held here as an invariant with its violations named.
    Private Sub CheckDiskMatrix()
        Dim ui = Localization.GetDict("en")
        Dim profiles = New String() {"plain", "fast", "ram", "sealed", "vault"}
        Dim protections = New DiskProtection() {DiskProtection.Obfuscated, DiskProtection.Encrypted, DiskProtection.Unknown}
        Dim transports = New String() {"", "initiator_missing", "initiator_disabled", "service_manager"}
        Dim rules As New Dictionary(Of String, List(Of String))
        For Each name In New String() {"mount", "unmount", "open", "save", "never-while-mounted", "auto-never-encrypted", "packaged-refuses",
                                       "packaged-read-path", "busy-accepts-nothing", "reason-has-words", "image-only-its-drive",
                                       "no-password-for-obfuscated", "default-action-safe"}
            rules(name) = New List(Of String)
        Next
        Dim combos = 0
        Dim fail = Sub(rule As String, what As String)
                       If rules(rule).Count < 6 Then rules(rule).Add(what)
                       If rules(rule).Count = 6 Then rules(rule).Add("..")
                   End Sub
        Dim mountedStates = {DiskRowState.ServerGone, DiskRowState.Unsaved, DiskRowState.Mounted}
        For Each state As DiskRowState In [Enum].GetValues(GetType(DiskRowState))
            For Each profile In profiles
                For Each protection In protections
                    For Each packaged In New Boolean() {False, True}
                        For Each reason In transports
                            combos += 1
                            Dim r = RecordFor(state, profile, protection)
                            If state = DiskRowState.Unsaved AndAlso profile <> "ram" Then r.Profile = "ram"
                            Dim ctx As New DiskContext With {.Packaged = packaged, .TransportReady = (reason = "" OrElse reason = "service_manager"),
                                                             .TransportReason = reason}
                            If reason = "service_manager" Then ctx.TransportReady = False
                            Dim tag = state.ToString() & "/" & r.Profile & "/" & protection.ToString() & "/" & If(packaged, "pkg", "setup") & "/" & reason
                            Dim ok = Function(a As DiskAction) DiskStates.WhyNot(a, r, state, ctx) = ""
                            Dim transportRefuses = (reason = "initiator_missing" OrElse reason = "initiator_disabled")

                            Dim wantMount = (state = DiskRowState.NotMounted OrElse state = DiskRowState.Unclean) AndAlso Not packaged AndAlso Not transportRefuses
                            If ok(DiskAction.Mount) <> wantMount Then fail("mount", tag)
                            Dim wantUnmount = mountedStates.Contains(state) AndAlso Not packaged
                            If ok(DiskAction.Unmount) <> wantUnmount Then fail("unmount", tag)
                            Dim wantOpen = (state = DiskRowState.Unsaved OrElse state = DiskRowState.Mounted OrElse state = DiskRowState.Image)
                            If ok(DiskAction.OpenDrive) <> wantOpen Then fail("open", tag)
                            Dim wantSave = (state = DiskRowState.Unsaved OrElse state = DiskRowState.Mounted) AndAlso r.IsRam AndAlso Not r.ReadOnly AndAlso Not packaged
                            If ok(DiskAction.SaveNow) <> wantSave Then fail("save", tag)
                            If mountedStates.Contains(state) OrElse state = DiskRowState.Busy Then
                                For Each a In {DiskAction.Format, DiskAction.Destroy, DiskAction.Compact, DiskAction.Grow, DiskAction.ChangePassword, DiskAction.Verify}
                                    If ok(a) Then fail("never-while-mounted", tag & ":" & a.ToString())
                                Next
                            End If
                            If protection = DiskProtection.Encrypted AndAlso ok(DiskAction.AutoOn) Then fail("auto-never-encrypted", tag)
                            If packaged Then
                                For Each a In {DiskAction.Mount, DiskAction.MountReadOnly, DiskAction.MountAs, DiskAction.Unmount, DiskAction.SaveNow,
                                               DiskAction.Format, DiskAction.AutoOn, DiskAction.AutoOff, DiskAction.UnmountImage}
                                    If ok(a) Then fail("packaged-refuses", tag & ":" & a.ToString())
                                Next
                                If state = DiskRowState.NotMounted AndAlso Not (ok(DiskAction.Info) AndAlso ok(DiskAction.Verify) AndAlso ok(DiskAction.Export)) Then
                                    fail("packaged-read-path", tag)
                                End If
                            End If
                            For Each a As DiskAction In [Enum].GetValues(GetType(DiskAction))
                                Dim why = DiskStates.WhyNot(a, r, state, ctx)
                                If why <> "" AndAlso (Not ui.ContainsKey(why) OrElse ui(why) = "") Then fail("reason-has-words", tag & ":" & why)
                                If state = DiskRowState.Busy AndAlso why = "" AndAlso
                                   Not (a = DiskAction.CopyPath OrElse a = DiskAction.ShowInFolder OrElse DiskStates.IsGlobal(a)) Then
                                    fail("busy-accepts-nothing", tag & ":" & a.ToString())
                                End If
                                If state = DiskRowState.Image AndAlso why = "" AndAlso
                                   Not (a = DiskAction.UnmountImage OrElse a = DiskAction.OpenDrive OrElse a = DiskAction.CopyPath OrElse
                                        a = DiskAction.ShowInFolder OrElse DiskStates.IsGlobal(a)) Then
                                    fail("image-only-its-drive", tag & ":" & a.ToString())
                                End If
                                If protection <> DiskProtection.Encrypted AndAlso DiskStates.AsksPassword(a, r) Then fail("no-password-for-obfuscated", tag & ":" & a.ToString())
                            Next
                            ' D3: a double-click mounts a disk at rest or opens a mounted one - nothing else, ever.
                            Dim d = DiskStates.DefaultAction(state)
                            If d.HasValue AndAlso Not (d.Value = DiskAction.Mount AndAlso Not mountedStates.Contains(state) AndAlso state <> DiskRowState.Image OrElse
                                                       d.Value = DiskAction.OpenDrive) Then
                                fail("default-action-safe", tag & ":" & d.Value.ToString())
                            End If
                        Next
                    Next
                Next
            Next
        Next
        For Each kv In rules
            Check("disk-matrix:" & kv.Key, kv.Value.Count = 0, combos.ToString() & " combinations" &
                  If(kv.Value.Count = 0, "", "; " & String.Join("; ", kv.Value.ToArray())))
        Next
        ' Spec 6.2 and section 9: Del is Remove from list, which touches no file - never Destroy.
        Check("disk-matrix:del-is-not-destructive", DiskStates.KindOf(DiskAction.Forget) <> DiskActionKind.Destructive AndAlso
                                                     DiskStates.KindOf(DiskAction.Destroy) = DiskActionKind.Destructive AndAlso
                                                     DiskStates.KindOf(DiskAction.Format) = DiskActionKind.Destructive, "")
        ' Several rows: an action applies when it applies to every one of them.
        Dim a1 = MgrRecord()
        Dim a2 = RecordFor(DiskRowState.Mounted, "plain", DiskProtection.Obfuscated)
        Dim why2 = DiskStates.WhyNotAll(DiskAction.Mount, New List(Of DiskRecord) From {a1, a2},
                                        New List(Of DiskRowState) From {DiskRowState.NotMounted, DiskRowState.Mounted}, New DiskContext())
        Check("disk-matrix:many-mixed", why2 = "vd_mgr_why_mixed", why2)
        why2 = DiskStates.WhyNotAll(DiskAction.MountAs, New List(Of DiskRecord) From {a1, a1},
                                    New List(Of DiskRowState) From {DiskRowState.NotMounted, DiskRowState.NotMounted}, New DiskContext())
        Check("disk-matrix:many-one-at-a-time", why2 = "vd_mgr_why_one_at_a_time", why2)
        why2 = DiskStates.WhyNotAll(DiskAction.NewDisk, New List(Of DiskRecord) From {a2, a2},
                                    New List(Of DiskRowState) From {DiskRowState.Busy, DiskRowState.Busy}, New DiskContext())
        Check("disk-matrix:global-ignores-selection", why2 = "", why2)
        ' Every delegated action opens a job the catalogue has (spec 7.3).
        For Each a As DiskAction In [Enum].GetValues(GetType(DiskAction))
            Dim key = DiskStates.JobKeyOf(a)
            If key = "" Then Continue For
            Check("disk-matrix:delegates:" & a.ToString(), JobCatalogue.GetJob(key) IsNot Nothing, key)
        Next
    End Sub

    ' Principle 4 and spec 7.1: every quick action's line is DiskCommands.Build's, a password travels
    ' by variable name only, and the ones that need consent are the ones that run one at a time.
    Private Sub CheckDiskQuick()
        Dim Q = DiskSampleQuoted
        Dim obf = MgrRecord()
        Dim enc = MgrVariant(Sub(r) r.Protection = DiskProtection.Encrypted)
        Dim mounted = MgrVariant(Sub(r)
                                     r.Letter = "W:" : r.ServerAlive = True
                                 End Sub)
        Dim ram = MgrVariant(Sub(r)
                                 r.Profile = "ram" : r.Letter = "R:" : r.ServerAlive = True : r.HasRam = True : r.RamDirty = 5
                             End Sub)
        Dim image = New DiskRecord With {.Kind = "image", .Path = "C:\i\a disc.iso", .Letter = "I:"}
        Dim withCred = New DiskOptions With {.HasCredential = True}
        Dim cases = New Object()() {
            New Object() {"mount", DiskAction.Mount, obf, New DiskOptions(), Q & " mount"},
            New Object() {"mount-cred-ignored-obfuscated", DiskAction.Mount, obf, New DiskOptions With {.HasCredential = True}, Q & " mount"},
            New Object() {"mount-encrypted", DiskAction.Mount, enc, withCred, Q & " mount pe:FILEDO_SHELL_CRED"},
            New Object() {"mount-ro", DiskAction.MountReadOnly, obf, New DiskOptions(), Q & " mount ro"},
            New Object() {"mount-as", DiskAction.MountAs, obf, New DiskOptions With {.Letter = "X:", .ReadOnly = True, .NoScan = True}, Q & " mount ro noscan as X:"},
            New Object() {"unmount", DiskAction.Unmount, mounted, New DiskOptions(), "W: unmount"},
            New Object() {"unmount-image", DiskAction.UnmountImage, image, New DiskOptions(), "I: unmount"},
            New Object() {"save", DiskAction.SaveNow, ram, New DiskOptions(), "R: save"},
            New Object() {"info", DiskAction.Info, enc, withCred, Q & " info"},
            New Object() {"verify", DiskAction.Verify, enc, withCred, Q & " verify pe:FILEDO_SHELL_CRED"},
            New Object() {"auto-on", DiskAction.AutoOn, obf, New DiskOptions(), "vd auto " & Q & " logon"},
            New Object() {"auto-off", DiskAction.AutoOff, obf, New DiskOptions(), "vd auto off " & Q},
            New Object() {"add", DiskAction.AddToList, MgrVariant(Sub(r) r.Registered = False), New DiskOptions With {.Name = "work2"}, "vd add " & Q & " as work2"},
            New Object() {"forget", DiskAction.Forget, obf, New DiskOptions(), "vd forget work"},
            New Object() {"mount-image", DiskAction.MountImage, image, withCred, """C:\i\a disc.iso"" mount"}}
        For Each c In cases
            Dim args = DiskStates.QuickCommand(DirectCast(c(1), DiskAction), DirectCast(c(2), DiskRecord), DirectCast(c(3), DiskOptions))
            Dim line = If(args Is Nothing, "(none)", ArgQuoting.JoinArgs(args))
            Check("disk-quick:" & DirectCast(c(0), String), line = DirectCast(c(4), String), line & " (want " & DirectCast(c(4), String) & ")")
            ' The line the run report keeps is the line itself: nothing in it is a password.
            If args IsNot Nothing Then
                Dim redacted = ArgQuoting.JoinArgs(Runner.RedactCredentialArgs(args))
                Check("disk-quick:" & DirectCast(c(0), String) & ":redaction-keeps-it", redacted = line, redacted)
            End If
        Next
        For Each a As DiskAction In [Enum].GetValues(GetType(DiskAction))
            If DiskStates.Elevates(a) AndAlso Not DiskStates.Serial(a) Then Check("disk-quick:elevating-is-serial:" & a.ToString(), False, "")
        Next
        Check("disk-quick:reads-run-beside", Not DiskStates.Serial(DiskAction.Info) AndAlso Not DiskStates.Serial(DiskAction.Verify) AndAlso
                                             DiskStates.Serial(DiskAction.Mount) AndAlso DiskStates.Elevates(DiskAction.Unmount) AndAlso
                                             Not DiskStates.Elevates(DiskAction.Verify), "")
        Check("disk-quick:password-only-encrypted", DiskStates.AsksPassword(DiskAction.Mount, enc) AndAlso Not DiskStates.AsksPassword(DiskAction.Mount, obf) AndAlso
                                                   Not DiskStates.AsksPassword(DiskAction.Unmount, enc) AndAlso Not DiskStates.AsksPassword(DiskAction.Info, enc), "")
        ' The snapshot's own command line, and nothing in it the redaction would take.
        Dim probe = ArgQuoting.JoinArgs(DiskStateProbe.Arguments)
        Check("disk-quick:snapshot-line", probe = "--no-history vd status json" AndAlso
                                          ArgQuoting.JoinArgs(Runner.RedactCredentialArgs(DiskStateProbe.Arguments)) = probe, probe)
    End Sub

    ' The window itself, built and fed snapshots but never shown: the rows in their order and words,
    ' the toolbar and the detail pane saying why, a failed read that keeps the last good state, a busy
    ' row, the packaged build, and every control named.
    Private Sub CheckDiskManagerWindow()
        Dim ui = Localization.GetDict(ShellSettings.Language())
        Dim m As DiskManagerForm = Nothing
        Try
            m = New DiskManagerForm()
            ' Never read: the window says the state is being read, never "no disks".
            Check("disk-mgr:before-first-read", Not m.ListShownForTest AndAlso m.EmptyTextForTest = ui("vd_mgr_reading"), m.EmptyTextForTest)
            m.ApplySnapshotForTest(Nothing, "vd_mgr_stale_timeout")
            Check("disk-mgr:first-read-failed", Not m.ListShownForTest AndAlso m.EmptyTextForTest.Contains(ui("vd_mgr_stale_timeout")) AndAlso
                                                 m.EmptyTextForTest <> ui("vd_mgr_empty"), m.EmptyTextForTest)

            Dim snap = GoldenSnapshot()
            m.ApplySnapshotForTest(snap, "")
            Dim rows = m.RowTextsForTest
            Check("disk-mgr:rows", rows.Count = 10 AndAlso m.ListShownForTest, rows.Count.ToString())
            If rows.Count <> 10 Then Return
            Dim order = String.Join(",", rows.Select(Function(r) If(r(1) = "", r(0), r(1))).ToArray())
            Check("disk-mgr:order", order = "I:,R:,S:,U:,W:,X:,archive,backup,junk,moved", order)
            Dim rowOf = Function(name As String) rows.First(Function(r) r(0) = name)
            Check("disk-mgr:state-words", rowOf("scratch")(2).Contains("180 MiB") AndAlso rowOf("old")(2) = ui("vd_mgr_state_server_gone") AndAlso
                                          rowOf("secrets")(2) = ui("vd_mgr_state_mounted_ro") AndAlso rowOf("backup")(2) = ui("vd_mgr_state_missing") AndAlso
                                          rowOf("moved")(2) = ui("vd_mgr_state_different") AndAlso rowOf("archive")(2) = ui("vd_mgr_state_not_mounted"),
                  rowOf("scratch")(2))
            Check("disk-mgr:protection-words", rowOf("secrets")(4) = ui("vd_mgr_prot_encrypted") AndAlso rowOf("work")(4) = ui("vd_mgr_prot_obfuscated") AndAlso
                                               rowOf("backup")(4) = "-" AndAlso rows(0)(4) = "-", rowOf("work")(4))
            Check("disk-mgr:image-name", rows(0)(0) = ui("vd_mgr_name_image") AndAlso rows(0)(3) = "iso", rows(0)(0))
            Check("disk-mgr:auto-column", rowOf("archive")(6) = ui("vd_mgr_auto_yes") AndAlso rowOf("work")(6) = "", rowOf("archive")(6))

            Dim key = Function(name As String) snap.Disks.First(Function(d) d.Name = name).Key
            Dim reason As String = ""

            m.SelectForTest(key("secrets"))
            Check("disk-mgr:mounted-no-mount", Not m.ToolbarStateForTest("mount", reason) AndAlso reason.Contains(ui("vd_mgr_why_already_mounted")), reason)
            Check("disk-mgr:mounted-unmount", m.ToolbarStateForTest("unmount", reason), reason)
            Check("disk-mgr:not-ram-no-save", Not m.ToolbarStateForTest("save", reason) AndAlso reason.Contains(ui("vd_mgr_why_not_ram")), reason)
            Dim detail = m.DetailForTest
            Check("disk-mgr:detail-encrypted", detail.Contains(ui("vd_facts_encrypted")) AndAlso detail.Contains(ui("vd_mgr_detail_open_while_mounted")) AndAlso
                                               Not detail.Contains(ui("vd_facts_obfuscated")), detail)
            Check("disk-mgr:detail-says-why", detail.Contains(ui("vd_mgr_why_not_ram")), detail)
            Dim menu = m.MenuForTest(True)
            Check("disk-mgr:menu-destructive-last", menu.Count > 2 AndAlso menu(menu.Count - 1).StartsWith(ui("vd_mgr_act_destroy")) AndAlso
                                                    menu(menu.Count - 2).StartsWith(ui("vd_mgr_act_format")) AndAlso menu(menu.Count - 3) = "-",
                  String.Join(" | ", menu.ToArray()))
            Check("disk-mgr:menu-no-format-while-mounted", menu.Any(Function(i) i.StartsWith(ui("vd_mgr_act_format") & " [") AndAlso i.Contains(ui("vd_mgr_why_mounted"))),
                  String.Join(" | ", menu.ToArray()))

            m.SelectForTest(key("archive"))
            Check("disk-mgr:at-rest-mounts", m.ToolbarStateForTest("mount", reason), reason)
            Check("disk-mgr:at-rest-buttons", m.DetailButtonsForTest.Contains(ui("vd_mgr_act_mount")) AndAlso
                                              m.DetailButtonsForTest.Contains(ui("vd_mgr_act_auto_off")), String.Join(",", m.DetailButtonsForTest.ToArray()))
            Check("disk-mgr:detail-obfuscated", m.DetailForTest.Contains(ui("vd_facts_obfuscated")) AndAlso m.DetailForTest.Contains(ui("vd_mgr_detail_clean_yes")),
                  m.DetailForTest)

            m.SelectForTest(key("old"))
            Check("disk-mgr:server-gone-no-open", Not m.ToolbarStateForTest("open", reason) AndAlso m.ToolbarStateForTest("unmount", reason), reason)
            m.SelectForTest(key("scratch"))
            Check("disk-mgr:ram-saves", m.ToolbarStateForTest("save", reason), reason)

            ' Several rows: what applies to all of them, and a count.
            m.SelectForTest(key("work"), key("secrets"))
            Check("disk-mgr:many", m.DetailForTest.StartsWith(Localization.Format(ui("vd_mgr_detail_many_fmt"), 2)) AndAlso m.ToolbarStateForTest("unmount", reason),
                  m.DetailForTest)

            ' A busy row accepts nothing else and says what it is doing.
            m.SetBusyForTest(key("work"), "unmount")
            m.SelectForTest(key("work"))
            Check("disk-mgr:busy-row", m.RowTextsForTest.First(Function(r) r(0) = "work")(2) = ui("vd_mgr_state_busy_unmount") AndAlso
                                       Not m.ToolbarStateForTest("open", reason) AndAlso reason.Contains(ui("vd_mgr_why_busy")), reason)
            m.SetBusyForTest(key("work"), "")

            ' A second Close after Stop and close must still wait for the child to finish.
            Check("disk-mgr:close-pending-stays-open", DiskManagerForm.DecisionOnClose(True, True, False) = DiskManagerForm.CloseDecision.WaitForRun AndAlso
                  DiskManagerForm.DecisionOnClose(True, False, False) = DiskManagerForm.CloseDecision.Ask AndAlso
                  DiskManagerForm.DecisionOnClose(False, True, False) = DiskManagerForm.CloseDecision.Allow AndAlso
                  DiskManagerForm.DecisionOnClose(True, True, True) = DiskManagerForm.CloseDecision.Allow, "")
            Check("disk-mgr:second-close-cancelled", m.PendingCloseCanceledForTest(), "")

            ' A read that fails keeps the last good state on screen and says so (principle 6).
            m.ApplySnapshotForTest(Nothing, "vd_mgr_stale_timeout")
            Check("disk-mgr:stale-keeps-rows", m.RowTextsForTest.Count = 10 AndAlso m.ListShownForTest, m.RowTextsForTest.Count.ToString())
            Check("disk-mgr:stale-says-so", m.SummaryForTest.Contains(ui("vd_mgr_stale_timeout")), m.SummaryForTest)
            m.ApplySnapshotForTest(snap, "")
            Check("disk-mgr:fresh-again", Not m.SummaryForTest.Contains(ui("vd_mgr_stale_timeout")), m.SummaryForTest)

            ' The Store build keeps the read path and says why it cannot mount.
            Packaging.OverrideForTest = True
            m.SelectForTest(key("archive"))
            Check("disk-mgr:packaged", Not m.ToolbarStateForTest("mount", reason) AndAlso reason.Contains(ui("vd_packaged")), reason)
            Packaging.OverrideForTest = Nothing

            ' Both themes apply without throwing, and every control a user operates has a name.
            For Each dark In New Boolean() {False, True}
                Theme.UsePaletteForTest(dark)
                m.ApplyTheme()
            Next
            Theme.UsePaletteForTest(Nothing)
            Dim problems As New List(Of String)
            WalkAccessible(m, problems)
            Check("a11y:disk-manager", problems.Count = 0, String.Join("; ", problems.Take(8).ToArray()))
            For Each dlg As Form In New Form() {New DiskPasswordDialog(ui, "work", Nothing), New DiskMountAsDialog(ui, "work", Nothing),
                                                New DiskNameDialog(ui, DiskSample, "work", New HashSet(Of String)(StringComparer.OrdinalIgnoreCase), Nothing)}
                Using dlg
                    Dim dp As New List(Of String)
                    WalkAccessible(dlg, dp)
                    Check("a11y:" & dlg.GetType().Name, dp.Count = 0 AndAlso dlg.CancelButton IsNot Nothing, String.Join("; ", dp.Take(8).ToArray()))
                End Using
            Next
        Catch ex As Exception
            Check("disk-mgr", False, ex.GetType().Name & ": " & ex.Message)
        Finally
            Packaging.OverrideForTest = Nothing
            Theme.UsePaletteForTest(Nothing)
            If m IsNot Nothing Then m.Dispose()
        End Try
    End Sub

    ' ---- SP-0063: icons, keys, help and first steps --------------------------------

    ' The meanings the Disk manager shows, the keyboard map, the two help windows, the first run, the
    ' build that cannot mount, and the painted controls - the things a person meets before their first
    ' disk. The contracts they answer to: ICON-SET rules 1 and 5 (one meaning, one glyph, from the
    ' vocabulary), ICON-RENDER rule 8 (a glyph-only control carries the canonical name), APP-BEHAVIOUR
    ' rules 1, 2, 9 and 11 (one leave path, text that grows, names, first run and hidden controls) and
    ' APP-STYLE (every colour a palette role, in both themes).
    Private Sub CheckDiskUi()
        Dim ui = Localization.GetDict(ShellSettings.Language())
        CheckDiskGlyphMap()
        CheckDiskShortcuts(ui)
        CheckDiskRowColours()
        CheckDiskButtonInk()
        CheckDiskHelpWindows()
        CheckDiskUiWindow(ui)
    End Sub

    ' ICON-SET rules 1 and 5: every action has a picture, from the vocabulary or - where the vocabulary
    ' has no record yet - the proposed id with its stand-in; one picture is one meaning; and the rail's
    ' Disks rows and the window's buttons read the same table, so a meaning cannot look different in two
    ' places.
    Private Sub CheckDiskGlyphMap()
        Dim pictures As New Dictionary(Of String, String)(StringComparer.Ordinal)
        For Each a As DiskAction In [Enum].GetValues(GetType(DiskAction))
            Dim g = DiskGlyphs.For(a)
            Dim ok = g IsNot Nothing AndAlso If(g.IsVocabulary, Glyphs.IsDrawable(g.Id), g.Pending.Contains(".") AndAlso g.FontName <> "")
            Check("disk-ui:glyph:" & a.ToString(), ok, If(g Is Nothing, "no glyph", g.ToString()))
            If g Is Nothing Then Continue For
            Dim picture = If(g.IsVocabulary, g.Id, "U+" & (AscW(g.Interim) And &HFFFF).ToString("X4"))
            Dim other As String = Nothing
            If pictures.TryGetValue(picture, other) Then
                Check("disk-ui:glyph-one-meaning:" & a.ToString(), other = g.Meaning, picture & " already shows " & other)
            Else
                pictures(picture) = g.Meaning
            End If
        Next
        Dim shared_ As New Dictionary(Of String, GlyphRef) From {
            {"rail_job_vd_new", DiskGlyphs.CreateDisk}, {"rail_job_vd_mount", DiskGlyphs.MountDisk},
            {"rail_job_vd_unmount", DiskGlyphs.UnmountDisk}, {"rail_job_vd_compact", DiskGlyphs.CompactDisk},
            {"rail_job_vd_grow", DiskGlyphs.GrowDisk}, {"rail_job_vd_format", DiskGlyphs.FormatDisk},
            {"rail_job_vd_seal", DiskGlyphs.SealDisk}, {"rail_job_vd_pass", DiskGlyphs.ChangePassword},
            {"rail_job_vd_auto", DiskGlyphs.AutoMount}, {"rail_job_vd_add", DiskGlyphs.RememberName}}
        For Each kv In shared_
            Dim row = RailRow.All.FirstOrDefault(Function(r) r.Key = kv.Key)
            Check("disk-ui:rail-shares:" & kv.Key, row IsNot Nothing AndAlso row.Glyph Is kv.Value, "")
        Next
        ' The window's own controls draw vocabulary meanings, each named as the catalog names it.
        For Each g In New GlyphRef() {DiskGlyphs.Help, DiskGlyphs.More, DiskGlyphs.CloseIt, DiskGlyphs.ClearInput,
                                      DiskGlyphs.OpenExternal, DiskGlyphs.ShowDetails, DiskGlyphs.StopRun, DiskGlyphs.ClearQueue}
            Check("disk-ui:glyph-window:" & g.Id, g.IsVocabulary AndAlso Glyphs.IsDrawable(g.Id), g.ToString())
        Next
        Check("disk-ui:glyph-cross-is-shared-declared", DiskGlyphs.CloseIt.Id = "nav.close" AndAlso DiskGlyphs.ClearInput.Id = "action.clear-input",
              "the catalog declares these two one drawing (sharedWith)")
    End Sub

    ' The keyboard map is data (DiskShortcuts.All): every chord found, unique, labelled in every table,
    ' never bound to Destroy or Format, and the spec's own list (6.2) present.
    Private Sub CheckDiskShortcuts(ui As Dictionary(Of String, String))
        Dim seen As New HashSet(Of Keys)()
        For Each s In DiskShortcuts.All
            Dim name = DiskShortcuts.KeyText(s.Chord)
            Check("disk-ui:key:unique:" & name, seen.Add(s.Chord), name)
            Check("disk-ui:key:found:" & name, DiskShortcuts.Find(s.Chord, True) Is s, name)
            Check("disk-ui:key:list-only:" & name, Not s.ListOnly OrElse DiskShortcuts.Find(s.Chord, False) Is Nothing, name)
            Check("disk-ui:key:label:" & name, ui.ContainsKey(s.LabelKey), s.LabelKey)
            For Each lang In Localization.Languages
                Check("disk-ui:key:label:" & name & ":" & lang, Localization.OwnKeysForTest(lang).Contains(s.LabelKey), s.LabelKey)
            Next
        Next
        Check("disk-ui:key:never-destroy",
              DiskShortcuts.All.All(Function(s) Not (s.Command = DiskCommand.Perform AndAlso s.Action.HasValue AndAlso
                                                     (s.Action.Value = DiskAction.Destroy OrElse s.Action.Value = DiskAction.Format))), "")
        Check("disk-ui:key:text", DiskShortcuts.KeyText(Keys.Control Or Keys.Shift Or Keys.M) = "Ctrl+Shift+M" AndAlso
                                  DiskShortcuts.KeyText(Keys.Delete) = "Del" AndAlso DiskShortcuts.KeyText(Keys.F1) = "F1" AndAlso
                                  DiskShortcuts.KeyText(Keys.Escape) = "Esc" AndAlso DiskShortcuts.KeyText(Keys.Return) = "Enter", "")
        For Each want In New String() {"Ctrl+M", "Ctrl+U", "Ctrl+E", "Ctrl+S", "Ctrl+N", "Del", "F5", "Ctrl+F", "Esc", "Enter", "F1", "Ctrl+O"}
            Check("disk-ui:key:spec:" & want, DiskShortcuts.All.Any(Function(s) DiskShortcuts.KeyText(s.Chord) = want), want)
        Next
        Dim labels = DiskShortcuts.All.Select(Function(s) s.LabelKey).Distinct().Count()
        Check("disk-ui:key:help-rows", DiskShortcuts.HelpRows().Count = labels, DiskShortcuts.HelpRows().Count.ToString() & " of " & labels.ToString())
        Check("disk-ui:key:suffix", DiskShortcuts.Suffix(DiskAction.Mount) = " (Ctrl+M)" AndAlso DiskShortcuts.Suffix(DiskAction.UnmountImage) = " (Ctrl+U)" AndAlso
                                    DiskShortcuts.Suffix(DiskAction.Compact) = "", "")
        Check("disk-ui:welcome-once", DiskManagerForm.NeedsWelcome(False, False) AndAlso Not DiskManagerForm.NeedsWelcome(True, False) AndAlso
                                      Not DiskManagerForm.NeedsWelcome(False, True), "")
    End Sub

    ' APP-STYLE: a row's text is the palette's Text on the selection role, the hover tint and the
    ' surface - 4.5:1 in both themes - and the state glyphs hold 3:1 on all three (ICON-RENDER rule 3).
    ' The teal selection with dark text the owner rejected on 2026-09-30 fails the first of these.
    Private Sub CheckDiskRowColours()
        For Each dark In New Boolean() {False, True}
            Dim p = Theme.PaletteFor(dark)
            Dim mode = If(dark, "dark", "light")
            For Each state In New Boolean()() {New Boolean() {False, False}, New Boolean() {False, True}, New Boolean() {True, False}}
                Dim back = DiskManagerForm.RowBackColour(p, state(0), state(1))
                Dim what = If(state(0), "selected", If(state(1), "hover", "plain"))
                Dim text = Theme.ContrastRatio(p.Text, back)
                Check("disk-ui:row-text:" & mode & ":" & what, text >= 4.5, text.ToString("0.00") & ":1")
                ' The tone each state's glyph is really drawn in on this background (DiskStates.ToneOn):
                ' the shared tone, or the palette's warning ink where the shared warning tone is under 3:1.
                For Each hue In New Object()() {New Object() {"ok", DiskRowState.Mounted}, New Object() {"warning", DiskRowState.Unsaved}, New Object() {"error", DiskRowState.ServerGone}}
                    Dim tone = DiskStates.ToneOn(DirectCast(hue(1), DiskRowState), p, back)
                    Dim c = Theme.ContrastRatio(tone, back)
                    Check("disk-ui:row-glyph:" & mode & ":" & what & ":" & CStr(hue(0)), c >= 3.0, c.ToString("0.00") & ":1")
                Next
            Next
            ' On the plain card the warning glyph keeps the shared tone; only a tint takes the ink.
            Check("disk-ui:row-glyph-shared-tone:" & mode,
                  DiskStates.ToneOn(DiskRowState.Unsaved, p, p.Surface).ToArgb() = p.StateWarning.ToArgb(), mode)
            Check("disk-ui:row-selection-role", DiskManagerForm.RowBackColour(p, True, True).ToArgb() = p.SurfaceSelected.ToArgb() AndAlso
                                                DiskManagerForm.RowBackColour(p, False, True).ToArgb() = p.ControlHover.ToArgb() AndAlso
                                                DiskManagerForm.RowBackColour(p, False, False).ToArgb() = p.Surface.ToArgb(), mode)
        Next
    End Sub

    ' A glyph button paints itself: its text and glyph are the palette's Text when it applies and the
    ' disabled text when it does not - readable on the dark theme, where WinForms' own disabled caption
    ' was near-black - and a danger button is the danger colour.
    Private Sub CheckDiskButtonInk()
        For Each dark In New Boolean() {False, True}
            Theme.UsePaletteForTest(dark)
            Try
                Dim p = Theme.Current
                Dim mode = If(dark, "dark", "light")
                For Each enabled In New Boolean() {True, False}
                    Using b As New GlyphButton With {.Glyph = DiskGlyphs.For(DiskAction.Verify), .Text = "Verify", .Enabled = enabled}
                        Dim ink = ButtonInk(b, p, {p.Text, p.TextDisabled})
                        Dim key = "disk-ui:button-ink:" & mode & ":" & If(enabled, "enabled", "disabled")
                        If enabled Then
                            Check(key, ink(0) > 40 AndAlso ink(1) * 10 < ink(0), "text-colour px " & ink(0).ToString() & ", disabled-colour px " & ink(1).ToString())
                        Else
                            Check(key, ink(1) > 40 AndAlso ink(0) * 10 < ink(1), "text-colour px " & ink(0).ToString() & ", disabled-colour px " & ink(1).ToString())
                        End If
                    End Using
                Next
                Using b As New GlyphButton With {.Glyph = DiskGlyphs.For(DiskAction.Destroy), .Text = "Destroy", .Danger = True}
                    Dim ink = ButtonInk(b, p, {p.Danger, p.Text})
                    Check("disk-ui:button-ink:" & mode & ":danger", ink(0) > 40 AndAlso ink(1) * 10 < ink(0), ink(0).ToString() & " / " & ink(1).ToString())
                End Using
                Using b As New GlyphButton With {.Glyph = DiskGlyphs.Help, .IconOnly = True, .Text = ""}
                    b.AccessibleName = "Help"
                    Dim bounds = b.GetPreferredSize(Size.Empty)
                    Check("disk-ui:button-icon-only-square:" & mode, bounds.Width = bounds.Height AndAlso bounds.Width >= Ui.Px(b, 24), bounds.ToString())
                End Using
            Finally
                Theme.UsePaletteForTest(Nothing)
            End Try
        Next
    End Sub

    ' Counts, on a painted button, the pixels within a small tolerance of each of the given colours.
    Private Function ButtonInk(b As GlyphButton, p As Theme.Palette, colours As Color()) As Integer()
        b.Font = Theme.FontBody()
        Dim bounds = b.GetPreferredSize(Size.Empty)
        b.Size = bounds
        Dim handle = b.Handle
        Dim counts(colours.Length - 1) As Integer
        Using bmp As New Bitmap(bounds.Width, bounds.Height)
            b.DrawToBitmap(bmp, New Rectangle(Point.Empty, bounds))
            For y = 0 To bounds.Height - 1
                For x = 0 To bounds.Width - 1
                    Dim c = bmp.GetPixel(x, y)
                    For i = 0 To colours.Length - 1
                        If Math.Abs(CInt(c.R) - colours(i).R) <= 6 AndAlso Math.Abs(CInt(c.G) - colours(i).G) <= 6 AndAlso
                           Math.Abs(CInt(c.B) - colours(i).B) <= 6 Then counts(i) += 1
                    Next
                Next
            Next
        End Using
        Return counts
    End Function

    ' The two help windows in every language: they build, nothing is a raw key, the leave button is
    ' the accept and the cancel one, they fit the screen, every control has a name, and the first
    ' steps offer the three ways to start and one to do nothing (APP-BEHAVIOUR rules 1, 2, 9, 11).
    Private Sub CheckDiskHelpWindows()
        For Each kv In DiskHelpLinks.All
            Check("disk-ui:link:" & kv.Key, kv.Value.StartsWith("https://", StringComparison.Ordinal) AndAlso Localization.GetDict("en").ContainsKey(kv.Key), kv.Value)
        Next
        Check("disk-ui:link:guide-page", Links.DiskGuide.StartsWith(Links.Site, StringComparison.Ordinal) AndAlso Links.DiskGuide.EndsWith("/virtual-disks.html", StringComparison.Ordinal) AndAlso
                                         Links.Guides.StartsWith(Links.Site, StringComparison.Ordinal), Links.DiskGuide)
        Check("disk-ui:link:share-guide", Links.ShareGuide.StartsWith(Links.Site, StringComparison.Ordinal) AndAlso Links.ShareGuide.EndsWith("/fms-sharing.html", StringComparison.Ordinal), Links.ShareGuide)
        For Each lang In Localization.Languages
            Dim d = Localization.GetDict(lang)
            For Each packaged In New Boolean() {False, True}
                Dim tag = lang & If(packaged, ":store", "")
                Using dlg As New DiskHelpDialog(d, Nothing, packaged)
                    Dim texts = DialogTexts(dlg)
                    Dim raw = texts.Where(Function(t) t.StartsWith("vd_", StringComparison.Ordinal) OrElse t.StartsWith("shell_", StringComparison.Ordinal)).Take(5).ToList()
                    Check("disk-ui:help:no-raw-keys:" & tag, raw.Count = 0, String.Join(",", raw.ToArray()))
                    Check("disk-ui:help:legend:" & tag, dlg.RowCount = DiskHelpDialog.Legend.Length, dlg.RowCount.ToString())
                    Check("disk-ui:help:keys-listed:" & tag, texts.Contains("Ctrl+M") AndAlso texts.Contains("F1") AndAlso texts.Contains("Ctrl+Shift+M"), "")
                    Check("disk-ui:help:leave-is-one-path:" & tag, dlg.CancelButton IsNot Nothing AndAlso dlg.AcceptButton Is dlg.CancelButton, "")
                    Check("disk-ui:help:fits-screen:" & tag, dlg.ClientSize.Height <= Screen.PrimaryScreen.WorkingArea.Height AndAlso dlg.ClientSize.Width > 300,
                          dlg.ClientSize.ToString())
                    Check("disk-ui:help:store-line:" & tag, texts.Contains(Localization.Multiline(d("vd_welcome_packaged"))) = packaged, "")
                    Dim problems As New List(Of String)
                    WalkAccessible(dlg, problems)
                    Check("a11y:DiskHelpDialog:" & tag, problems.Count = 0, String.Join("; ", problems.Take(6).ToArray()))
                End Using
                Using dlg As New DiskWelcomeDialog(d, Nothing, packaged)
                    Dim texts = DialogTexts(dlg)
                    Dim raw = texts.Where(Function(t) t.StartsWith("vd_", StringComparison.Ordinal) OrElse t.StartsWith("shell_", StringComparison.Ordinal)).Take(5).ToList()
                    Check("disk-ui:welcome:no-raw-keys:" & tag, raw.Count = 0, String.Join(",", raw.ToArray()))
                    Check("disk-ui:welcome:three-ways-and-not-now:" & tag, dlg.AnswerCount = 3 AndAlso dlg.CancelButton IsNot Nothing AndAlso dlg.AcceptButton Is dlg.CancelButton AndAlso
                                                                           texts.Contains(Localization.Multiline(d("vd_welcome_later"))) AndAlso dlg.Answer = DiskWelcomeAnswer.NotNow, "")
                    Check("disk-ui:welcome:protection-words:" & tag, texts.Any(Function(t) t.Contains(Localization.Multiline(d("vd_facts_obfuscated")))) AndAlso
                                                                     texts.Any(Function(t) t.Contains(Localization.Multiline(d("vd_facts_encrypted")))), "")
                    Check("disk-ui:welcome:store-line:" & tag, texts.Contains(Localization.Multiline(d("vd_welcome_packaged"))) = packaged, "")
                    Check("disk-ui:welcome:fits-screen:" & tag, dlg.ClientSize.Height <= Screen.PrimaryScreen.WorkingArea.Height, dlg.ClientSize.ToString())
                    Dim problems As New List(Of String)
                    WalkAccessible(dlg, problems)
                    Check("a11y:DiskWelcomeDialog:" & tag, problems.Count = 0, String.Join("; ", problems.Take(6).ToArray()))
                End Using
            Next
        Next
    End Sub

    ' Every text a help window shows: its labels, links and buttons.
    Private Function DialogTexts(dlg As DiskPageDialog) As List(Of String)
        Dim out As New List(Of String)
        For Each c In dlg.AllChildren(dlg)
            If (TypeOf c Is Label OrElse TypeOf c Is Button) AndAlso Not String.IsNullOrEmpty(c.Text) Then out.Add(c.Text)
        Next
        Return out
    End Function

    ' SP-0080: the Autostart surface. The guard's words as a state matrix, the last-run rendering
    ' with its empty case, the snapshot reader over the golden document, the packaged build's hidden
    ' entry, the guard's one command line - and the dialog itself in five locales, its encrypted row
    ' disabled with the reason beside it.
    Private Sub CheckDiskAutostart()
        ' The guard's state in words. Nothing - a snapshot without the guard field - reads as off.
        Check("disk-auto:word-nothing", DiskAutostart.GuardWordKey(Nothing) = "vd_auto_guard_off", "")
        Check("disk-auto:word-off", DiskAutostart.GuardWordKey(New DiskGuardState()) = "vd_auto_guard_off", "")
        Check("disk-auto:word-running", DiskAutostart.GuardWordKey(New DiskGuardState With {.Installed = True, .Running = True}) = "vd_auto_guard_on_running", "")
        Check("disk-auto:word-stale", DiskAutostart.GuardWordKey(New DiskGuardState With {.Installed = True}) = "vd_auto_guard_on_stale", "")

        ' The golden snapshot the CLI's test holds its output to carries a guard that is on and
        ' running with a last run of two rows; the reader sees it, Nothing where it is absent.
        Dim golden = GoldenSnapshot()
        Dim noGuard As DiskSnapshot = Nothing
        Dim problem As String = ""
        Check("disk-auto:snapshot-guard", golden.Guard IsNot Nothing AndAlso golden.Guard.Installed AndAlso golden.Guard.Running, "")
        Check("disk-auto:snapshot-guard-rows", golden.Guard IsNot Nothing AndAlso golden.Guard.Containers.Count = 2, "")
        noGuard = DiskSnapshot.Parse("{""schema"":""filedo.vd-status"",""version"":1,""at"":""2026-09-30T05:00:00Z"",""disks"":[]}", problem)
        Check("disk-auto:snapshot-without-guard", problem = "" AndAlso noGuard IsNot Nothing AndAlso noGuard.Guard Is Nothing, problem)

        ' The one command line of the guard's switch, built where every line is built.
        Dim cmdOn = ArgQuoting.JoinArgs(DiskCommands.Build("guard", "", New DiskOptions With {.GuardOn = True}).ToArray())
        Dim cmdOff = ArgQuoting.JoinArgs(DiskCommands.Build("guard", "", New DiskOptions With {.GuardOn = False}).ToArray())
        Check("disk-auto:cmd-on", cmdOn = "vd guard on", cmdOn)
        Check("disk-auto:cmd-off", cmdOff = "vd guard off", cmdOff)

        ' The packaged build hides the whole surface - the entry out of every menu, and the why that
        ' says the packaged build cannot.
        For Each packaged In New Boolean() {False, True}
            Dim tag = If(packaged, "store", "setup")
            Check("disk-auto:hidden:" & tag,
                  DiskStates.HiddenInBuild(DiskAction.Autostart, New DiskContext With {.Packaged = packaged}) = packaged, "")
            Dim why = DiskStates.WhyNot(DiskAction.Autostart, MgrRecord(), DiskRowState.NotMounted, New DiskContext With {.Packaged = packaged})
            Check("disk-auto:why:" & tag, why = If(packaged, "vd_packaged", ""), why)
            ' The guard belongs to the account: the entry opens with nothing selected, and a busy row
            ' does not take it away.
            Dim whyNone = DiskStates.WhyNotAll(DiskAction.Autostart, New List(Of DiskRecord), New List(Of DiskRowState),
                                               New DiskContext With {.Packaged = packaged})
            Check("disk-auto:no-selection:" & tag, whyNone = If(packaged, "vd_packaged", ""), whyNone)
            Dim whyBusy = DiskStates.WhyNot(DiskAction.Autostart, MgrRecord(), DiskRowState.Busy, New DiskContext With {.Packaged = packaged})
            Check("disk-auto:busy-row:" & tag, whyBusy = If(packaged, "vd_packaged", ""), whyBusy)
        Next
        Check("disk-auto:global", DiskStates.IsGlobal(DiskAction.Autostart), "")

        ' The last-run rendering in five locales: never ran, ran with nothing mounted, ran clean,
        ' ran out of time - the reason arrives as the console wrote it.
        Dim neverRan As New DiskGuardState With {.Installed = True, .Running = True}
        Dim emptyRun As New DiskGuardState With {.Installed = True, .LastRun = DateTimeOffset.Now, .Ended = "session"}
        Dim cleanRun As New DiskGuardState With {.Installed = True, .LastRun = DateTimeOffset.Now, .Ended = "session"}
        cleanRun.Containers.Add(New DiskGuardRunRow With {.Name = "work", .Action = "unmount", .Outcome = "unmounted"})
        cleanRun.Containers.Add(New DiskGuardRunRow With {.Name = "scratch", .Action = "save", .Outcome = "saved"})
        cleanRun.Containers.Add(New DiskGuardRunRow With {.Name = "scratch", .Action = "unmount", .Outcome = "unmounted"})
        ' A ram disk is two rows and one container: saved, then an unmount that ran out of time, is
        ' none of one closed - never "one of two".
        Dim ramLeftRun As New DiskGuardState With {.Installed = True, .LastRun = DateTimeOffset.Now, .Ended = "session"}
        ramLeftRun.Containers.Add(New DiskGuardRunRow With {.Name = "scratch", .Action = "save", .Outcome = "saved"})
        ramLeftRun.Containers.Add(New DiskGuardRunRow With {.Name = "scratch", .Action = "unmount", .Outcome = "unfinished", .Reason = "did not finish within 10 s"})
        Dim leftoverRun As New DiskGuardState With {.Installed = True, .LastRun = DateTimeOffset.Now, .Ended = "session"}
        leftoverRun.Containers.Add(New DiskGuardRunRow With {.Name = "work", .Action = "unmount", .Outcome = "unmounted"})
        leftoverRun.Containers.Add(New DiskGuardRunRow With {.Name = "big", .Action = "unmount", .Outcome = "unfinished", .Reason = "did not finish within 10 s"})
        For Each lang In Localization.Languages
            Dim d = Localization.GetDict(lang)
            Dim tag = "disk-auto:run:" & lang
            Dim never = DiskAutostart.GuardRunLines(neverRan, d)
            Check(tag & ":never", never.Count = 1 AndAlso never(0) = d("vd_auto_guard_run_never"), never(0))
            Dim empty = DiskAutostart.GuardRunLines(emptyRun, d)
            Check(tag & ":empty", empty.Count = 1 AndAlso empty(0).Contains(d("vd_auto_guard_run_empty")), empty(0))
            Dim clean = DiskAutostart.GuardRunLines(cleanRun, d)
            Check(tag & ":clean", clean.Count = 1 AndAlso
                                     clean(0) = Localization.Format(d("vd_auto_guard_run_ok_fmt"), cleanRun.LastRun.Value.LocalDateTime.ToString("g"), 2), clean(0))
            Dim ramLeft = DiskAutostart.GuardRunLines(ramLeftRun, d)
            Check(tag & ":ram-left", ramLeft.Count = 2 AndAlso
                                        ramLeft(0) = Localization.Format(d("vd_auto_guard_run_left_fmt"), ramLeftRun.LastRun.Value.LocalDateTime.ToString("g"), 0, 1),
                  String.Join(" | ", ramLeft))
            Dim left = DiskAutostart.GuardRunLines(leftoverRun, d)
            Check(tag & ":leftover", left.Count = 2 AndAlso left(1).StartsWith("big:", StringComparison.Ordinal) AndAlso
                                        left(1).Contains(d("vd_auto_guard_outcome_unfinished")) AndAlso
                                        left(1).Contains("did not finish within 10 s"), left(1))
        Next

        ' The dialog itself, in every locale: three rows of the manager's list, the encrypted one
        ' disabled with its reason beside it, the guard's word, the last run told, and one leave path.
        For Each lang In Localization.Languages
            Dim d = Localization.GetDict(lang)
            Dim rows As New List(Of DiskRecord) From {
                MgrRecord(),
                MgrVariant(Sub(r)
                               r.Name = "vault" : r.Protection = DiskProtection.Encrypted : r.AutoMount = False
                           End Sub),
                MgrVariant(Sub(r)
                               r.Name = "mounted" : r.AutoMount = True
                           End Sub)}
            Dim guardState As New DiskGuardState With {.Installed = True, .Running = True, .LastRun = DateTimeOffset.Now, .Ended = "session"}
            guardState.Containers.Add(New DiskGuardRunRow With {.Name = "work", .Action = "unmount", .Outcome = "unmounted"})
            guardState.Containers.Add(New DiskGuardRunRow With {.Name = "big", .Action = "unmount", .Outcome = "unfinished", .Reason = "did not finish within 10 s"})
            Dim switchedOn As New List(Of Boolean)
            Dim performed As New List(Of String)
            Using dlg As New DiskAutostartDialog(d, Nothing,
                                                 Function() rows, Function() guardState,
                                                 Sub(a, sel) performed.Add(DiskStates.VerbOf(a)),
                                                 Sub(onOff) switchedOn.Add(onOff))
                Dim texts = dlg.TextsForTest
                Dim raw = texts.Where(Function(t) t.StartsWith("vd_", StringComparison.Ordinal) OrElse t.StartsWith("shell_", StringComparison.Ordinal)).Take(5).ToList()
                Check("disk-auto:dialog:no-raw-keys:" & lang, raw.Count = 0, String.Join(",", raw.ToArray()))
                Check("disk-auto:dialog:rows:" & lang, dlg.LogonRowCountForTest = 3, dlg.LogonRowCountForTest.ToString())
                Dim switches = dlg.LogonSwitchesForTest()
                Check("disk-auto:dialog:matrix:" & lang, switches.Count = 3 AndAlso switches(0).Value AndAlso
                                                             Not switches(1).Value AndAlso switches(2).Value,
                      String.Join(";", switches.Select(Function(s) s.Key & "=" & s.Value.ToString()).ToArray()))
                Check("disk-auto:dialog:encrypted-why:" & lang, texts.Contains(d("vd_mgr_why_encrypted_auto")), "")
                Check("disk-auto:dialog:guard-word:" & lang, texts.Contains(d("vd_auto_guard_on_running")), "")
                Check("disk-auto:dialog:run-told:" & lang,
                      texts.Any(Function(t) t.StartsWith("big:", StringComparison.Ordinal)) AndAlso
                      Not texts.Any(Function(t) t.StartsWith("work:", StringComparison.Ordinal)), "")
                Check("disk-auto:dialog:leave:" & lang, dlg.CancelButton IsNot Nothing AndAlso dlg.AcceptButton Is dlg.CancelButton, "")
                Check("disk-auto:dialog:fits-screen:" & lang, dlg.ClientSize.Height <= Screen.PrimaryScreen.WorkingArea.Height AndAlso
                                                              dlg.ClientSize.Width > 300, dlg.ClientSize.ToString())
                ' The longest captions (Russian, German) must not push a switch or a line off the page.
                Dim overflow = dlg.OverflowForTest()
                Check("disk-auto:dialog:fits-width:" & lang, overflow <= 0, overflow.ToString() & " px too wide")
                Dim problems As New List(Of String)
                WalkAccessible(dlg, problems)
                Check("a11y:DiskAutostartDialog:" & lang, problems.Count = 0, String.Join("; ", problems.Take(6).ToArray()))
                Check("disk-auto:dialog:delegates-idle:" & lang, performed.Count = 0 AndAlso switchedOn.Count = 0, "")
            End Using
        Next
    End Sub

    ' AUD-82-F1 and AUD-88-F2: the Autostart entry opens the dialog through the gesture a click makes
    ' (Perform), and opens it again after it was closed. The rows above read the pure words and passed
    ' while the entry did nothing at all (KindOf sent it to the quick actions, which have no command
    ' line for it) and, once it opened, while the closed modal dialog stayed held and a second open
    ' showed nothing.
    Private Sub CheckDiskAutostartOpens()
        Check("disk-auto:open:kind", DiskStates.KindOf(DiskAction.Autostart) = DiskActionKind.Local, DiskStates.KindOf(DiskAction.Autostart).ToString())
        Dim m As DiskManagerForm = Nothing
        Try
            Packaging.OverrideForTest = False
            m = New DiskManagerForm()
            Dim snap = GoldenSnapshot()
            m.ApplySnapshotForTest(snap, "")

            ' Nothing selected: the entry is global, it needs no row.
            Dim dlg As DiskAutostartDialog = Nothing
            Check("disk-auto:open:no-selection", OpenAutostartThroughPerform(m, dlg), "")
            Check("disk-auto:open:released", dlg IsNot Nothing AndAlso dlg.IsDisposed AndAlso m.AutostartDialogForTest Is Nothing,
                  If(dlg Is Nothing, "no dialog", "disposed " & dlg.IsDisposed.ToString()))
            ' The same entry a second time, and with a registered row selected.
            Check("disk-auto:open:again", OpenAutostartThroughPerform(m, dlg), "")
            m.SelectForTest(snap.Disks.First(Function(d) d.Name = "archive").Key)
            Check("disk-auto:open:with-row", OpenAutostartThroughPerform(m, dlg), "")
            Check("disk-auto:open:released-again", dlg IsNot Nothing AndAlso dlg.IsDisposed AndAlso m.AutostartDialogForTest Is Nothing, "")
        Catch ex As Exception
            Check("disk-auto:open", False, ex.GetType().Name & ": " & ex.Message)
        Finally
            Packaging.OverrideForTest = Nothing
            If m IsNot Nothing Then m.Dispose()
        End Try
    End Sub

    ' Runs the Autostart gesture on a window that is not on screen and closes the modal dialog from a
    ' timer once it is up. True when a dialog was held while the timer ticked; shown is that dialog.
    Private Function OpenAutostartThroughPerform(m As DiskManagerForm, ByRef shown As DiskAutostartDialog) As Boolean
        Dim seen As DiskAutostartDialog = Nothing
        Using t As New Windows.Forms.Timer With {.Interval = 150}
            AddHandler t.Tick, Sub()
                                   t.Stop()
                                   Try
                                       seen = m.AutostartDialogForTest
                                       Dim up = If(seen, Application.OpenForms.OfType(Of DiskAutostartDialog)().FirstOrDefault())
                                       If up IsNot Nothing Then up.Close()
                                   Catch
                                   End Try
                               End Sub
            t.Start()
            m.Perform(DiskAction.Autostart)
        End Using
        shown = seen
        Return seen IsNot Nothing
    End Function

    ' The window: its buttons carry a glyph and a name and a tooltip, the icon-only ones the meaning's
    ' canonical name; the detail pane closes with a cross and comes back from a bar; the filter is one
    ' unit; the menus carry pictures; the build that cannot mount hides what it cannot run.
    Private Sub CheckDiskUiWindow(ui As Dictionary(Of String, String))
        Dim m As DiskManagerForm = Nothing
        Try
            m = New DiskManagerForm()
            Dim snap = GoldenSnapshot()
            m.ApplySnapshotForTest(snap, "")
            Dim key = Function(name As String) snap.Disks.First(Function(d) d.Name = name).Key
            m.SelectForTest(key("archive"))
            Dim reason As String = ""

            Dim buttons = m.GlyphButtonsForTest
            Check("disk-ui:buttons-present", buttons.Count >= 14, buttons.Count.ToString())
            For i = 0 To buttons.Count - 1
                Dim b = buttons(i)
                Dim id = i.ToString() & ":" & If(b.AccessibleName, "")
                Check("disk-ui:button-name:" & id, Not String.IsNullOrEmpty(b.AccessibleName), "")
                Check("disk-ui:button-glyph:" & id, b.Glyph IsNot Nothing OrElse b.Picture IsNot Nothing, "")
                Check("disk-ui:button-tip:" & id, m.TipForTest(b) <> "", "")
                If b.IconOnly Then
                    Dim nameKey = DiskGlyphs.NameKey(b.Glyph)
                    Check("disk-ui:button-icon-only-name:" & id, b.Text = "" AndAlso nameKey <> "" AndAlso b.AccessibleName = ui(nameKey), b.AccessibleName & " vs " & nameKey)
                Else
                    Check("disk-ui:button-caption-is-name:" & id, b.AccessibleName = b.Text, b.AccessibleName & " vs " & b.Text)
                End If
            Next

            ' The detail pane: closed by a cross at its own edge, brought back from a bar or the More menu;
            ' no toolbar button is called "Hide details" (owner, 2026-09-30).
            Check("disk-ui:detail-cross", m.DetailCloseForTest.IconOnly AndAlso m.DetailCloseForTest.Glyph.Id = "nav.close" AndAlso
                                          m.DetailCloseForTest.AccessibleName = ui("vd_mgr_name_close"), "")
            Check("disk-ui:no-hide-button", Not buttons.Any(Function(b) b.Text = ui("vd_mgr_detail_hide")) AndAlso
                                            Not buttons.Any(Function(b) b.Text = ui("vd_mgr_detail_show") AndAlso b IsNot m.DetailShowForTest), "")
            Check("disk-ui:detail-starts-open", m.DetailOpenForTest, "")
            Dim more = m.MenuItemsForTest("more")
            Check("disk-ui:detail-hide-in-more", more.Any(Function(i) i.Text = ui("vd_mgr_detail_hide")), String.Join(" | ", more.Select(Function(i) i.Text).ToArray()))
            m.ToggleDetailForTest()
            Check("disk-ui:detail-hidden", Not m.DetailOpenForTest, "")
            Check("disk-ui:detail-bar-brings-it-back", m.DetailShowForTest.Text = ui("vd_mgr_detail_show") AndAlso
                                                       m.MenuItemsForTest("more").Any(Function(i) i.Text = ui("vd_mgr_detail_show")), "")
            m.ToggleDetailForTest()
            Check("disk-ui:detail-back", m.DetailOpenForTest, "")

            ' The filter: label, box and clear button are one unit; the clear button follows the text; Esc clears.
            Check("disk-ui:filter-one-unit", m.FilterIsOneUnitForTest, "")
            Check("disk-ui:filter-clear-icon", m.FilterClearForTest.IconOnly AndAlso m.FilterClearForTest.Glyph.Id = "action.clear-input" AndAlso
                                               m.FilterClearForTest.AccessibleName = ui("vd_mgr_name_clear"), "")
            Check("disk-ui:filter-clear-off-when-empty", Not m.FilterClearForTest.Enabled, "")
            m.SetFilterForTest("wor")
            Check("disk-ui:filter-clear-on-with-text", m.FilterClearForTest.Enabled AndAlso m.RowTextsForTest.Count < 10, m.RowTextsForTest.Count.ToString())
            Check("disk-ui:filter-esc-clears", m.RunShortcut(DiskShortcuts.Find(Keys.Escape, False)) AndAlso m.FilterTextForTest = "" AndAlso Not m.FilterClearForTest.Enabled, m.FilterTextForTest)
            Check("disk-ui:filter-esc-on-empty-passes", Not m.RunShortcut(DiskShortcuts.Find(Keys.Escape, False)), "")

            ' Each window opens the other with its destination's program icon:
            ' FileDO operations here, and Disk manager inside the shell's Disks group.
            Dim mainButton = buttons.FirstOrDefault(Function(b) b.Text = ui("vd_mgr_btn_main"))
            Check("disk-ui:main-window-button", mainButton IsNot Nothing AndAlso mainButton.Picture IsNot Nothing AndAlso mainButton.Glyph Is Nothing AndAlso
                                                m.TipForTest(mainButton).Contains(DiskShortcuts.KeyText(Keys.Control Or Keys.Shift Or Keys.O)), "")
            If mainButton IsNot Nothing AndAlso mainButton.Picture IsNot Nothing Then
                Using expected = AppIcon.Mark(Global.FileDOGUI.Ui.Px(m, 24))
                    Dim actual = DirectCast(mainButton.Picture, Bitmap)
                    Dim matches = expected IsNot Nothing AndAlso expected.Size = actual.Size
                    If matches Then
                        For y = 0 To actual.Height - 1
                            For x = 0 To actual.Width - 1
                                If actual.GetPixel(x, y) <> expected.GetPixel(x, y) Then matches = False
                            Next
                        Next
                    End If
                    Check("disk-ui:operations-program-icon", matches, "the operations button uses the FileDO program icon")
                End Using
            End If
            Dim raised = 0
            AddHandler m.ShellRequested, Sub() raised += 1
            m.RunShortcut(DiskShortcuts.Find(Keys.Control Or Keys.Shift Or Keys.O, False))
            Check("disk-ui:main-window-key-raises", raised = 1, raised.ToString())
            Check("disk-ui:one-picture-button", m.GlyphButtonsForTest.Where(Function(b) b.Picture IsNot Nothing).Count() = 1, "the product mark is on exactly one button")
            Using shell As New ShellForm()
                Dim opener = shell.DiskManagerButtonForTest
                Dim managerIndex = Array.FindIndex(RailRow.All, Function(r) r.Key = RailRow.DiskManagerKey)
                Check("disk-ui:manager-in-disks-group", managerIndex > 0 AndAlso RailRow.All(managerIndex - 1).Key = "rail_group_disks", "")
                Check("disk-ui:shell-opens-manager-button", opener IsNot Nothing AndAlso opener.ProductIcon Is DiskManagerIcon.DiskManagerIcon() AndAlso
                                                            opener.Text = ui(RailRow.DiskManagerKey) AndAlso opener.AccessibleName = opener.Text AndAlso
                                                            shell.TipForTest(opener).Contains(DiskShortcuts.KeyText(Keys.Control Or Keys.Shift Or Keys.D)), "")
            End Using

            ' Tab order: toolbar, list, detail, its bar, strip.
            Dim tab = m.TabOrderForTest
            Check("disk-ui:tab-order", tab.Zip(tab.Skip(1), Function(x, y) x < y).All(Function(ok) ok), String.Join(",", tab.Select(Function(t) t.ToString()).ToArray()))

            ' Menus carry the meaning's picture, and the Help menu the links.
            m.SelectForTest(key("work"))
            For Each item In m.MenuItemsForTest("row")
                Dim mi = TryCast(item, ToolStripMenuItem)
                If mi Is Nothing Then Continue For
                Check("disk-ui:menu-image:" & mi.Text, mi.Image IsNot Nothing, "")
            Next
            Dim help = m.MenuItemsForTest("help")
            Check("disk-ui:help-menu", help.Count >= 8 AndAlso TryCast(help(0), ToolStripMenuItem).Text = ui("vd_help_menu_help") AndAlso
                                       TryCast(help(0), ToolStripMenuItem).ShortcutKeyDisplayString = "F1", help.Count.ToString())
            Dim linkItems = help.OfType(Of ToolStripMenuItem)().Where(Function(i) If(i.ToolTipText, "").StartsWith("https://", StringComparison.Ordinal)).ToList()
            Check("disk-ui:help-menu-links", linkItems.Count = DiskHelpLinks.All.Length AndAlso linkItems.All(Function(i) i.Image IsNot Nothing), linkItems.Count.ToString())
            Check("disk-ui:help-menu-first-steps", help.OfType(Of ToolStripMenuItem)().Any(Function(i) i.Text = ui("vd_help_menu_first")), "")
            Check("disk-ui:key-help-and-menu", DiskShortcuts.Find(Keys.F1, False).Command = DiskCommand.Help, "")

            ' The build that cannot mount: hidden, not disabled (APP-BEHAVIOUR rule 11) - and the read path stays.
            For Each a As DiskAction In [Enum].GetValues(GetType(DiskAction))
                Check("disk-ui:hidden-only-when-refused:" & a.ToString(),
                      Not DiskStates.HiddenInBuild(a, New DiskContext With {.Packaged = False}) AndAlso
                      (Not DiskStates.HiddenInBuild(a, New DiskContext With {.Packaged = True}) OrElse
                       MgrVariants().All(Function(r) [Enum].GetValues(GetType(DiskRowState)).Cast(Of DiskRowState)().All(
                           Function(s) DiskStates.WhyNot(a, r, s, New DiskContext With {.Packaged = True}) <> ""))), "")
            Next
            Packaging.OverrideForTest = True
            m.SelectForTest(key("archive"))
            Check("disk-ui:store-hides-mount", Not m.ToolbarVisibleForTest("mount") AndAlso Not m.ToolbarVisibleForTest("unmount") AndAlso
                                               Not m.ToolbarVisibleForTest("save") AndAlso m.ToolbarVisibleForTest("open"), "")
            Dim storeMenu = m.MenuItemsForTest("row").OfType(Of ToolStripMenuItem)().Select(Function(i) i.Text).ToList()
            Check("disk-ui:store-menu-has-no-mount", Not storeMenu.Contains(ui("vd_mgr_act_mount")) AndAlso Not storeMenu.Contains(ui("vd_mgr_act_mount_ro")) AndAlso
                                                     Not storeMenu.Contains(ui("vd_mgr_act_format")) AndAlso Not storeMenu.Contains(ui("vd_mgr_act_auto_on")) AndAlso
                                                     storeMenu.Contains(ui("vd_mgr_act_info")) AndAlso storeMenu.Contains(ui("vd_mgr_act_verify")) AndAlso
                                                     storeMenu.Contains(ui("vd_mgr_act_export")), String.Join(" | ", storeMenu.ToArray()))
            Check("disk-ui:store-detail-has-no-mount", Not m.DetailButtonsForTest.Contains(ui("vd_mgr_act_mount")) AndAlso m.DetailButtonsForTest.Contains(ui("vd_mgr_act_info")),
                  String.Join(",", m.DetailButtonsForTest.ToArray()))
            Packaging.OverrideForTest = Nothing
            m.SelectForTest(key("archive"))
            Check("disk-ui:build-mounts-again", m.ToolbarVisibleForTest("mount") AndAlso m.ToolbarStateForTest("mount", reason), reason)
        Catch ex As Exception
            Check("disk-ui", False, ex.GetType().Name & ": " & ex.Message)
        Finally
            Packaging.OverrideForTest = Nothing
            Theme.UsePaletteForTest(Nothing)
            If m IsNot Nothing Then m.Dispose()
        End Try
    End Sub

    ' The pieces around the window: the start switch, the files the watcher reacts to, the lines of
    ' `info` kept for the detail pane, and the names a container is added under.
    Private Sub CheckDiskHost()
        Check("disk-host:switch", Program.WantsDiskManager(New String() {"filedo_win.exe", "--disks"}) AndAlso
                                  Program.WantsDiskManager(New String() {"filedo_win.exe", "--DISKS"}) AndAlso
                                  Not Program.WantsDiskManager(New String() {"filedo_win.exe"}) AndAlso
                                  Not Program.WantsDiskManager(New String() {"--disks"}), "")
        Check("disk-host:no-file-target", Program.StartupTargetFrom(New String() {"filedo_win.exe", "--disks"}) Is Nothing, "")
        Check("disk-host:message", AppHost.ShowDisksMessage <> 0, AppHost.ShowDisksMessage.ToString())
        ' A plain second start asks the running copy for its shell (the Start menu's FileDO entry while only
        ' the Disk manager is open): its own registered message, never the manager's.
        Check("disk-host:shell-message", AppHost.ShowShellMessage <> 0 AndAlso AppHost.ShowShellMessage <> AppHost.ShowDisksMessage,
              AppHost.ShowShellMessage.ToString())
        Check("disk-host:watched", DiskManagerForm.IsWatchedName("vdisk-state.json") AndAlso DiskManagerForm.IsWatchedName("VD-REGISTRY.JSON") AndAlso
                                   DiskManagerForm.IsWatchedName("vd-0123abcd.status.json") AndAlso Not DiskManagerForm.IsWatchedName("vd-0123abcd.handoff.json") AndAlso
                                   Not DiskManagerForm.IsWatchedName("history.json"), "")
        Check("disk-host:state-root", DiskManagerForm.StateRoot().EndsWith("state", StringComparison.OrdinalIgnoreCase), DiskManagerForm.StateRoot())
        Dim info = DiskManagerForm.InfoLines(vbLf & "2026-09-30 00:59:27 sza@ukr.net 2609300059" & vbLf & "Container:     C:\a.fdd" & vbLf &
                                             "Protection:    obfuscated" & vbLf & vbLf & " Finish:2026-09-30 00:59:28, Duration: 0s" & vbLf)
        Check("disk-host:info-lines", info = "Container:     C:\a.fdd" & Environment.NewLine & "Protection:    obfuscated", info)
        Dim taken As New HashSet(Of String)(StringComparer.OrdinalIgnoreCase) From {"work", "WORK-2"}
        Check("disk-host:name-free", DiskNameDialog.Suggest("backup", taken) = "backup", "")
        Check("disk-host:name-taken", DiskNameDialog.Suggest("Work", taken) = "Work-3", DiskNameDialog.Suggest("Work", taken))
        Check("disk-host:name-cleaned", DiskNameDialog.Suggest("my disk (1)", taken) = "my-disk--1", DiskNameDialog.Suggest("my disk (1)", taken))
        Check("disk-host:name-empty", DiskNameDialog.Suggest("Диск", taken) = "disk", DiskNameDialog.Suggest("Диск", taken))
        Check("disk-host:name-rule", DiskStates.IsUsableName("work") AndAlso DiskStates.IsUsableName("a_b-1") AndAlso Not DiskStates.IsUsableName("-x") AndAlso
                                     Not DiskStates.IsUsableName(New String("a"c, 41)) AndAlso Not DiskStates.IsUsableName("C:") AndAlso
                                     Not DiskStates.IsUsableName("work 1") AndAlso Not DiskStates.IsUsableName(""), "")
        Check("disk-host:image-paths", DiskManagerForm.IsImagePath("C:\a.VHDX") AndAlso DiskManagerForm.IsImagePath("a.iso") AndAlso
                                       Not DiskManagerForm.IsImagePath("a.fdd"), "")
        CheckHiddenManagerKeepsLayout()
    End Sub

    ' AUD-85-F1: the host builds a Disk Manager it never shows (a hidden start, the first
    ' minimize-to-tray) so that the tray has a snapshot to count. Its OnLoad never applied the user's
    ' saved columns and placement, so what such a window holds is the defaults - and closing it, at
    ' Exit or at sign-out, wrote them over the user's. A window that was never shown closes without
    ' writing; one that was shown still saves.
    Private Sub CheckHiddenManagerKeepsLayout()
        Dim values = ShellSettings.ValuesForTest
        If values Is Nothing Then Return
        Dim saved As New Dictionary(Of String, Object)(values)
        Dim wasReads = DiskManagerForm.SuppressReads
        Dim wasWelcome = DiskManagerForm.SuppressWelcome
        Dim m As DiskManagerForm = Nothing
        Dim layout = Function() String.Join(";", values.Where(Function(kv) kv.Key.StartsWith("DiskManager", StringComparison.Ordinal)).
                                                OrderBy(Function(kv) kv.Key, StringComparer.Ordinal).
                                                Select(Function(kv) kv.Key & "=" & Convert.ToString(kv.Value)).ToArray())
        Try
            DiskManagerForm.SuppressReads = True
            DiskManagerForm.SuppressWelcome = True

            values.Clear()
            ShellSettings.SetDiskManagerColumns(New Integer() {111, 222, 333, 444, 555, 666, 777, 88, 99})
            ShellSettings.SetDiskManagerSort(2, True)
            ShellSettings.SavePlacementOf(ShellSettings.DiskManagerPrefix, 10, 10, 900, 600, False, 96)
            Dim before = layout()
            m = New DiskManagerForm()
            Dim handle = m.Handle
            m.Close()
            m.Dispose()
            m = Nothing
            Check("disk-host:hidden-close-keeps-layout", layout() = before, before & " -> " & layout())

            ' "Reset window positions" is consumed by the hidden window's constructor; its close must
            ' not turn the defaults it holds into a saved placement.
            values.Clear()
            ShellSettings.SavePlacementOf(ShellSettings.DiskManagerPrefix, 10, 10, 900, 600, False, 96)
            ShellSettings.ResetPlacements()
            before = layout()
            m = New DiskManagerForm()
            handle = m.Handle
            m.Close()
            m.Dispose()
            m = Nothing
            Check("disk-host:hidden-close-keeps-reset", layout() = before AndAlso Convert.ToInt32(values("DiskManagerPlacementV")) = 0, before & " -> " & layout())

            ' A window that was shown is the user's: it still saves its layout when it closes.
            values.Clear()
            m = New DiskManagerForm()
            m.StartPosition = FormStartPosition.Manual
            m.ShowInTaskbar = False
            m.Show()
            m.Bounds = New Rectangle(-32000, -32000, m.MinimumSize.Width, m.MinimumSize.Height)
            Settle()
            m.Close()
            m.Dispose()
            m = Nothing
            Check("disk-host:shown-close-saves-layout", values.ContainsKey("DiskManagerColumns") AndAlso values.ContainsKey("DiskManagerPlacementV"), layout())
        Catch ex As Exception
            Check("disk-host:hidden-close", False, ex.GetType().Name & ": " & ex.Message)
        Finally
            DiskManagerForm.SuppressReads = wasReads
            DiskManagerForm.SuppressWelcome = wasWelcome
            If m IsNot Nothing Then m.Dispose()
            values.Clear()
            For Each kv In saved
                values(kv.Key) = kv.Value
            Next
        End Try
    End Sub

End Module
