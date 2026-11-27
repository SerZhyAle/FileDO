' SP-0169 slice S5: the complete-frame timings of the shell's destinations (SETTINGS-NAVIGATION 2.0
' section 5 rule 4, WINDOWS-UI section 5). Run as the last step of `ui-drive run` (UiDrive.vb), on
' the real screen, because a frame is only complete once the compositor has presented it.
'
' One sample is activation to presented frame, not handler return: the clock starts before the rail
' row's own click path (ShellForm.SelectPageForTest) and stops when the posted work that path queued
' has run, every pending paint of the window and its children has been painted (RedrawWindow
' RDW_UPDATENOW) and the desktop compositor has presented the result (DwmFlush). The handler's return
' is kept beside it, so a slow paint and a slow handler can be told apart.
'
' First visit: 20 fresh shell windows, each opened on About (the cheapest page, which is then not
' measured in them) and visiting every other destination once, in an order rotated per window, so the
' lazily built job page is paid by a different destination each time; and 20 more fresh windows opened
' on Command that visit About only. The window's own opening (new, show, presented) is a sample of its
' own, "window-open". Warm: one window, opened once, 20 rounds through every destination in rail order -
' every transition comes from another page, and the rounds show whether switching grows its work.
'
' Every sample is a row of page-timings.csv (raw, none dropped); page-timings.txt names the machine,
' the system, the runtime, the build, the language, the DPI and the interface scale, and per
' destination and kind the count, the nearest-rank p95, the median and the maximum. Checks: warm p95
' below 150 ms, first-visit p95 below 500 ms, no destination over its budget in two or more samples
' (a repeated delay fails even where the percentile hides it), no sample that never settled, and the
' window's user and GDI objects not growing across the warm rounds. Settings go to an in-process
' dictionary (ShellSettings.ValuesForTest), so the run writes nothing of the person's.
Imports System.Runtime.InteropServices

Module PageTiming

    Friend Const WarmBudgetMs As Double = 150
    Friend Const FirstBudgetMs As Double = 500
    Private Const Rounds As Integer = 20
    Private Const SettleCapMs As Integer = 3000

    Friend NotInheritable Class Verdict
        Friend ReadOnly Name As String
        Friend ReadOnly Ok As Boolean
        Friend ReadOnly Detail As String

        Friend Sub New(name As String, ok As Boolean, detail As String)
            Me.Name = name
            Me.Ok = ok
            Me.Detail = detail
        End Sub
    End Class

    Private NotInheritable Class Sample
        Friend Kind As String
        Friend Destination As String
        Friend Window As Integer
        Friend Round As Integer
        Friend FromKey As String
        Friend HandlerMs As Double
        Friend FrameMs As Double
        Friend Settled As Boolean
    End Class

    <DllImport("dwmapi.dll")>
    Private Function DwmFlush() As Integer
    End Function

    <DllImport("user32.dll")>
    Private Function GetQueueStatus(flags As UInteger) As UInteger
    End Function

    <DllImport("user32.dll")>
    Private Function RedrawWindow(hWnd As IntPtr, lprcUpdate As IntPtr, hrgnUpdate As IntPtr, flags As UInteger) As <MarshalAs(UnmanagedType.Bool)> Boolean
    End Function

    <DllImport("user32.dll")>
    Private Function GetGuiResources(hProcess As IntPtr, uiFlags As UInteger) As UInteger
    End Function

    ' Posted and sent messages and paints - what the click path queues. Timers and input are left out:
    ' a periodic timer would keep the queue busy forever and is not part of the switch.
    Private Const QS_PAINT As UInteger = &H20
    Private Const QS_POSTMESSAGE As UInteger = &H8
    Private Const QS_SENDMESSAGE As UInteger = &H40
    Private Const RDW_UPDATENOW As UInteger = &H100
    Private Const RDW_ALLCHILDREN As UInteger = &H80
    Private Const GR_GDIOBJECTS As UInteger = 0
    Private Const GR_USEROBJECTS As UInteger = 1

    Friend Function Run(folder As String) As List(Of Verdict)
        Dim verdicts As New List(Of Verdict)()
        Dim samples As New List(Of Sample)()
        Dim growth As String = ""
        Dim growthOk As Boolean = True
        Dim savedValues = ShellSettings.ValuesForTest
        ShellSettings.ValuesForTest = New Dictionary(Of String, Object)()
        Try
            Dim destinations As List(Of String) = Nothing
            ' First visits, every destination but About.
            For w = 1 To Rounds
                Dim shell = OpenShell("rail_job_about", w, samples)
                Try
                    If destinations Is Nothing Then destinations = DestinationKeys(shell)
                    Dim order = Rotate(destinations.Where(Function(k) k <> "rail_job_about").ToList(), w - 1)
                    Dim from = "rail_job_about"
                    For Each key In order
                        samples.Add(Visit(shell, key, from, "first", w, 1))
                        from = key
                    Next
                Finally
                    CloseShell(shell)
                End Try
            Next
            ' First visits of About.
            For w = 1 To Rounds
                Dim shell = OpenShell("rail_job_command", Rounds + w, samples)
                Try
                    samples.Add(Visit(shell, "rail_job_about", "rail_job_command", "first", Rounds + w, 1))
                Finally
                    CloseShell(shell)
                End Try
            Next
            ' Warm rounds in one window. The first pass visits every page once so the warm rounds start warm.
            Dim warmShell = OpenShell("rail_job_command", 2 * Rounds + 1, Nothing)
            Try
                Dim from = "rail_job_command"
                For Each key In destinations
                    Visit(warmShell, key, from, "prime", 0, 0)
                    from = key
                Next
                Dim objectsAfterFirst As Long = 0
                For r = 1 To Rounds
                    For Each key In destinations
                        samples.Add(Visit(warmShell, key, from, "warm", 2 * Rounds + 1, r))
                        from = key
                    Next
                    If r = 1 Then objectsAfterFirst = GuiObjects()
                Next
                Dim objectsAfterLast = GuiObjects()
                Dim grew = objectsAfterLast - objectsAfterFirst
                growthOk = grew <= Math.Max(30L, objectsAfterFirst \ 20L)
                growth = "user+gdi objects after round 1: " & objectsAfterFirst.ToString() & ", after round " &
                         Rounds.ToString() & ": " & objectsAfterLast.ToString()
            Finally
                CloseShell(warmShell)
            End Try
        Catch ex As Exception
            ShellLog.Write("page-timing", ex)
            verdicts.Add(New Verdict("page-timing:run", False, ex.GetType().Name & " (see the shell log)"))
        Finally
            ShellSettings.ValuesForTest = savedValues
        End Try

        WriteSamples(IO.Path.Combine(folder, "page-timings.csv"), samples)
        Dim summary As New Text.StringBuilder()
        summary.AppendLine("page timings (SP-0169 S5) - activation to presented frame, milliseconds")
        For Each line In Environment_()
            summary.AppendLine(line)
        Next
        summary.AppendLine("budgets: warm p95 < " & Num(WarmBudgetMs) & ", first visit p95 < " & Num(FirstBudgetMs) &
                           "; nearest-rank p95 over every recorded sample")
        summary.AppendLine("kind,destination,count,p95,median,max,over-budget,handler-p95,unsettled")
        For Each bucket In samples.GroupBy(Function(s) s.Kind & "|" & s.Destination).OrderBy(Function(g) g.Key, StringComparer.Ordinal)
            Dim list = bucket.ToList()
            Dim kind = list(0).Kind
            Dim budget = If(kind = "warm", WarmBudgetMs, FirstBudgetMs)
            Dim frames = list.Select(Function(s) s.FrameMs).ToList()
            Dim p95 = NearestRank(frames, 0.95)
            Dim median = NearestRank(frames, 0.5)
            Dim max = frames.Max()
            Dim over = frames.Where(Function(v) v >= budget).Count()
            Dim unsettled = list.Where(Function(s) Not s.Settled).Count()
            Dim handlerP95 = NearestRank(list.Select(Function(s) s.HandlerMs).ToList(), 0.95)
            summary.AppendLine(String.Join(",", kind, list(0).Destination, list.Count.ToString(), Num(p95), Num(median), Num(max),
                                           over.ToString(), Num(handlerP95), unsettled.ToString()))
            Dim name = "page-timing:" & kind & ":" & list(0).Destination
            Dim enough = list.Count >= Rounds
            verdicts.Add(New Verdict(name & ":p95", enough AndAlso p95 < budget,
                                     "p95 " & Num(p95) & " ms of " & list.Count.ToString() & ", max " & Num(max) & ", budget " & Num(budget)))
            verdicts.Add(New Verdict(name & ":repeated", over < 2, over.ToString() & " samples at or over " & Num(budget) & " ms"))
            verdicts.Add(New Verdict(name & ":settled", unsettled = 0, unsettled.ToString() & " samples hit the " & SettleCapMs.ToString() & " ms cap"))
        Next
        verdicts.Add(New Verdict("page-timing:warm-no-growth", growthOk AndAlso growth <> "", If(growth = "", "not measured", growth)))
        summary.AppendLine(growth)
        For Each v In verdicts.Where(Function(x) Not x.Ok)
            summary.AppendLine("FAIL " & v.Name & " - " & v.Detail)
        Next
        Try
            IO.File.WriteAllText(IO.Path.Combine(folder, "page-timings.txt"), summary.ToString())
        Catch ex As Exception
            verdicts.Add(New Verdict("page-timing:summary-written", False, ex.GetType().Name))
        End Try
        Return verdicts
    End Function

    ' The rail's destinations in rail order: every row that opens a page here. Group headers fold the
    ' rail and the Disk Manager row opens a window of its own, so neither is a page switch.
    Private Function DestinationKeys(shell As ShellForm) As List(Of String)
        Return shell.RailEntriesForTest().Where(Function(e) Not e.IsGroupHeader AndAlso e.Key <> RailRow.DiskManagerKey).
            Select(Function(e) e.Key).Distinct().ToList()
    End Function

    Private Function Rotate(keys As List(Of String), by As Integer) As List(Of String)
        If keys.Count = 0 Then Return keys
        Dim n = by Mod keys.Count
        Return keys.Skip(n).Concat(keys.Take(n)).ToList()
    End Function

    ' A fresh window on the given page, shown and presented; its opening is a "window-open" sample when
    ' a list is given.
    Private Function OpenShell(startKey As String, window As Integer, samples As List(Of Sample)) As ShellForm
        ShellSettings.SetLastPage(startKey)
        Dim clock = Stopwatch.StartNew()
        Dim shell As New ShellForm()
        shell.Show()
        Dim handler = clock.Elapsed.TotalMilliseconds
        Dim settled = CompleteFrame(shell)
        Dim frame = clock.Elapsed.TotalMilliseconds
        If samples IsNot Nothing Then
            samples.Add(New Sample With {.Kind = "first", .Destination = "window-open", .Window = window, .Round = 1,
                                         .FromKey = "", .HandlerMs = handler, .FrameMs = frame, .Settled = settled})
        End If
        Return shell
    End Function

    Private Sub CloseShell(shell As ShellForm)
        If shell Is Nothing Then Return
        Try
            shell.Close()
            shell.Dispose()
        Catch
        End Try
        Application.DoEvents()
    End Sub

    Private Function Visit(shell As ShellForm, key As String, from As String, kind As String, window As Integer, round As Integer) As Sample
        Dim clock = Stopwatch.StartNew()
        shell.SelectPageForTest(key)
        Dim handler = clock.Elapsed.TotalMilliseconds
        Dim settled = CompleteFrame(shell)
        Dim frame = clock.Elapsed.TotalMilliseconds
        Return New Sample With {.Kind = kind, .Destination = key, .Window = window, .Round = round, .FromKey = from,
                                .HandlerMs = handler, .FrameMs = frame, .Settled = settled}
    End Function

    ' The posted work runs, every pending paint is painted, and the compositor presents the frame.
    ' False when the queue was still busy at the cap - that sample's number is then a floor, not a time.
    Private Function CompleteFrame(shell As ShellForm) As Boolean
        Dim cap = Stopwatch.StartNew()
        Dim settled = False
        Do
            Application.DoEvents()
            If ((GetQueueStatus(QS_POSTMESSAGE Or QS_SENDMESSAGE Or QS_PAINT) >> 16) And &HFFFFUI) = 0UI Then
                settled = True
                Exit Do
            End If
        Loop While cap.ElapsedMilliseconds < SettleCapMs
        If shell.IsHandleCreated Then RedrawWindow(shell.Handle, IntPtr.Zero, IntPtr.Zero, RDW_UPDATENOW Or RDW_ALLCHILDREN)
        DwmFlush()
        Return settled
    End Function

    Private Function GuiObjects() As Long
        Using self = Process.GetCurrentProcess()
            Return CLng(GetGuiResources(self.Handle, GR_USEROBJECTS)) + CLng(GetGuiResources(self.Handle, GR_GDIOBJECTS))
        End Using
    End Function

    ' Nearest rank: the smallest sample with at least the fraction of all samples at or below it.
    Friend Function NearestRank(values As List(Of Double), fraction As Double) As Double
        If values Is Nothing OrElse values.Count = 0 Then Return 0
        Dim sorted = values.OrderBy(Function(v) v).ToList()
        Dim rank = CInt(Math.Ceiling(fraction * sorted.Count))
        Return sorted(Math.Max(1, Math.Min(sorted.Count, rank)) - 1)
    End Function

    Private Function Environment_() As List(Of String)
        Dim lines As New List(Of String)()
        lines.Add("build: " & LogReport.BuildStamp())
        lines.Add("system: " & RuntimeInformation.OSDescription & " (" & RuntimeInformation.OSArchitecture.ToString() & ")")
        lines.Add("runtime: " & RuntimeInformation.FrameworkDescription & ", process " & If(Environment.Is64BitProcess, "64-bit", "32-bit"))
        lines.Add("processor: " & If(Environment.GetEnvironmentVariable("PROCESSOR_IDENTIFIER"), "unknown") & ", " &
                  Environment.ProcessorCount.ToString() & " logical")
        Dim dpi As Integer = 96
        Using g = Graphics.FromHwnd(IntPtr.Zero)
            dpi = CInt(g.DpiX)
        End Using
        lines.Add("language: " & ShellSettings.Language() & ", dpi: " & dpi.ToString() & ", interface scale: " &
                  Num(UiTokens.InterfaceScale) & ", screens: " & Screen.AllScreens.Length.ToString())
        lines.Add("samples: " & Rounds.ToString() & " first visits and " & Rounds.ToString() & " warm transitions per destination")
        Return lines
    End Function

    Private Sub WriteSamples(path As String, samples As List(Of Sample))
        Dim csv As New Text.StringBuilder()
        csv.AppendLine("kind,destination,window,round,from,handler_ms,frame_ms,settled")
        For Each s In samples
            csv.AppendLine(String.Join(",", s.Kind, s.Destination, s.Window.ToString(), s.Round.ToString(), s.FromKey,
                                       Num(s.HandlerMs), Num(s.FrameMs), If(s.Settled, "1", "0")))
        Next
        Try
            IO.File.WriteAllText(path, csv.ToString())
        Catch ex As Exception
            ShellLog.Write("page-timing", ex)
        End Try
    End Sub

    Private Function Num(value As Double) As String
        Return value.ToString("0.0", Globalization.CultureInfo.InvariantCulture)
    End Function

End Module
