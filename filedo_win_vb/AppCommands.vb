Imports System.IO
Imports System.Reflection
Imports System.Security.Principal
Imports System.Text
Imports System.Text.RegularExpressions
Imports System.Web.Script.Serialization

' The app commands of FileDO (APP-COMMANDS 0.1): what the running filedo_win.exe does when another program
' tells it to. filedo.exe is the second process (rule 9): it carries a verb of the manifest here over the
' command pipe (CommandListener.vb) and exits with the answer's exit code. This side holds the grammar - the
' second process forwards `command` and the raw `args` (rule 7) and this module reads them with the one
' manifest-driven parser - and the answer vocabulary of rules 11 and 12.
'
' The manifest is docs/cli-commands.json, embedded as it ships; filedo.exe embeds the same bytes and a test
' holds the two equal (case C1). Every verb of this version is `safe`; the gate of rule 13 is built and tested
' and refuses nothing yet. Its one setting lives in Settings, is off until the person turns it on, and no
' command changes it (ShellSettings.SetAcceptsRestrictedCommands has the Settings page as its only caller).

' One argument of a manifest command (rule 18: name, kind, required, values).
Friend NotInheritable Class CommandArgSpec
    Friend Name As String = ""
    Friend Kind As String = "string"
    Friend Required As Boolean
    Friend ReadOnly Values As New List(Of String)
End Class

' One alias of a manifest command (APP-COMMANDS 0.2 item E).
Friend NotInheritable Class CommandAliasSpec
    Friend Spelling As String = ""
    Friend Exe As String = ""
    Friend Legacy As Boolean
    Friend Answers As Boolean = True
End Class

' One command of the manifest, with what the listener needs of it.
Friend NotInheritable Class CommandSpec
    Friend Name As String = ""
    Friend Risk As String = ""
    Friend Audience As String = ""
    Friend Forward As String = ""
    Friend ColdStart As String = ""
    Friend TimeoutMs As Integer
    Friend ReadOnly Args As New List(Of CommandArgSpec)
    Friend ReadOnly Aliases As New List(Of CommandAliasSpec)

    Friend ReadOnly Property IsForwardNever As Boolean
        Get
            Return String.Equals(Forward, "never", StringComparison.OrdinalIgnoreCase)
        End Get
    End Property

    Friend Function HasLegacyAlias() As Boolean
        Return Aliases.Any(Function(a) a.Legacy)
    End Function

    ' Rule 8: the completion bound, 10 000 ms when the manifest states none, 600 000 at most.
    Friend ReadOnly Property CompletionMs As Integer
        Get
            If TimeoutMs <= 0 Then Return 10000
            Return Math.Min(TimeoutMs, 600000)
        End Get
    End Property
End Class

