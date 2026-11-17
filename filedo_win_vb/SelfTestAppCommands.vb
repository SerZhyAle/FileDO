Imports System.IO
Imports System.IO.Pipes
Imports System.Text
Imports System.Text.RegularExpressions
Imports System.Threading
Imports System.Web.Script.Serialization

' SP-0216 / APP-COMMANDS 0.1: the first instance's half of the app commands - the embedded manifest (C1), the
' parser and its refusals (C4), busy inside the bound when a modal window is open or the window's thread does
' not pump (C5, C6), the gate of rule 13 (C8), the log line of rule 16, the pipe name of APP-ACTIVATION rule 3,
' and the frozen starts of filedo_win.exe (C13). The second process's half is cmd/filedo/appcmd_test.go. Rows
' are named `appcmd:`; FILEDO_SELFTEST_ONLY=appcmd runs just these. The pipe here is a fresh name per run,
' never the user's own command pipe.
Partial Public Module SelfTest

    Private Sub CheckAppCommands()
        CheckAppCommandManifest()
        CheckAppCommandParser()
        CheckAppCommandGate()
        CheckAppCommandBusy()
        CheckAppCommandListener()
        CheckAppCommandLog()
        CheckAppCommandFrozenStarts()
        CheckAppCommandContainerOpen()
        CheckAppCommandWindowVerbs()
        CheckAppCommandJobs()
        CheckAppCommandPending()
    End Sub

    ' SP-0218, the job verbs, window side: the declared rows and their options, the snapshot (shape, no path, the count),
    ' what the two windows list and stop, and a stop that finds nothing running saying 0 twice. The wait of the listener is
    ' CheckAppCommandPending; the answers with a live first instance are release checks.
    Private Sub CheckAppCommandJobs()
        Dim m = AppCommands.Manifest()
        Dim list = m.Find("job list")
        Dim stopSpec = m.Find("job stop")
        Dim quitSpec = m.Find("quit")
        Check("appcmd:job-verbs-declared", list IsNot Nothing AndAlso list.Risk = "safe" AndAlso list.ColdStart = "refuse" AndAlso
              stopSpec IsNot Nothing AndAlso stopSpec.Risk = "safe" AndAlso stopSpec.ColdStart = "oneshot" AndAlso
              quitSpec IsNot Nothing AndAlso quitSpec.Args.Any(Function(a) a.Name = "stop-jobs" AndAlso a.Kind = "bool"),
              "list=" & If(list Is Nothing, "missing", list.ColdStart) & " stop=" & If(stopSpec Is Nothing, "missing", stopSpec.ColdStart))
        Dim r = AppCmdRead(AppCmdLine("job stop", {"--where", "manager"}))
        Check("appcmd:job-stop-where-manager", r.Status = "done" AndAlso r.Message = "accepted:job stop:where=manager", r.Message)
        r = AppCmdRead(AppCmdLine("job stop", {"--where", "everything"}))
        Check("appcmd:job-stop-where-is-an-enum", r.ExitCode = 10 AndAlso r.Reason = "bad-argument", r.Message)
        r = AppCmdRead(AppCmdLine("quit", {"--stop-jobs"}))
        Check("appcmd:quit-takes-stop-jobs", r.Status = "done" AndAlso r.Message = "accepted:quit:stop-jobs=true", r.Message)

        ' The snapshot: a run of the main window and an operation waiting in the Disk Manager's queue.
        Dim shellRun As New RunInfo With {.Place = RunSnapshot.WhereShell, .Verb = "test", .State = RunSnapshot.StateRunning, .StepName = "write", .Percent = 45}
        Dim queuedOp As New RunInfo With {.Place = RunSnapshot.WhereManager, .Verb = "unmount", .State = RunSnapshot.StateQueued}
        Dim both As New List(Of RunInfo) From {shellRun, queuedOp}
        Dim line = CommandResponse.Done("x", New Dictionary(Of String, Object) From {{"jobs", RunSnapshot.ToData(both)}}).ToLine()
        Check("appcmd:job-snapshot-shape", line.Contains("""where"":""shell""") AndAlso line.Contains("""verb"":""test""") AndAlso line.Contains("""state"":""running""") AndAlso
              line.Contains("""step"":""write""") AndAlso line.Contains("""percent"":45") AndAlso line.Contains("""where"":""manager""") AndAlso
              line.Contains("""state"":""queued""") AndAlso line.Contains("""percent"":null"), line)
        Check("appcmd:job-list-holds-both", RunSnapshot.ToData(both).Count = 2 AndAlso both.Select(Function(x) x.Place).Distinct().Count() = 2, line)
        Dim empty = CommandResponse.Done("x", New Dictionary(Of String, Object) From {{"jobs", RunSnapshot.ToData(New List(Of RunInfo)())}}).ToLine()
        Check("appcmd:job-list-empty", empty.Contains("""jobs"":[]"), empty)
        Check("appcmd:running-jobs-is-the-snapshot-count", RunSnapshot.RunningCount(both) = 1 AndAlso RunSnapshot.RunningCount(New List(Of RunInfo)()) = 0 AndAlso
              RunSnapshot.Includes("all", "shell") AndAlso RunSnapshot.Includes("", "manager") AndAlso RunSnapshot.Includes("shell", "shell") AndAlso
              Not RunSnapshot.Includes("shell", "manager") AndAlso Not RunSnapshot.Includes("manager", "shell"), RunSnapshot.RunningCount(both).ToString())
        Dim leaky As New RunInfo With {.Place = RunSnapshot.WhereShell, .Verb = RunSnapshot.SafeToken("C:\Users\ann\secret.txt", "job"),
                                       .State = RunSnapshot.StateRunning, .StepName = RunSnapshot.SafeToken("C:\Users\ann\secret.txt", ""), .Percent = -1}
        Dim leakyLine = CommandResponse.Done("x", New Dictionary(Of String, Object) From {{"jobs", RunSnapshot.ToData(New List(Of RunInfo) From {leaky})}}).ToLine()
        Check("appcmd:job-snapshot-has-no-path", Not leakyLine.Contains("Users") AndAlso Not leakyLine.Contains("secret") AndAlso leakyLine.Contains("""verb"":""job""") AndAlso
              RunSnapshot.SafeToken("scan-folder", "") = "scan-folder" AndAlso RunSnapshot.SafeToken(New String("a"c, 41), "") = "", leakyLine)

        ' The Disk Manager: running first, then the queue; the Stop button's request on the verification alone; the queue emptied.
        Dim wasReads = DiskManagerForm.SuppressReads
        Dim wasWelcome = DiskManagerForm.SuppressWelcome
        Dim mgr As DiskManagerForm = Nothing
        Dim shell As ShellForm = Nothing
        Try
            DiskManagerForm.SuppressReads = True
            DiskManagerForm.SuppressWelcome = True
            mgr = New DiskManagerForm()
            Dim probe = mgr.JobsForTest()
            Dim listed = CType(probe("listed"), List(Of RunInfo))
            Dim states = String.Join(",", listed.Select(Function(x) x.State).ToArray())
            Dim verbs = String.Join(",", listed.Select(Function(x) x.Verb).ToArray())
            Check("appcmd:job-snapshot-manager-lists-running-then-queue", listed.Count = 4 AndAlso states = "running,running,queued,queued" AndAlso verbs = "verify,mount,unmount,save" AndAlso
                  listed.All(Function(x) x.Place = RunSnapshot.WhereManager) AndAlso listed.Where(Function(x) x.State = RunSnapshot.StateQueued).All(Function(x) x.StepName = "" AndAlso x.Percent = -1),
                  states & " / " & verbs)
            Dim managerLine = CommandResponse.Done("x", New Dictionary(Of String, Object) From {{"jobs", RunSnapshot.ToData(listed)}}).ToLine()
            Check("appcmd:job-snapshot-manager-has-no-name", Not managerLine.Contains("jobs-") AndAlso Not managerLine.Contains("selftest"), managerLine)
            Check("appcmd:job-stop-asks-only-what-the-stop-button-asks", CInt(probe("asked")) = 1 AndAlso CBool(probe("verifyStopped")) AndAlso Not CBool(probe("mountStopped")) AndAlso
                  CInt(probe("cleared")) = 2 AndAlso CInt(probe("queueLeft")) = 0 AndAlso CInt(probe("runningLeft")) = 2,
                  "asked=" & probe("asked").ToString() & " cleared=" & probe("cleared").ToString() & " queueLeft=" & probe("queueLeft").ToString())

            ' Nothing running: both windows list nothing and ask nothing, again and again.
            shell = New ShellForm()
            Dim cleared1 As Integer, cleared2 As Integer
            Dim first = shell.StopRuns() + mgr.StopForCommand(cleared1)
            Dim second = shell.StopRuns() + mgr.StopForCommand(cleared2)
            Check("appcmd:job-shell-idle-lists-nothing", shell.Runs().Count = 0 AndAlso mgr.Runs().Count = 0, shell.Runs().Count.ToString())
            Check("appcmd:job-stop-twice-says-zero", first = 0 AndAlso second = 0 AndAlso cleared1 = 0 AndAlso cleared2 = 0 AndAlso
                  AppCommands.StopData(0, 0)("stopped").Equals(0) AndAlso AppCommands.StopMessage(0, 0) = Localization.T("appcmd_done_stop_none"), first.ToString() & "/" & second.ToString())
        Finally
            If mgr IsNot Nothing Then mgr.Dispose()
            If shell IsNot Nothing Then shell.Dispose()
            DiskManagerForm.SuppressReads = wasReads
            DiskManagerForm.SuppressWelcome = wasWelcome
        End Try
    End Sub

    ' SP-0218, the listener's wait: a command that answers "still to come" (PendingCommand) is waited for on the listener's
    ' own thread, the window's thread asked a few times and never blocked. The answer goes out only once the thing waited for
    ' has held at two looks; past the bound the closing delegate (a close, for quit) is never asked and the answer is 2. The
    ' manifest here is the test's own, with a bound of 1.5 s, so the wait ends at the 1 s floor.
    Private Sub CheckAppCommandPending()
        Dim probe = CommandManifest.Parse("{""schemaVersion"":1,""product"":""T"",""cliVersion"":""1.0"",""commands"":[{""name"":""probe wait"",""risk"":""safe"",""coldStart"":""refuse"",""timeoutMs"":1500,""args"":[]}]}")
        Dim name = "FileDO_Commands_selftest_" & Guid.NewGuid().ToString("N")
        Dim order As New List(Of String)()
        Dim looks = 0
        Dim holdFor = 3
        Dim poll As Func(Of Boolean) =
            Function()
                order.Add("poll")
                looks += 1
                Return holdFor >= 0 AndAlso looks > holdFor
            End Function
        Dim finish As Func(Of CommandResponse) =
            Function()
                order.Add("done")
                Return CommandResponse.Done("ended", New Dictionary(Of String, Object) From {{"stopped", 1}})
            End Function
        Dim late As Func(Of CommandResponse) =
            Function()
                order.Add("timeout")
                Return CommandResponse.Failed(2, "timeout", "late", New Dictionary(Of String, Object) From {{"stopped", 1}})
            End Function
        Dim listener As CommandListener = Nothing
        Try
            listener = New CommandListener(name, probe,
                                           Function(r As CommandRequest) As CommandResponse
                                               looks = 0
                                               Return New CommandResponse With {.Pending = New PendingCommand With {.Poll = poll, .Done = finish, .TimedOut = late}}
                                           End Function)
        Catch ex As Exception
            Check("appcmd:pending-listener-starts", False, ex.Message)
            Return
        End Try
        Try
            Dim took As Long
            Dim answer = AppCmdExchange(name, AppCmdLine("probe wait", {}, id:="bbbbbbbb-0000-4000-8000-000000000001"), True, took)
            Dim polls = order.Where(Function(s) s = "poll").Count()
            Check("appcmd:job-stop-asks-then-waits", answer IsNot Nothing AndAlso answer.Contains("""status"":""done""") AndAlso answer.Contains("""stopped"":1") AndAlso
                  answer.Contains("bbbbbbbb-0000-4000-8000-000000000001") AndAlso polls = 5 AndAlso took >= 350, If(answer, "no answer") & " polls=" & polls.ToString() & " in " & took.ToString() & " ms")
            Check("appcmd:quit-stop-jobs-waits-then-closes", order.Count > 0 AndAlso order.Last() = "done" AndAlso order.Where(Function(s) s = "done").Count() = 1 AndAlso
                  order.Where(Function(s) s = "timeout").Count() = 0 AndAlso polls >= 2, String.Join(",", order.ToArray()))

            order.Clear()
            holdFor = -1
            answer = AppCmdExchange(name, AppCmdLine("probe wait", {}, id:="bbbbbbbb-0000-4000-8000-000000000002"), True, took)
            Check("appcmd:job-stop-past-the-bound", answer IsNot Nothing AndAlso answer.Contains("""status"":""failed""") AndAlso answer.Contains("""exitCode"":2") AndAlso
                  answer.Contains("""reason"":""timeout""") AndAlso answer.Contains("""stopped"":1") AndAlso took >= 900 AndAlso took < 2800, If(answer, "no answer") & " in " & took.ToString() & " ms")
            Check("appcmd:quit-stop-jobs-past-the-bound-closes-nothing", order.Where(Function(s) s = "done").Count() = 0 AndAlso order.Where(Function(s) s = "timeout").Count() = 1 AndAlso
                  order.Where(Function(s) s = "poll").Count() >= 5, String.Join(",", order.Distinct().ToArray()) & " polls=" & order.Where(Function(s) s = "poll").Count().ToString())
        Finally
            listener.StopListening(500)
        End Try
    End Sub

    ' SP-0217, the window verbs: each is a safe start verb of the manifest that waits for nothing; its request reads with
    ' --json and refuses anything else with 10 (no scoped option such as --group is declared); a modal window answers each
    ' of the three busy (11), before any window is touched. The launch spellings are the front's (appcmd_window_verbs_test.go).
    Private Sub CheckAppCommandWindowVerbs()
        Dim m = AppCommands.Manifest()
        Dim verbs = New String() {"disk-manager show", "settings show", "tray start"}
        For Each verb In verbs
            Dim spec = m.Find(verb)
            Check("appcmd:window-verb:" & verb.Replace(" ", "-") & ":declared", spec IsNot Nothing AndAlso spec.Risk = "safe" AndAlso spec.ColdStart = "start", If(spec Is Nothing, "missing", spec.Risk & "/" & spec.ColdStart))
        Next
        Dim r = AppCmdRead(AppCmdLine("settings show", {"--json"}))
        Check("appcmd:window-verb:settings-show:json", r.Status = "done" AndAlso r.Message = "accepted:settings show:json=true", r.Message)
        r = AppCmdRead(AppCmdLine("tray start", {"value"}))
        Check("appcmd:window-verb:tray-start:bare-value-refused", r.ExitCode = 10 AndAlso r.Reason = "bad-argument", r.Message)
        r = AppCmdRead(AppCmdLine("disk-manager show", {"--group", "x"}))
        Check("appcmd:window-verb:no-scoped-option", r.ExitCode = 10 AndAlso r.Reason = "bad-argument", r.Message)
        Dim seen As New List(Of String)()
        Using f As New Form With {.ShowInTaskbar = False, .StartPosition = FormStartPosition.Manual, .Location = New Point(-32000, -32000), .Size = New Size(120, 80), .Opacity = 0}
            AddHandler f.Shown, Sub()
                                    For Each name In verbs
                                        Dim request As CommandRequest = Nothing
                                        AppCommands.ReadRequest(AppCmdLine(name, {}), m, request)
                                        seen.Add(AppCommands.Execute(request).Status)
                                    Next
                                    f.BeginInvoke(New MethodInvoker(AddressOf f.Close))
                                End Sub
            ShowOwnedModal(f)
        End Using
        Check("appcmd:window-verbs-modal-is-busy", seen.Count = verbs.Length AndAlso seen.All(Function(s) s = "busy"), String.Join(",", seen))
    End Sub

    ' APP-BEHAVIOUR rule 1: a dialog always has an owner, in the self-test too. An invisible form off the screen
    ' stands in for the window the dialog would belong to; it is not modal, so it never counts as an open modal.
    Private Sub ShowOwnedModal(dialog As Form)
        Using owner As New Form With {.ShowInTaskbar = False, .FormBorderStyle = FormBorderStyle.None, .StartPosition = FormStartPosition.Manual, .Location = New Point(-32000, -32000), .Size = New Size(1, 1), .Opacity = 0}
            owner.Show()
            dialog.ShowDialog(owner)
        End Using
    End Sub

    Private Function AppCmdLine(command As String, args As String(), Optional schemaVersion As Integer = 1, Optional id As String = "11111111-2222-4333-8444-555555555555") As String
        Dim d As New Dictionary(Of String, Object) From {
            {"schemaVersion", schemaVersion}, {"command", command}, {"args", args},
            {"workingDir", "C:\"}, {"origin", "cli"}, {"requestId", id}, {"wait", "done"}}
        Return AppCommands.Json().Serialize(d)
    End Function

    Private Function AppCmdRead(line As String, Optional m As CommandManifest = Nothing) As CommandResponse
        Dim request As CommandRequest = Nothing
        Dim r = AppCommands.ReadRequest(line, If(m, AppCommands.Manifest()), request)
        If r Is Nothing Then r = CommandResponse.Done("accepted:" & request.Command & ":" & String.Join(",", request.Options.Select(Function(kv) kv.Key & "=" & kv.Value).ToArray()))
        Return r
    End Function

    ' C1, the first instance's half: the manifest is embedded, parses, holds the five base verbs with a cold-start
    ' mode each, and - run from a checkout - is the bytes of docs/cli-commands.json.
    Private Sub CheckAppCommandManifest()
        Dim m = AppCommands.Manifest()
        Check("appcmd:manifest-embedded", AppCommands.ManifestText().Length > 0 AndAlso m.SchemaVersion = 1 AndAlso m.Product = "FileDO" AndAlso m.CliVersion <> "", m.CliVersion)
        For Each verb In New String() {"commands", "version", "status", "show", "quit"}
            Dim spec = m.Find(verb)
            Check("appcmd:base-verb:" & verb, spec IsNot Nothing AndAlso spec.Risk = "safe" AndAlso
                  New String() {"start", "oneshot", "refuse"}.Contains(spec.ColdStart), If(spec Is Nothing, "missing", spec.ColdStart))
        Next
        Check("appcmd:cold-start-declared", m.Commands.All(Function(c) New String() {"start", "oneshot", "refuse"}.Contains(c.ColdStart)), "")
        Dim dir = AppDomain.CurrentDomain.BaseDirectory
        For i = 0 To 6
            If dir Is Nothing Then Exit For
            Dim candidate = Path.Combine(dir, "docs", "cli-commands.json")
            If File.Exists(candidate) Then
                Dim onDisk = File.ReadAllText(candidate, New UTF8Encoding(False)).Replace(vbCrLf, vbLf)
                Check("appcmd:manifest-equals-docs-file", onDisk = AppCommands.ManifestText().Replace(vbCrLf, vbLf), candidate)
                Exit For
            End If
            dir = Path.GetDirectoryName(dir.TrimEnd(Path.DirectorySeparatorChar))
        Next
    End Sub

    ' C4: unknown verb, unknown option, oversized, malformed, newer schema - each a refusal with its exit code,
    ' nothing thrown, and the request's id repeated.
    Private Sub CheckAppCommandParser()
        Dim r = AppCmdRead(AppCmdLine("frobnicate", {}))
        Check("appcmd:unknown-verb", r.ExitCode = 10 AndAlso r.Reason = "unknown-verb" AndAlso r.RequestId = "11111111-2222-4333-8444-555555555555", r.Reason)
        r = AppCmdRead(AppCmdLine("ui", {}))
        Check("appcmd:alias-is-not-a-command", r.ExitCode = 10 AndAlso r.Reason = "unknown-verb", r.Reason)
        r = AppCmdRead(AppCmdLine("status", {"--bogus"}))
        Check("appcmd:unknown-option", r.ExitCode = 10 AndAlso r.Reason = "bad-argument" AndAlso r.Status = "refused", r.Message)
        r = AppCmdRead(AppCmdLine("status", {"--C:\Users\x"}))
        Check("appcmd:message-carries-no-path", r.ExitCode = 10 AndAlso Not r.Message.Contains("Users"), r.Message)
        r = AppCmdRead(AppCmdLine("status", {"value"}))
        Check("appcmd:bare-value-refused", r.ExitCode = 10 AndAlso r.Reason = "bad-argument", r.Message)
        r = AppCmdRead(AppCmdLine("STATUS", {"--JSON"}))
        Check("appcmd:case-insensitive-verb-and-option", r.Status = "done" AndAlso r.Message = "accepted:status:json=true", r.Message)
        r = AppCmdRead(AppCmdLine("status", {"--json=false", "--"}))
        Check("appcmd:bool-with-value", r.Status = "done" AndAlso r.Message = "accepted:status:json=false", r.Message)
        r = AppCmdRead(AppCmdLine("status", {"--json", "--json"}))
        Check("appcmd:option-twice", r.ExitCode = 10, r.Message)
        r = AppCmdRead(AppCmdLine("status", {New String("x"c, 33 * 1024)}))
        Check("appcmd:argument-too-large", r.ExitCode = 10 AndAlso r.Reason = "too-large", r.Reason)
        r = AppCmdRead(AppCmdLine("status", {New String("x"c, 30 * 1024), New String("y"c, 30 * 1024), New String("z"c, 6 * 1024)}))
        Check("appcmd:request-too-large", r.ExitCode = 10 AndAlso r.Reason = "too-large", r.Reason)
        For Each bad In New String() {"", "   ", "{", "[]", "null", "{""schemaVersion"":1,""command"":""status"",""args"":""--json""}", "{""command"":""status""}", "{""schemaVersion"":""1"",""command"":""status""}"}
            Dim threw = False
            Try
                r = AppCmdRead(bad)
            Catch ex As Exception
                threw = True
            End Try
            Check("appcmd:malformed-refused:" & bad.Length.ToString(), Not threw AndAlso r.ExitCode = 10 AndAlso r.Reason = "bad-argument", bad)
        Next
        r = AppCmdRead(AppCmdLine("status", {}, schemaVersion:=2))
        Check("appcmd:newer-schema", r.ExitCode = 2 AndAlso r.Reason = "schema" AndAlso r.Status = "refused", r.Reason)
        r = AppCmdRead("{""schemaVersion"":1,""command"":""status"",""args"":[]}")
        Check("appcmd:missing-request-id-refused", r.ExitCode = 10 AndAlso r.Reason = "bad-argument" AndAlso r.RequestId = "", r.Reason & ":" & r.RequestId)

        ' The kinds of rule 18 on a manifest of the test's own.
        Dim probe = CommandManifest.Parse("{""schemaVersion"":1,""product"":""T"",""cliVersion"":""1.0"",""commands"":[{""name"":""probe run"",""risk"":""safe"",""coldStart"":""refuse"",""timeoutMs"":1000,""args"":[" &
                                          "{""name"":""count"",""kind"":""int"",""required"":true},{""name"":""mode"",""kind"":""enum"",""values"":[""fast"",""slow""]},{""name"":""out"",""kind"":""path""}]}]}")
        r = AppCmdRead(AppCmdLine("probe run", {"--count", "3", "--mode=fast", "--out", "C:\a b.txt"}), probe)
        Check("appcmd:kinds-accepted", r.Status = "done" AndAlso r.Message.Contains("count=3") AndAlso r.Message.Contains("mode=fast") AndAlso r.Message.Contains("out=C:\a b.txt"), r.Message)
        r = AppCmdRead(AppCmdLine("probe run", {"--count", "x"}), probe)
        Check("appcmd:int-refused", r.ExitCode = 10, r.Message)
        r = AppCmdRead(AppCmdLine("probe run", {"--count", "1", "--mode", "Fast"}), probe)
        Check("appcmd:enum-value-is-case-sensitive", r.ExitCode = 10, r.Message)
        r = AppCmdRead(AppCmdLine("probe run", {"--mode", "slow"}), probe)
        Check("appcmd:required-missing", r.ExitCode = 10, r.Message)
        r = AppCmdRead(AppCmdLine("probe run", {"--count"}), probe)
        Check("appcmd:value-missing", r.ExitCode = 10, r.Message)

        ' The response line of rule 7.
        Dim done = CommandResponse.Done("ok")
        done.RequestId = "abc"
        Dim back = New JavaScriptSerializer().Deserialize(Of Dictionary(Of String, Object))(done.ToLine())
        Check("appcmd:response-line", CInt(back("schemaVersion")) = 1 AndAlso CStr(back("status")) = "done" AndAlso CInt(back("exitCode")) = 0 AndAlso
              CStr(back("requestId")) = "abc" AndAlso Not back.ContainsKey("reason") AndAlso Not done.ToLine().Contains(vbLf), done.ToLine())
        Dim busy = CommandResponse.Busy("b")
        Check("appcmd:busy-is-11", busy.ExitCode = 11 AndAlso busy.Reason = "busy" AndAlso busy.Status = "busy", "")
    End Sub

    ' C8: the setting of rule 13 off - a verb of any class but `safe` is refused 12, switch-off, with the
    ' setting named; the `safe` verbs still pass; the setting is off by default.
    Private Sub CheckAppCommandGate()
        Dim saved = ShellSettings.ValuesForTest
        ShellSettings.ValuesForTest = New Dictionary(Of String, Object)()
        Try
            Check("appcmd:setting-off-by-default", Not ShellSettings.AcceptsRestrictedCommands(), "")
            Dim m = CommandManifest.Parse("{""schemaVersion"":1,""commands"":[{""name"":""probe read"",""risk"":""reads-user-data"",""coldStart"":""refuse""},{""name"":""probe write"",""risk"":""writes-user-data"",""coldStart"":""refuse""},{""name"":""probe send"",""risk"":""leaves-machine"",""coldStart"":""refuse""},{""name"":""probe odd"",""risk"":""unheard-of"",""coldStart"":""refuse""},{""name"":""probe safe"",""risk"":""safe"",""coldStart"":""refuse""},{""name"":""probe never"",""risk"":""writes-user-data"",""forward"":""never"",""coldStart"":""oneshot""},{""name"":""probe legacy"",""risk"":""writes-user-data"",""coldStart"":""refuse"",""aliases"":[{""spelling"":""--old"",""legacy"":true}]}]}")
            For Each name In New String() {"probe read", "probe write", "probe send", "probe odd"}
                Dim r = AppCommands.Gate(m.Find(name))
                Check("appcmd:switch-off:" & name, r IsNot Nothing AndAlso r.ExitCode = 12 AndAlso r.Reason = "switch-off" AndAlso
                      r.Message.Contains(Localization.T("settings_commands_restricted")), If(r Is Nothing, "accepted", r.Message))
            Next
            Check("appcmd:switch-off:safe-passes", AppCommands.Gate(m.Find("probe safe")) Is Nothing, "")
            Check("appcmd:switch-off:forward-never-passes", AppCommands.Gate(m.Find("probe never")) Is Nothing, "")
            Check("appcmd:legacy-carrier-validated", AppCommands.Gate(m.Find("probe legacy"), True) Is Nothing AndAlso
                  AppCommands.Gate(m.Find("probe write"), True) IsNot Nothing, "")
            Check("appcmd:switch-off:shipped-verbs-pass", AppCommands.Manifest().Commands.All(Function(c) AppCommands.Gate(c) Is Nothing), "")
            ShellSettings.SetAcceptsRestrictedCommands(True)
            Check("appcmd:switch-on:restricted-passes", AppCommands.Gate(m.Find("probe read")) Is Nothing AndAlso ShellSettings.AcceptsRestrictedCommands(), "")
        Finally
            ShellSettings.ValuesForTest = saved
        End Try
    End Sub

    ' C6: a modal window open is busy; no host (the program closing) is busy.
    Private Sub CheckAppCommandBusy()
        Check("appcmd:no-modal-at-rest", Not AppCommands.ModalOpen(), "")
        Dim seen = False
        Using f As New Form With {.ShowInTaskbar = False, .StartPosition = FormStartPosition.Manual, .Location = New Point(-32000, -32000), .Size = New Size(120, 80), .Opacity = 0}
            AddHandler f.Shown, Sub()
                                    seen = AppCommands.ModalOpen()
                                    f.BeginInvoke(New MethodInvoker(AddressOf f.Close))
                                End Sub
            ShowOwnedModal(f)
        End Using
        Check("appcmd:modal-is-busy", seen, "")
        If AppHost.Current Is Nothing Then
            Dim request As CommandRequest = Nothing
            AppCommands.ReadRequest(AppCmdLine("status", {}), AppCommands.Manifest(), request)
            Dim r = AppCommands.Execute(request)
            Check("appcmd:closing-is-busy", r.ExitCode = 11 AndAlso r.Status = "busy", r.Status)
        End If
    End Sub

    ' One request and one answer over a real pipe; `pump` is whether this (the window's) thread pumps meanwhile.
    Private Function AppCmdExchange(pipeName As String, line As String, pump As Boolean, ByRef elapsedMs As Long) As String
        Dim answer As String = Nothing
        Dim took As Long = -1
        Dim client As New Thread(Sub()
                                     Try
                                         Using c As New NamedPipeClientStream(".", pipeName, PipeDirection.InOut)
                                             c.Connect(1000)
                                             Dim sw = Diagnostics.Stopwatch.StartNew()
                                             Dim bytes = Encoding.UTF8.GetBytes(line & vbLf)
                                             c.Write(bytes, 0, bytes.Length)
                                             c.Flush()
                                             Using reader As New StreamReader(c, New UTF8Encoding(False), False, 4096, True)
                                                 answer = reader.ReadLine()
                                             End Using
                                             took = sw.ElapsedMilliseconds
                                         End Using
                                     Catch ex As Exception
                                         answer = "client: " & ex.Message
                                     End Try
                                 End Sub) With {.IsBackground = True}
        client.Start()
        Dim deadline = DateTime.UtcNow.AddSeconds(8)
        If Not pump Then Thread.Sleep(1500)
        While client.IsAlive AndAlso DateTime.UtcNow < deadline
            Application.DoEvents()
            Thread.Sleep(10)
        End While
        ' Whatever the window's thread was given and gave up meanwhile runs now, and must do nothing.
        For i = 1 To 20
            Application.DoEvents()
            Thread.Sleep(5)
        Next
        elapsedMs = took
        Return answer
    End Function

    ' C3 and C5 at the listener: a command runs once on the window's thread and its answer comes back on the same
    ' connection; a window thread that does not pump is answered busy inside the bound and the command is not run
    ' afterwards; an oversized or malformed line is refused and the next command still works.
    Private Sub CheckAppCommandListener()
        Dim name = "FileDO_Commands_selftest_" & Guid.NewGuid().ToString("N")
        Dim calls = 0
        Dim listener As CommandListener = Nothing
        Try
            listener = New CommandListener(name, AppCommands.Manifest(),
                                           Function(r As CommandRequest) As CommandResponse
                                               Interlocked.Increment(calls)
                                               Return CommandResponse.Done("ok", New Dictionary(Of String, Object) From {{"pid", 7}})
                                           End Function)
        Catch ex As Exception
            Check("appcmd:listener-starts", False, ex.Message)
            Return
        End Try
        Try
            Dim took As Long
            Dim answer = AppCmdExchange(name, AppCmdLine("status", {}, id:="aaaaaaaa-0000-4000-8000-000000000001"), True, took)
            Check("appcmd:pipe-roundtrip", answer IsNot Nothing AndAlso answer.Contains("""status"":""done""") AndAlso
                  answer.Contains("aaaaaaaa-0000-4000-8000-000000000001") AndAlso answer.Contains("""pid"":7") AndAlso calls = 1, If(answer, "no answer"))

            answer = AppCmdExchange(name, AppCmdLine("status", {}, id:="aaaaaaaa-0000-4000-8000-000000000002"), False, took)
            Check("appcmd:pipe-busy-when-window-thread-does-not-pump", answer IsNot Nothing AndAlso answer.Contains("""status"":""busy""") AndAlso
                  answer.Contains("""exitCode"":11") AndAlso took >= 0 AndAlso took < 1400, If(answer, "no answer") & " in " & took.ToString() & " ms")
            Check("appcmd:pipe-busy-command-not-run-later", calls = 1, "calls=" & calls.ToString())

            answer = AppCmdExchange(name, New String("x"c, 65 * 1024), True, took)
            Check("appcmd:pipe-too-large", answer IsNot Nothing AndAlso answer.Contains("""reason"":""too-large""") AndAlso answer.Contains("""exitCode"":10"), If(answer, "no answer"))
            answer = AppCmdExchange(name, "not json", True, took)
            Check("appcmd:pipe-malformed", answer IsNot Nothing AndAlso answer.Contains("""exitCode"":10") AndAlso answer.Contains("""requestId"":"""""), If(answer, "no answer"))
            Dim bigName = "FileDO_Commands_selftest_" & Guid.NewGuid().ToString("N")
            Dim oversizeManifest = CommandManifest.Parse("{""schemaVersion"":1,""product"":""T"",""cliVersion"":""1.0"",""commands"":[{""name"":""status"",""risk"":""safe"",""coldStart"":""refuse""}]}")
            Dim probeListener = New CommandListener(bigName, oversizeManifest,
                                                    Function(r As CommandRequest) As CommandResponse
                                                        Return CommandResponse.Done("ok", New Dictionary(Of String, Object) From {{"blob", New String("z"c, 70 * 1024)}})
                                                    End Function)
            Try
                Dim bigAns = AppCmdExchange(bigName, AppCmdLine("status", {}, id:="aaaaaaaa-0000-4000-8000-000000000003"), True, took)
                Check("appcmd:pipe-response-too-large", bigAns IsNot Nothing AndAlso bigAns.Contains("""exitCode"":1") AndAlso bigAns.Contains("""reason"":""response-too-large"""), If(bigAns, "no answer"))
            Finally
                probeListener.StopListening(500)
            End Try
            answer = AppCmdExchange(name, AppCmdLine("status", {"--bogus"}), True, took)
            Check("appcmd:pipe-unknown-option", answer IsNot Nothing AndAlso answer.Contains("""exitCode"":10") AndAlso calls = 1, If(answer, "no answer"))
            answer = AppCmdExchange(name, AppCmdLine("status", {}), True, took)
            Check("appcmd:pipe-status-after-refusals", answer IsNot Nothing AndAlso answer.Contains("""status"":""done""") AndAlso calls = 2, If(answer, "no answer"))
        Finally
            listener.StopListening(500)
        End Try
        Dim gone = False
        Try
            Using c As New NamedPipeClientStream(".", name, PipeDirection.InOut)
                c.Connect(200)
            End Using
        Catch ex As Exception
            gone = True
        End Try
        Check("appcmd:pipe-closed-after-stop", gone, "")
    End Sub

    ' Rule 16: one line per command - verb, origin, caller, outcome - with a credential never in it.
    Private Sub CheckAppCommandLog()
        Dim request As New CommandRequest With {.Command = "status", .Origin = "cli", .Args = New List(Of String) From {"p:hunter2secret", "--json"}}
        Dim line = AppCommands.LogLine(request, 4321, CommandResponse.Done("ok"))
        Check("appcmd:log-line", line.StartsWith("app command status", StringComparison.Ordinal) AndAlso line.Contains("origin=cli") AndAlso
              line.Contains("caller=4321") AndAlso line.Contains("outcome=done") AndAlso line.Contains("exit=0"), line)
        Check("appcmd:log-redacted", Not line.Contains("hunter2secret"), line)
        Dim refused = AppCommands.LogLine(Nothing, 0, CommandResponse.Refused(10, "bad-argument", "x"))
        Check("appcmd:log-unread-request", refused.Contains("reason=bad-argument") AndAlso refused.Contains("outcome=refused"), refused)
        Check("appcmd:pipe-name-shape", Regex.IsMatch(AppCommands.PipeName(), "^FileDO_Commands_S-1-[0-9-]+_[0-9]+$"), AppCommands.PipeName())
        Using self = Process.GetCurrentProcess()
            Check("appcmd:same-elevation-is-not-refused", Not CommandListener.ElevatedOver(self.Id), "")
        End Using
    End Sub

    ' C13 for filedo_win.exe: the shipped start spellings that the manifest lists as aliases of `show` (and the
    ' --disks family the window verbs will list) decide exactly as before.
    Private Sub CheckAppCommandFrozenStarts()
        Dim show = AppCommands.Manifest().Find("show")
        Dim aliases = If(show Is Nothing, New List(Of CommandAliasSpec)(), show.Aliases)
        For Each spelling In New String() {"", "--startup", "--legacy-builder", "ui", "gui", "-ui"}
            Check("appcmd:alias-listed:" & If(spelling = "", "(empty)", spelling),
                  aliases.Any(Function(a) a.Spelling = spelling AndAlso a.Legacy AndAlso Not a.Answers), "")
        Next
        Dim missingFolder = Path.Combine(Path.GetTempPath(), "filedo_missing_out_" & Guid.NewGuid().ToString("N"))
        Dim missingCode = Program.RunToolingForTest({"filedo_win.exe", "help-pages", "build", "--out", missingFolder})
        Check("appcmd:tooling-missing-folder-refused", missingCode = 11 AndAlso Not Directory.Exists(missingFolder), "code=" & missingCode.ToString())
        Dim exe = "filedo_win.exe"
        Check("appcmd:start:bare", Program.StartupTargetFrom({exe}) Is Nothing AndAlso Not Program.WantsDiskManager({exe}) AndAlso Not StartupLaunch.HiddenStart({exe}), "")
        Check("appcmd:start:--startup", Program.StartupTargetFrom({exe, "--startup"}) Is Nothing AndAlso Not Program.WantsDiskManager({exe, "--startup"}) AndAlso Not StartupLaunch.HiddenStart({exe, "--startup"}), "")
        Check("appcmd:start:--legacy-builder", Program.StartupTargetFrom({exe, "--legacy-builder"}) Is Nothing AndAlso Not Program.WantsDiskManager({exe, "--legacy-builder"}), "")
        Check("appcmd:start:--disks", Program.WantsDiskManager({exe, "--disks"}) AndAlso Program.WantsDiskManager({exe, "--DISKS"}) AndAlso Program.StartupTargetFrom({exe, "--disks"}) Is Nothing, "")
        Check("appcmd:start:--startup+--disks", Program.WantsDiskManager({exe, "--startup", "--disks"}) AndAlso Not StartupLaunch.HiddenStart({exe, "--startup", "--disks"}), "")
        Check("appcmd:start:--startup+--tray", StartupLaunch.HiddenStart({exe, "--startup", "--tray"}) AndAlso Not Program.WantsDiskManager({exe, "--startup", "--tray"}), "")
        Check("appcmd:start:--tray-alone", Not StartupLaunch.HiddenStart({exe, "--tray"}), "")
        Check("appcmd:start:second-plain-start-hands-over", Program.WantsHandOver(Nothing, False) AndAlso Not Program.WantsHandOver(Nothing, True), "")
        Check("appcmd:start:messages-registered", AppHost.ShowShellMessage <> 0 AndAlso AppHost.ShowDisksMessage <> 0, "")
    End Sub

    ' SP-0228 / C13 for the container-file start of filedo_win.exe: `open --path <file> [--mode <mount|mount-ro|unmount>]`
    ' refuses usage errors with 10, missing files with 11, accepts valid starts in all three modes, and leaves bare-path
    ' starts untouched.
    Private Sub CheckAppCommandContainerOpen()
        Dim target As String = Nothing
        Dim mode As DiskRoute.StartMode = DiskRoute.StartMode.None
        Check("appcmd:open-bare-falls-through", Program.ParseOpenForTest({"filedo_win.exe", "notes.txt"}, target, mode) = -1, "")
        Check("appcmd:open-missing-path-is-10", Program.ParseOpenForTest({"filedo_win.exe", "open"}, target, mode) = 10, "")
        Check("appcmd:open-unknown-option-is-10", Program.ParseOpenForTest({"filedo_win.exe", "open", "--bogus"}, target, mode) = 10, "")
        Check("appcmd:open-bare-value-is-10", Program.ParseOpenForTest({"filedo_win.exe", "open", "a.fdd"}, target, mode) = 10, "")
        Dim missing = Path.Combine(Path.GetTempPath(), "filedo_open_missing_" & Guid.NewGuid().ToString("N") & ".fdd")
        Check("appcmd:open-missing-file-is-11", Program.ParseOpenForTest({"filedo_win.exe", "open", "--path", missing}, target, mode) = 11, "")

        Dim sample = Path.Combine(Path.GetTempPath(), "filedo_open_sample_" & Guid.NewGuid().ToString("N") & ".fdd")
        File.WriteAllText(sample, "x")
        Try
            Dim code = Program.ParseOpenForTest({"filedo_win.exe", "open", "--path", sample}, target, mode)
            Check("appcmd:open-default-mount", code = 0 AndAlso String.Equals(target, Path.GetFullPath(sample), StringComparison.OrdinalIgnoreCase) AndAlso mode = DiskRoute.StartMode.Mount, code.ToString())
            code = Program.ParseOpenForTest({"filedo_win.exe", "OPEN", "--PATH=" & sample, "--MODE=mount-ro"}, target, mode)
            Check("appcmd:open-mode-mount-ro", code = 0 AndAlso mode = DiskRoute.StartMode.MountReadOnly, code.ToString())
            code = Program.ParseOpenForTest({"filedo_win.exe", "open", "--path", sample, "--mode", "unmount"}, target, mode)
            Check("appcmd:open-mode-unmount", code = 0 AndAlso mode = DiskRoute.StartMode.Unmount, code.ToString())
            Check("appcmd:open-mode-case-sensitive", Program.ParseOpenForTest({"filedo_win.exe", "open", "--path", sample, "--mode", "Mount-Ro"}, target, mode) = 10, "")
            Check("appcmd:open-mode-unknown-is-10", Program.ParseOpenForTest({"filedo_win.exe", "open", "--path", sample, "--mode", "bogus"}, target, mode) = 10, "")
        Finally
            Try
                If File.Exists(sample) Then File.Delete(sample)
            Catch
            End Try
        End Try

        Dim doc = AppCommands.Json().Deserialize(Of Dictionary(Of String, Object))(AppCommands.ManifestText())
        Dim openEntry = PartJson.Items(doc, "commands").FirstOrDefault(Function(c) PartJson.Str(c, "name") = "open")
        Check("appcmd:open-manifest-metadata", openEntry IsNot Nothing AndAlso
              PartJson.Str(openEntry, "exe") = "filedo_win.exe" AndAlso
              PartJson.Str(openEntry, "defaultFor") = "filedo_win.exe" AndAlso
              PartJson.Str(openEntry, "forward") = "never" AndAlso
              PartJson.Str(openEntry, "coldStart") = "oneshot", "")
        Dim aliasList = If(openEntry Is Nothing, Nothing, TryCast(PartJson.Value(openEntry, "aliases"), IEnumerable))
        Dim spellings As New List(Of String)()
        If aliasList IsNot Nothing Then
            For Each rawA In aliasList
                Dim ad = TryCast(rawA, Dictionary(Of String, Object))
                If ad IsNot Nothing AndAlso PartJson.Bool(ad, "legacy") AndAlso Object.Equals(PartJson.Value(ad, "answers"), False) Then
                    spellings.Add(PartJson.Str(ad, "spelling"))
                End If
            Next
        End If
        Check("appcmd:open-legacy-aliases", New String() {"{path}", "--mount {path}", "--mount-ro {path}", "--unmount {path}"}.All(Function(s) spellings.Contains(s)), String.Join(",", spellings))
    End Sub

End Module