Friend NotInheritable Class CommandManifest
    Friend SchemaVersion As Integer
    Friend Product As String = ""
    Friend CliVersion As String = ""
    Friend ReadOnly Commands As New List(Of CommandSpec)
    Friend Raw As Dictionary(Of String, Object)

    ' The canonical verb, compared without regard to case (rule 1); never an alias (rule 7).
    Friend Function Find(command As String) As CommandSpec
        If String.IsNullOrEmpty(command) Then Return Nothing
        Return Commands.FirstOrDefault(Function(c) String.Equals(c.Name, command, StringComparison.OrdinalIgnoreCase))
    End Function

    Friend Shared Function Parse(json As String) As CommandManifest
        Dim doc = AppCommands.Json().Deserialize(Of Dictionary(Of String, Object))(json)
        Dim m As New CommandManifest With {
            .Raw = doc,
            .SchemaVersion = CInt(PartJson.Num(doc, "schemaVersion")),
            .Product = PartJson.Str(doc, "product"),
            .CliVersion = PartJson.Str(doc, "cliVersion")}
        Dim productExe = PartJson.Str(doc, "exe")
        If productExe = "" Then productExe = "filedo.exe"
        For Each c In PartJson.Items(doc, "commands")
            ' APP-COMMANDS rule 18 `exe`: a verb of another executable of the product (the developer tooling of the window
            ' program, SP-0219) is started by that executable from its own command line; this pipe never answers it.
            Dim exe = PartJson.Str(c, "exe")
            If exe <> "" AndAlso Not String.Equals(exe, "filedo.exe", StringComparison.OrdinalIgnoreCase) Then Continue For
            Dim verbExe = If(exe <> "", exe, productExe)
            Dim spec As New CommandSpec With {
                .Name = PartJson.Str(c, "name"),
                .Risk = PartJson.Str(c, "risk"),
                .Audience = PartJson.Str(c, "audience"),
                .Forward = PartJson.Str(c, "forward"),
                .ColdStart = PartJson.Str(c, "coldStart"),
                .TimeoutMs = CInt(PartJson.Num(c, "timeoutMs"))}
            Dim rawAliases = TryCast(PartJson.Value(c, "aliases"), IEnumerable)
            If rawAliases IsNot Nothing Then
                For Each rawA In rawAliases
                    Dim s = TryCast(rawA, String)
                    If s IsNot Nothing Then
                        spec.Aliases.Add(New CommandAliasSpec With {.Spelling = s, .Exe = verbExe, .Legacy = False, .Answers = True})
                        Continue For
                    End If
                    Dim ad = TryCast(rawA, Dictionary(Of String, Object))
                    If ad Is Nothing Then Continue For
                    Dim aExe = PartJson.Str(ad, "exe")
                    Dim ansVal = PartJson.Value(ad, "answers")
                    Dim answers = True
                    If TypeOf ansVal Is Boolean Then answers = CBool(ansVal)
                    spec.Aliases.Add(New CommandAliasSpec With {
                        .Spelling = PartJson.Str(ad, "spelling"),
                        .Exe = If(aExe <> "", aExe, verbExe),
                        .Legacy = PartJson.Bool(ad, "legacy"),
                        .Answers = answers})
                Next
            End If
            For Each a In PartJson.Items(c, "args")
                Dim arg As New CommandArgSpec With {
                    .Name = PartJson.Str(a, "name"),
                    .Kind = PartJson.Str(a, "kind"),
                    .Required = PartJson.Bool(a, "required")}
                Dim values = TryCast(PartJson.Value(a, "values"), IEnumerable)
                If values IsNot Nothing Then
                    For Each v In values
                        Dim s = TryCast(v, String)
                        If s IsNot Nothing Then arg.Values.Add(s)
                    Next
                End If
                spec.Args.Add(arg)
            Next
            m.Commands.Add(spec)
        Next
        Return m
    End Function
End Class

' A request the parser accepted.
Friend NotInheritable Class CommandRequest
    Friend Spec As CommandSpec
    Friend Command As String = ""
    Friend Args As New List(Of String)
    Friend Options As New Dictionary(Of String, String)(StringComparer.OrdinalIgnoreCase)
    Friend RequestId As String = ""
    Friend WorkingDir As String = ""
    Friend Origin As String = ""
    Friend Legacy As Boolean
End Class

' An answer that is still to come (SP-0218). The command's work was started on the window's thread, and what it waits
' for - runs that end through continuations of that same thread - cannot be waited for there. The listener waits on
' its own thread and asks `Poll` on the window's thread until it holds, then gives `Done`; past its bound it gives
' `TimedOut`. Every delegate runs on the window's thread, one at a time, and never blocks it.
Friend NotInheritable Class PendingCommand
    ' True when nothing is left to wait for.
    Friend Poll As Func(Of Boolean)
    ' The answer once `Poll` held (twice running, 100 ms apart, so the pages have shown what the runs left).
    Friend Done As Func(Of CommandResponse)
    ' The answer when the bound passed first.
    Friend TimedOut As Func(Of CommandResponse)
End Class

' The response line of rule 7.
Friend NotInheritable Class CommandResponse
    Friend RequestId As String = ""
    Friend Status As String = ""
    Friend ExitCode As Integer
    Friend Reason As String = ""
    Friend Message As String = ""
    Friend Data As Dictionary(Of String, Object)
    ' Set only by a command that waits (job stop, quit --stop-jobs): the listener resolves it before answering, and it
    ' is never part of the response line.
    Friend Pending As PendingCommand

    Friend Shared Function Done(message As String, Optional data As Dictionary(Of String, Object) = Nothing) As CommandResponse
        Return New CommandResponse With {.Status = "done", .ExitCode = 0, .Message = message, .Data = data}
    End Function

    Friend Shared Function Refused(code As Integer, reason As String, message As String) As CommandResponse
        Return New CommandResponse With {.Status = "refused", .ExitCode = code, .Reason = reason, .Message = message}
    End Function

    Friend Shared Function Busy(message As String) As CommandResponse
        Return New CommandResponse With {.Status = "busy", .ExitCode = 11, .Reason = "busy", .Message = message}
    End Function

    Friend Shared Function Failed(code As Integer, reason As String, message As String, Optional data As Dictionary(Of String, Object) = Nothing) As CommandResponse
        Return New CommandResponse With {.Status = "failed", .ExitCode = code, .Reason = reason, .Message = message, .Data = data}
    End Function

    Friend Function ToLine() As String
        Dim d As New Dictionary(Of String, Object) From {
            {"schemaVersion", 1}, {"requestId", If(RequestId, "")}, {"status", Status}, {"exitCode", ExitCode}}
        If Not String.IsNullOrEmpty(Reason) AndAlso Status <> "done" AndAlso Status <> "accepted" Then d("reason") = Reason
        If Not String.IsNullOrEmpty(Message) Then d("message") = Message
        If Data IsNot Nothing Then d("data") = Data
        Return AppCommands.Json().Serialize(d)
    End Function
End Class

Friend Module AppCommands

    ' Rule 3: a request is at most 64 KiB as UTF-8, one argument at most 32 KiB.
    Friend Const MaxRequestBytes As Integer = 64 * 1024
    Friend Const MaxArgBytes As Integer = 32 * 1024

    ' The reader and writer of the request, the response and the manifest, with no length cap of its own: the
    ' bounds of rule 3 are checked before anything is read.
    Friend Function Json() As JavaScriptSerializer
        Return New JavaScriptSerializer() With {.MaxJsonLength = Integer.MaxValue}
    End Function

    Private Const ManifestResource As String = "FileDOGUI.cli-commands.json"
    Private manifestValue As CommandManifest
    Private ReadOnly manifestGate As New Object()

    ' The pipe of APP-ACTIVATION rule 3, third shape: `<App>_<purpose>_<userSid>_<sessionId>`. filedo.exe
    ' builds the same name (appcmd_pipe_windows.go). Once shipped it is a frozen anchor.
    Friend Function PipeName() As String
        Dim sid = "unknown"
        Try
            Using id = WindowsIdentity.GetCurrent()
                If id.User IsNot Nothing Then sid = id.User.Value
            End Using
        Catch ex As Exception
            ShellLog.Write("read the user for the command pipe", ex)
        End Try
        Dim session = 0
        Try
            Using p = Process.GetCurrentProcess()
                session = p.SessionId
            End Using
        Catch ex As Exception
            ShellLog.Write("read the session for the command pipe", ex)
        End Try
        Return "FileDO_Commands_" & sid & "_" & session.ToString(Globalization.CultureInfo.InvariantCulture)
    End Function

    ' The embedded manifest, exactly the bytes of docs/cli-commands.json.
    Friend Function ManifestText() As String
        Using s = Assembly.GetExecutingAssembly().GetManifestResourceStream(ManifestResource)
            If s Is Nothing Then Return ""
            Using r As New StreamReader(s, New UTF8Encoding(False))
                Return r.ReadToEnd()
            End Using
        End Using
    End Function

    Friend Function Manifest() As CommandManifest
        SyncLock manifestGate
            If manifestValue Is Nothing Then
                Try
                    manifestValue = CommandManifest.Parse(ManifestText())
                Catch ex As Exception
                    ' A manifest that cannot be read leaves no verb to accept: every command is unknown-verb.
                    ShellLog.Write("read the command manifest", ex)
                    manifestValue = New CommandManifest()
                End Try
            End If
            Return manifestValue
        End SyncLock
    End Function

    ' ---- the parser ---------------------------------------------------------

    ' Reads one request line against a manifest. Nothing means accepted, with `request` filled; anything else
    ' is the answer, already carrying the request's id when one could be read. Never throws (rule 3).
    Friend Function ReadRequest(line As String, m As CommandManifest, ByRef request As CommandRequest) As CommandResponse
        request = Nothing
        If line Is Nothing OrElse line.Trim().Length = 0 Then Return CommandResponse.Refused(10, "bad-argument", Localization.T("appcmd_malformed"))
        If Encoding.UTF8.GetByteCount(line) > MaxRequestBytes Then Return CommandResponse.Refused(10, "too-large", Localization.T("appcmd_too_large"))
        Dim doc As Dictionary(Of String, Object) = Nothing
        Try
            doc = AppCommands.Json().Deserialize(Of Dictionary(Of String, Object))(line)
        Catch ex As Exception
            doc = Nothing
        End Try
        If doc Is Nothing Then Return CommandResponse.Refused(10, "bad-argument", Localization.T("appcmd_malformed"))

        Dim id = PartJson.Str(doc, "requestId")
        ' Item I.3 and item X: a profile request carries a non-empty requestId; a line without one
        ' is unreadable and answered refused (bad-argument) with requestId "".
        If String.IsNullOrWhiteSpace(id) Then Return CommandResponse.Refused(10, "bad-argument", Localization.T("appcmd_malformed"))
        Dim answer = Function(r As CommandResponse) As CommandResponse
                         r.RequestId = id
                         Return r
                     End Function

        Dim sv = PartJson.Value(doc, "schemaVersion")
        If Not (TypeOf sv Is Integer) Then Return answer(CommandResponse.Refused(10, "bad-argument", Localization.T("appcmd_malformed")))
        If CInt(sv) > 1 Then Return answer(CommandResponse.Refused(2, "schema", Localization.T("appcmd_schema")))
        If CInt(sv) < 1 Then Return answer(CommandResponse.Refused(10, "bad-argument", Localization.T("appcmd_malformed")))

        Dim command = PartJson.Str(doc, "command")
        Dim spec = m.Find(command)
        If spec Is Nothing Then Return answer(CommandResponse.Refused(10, "unknown-verb", Localization.T("appcmd_unknown_verb")))

        Dim args As New List(Of String)
        Dim rawArgs = PartJson.Value(doc, "args")
        If rawArgs IsNot Nothing Then
            Dim list = TryCast(rawArgs, IEnumerable)
            If list Is Nothing OrElse TypeOf rawArgs Is String Then Return answer(CommandResponse.Refused(10, "bad-argument", Localization.T("appcmd_malformed")))
            For Each a In list
                Dim s = TryCast(a, String)
                If s Is Nothing Then Return answer(CommandResponse.Refused(10, "bad-argument", Localization.T("appcmd_malformed")))
                If Encoding.UTF8.GetByteCount(s) > MaxArgBytes Then Return answer(CommandResponse.Refused(10, "too-large", Localization.T("appcmd_too_large")))
                args.Add(s)
            Next
        End If

        Dim options As Dictionary(Of String, String) = Nothing
        Dim problem = ParseArgs(spec, args, options)
        If problem IsNot Nothing Then Return answer(CommandResponse.Refused(10, "bad-argument", problem))

        request = New CommandRequest With {
            .Spec = spec, .Command = spec.Name, .Args = args, .Options = options, .RequestId = id,
            .WorkingDir = PartJson.Str(doc, "workingDir"), .Origin = PartJson.Str(doc, "origin"),
            .Legacy = PartJson.Bool(doc, "legacy")}
        Return Nothing
    End Function

    ' The one reading of a verb's arguments (rule 1, rule 3), the same as filedo.exe's for a cold start:
    ' `--name`, `--name=value` or `--name value`; names without regard to case, values exactly as typed;
    ' `--` ends the options. No verb of this manifest takes a bare value. Nothing means accepted; otherwise
    ' one sentence that says what to change, with no value of the caller's in it.
    Friend Function ParseArgs(spec As CommandSpec, args As IList(Of String), ByRef options As Dictionary(Of String, String)) As String
        options = New Dictionary(Of String, String)(StringComparer.OrdinalIgnoreCase)
        Dim i = 0
        While i < args.Count
            Dim a = If(args(i), "")
            If a = "--" Then
                If i + 1 < args.Count Then Return Localization.Format(Localization.T("appcmd_arg_value"), spec.Name)
                Exit While
            End If
            If Not a.StartsWith("--", StringComparison.Ordinal) OrElse a.Length = 2 Then
                Return Localization.Format(Localization.T("appcmd_arg_value"), spec.Name)
            End If
            Dim body = a.Substring(2)
            Dim eq = body.IndexOf("="c)
            Dim name = If(eq >= 0, body.Substring(0, eq), body)
            Dim hasValue = eq >= 0
            Dim value = If(hasValue, body.Substring(eq + 1), "")
            Dim arg = spec.Args.FirstOrDefault(Function(x) String.Equals(x.Name, name, StringComparison.OrdinalIgnoreCase))
            If arg Is Nothing Then Return Localization.Format(Localization.T("appcmd_arg_unknown"), SafeWord(name), spec.Name)
            If options.ContainsKey(arg.Name) Then Return Localization.Format(Localization.T("appcmd_arg_twice"), arg.Name)
            If arg.Kind = "bool" Then
                If Not hasValue Then
                    value = "true"
                ElseIf value <> "true" AndAlso value <> "false" Then
                    Return Localization.Format(Localization.T("appcmd_arg_bool"), arg.Name)
                End If
                options(arg.Name) = value
                i += 1
                Continue While
            End If
            If Not hasValue Then
                If i + 1 >= args.Count Then Return Localization.Format(Localization.T("appcmd_arg_needs"), arg.Name)
                i += 1
                value = If(args(i), "")
            End If
            Select Case arg.Kind
                Case "int"
                    Dim n As Integer
                    If Not Integer.TryParse(value, Globalization.NumberStyles.AllowLeadingSign, Globalization.CultureInfo.InvariantCulture, n) Then
                        Return Localization.Format(Localization.T("appcmd_arg_int"), arg.Name)
                    End If
                Case "enum"
                    If Not arg.Values.Contains(value) Then Return Localization.Format(Localization.T("appcmd_arg_enum"), arg.Name, String.Join(", ", arg.Values))
            End Select
            options(arg.Name) = value
            i += 1
        End While
        For Each arg In spec.Args
            If arg.Required AndAlso Not options.ContainsKey(arg.Name) Then Return Localization.Format(Localization.T("appcmd_arg_required"), spec.Name, arg.Name)
        Next
        Return Nothing
    End Function

    ' An option name the caller typed goes into the sentence only when it is an option-shaped word; a path or
    ' anything longer never does (rule 7: the message carries no path and no secret).
    Private Function SafeWord(word As String) As String
        If word IsNot Nothing AndAlso Regex.IsMatch(word, "^[A-Za-z][A-Za-z0-9-]{0,31}$") Then Return word
        Return "?"
    End Function

    ' ---- risk, busy, the work ----------------------------------------------

    ' Rule 13, item C.4 and item F.2: a `safe` or `forward: never` verb is always accepted; a
    ' request carrying `legacy: true` is accepted when the verb lists at least one legacy alias;
    ' any other class requires the person's one setting to be on.
    Friend Function Gate(spec As CommandSpec, Optional legacy As Boolean = False) As CommandResponse
        If spec Is Nothing OrElse spec.IsForwardNever OrElse String.Equals(spec.Risk, "safe", StringComparison.Ordinal) Then Return Nothing
        If legacy AndAlso spec.HasLegacyAlias() Then Return Nothing
        If ShellSettings.AcceptsRestrictedCommands() Then Return Nothing
        Return CommandResponse.Refused(12, "switch-off", Localization.Format(Localization.T("appcmd_switch_off"), Localization.T("settings_commands_restricted")))
    End Function

    ' Rule 10: a modal window - a ShellDialog, the Disk Manager's first-steps window, any dialog shown with
    ' ShowDialog - means the program cannot take a command now. Read on the window's thread.
    Friend Function ModalOpen() As Boolean
        For Each f In Application.OpenForms.Cast(Of Form)().ToList()
            If Not f.IsDisposed AndAlso f.Modal AndAlso f.Visible Then Return True
        Next
        Return False
    End Function

    ' What `status` reports (manifest `returns`). No mounted-disk count: the Disk Manager's figure is 0 when
    ' its window was never built, and `filedo vd status json` is the authority for disks.
    Friend Function StatusData(host As AppHost) As Dictionary(Of String, Object)
        Dim pid = 0
        Using p = Process.GetCurrentProcess()
            pid = p.Id
        End Using
        Return New Dictionary(Of String, Object) From {
            {"version", LogReport.BuildStamp()},
            {"pid", pid},
            {"shellOpen", host.ShellOpen},
            {"managerOpen", host.ManagerOpen},
            {"trayHeld", host.TrayHeld},
            {"runningJobs", host.RunningJobs},
            {"acceptsRestrictedCommands", ShellSettings.AcceptsRestrictedCommands()}}
    End Function

    ' Runs an accepted command on the window's thread and answers it. Busy first (rule 10): with a modal
    ' dialog open, a first run's window up or the program closing nothing is done.
    Friend Function Execute(request As CommandRequest) As CommandResponse
        Dim host = AppHost.Current
        If host Is Nothing OrElse host.IsClosing OrElse ModalOpen() Then Return CommandResponse.Busy(Localization.T("appcmd_busy"))
        Dim m = Manifest()
        Select Case request.Spec.Name
            Case "status"
                Return CommandResponse.Done(Localization.T("appcmd_done_status"), StatusData(host))
            Case "show"
                host.ShowShell()
                Return CommandResponse.Done(Localization.T("appcmd_done_show"))
            Case "disk-manager show"
                ' The Disk Manager on its own when the program has no window: ShowManager builds or brings forward
                ' the manager and never the shell (SP-0217 A1).
                host.ShowManager()
                Return CommandResponse.Done(Localization.T("appcmd_done_manager"))
            Case "settings show"
                ' The main window on the Settings page, on the screen the person is using - the pointer decides
                ' for a caller with no window of its own (SP-0217 A2, A4).
                host.ShowSettings()
                Return CommandResponse.Done(Localization.T("appcmd_done_settings"))
            Case "tray start"
                ' Answered and left alone: the tray icon belongs to every running copy, and a copy already running
                ' keeps its windows as they are (SP-0217 A3). The hidden start is the cold start, in Program.vb.
                Return CommandResponse.Done(Localization.T("appcmd_done_tray"))
            Case "job list"
                ' The one snapshot of the runs, read here on the window's thread (SP-0218 A1-A3).
                Dim runs = host.Runs()
                Return CommandResponse.Done(JobsMessage(runs), New Dictionary(Of String, Object) From {{"jobs", RunSnapshot.ToData(runs)}})
            Case "job stop"
                ' The Stop button's own request on every run of the scope and the Disk Manager's queue emptied, then the
                ' wait for the scope to hold no run - waited out by the listener, never on this thread (SP-0218 A5, A6).
                Dim scope As String = Nothing
                If Not request.Options.TryGetValue("where", scope) OrElse String.IsNullOrEmpty(scope) Then scope = "all"
                Dim cleared As Integer
                Dim asked = host.StopRuns(scope, cleared)
                Dim data = StopData(asked, cleared)
                If host.RunsLeft(scope) = 0 Then Return CommandResponse.Done(StopMessage(asked, cleared), data)
                Return AfterRunsEnd(scope,
                                    Function() CommandResponse.Done(StopMessage(asked, cleared), data),
                                    Function() CommandResponse.Failed(2, "timeout", Localization.T("appcmd_stop_timeout"), data))
            Case "quit"
                ' The window's own Exit asks the person while a run goes on (ShellForm.ConfirmCloseWhileRunning); a
                ' command never waits on a person it did not ask for (rule 4), so without --stop-jobs it refuses. With
                ' it the runs are asked to stop as `job stop` asks, and the close is the one the window's Exit makes,
                ' once they have ended (SP-0218 A10).
                Dim stopJobs As String = Nothing
                request.Options.TryGetValue("stop-jobs", stopJobs)
                If String.Equals(stopJobs, "true", StringComparison.Ordinal) AndAlso host.RunsLeft("all") > 0 Then
                    Dim cleared As Integer
                    Dim asked = host.StopRuns("all", cleared)
                    Dim data = StopData(asked, cleared)
                    Return AfterRunsEnd("all", AddressOf CloseProgram,
                                        Function() CommandResponse.Failed(2, "timeout", Localization.T("appcmd_quit_stop_timeout"), data))
                End If
                If host.RunningJobs > 0 Then Return CommandResponse.Busy(Localization.T("appcmd_busy_runs"))
                Return CloseProgram()
            Case "version"
                Return CommandResponse.Done(Localization.Format(Localization.T("appcmd_done_version"), m.Product, LogReport.BuildStamp(), m.CliVersion),
                                            New Dictionary(Of String, Object) From {{"product", m.Product}, {"version", LogReport.BuildStamp()}, {"cliVersion", m.CliVersion}})
            Case "commands"
                Return CommandResponse.Done(Localization.T("appcmd_done_commands"), m.Raw)
        End Select
        Return CommandResponse.Refused(10, "unknown-verb", Localization.T("appcmd_unknown_verb"))
    End Function

    ' `job list`: the sentence of the answer; the list itself is `data.jobs`.
    Private Function JobsMessage(runs As IList(Of RunInfo)) As String
        If runs.Count = 0 Then Return Localization.T("appcmd_done_jobs_none")
        Dim running = RunSnapshot.RunningCount(runs)
        Return Localization.Format(Localization.T("appcmd_done_jobs"), running, runs.Count - running)
    End Function

    ' What `job stop` returns (manifest `returns`): the runs it asked and the queued operations it took out.
    Friend Function StopData(asked As Integer, cleared As Integer) As Dictionary(Of String, Object)
        Return New Dictionary(Of String, Object) From {{"stopped", asked}, {"cleared", cleared}}
    End Function

    Friend Function StopMessage(asked As Integer, cleared As Integer) As String
        If asked = 0 AndAlso cleared = 0 Then Return Localization.T("appcmd_done_stop_none")
        Return Localization.Format(Localization.T("appcmd_done_stop"), asked, cleared)
    End Function

    ' The answer of a command that has asked runs to stop and goes on when `scope` (all, shell, manager) holds none.
    ' The program already gone counts as none left. See PendingCommand: the listener does the waiting.
    Private Function AfterRunsEnd(scope As String, done As Func(Of CommandResponse), timedOut As Func(Of CommandResponse)) As CommandResponse
        Dim pending As New PendingCommand With {
            .Poll = Function() AppHost.Current Is Nothing OrElse AppHost.Current.RunsLeft(scope) = 0,
            .Done = done,
            .TimedOut = timedOut}
        Return New CommandResponse With {.Pending = pending}
    End Function

    ' The close `quit` makes: the window's own Exit (AppHost.RequestExit), asked once nothing runs. A modal window opened
    ' since the command was taken means a person is deciding something - busy, nothing closed (rule 4, rule 10).
    Private Function CloseProgram() As CommandResponse
        Dim host = AppHost.Current
        If host Is Nothing Then Return CommandResponse.Done(Localization.T("appcmd_done_quit"))
        If ModalOpen() Then Return CommandResponse.Busy(Localization.T("appcmd_busy"))
        host.RequestExit()
        If AppHost.Current Is Nothing Then Return CommandResponse.Done(Localization.T("appcmd_done_quit"))
        Return CommandResponse.Failed(1, "not-closed", Localization.T("appcmd_quit_failed"))
    End Function

    ' Rule 16: verb, origin, the caller's process id, the outcome - the arguments redacted as every start line
    ' is (CLI-EVENT-STREAM rule 13, Program.StartLogLine), cut to a bounded length. No ticket id, no secret.
    Friend Function LogLine(request As CommandRequest, callerPid As Integer, response As CommandResponse) As String
        Dim verb = If(request Is Nothing, "(unread)", request.Command)
        Dim origin = If(request Is Nothing OrElse request.Origin = "", "-", SafeWord(request.Origin))
        Dim args = If(request Is Nothing, "", Program.StartLogLine(request.Args.ToArray()))
        If args.Length > 512 Then args = args.Substring(0, 512) & ".."
        Dim b As New StringBuilder("app command ")
        b.Append(verb).Append(" origin=").Append(origin)
        b.Append(" caller=").Append(callerPid.ToString(Globalization.CultureInfo.InvariantCulture))
        b.Append(" outcome=").Append(response.Status).Append(" exit=").Append(response.ExitCode.ToString(Globalization.CultureInfo.InvariantCulture))
        If Not String.IsNullOrEmpty(response.Reason) AndAlso response.Status <> "done" Then b.Append(" reason=").Append(response.Reason)
        If args <> "" Then b.Append(" args=").Append(args)
        Return b.ToString()
    End Function

End Module
