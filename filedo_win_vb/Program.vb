' The entry point of filedo_win.exe.
'
' The window is the shell (ShellForm, SP-0006). The command builder that shipped before it -
' MainForm, AboutForm and the --legacy-builder switch - was retired in SP-0014 (the owner's D1): the
' shell's Command page is where a hand-written command line lives now. `--legacy-builder` is still
' accepted on the command line, so a shortcut that carries it keeps working, and opens the shell.
'
' Three things happen here before any window exists, and each one is a rule:
'   - `--selftest` runs the shell's own gate and exits with its code, before anything is shown
'     (SelfTest.vb), so a test run never depends on a window being on somebody's screen.
'     `--write-menu-icons <folder>` is the same kind of start: it writes the Explorer icons
'     (MenuIcons.vb) and exits.
'   - Every exception nothing else caught is logged and shown as a cause and actions, never as the
'     stock WinForms dialog with a stack trace in it (APP-BEHAVIOUR rule 6).
'   - A second copy hands the desktop back to the one already open - unless it was started on a
'     file, which is what that start was about. A second `--disks` asks the running copy to bring
'     its Disk Manager forward (SP-0063 4.2); the first `--disks` opens the manager alone.
Imports System.Threading

Module Program

    Private Declare Function SetForegroundWindow Lib "user32" (hWnd As IntPtr) As Boolean
    ' Posts the request to the window's own thread; ShowWindow would wait for it, and a hung first copy
    ' would hold this start (APP-ACTIVATION 0.13, a hung receiver).
    Private Declare Function ShowWindowAsync Lib "user32" (hWnd As IntPtr, nCmdShow As Integer) As Boolean
    Private Declare Function IsIconic Lib "user32" (hWnd As IntPtr) As Boolean
    Private Declare Function IsHungAppWindow Lib "user32" (hWnd As IntPtr) As Boolean
    Private Declare Function GetForegroundWindow Lib "user32" () As IntPtr
    Private Const SW_RESTORE As Integer = 9

    ' SHELL-12: the single-instance claim, held for the life of the process. A named mutex in the
    ' session namespace, because two quick starts both find no window by process name and both open.
    Private Const InstanceMutexName As String = "Local\FileDO.Shell"
    Private instanceMutex As Mutex

    ' How long a start that lost the claim waits for the first copy's window to exist.
    Private Const HandOverWaitMs As Integer = 5000

    ' The developer tooling of this program (APP-COMMANDS rule 2, SP-0219). Five switches have a verb spelling of the
    ' command manifest beside their frozen spelling, and both reach the same handler: the table below is the one parser
    ' of them. A verb is read from the first words of the line, its option after them; a frozen spelling is found
    ' anywhere on the line and its value is the next argument, as the switches were read before SP-0219. Each one runs
    ' before the single-instance claim and before anything is shown, and none is handed to a running window program.
    Private NotInheritable Class ToolingSpec
        Friend ReadOnly Verb As String
        Friend ReadOnly Legacy As String
        Friend ReadOnly TakesFolder As Boolean
        Friend ReadOnly Handler As Func(Of String, Integer)

        Friend Sub New(verb As String, legacy As String, takesFolder As Boolean, handler As Func(Of String, Integer))
            Me.Verb = verb
            Me.Legacy = legacy
            Me.TakesFolder = takesFolder
            Me.Handler = handler
        End Sub
    End Class

    ' What a command line asks of the developer tooling: the spec, the folder (Nothing when none was given), and the
    ' usage error of a verb spelling that is not well formed (exit 10).
    Private NotInheritable Class ToolingCall
        Friend ReadOnly Spec As ToolingSpec
        Friend ReadOnly Folder As String
        Friend ReadOnly Problem As String
        Friend ReadOnly IsVerb As Boolean

        Friend Sub New(spec As ToolingSpec, folder As String, problem As String, Optional isVerb As Boolean = False)
            Me.Spec = spec
            Me.Folder = folder
            Me.Problem = problem
            Me.IsVerb = isVerb
        End Sub

        Friend Function Run() As Integer
            If Problem IsNot Nothing Then
                Console.Error.WriteLine(Problem)
                Return 10
            End If
            ' APP-COMMANDS 0.3 item V.2: a folder-only verb never creates a missing folder and exits 11 (target-missing).
            If IsVerb AndAlso Spec.TakesFolder AndAlso Not IO.Directory.Exists(Folder) Then
                Console.Error.WriteLine(Folder & " does not exist; create it first.")
                Return 11
            End If
            Return Spec.Handler.Invoke(Folder)
        End Function
    End Class

    ' The one table. Its order is the order the switches were checked in, so a line that carries two frozen spellings
    ' keeps the start it always had.
    Private ReadOnly Tooling As ToolingSpec() = {
        New ToolingSpec("menu-icons write", "--write-menu-icons", True, AddressOf RunWriteMenuIcons), _
        New ToolingSpec("ui-drive run", "--ui-drive", True, AddressOf RunUiDrive), _
        New ToolingSpec("screens capture", "--capture-screens", True, AddressOf RunCaptureScreens), _
        New ToolingSpec("help-pages build", "--build-help", True, AddressOf RunBuildHelp), _
        New ToolingSpec("selftest run", "--selftest", False, AddressOf RunSelfTest)}

    ' What the command line asks of the developer tooling, or Nothing when it asks none of it. A verb is matched on the
    ' first words, without regard to case; a frozen spelling is matched exactly, anywhere on the line.
    Private Function ToolingCallOf(argv As String()) As ToolingCall
        For Each spec As ToolingSpec In Tooling
            Dim words = spec.Verb.Split(" "c)
            If argv.Length < words.Length + 1 Then Continue For
            Dim named = True
            For i = 0 To words.Length - 1
                If Not String.Equals(argv(i + 1), words(i), StringComparison.OrdinalIgnoreCase) Then named = False
            Next
            If named Then Return VerbCall(spec, argv, words.Length + 1)
        Next
        For Each spec As ToolingSpec In Tooling
            Dim legacyAt = Array.IndexOf(argv, spec.Legacy)
            If legacyAt >= 0 Then
                Dim folder As String = Nothing
                If spec.TakesFolder AndAlso legacyAt + 1 < argv.Length Then folder = argv(legacyAt + 1)
                Return New ToolingCall(spec, folder, Nothing)
            End If
        Next
        Return Nothing
    End Function

    ' The verb spelling: its words, then nothing (a verb without a folder) or `--out <folder>` or `--out=<folder>` (a verb
    ' that writes one). Anything else is a usage error, said once on standard error.
    Private Function VerbCall(spec As ToolingSpec, argv As String(), first As Integer) As ToolingCall
        If Not spec.TakesFolder Then
            If argv.Length = first Then Return New ToolingCall(spec, Nothing, Nothing, True)
            Return New ToolingCall(spec, Nothing, spec.Verb & " takes no option; remove " & argv(first), True)
        End If
        If argv.Length = first + 2 AndAlso String.Equals(argv(first), "--out", StringComparison.OrdinalIgnoreCase) AndAlso argv(first + 1).Length > 0 Then
            Return New ToolingCall(spec, argv(first + 1), Nothing, True)
        End If
        If argv.Length = first + 1 AndAlso argv(first).StartsWith("--out=", StringComparison.OrdinalIgnoreCase) AndAlso argv(first).Length > 6 Then
            Return New ToolingCall(spec, argv(first).Substring(6), Nothing, True)
        End If
        Return New ToolingCall(spec, Nothing, spec.Verb & " needs --out <folder>", True)
    End Function

    Friend Function RunToolingForTest(argv As String()) As Integer
        Dim call_ = ToolingCallOf(argv)
        If call_ Is Nothing Then Return -1
        Return call_.Run()
    End Function

    ' `--write-menu-icons <folder>` and `menu-icons write --out <folder>`: writes the Explorer icons (MenuIcons.vb) and
    ' prints each file written. 0 when every file was written, 1 otherwise.
    Private Function RunWriteMenuIcons(folder As String) As Integer
        Dim code = 1
        Try
            If folder IsNot Nothing AndAlso Glyphs.Problems().Count = 0 Then
                For Each written In MenuIcons.WriteAll(folder)
                    Console.WriteLine(written)
                Next
                code = 0
            End If
        Catch ex As Exception
            ShellLog.Write("write-menu-icons", ex)
        End Try
        Return code
    End Function

    ' `--ui-drive <folder>` and `ui-drive run --out <folder>`: the driven visual acceptance of SP-0150 (UiDrive.vb). Exits
    ' 0 or 1; it restores the person's own HKCU values afterwards.
    Private Function RunUiDrive(folder As String) As Integer
        If folder Is Nothing Then Return 1
        Return UiDrive.Run(folder)
    End Function

    ' `--capture-screens <folder>` and `screens capture --out <folder>`: the guide screenshots (Capture.vb). It builds
    ' windows and writes no setting of the person's.
    Private Function RunCaptureScreens(folder As String) As Integer
        If folder Is Nothing Then Return 1
        Return Capture.Run(folder)
    End Function

    ' `--build-help <folder>` and `help-pages build --out <folder>`: the help pages and their pictures, in every language
    ' and both themes (HelpPages.vb).
    Private Function RunBuildHelp(folder As String) As Integer
        If folder Is Nothing Then Return 1
        Return HelpPages.Build(folder)
    End Function

    ' `--selftest` and `selftest run`: the shell's own gate, which exits with its code. Whatever happens inside, the gate
    ' ends with a log and a code - a crash is a FAIL, never a process that dies with nothing written (SHELL-15).
    Private Function RunSelfTest(folder As String) As Integer
        Try
            Return SelfTest.Run()
        Catch ex As Exception
            Return SelfTest.WriteCrash(ex)
        End Try
    End Function

    ' Opening a .fdd or .fd-sec container file in its own window (`filedo_win.exe open --path <file> [--mode <mount|mount-ro|unmount>]`,
    ' SP-0228 / APP-COMMANDS 0.2 item A & item C). A `forward: never`, `oneshot` verb: strict parsing (exit 10 on usage errors),
    ' exit 11 when the named file does not exist, and no single-instance claim or handover.
    Private NotInheritable Class OpenCall
        Friend ReadOnly TargetPath As String
        Friend ReadOnly Mode As DiskRoute.StartMode
        Friend ReadOnly ExitCode As Integer
        Friend ReadOnly Message As String

        Friend Sub New(targetPath As String, mode As DiskRoute.StartMode, exitCode As Integer, message As String)
            Me.TargetPath = targetPath
            Me.Mode = mode
            Me.ExitCode = exitCode
            Me.Message = message
        End Sub
    End Class

    Private Function OpenCallOf(argv As String()) As OpenCall
        If argv Is Nothing OrElse argv.Length < 2 Then Return Nothing
        If Not String.Equals(argv(1), "open", StringComparison.OrdinalIgnoreCase) Then Return Nothing

        Dim pathVal As String = Nothing
        Dim modeVal As String = Nothing
        Dim seenPath = False
        Dim seenMode = False
        Dim i = 2
        While i < argv.Length
            Dim a = If(argv(i), "")
            If a = "--" Then
                If i + 1 < argv.Length Then Return New OpenCall(Nothing, DiskRoute.StartMode.None, 10, "open takes no bare value; use --path <file>")
                Exit While
            End If
            If Not a.StartsWith("--", StringComparison.Ordinal) OrElse a.Length = 2 Then
                Return New OpenCall(Nothing, DiskRoute.StartMode.None, 10, "open takes --path <file>; remove " & a)
            End If
            Dim body = a.Substring(2)
            Dim eq = body.IndexOf("="c)
            Dim name = If(eq >= 0, body.Substring(0, eq), body)
            Dim hasValue = eq >= 0
            Dim value = If(hasValue, body.Substring(eq + 1), "")
            If String.Equals(name, "path", StringComparison.OrdinalIgnoreCase) Then
                If seenPath Then Return New OpenCall(Nothing, DiskRoute.StartMode.None, 10, "--path is given twice")
                seenPath = True
                If Not hasValue Then
                    If i + 1 >= argv.Length Then Return New OpenCall(Nothing, DiskRoute.StartMode.None, 10, "--path needs a file")
                    i += 1
                    value = If(argv(i), "")
                End If
                If String.IsNullOrWhiteSpace(value) Then Return New OpenCall(Nothing, DiskRoute.StartMode.None, 10, "--path needs a file")
                pathVal = value
            ElseIf String.Equals(name, "mode", StringComparison.OrdinalIgnoreCase) Then
                If seenMode Then Return New OpenCall(Nothing, DiskRoute.StartMode.None, 10, "--mode is given twice")
                seenMode = True
                If Not hasValue Then
                    If i + 1 >= argv.Length Then Return New OpenCall(Nothing, DiskRoute.StartMode.None, 10, "--mode needs one of mount, mount-ro, unmount")
                    i += 1
                    value = If(argv(i), "")
                End If
                If value <> "mount" AndAlso value <> "mount-ro" AndAlso value <> "unmount" Then
                    Return New OpenCall(Nothing, DiskRoute.StartMode.None, 10, "--mode takes one of mount, mount-ro, unmount")
                End If
                modeVal = value
            Else
                Return New OpenCall(Nothing, DiskRoute.StartMode.None, 10, "--" & name & " is not an option of open")
            End If
            i += 1
        End While

        If Not seenPath OrElse String.IsNullOrWhiteSpace(pathVal) Then
            Return New OpenCall(Nothing, DiskRoute.StartMode.None, 10, "open needs --path <file>")
        End If
        Dim fullPath As String
        Try
            fullPath = IO.Path.GetFullPath(pathVal)
        Catch ex As Exception
            Return New OpenCall(Nothing, DiskRoute.StartMode.None, 10, "open needs a valid file path")
        End Try
        If Not IO.File.Exists(fullPath) Then
            Return New OpenCall(Nothing, DiskRoute.StartMode.None, 11, fullPath & " does not exist.")
        End If
        Dim startMode = DiskRoute.StartMode.Mount
        If modeVal = "mount-ro" Then
            startMode = DiskRoute.StartMode.MountReadOnly
        ElseIf modeVal = "unmount" Then
            startMode = DiskRoute.StartMode.Unmount
        End If
        Return New OpenCall(fullPath, startMode, 0, Nothing)
    End Function

    Friend Function ParseOpenForTest(argv As String(), ByRef target As String, ByRef mode As DiskRoute.StartMode) As Integer
        target = Nothing
        mode = DiskRoute.StartMode.None
        Dim call_ = OpenCallOf(argv)
        If call_ Is Nothing Then Return -1
        target = call_.TargetPath
        mode = call_.Mode
        Return call_.ExitCode
    End Function
    <STAThread>
    Public Sub Main()
        Application.EnableVisualStyles()
        Application.SetCompatibleTextRenderingDefault(False)

        Dim argv = Environment.GetCommandLineArgs()

        ' The developer tooling (SP-0219, see ToolingCallOf): a verb or a frozen switch runs its handler, and the
        ' program exits with the handler's code before anything is shown or claimed.
        Dim tooling = ToolingCallOf(argv)
        If tooling IsNot Nothing Then
            Environment.Exit(tooling.Run())
            Return
        End If

        ' The canonical container-open verb (SP-0228): `filedo_win.exe open --path <file> [--mode <mount|mount-ro|unmount>]`
        ' runs in its own process with no single-instance claim and no handover (APP-COMMANDS 0.2 item C.1 / C.5).
        Dim openCall = OpenCallOf(argv)
        If openCall IsNot Nothing AndAlso openCall.ExitCode <> 0 Then
            Console.Error.WriteLine(openCall.Message)
            Environment.Exit(openCall.ExitCode)
            Return
        End If

        Application.SetUnhandledExceptionMode(UnhandledExceptionMode.CatchException)
        AddHandler Application.ThreadException, AddressOf OnThreadException
        AddHandler AppDomain.CurrentDomain.UnhandledException, AddressOf OnUnhandledException

        ' A second copy normally hands the desktop back to the one already open. A copy started ON
        ' a file must not: the file is what this start is about, and focusing somebody else's
        ' window would drop it. Each double-clicked container therefore gets its own window - which
        ' is also what makes the "remove the copy now" of a reveal belong to one file rather than
        ' to whichever reveal ran last.
        Dim target = If(openCall IsNot Nothing, openCall.TargetPath, StartupTarget())
        ' `--disks` starts straight into the Disk Manager, with no shell window (SP-0063 4.2); a
        ' second one while this program runs brings the running copy's manager forward (D1).
        Dim disksOnly = openCall Is Nothing AndAlso target Is Nothing AndAlso WantsDiskManager(Environment.GetCommandLineArgs())
        Dim first = If(openCall IsNot Nothing, False, ClaimSingleInstance())
        If openCall Is Nothing AndAlso WantsHandOver(target, first) Then
            If disksOnly AndAlso AppHost.HandOverDisks() Then Return
            ' A plain start asks the running copy for its shell window - which it opens if the copy was
            ' started as the Disk Manager alone (the Start menu's FileDO entry after its Disk Manager
            ' entry). A copy that does not listen (an older one) is focused as before.
            If Not disksOnly AndAlso AppHost.HandOverShell() Then Return
            If HandOverToRunningCopy(disksOnly) Then Return
        End If

        ' A start on a `.fdd` (SP-0004 6.4, T6.18) is answered before any window exists: a container
        ' that is mounted already opens its drive in Explorer and nothing else, which is what a user
        ' means by opening it - no window flashes up only to point elsewhere.
        Dim diskDecision As DiskRoute.Decision = Nothing
        If target IsNot Nothing AndAlso DiskRoute.IsContainer(target) Then
            diskDecision = If(openCall IsNot Nothing,
                              DiskStartDecision(target, openCall.Mode),
                              DiskStartDecision(target, Environment.GetCommandLineArgs()))
            If diskDecision.Action = DiskRoute.RouteAction.OpenDrive Then
                Ui.OpenFolder(Nothing, diskDecision.Letter & "\")
                Return
            End If
        End If

        ' SHELL-02: the retention the Settings page promises - reports 30 days, run files 7 - is
        ' kept here, once per start, off the window's thread.
        StartSweep()

        ' Retire closed session logs even when this start produces no error or debug line.
        ShellLog.Info("session started")

        ShellLog.Debug("start: " & StartLogLine(Environment.GetCommandLineArgs().Skip(1).ToArray()))

        ' The process's windows - the shell, the Disk Manager - live in one AppHost, which ends the
        ' process when the last of them has closed (SP-0063 D1).
        Dim host As AppHost = Nothing
        Try
            If target Is Nothing AndAlso StartupLaunch.HiddenStart(argv) Then
                host = New AppHost(Nothing, False, True)
            ElseIf disksOnly Then
                host = New AppHost(Nothing, True)
            Else
                Dim startShell As New ShellForm(target, diskDecision)
                ' A plain start, with no file to open, is where leftover test files are asked about (SP-0194).
                startShell.OffersLeftoverCleanup = (target Is Nothing)
                host = New AppHost(startShell, False)
            End If
        Catch ex As Exception
            ' The window could not be built. There is no window to own the message, so it stands on
            ' its own - and it still says what happened and what can be done, not the exception.
            ShellLog.Write("the window could not open", ex)
            ShellDialog.Problem(Nothing, Localization.Format(Localization.T("shell_open_failed"), Problems.Cause(ex)))
            Return
        End Try
        ' The first copy of the session takes commands from other programs (APP-COMMANDS 0.1,
        ' CommandListener.vb); a copy that lost the claim - a second window opened on a file - does not. The two
        ' registered window messages above stay as they are for the focus-only start.
        If first Then CommandListener.Start()
        Application.Run(host)
        ' A `quit` ends the message loop from inside its own command: its answer is still on its way.
        CommandListener.Shutdown(1500)
    End Sub

    ' `--disks`, in any case: the Disk Manager on its own.
    Friend Function WantsDiskManager(args As String()) As Boolean
        If args Is Nothing Then Return False
        Return args.Skip(1).Any(Function(a) String.Equals(a, "--disks", StringComparison.OrdinalIgnoreCase))
    End Function

    ' The file the program was started on, if it was started on one.
    '
    ' Explorer hands a file to this program as a bare path (SP-0005 9.4), and the switches this
    ' program understands are all prefixed, so "the first argument that is not a switch and is a
    ' file on disk" needs no new grammar. Anything else is left to the shell's own opening page.
    '
    ' A `.fdd` disk container (SP-0004 6.4) comes with a double-dash switch or none: a plain start
    ' mounts, `--mount-ro "<x.fdd>"` mounts read-only and `--unmount "<x.fdd>"` unmounts - the switch
    ' is skipped here like any other and read by DiskRoute.ModeFrom.
    '
    ' The path is made absolute here, against the folder the program was started in (SHELL-04):
    ' `filedo_win.exe notes.txt` from a console means the notes.txt the user is looking at, and
    ' The -debug start line (SP-0004 spec 7.2 item 2). The window's own arguments are paths and
    ' switches - a double-clicked .fdd is still only a path - and a credential never travels on
    ' this command line. The line goes through the run report's redaction all the same, and a
    ' p: token that is a credential rather than a path on drive P: is screened wherever it stands,
    ' so one can never reach the log; SelfTest holds it.
    Friend Function StartLogLine(args As String()) As String
        Dim out = Runner.RedactCredentialArgs(If(args, New String() {}))
        For i = 0 To out.Count - 1
            Dim t = out(i)
            If t.StartsWith("p:", StringComparison.Ordinal) AndAlso t <> "p:***" Then
                Dim v = t.Substring(2)
                If v = "" OrElse (v(0) <> "\"c AndAlso v(0) <> "/"c) Then out(i) = "p:***"
            End If
        Next
        Return String.Join(" ", out)
    End Function

    ' filedo.exe - which runs in %LOCALAPPDATA%\FileDO - would read a bare name as one of its own.
    Friend Function StartupTarget() As String
        Return StartupTargetFrom(Environment.GetCommandLineArgs())
    End Function

    Friend Function StartupTargetFrom(args As String()) As String
        If args Is Nothing Then Return Nothing
        For i As Integer = 1 To args.Length - 1
            Dim a = args(i)
            If String.IsNullOrWhiteSpace(a) Then Continue For
            If a.StartsWith("-") OrElse a.StartsWith("/") Then Continue For
            Try
                If IO.File.Exists(a) Then Return IO.Path.GetFullPath(a)
            Catch
            End Try
        Next
        Return Nothing
    End Function

    ' How a start on a container is answered: the switch it carries (`--mount-ro`, `--unmount`, or
    ' none for a plain double-click, which mounts) and what the console says about the container -
    ' `filedo.exe <x.fdd> info`, never a reader of this program's own.
    Friend Function DiskStartDecision(path As String, args As String()) As DiskRoute.Decision
        Return DiskStartDecision(path, DiskRoute.ModeFrom(args))
    End Function

    Friend Function DiskStartDecision(path As String, mode As DiskRoute.StartMode) As DiskRoute.Decision
        If mode = DiskRoute.StartMode.None Then mode = DiskRoute.StartMode.Mount
        Return DiskRoute.Decide(path, mode, DiskProbe.Info(path), Packaging.IsPackaged())
    End Function

    ' True when this process is the first copy in the session. A mutex that cannot be made is not a
    ' reason to open no window, so that case counts as first.
    Private Function ClaimSingleInstance() As Boolean
        Try
            Dim createdNew As Boolean
            instanceMutex = New Mutex(False, InstanceMutexName, createdNew)
            Return createdNew
        Catch ex As Exception
            ShellLog.Write("single-instance claim", ex)
            Return True
        End Try
    End Function

    ' The focus-only profile of APP-ACTIVATION 0.13: a second start with no file hands the desktop back to
    ' the first copy. A start that carries a file is a per-target start - it opens its own window, because
    ' the file is what the start is about and focusing another window would lose it. The first copy of
    ' the session never hands over. FileDO is therefore single-instance per session for plain starts and
    ' deliberately multi-window per file target; it is never described as a globally single-window app.
    Friend Function WantsHandOver(target As String, first As Boolean) As Boolean
        Return target Is Nothing AndAlso Not first
    End Function

    ' A start that lost the claim waits for the first copy's window - it may still be opening - and
    ' brings it forward. When none appears in time, or the one there does not respond, this start opens
    ' its own rather than nothing: a second launch must not vanish.
    Private Function HandOverToRunningCopy(Optional wantsManager As Boolean = False) As Boolean
        Dim deadline = DateTime.Now.AddMilliseconds(HandOverWaitMs)
        Do
            ' The first copy may still be constructing its listener, especially on a hidden start.
            If wantsManager Then
                If AppHost.HandOverDisks() Then Return True
            Else
                If AppHost.HandOverShell() Then Return True
            End If
            If FocusWindow(RunningCopyWindow()) Then Return True
            If DateTime.Now >= deadline Then Exit Do
            Thread.Sleep(100)
        Loop
        Return False
    End Function

    ' The fallback for a first copy that does not listen (an older one): restore and focus its window.
    ' Both calls return at once - the restore is posted, not waited for - and a window the system calls
    ' hung is not claimed, so a hung first copy cannot hold this start nor swallow it.
    ' A claim is made only when the window is the foreground window afterwards: the system may refuse
    ' SetForegroundWindow, and a refused call hands nothing over (APP-ACTIVATION section 7C), so the
    ' caller keeps waiting and then opens its own window.
    Friend Function FocusWindow(hwnd As IntPtr) As Boolean
        If hwnd = IntPtr.Zero Then Return False
        If IsHungAppWindow(hwnd) Then Return False
        If IsIconic(hwnd) Then ShowWindowAsync(hwnd, SW_RESTORE)
        SetForegroundWindow(hwnd)
        Return GetForegroundWindow() = hwnd
    End Function

    Private Function RunningCopyWindow() As IntPtr
        Try
            Using me_ = Process.GetCurrentProcess()
                Dim others = Process.GetProcessesByName(me_.ProcessName)
                Try
                    For Each p In others
                        If p.Id <> me_.Id AndAlso p.MainWindowHandle <> IntPtr.Zero Then Return p.MainWindowHandle
                    Next
                Finally
                    For Each p In others
                        p.Dispose()
                    Next
                End Try
            End Using
        Catch ex As Exception
            ' Not finding the other copy only means a second window opens.
            ShellLog.Write("single-instance check", ex)
        End Try
        Return IntPtr.Zero
    End Function

    Private Sub StartSweep()
        Tasks.Task.Run(Sub()
                           Try
                               Runner.SweepOldRuns()
                           Catch ex As Exception
                               ShellLog.Write("sweep old runs and reports", ex)
                           End Try
                       End Sub)
    End Sub

    ' An exception on the window's thread that no code caught. The window stays open; the user is
    ' told what happened in words, offered the logs, and the detail is in the log.
    Private Sub OnThreadException(sender As Object, e As ThreadExceptionEventArgs)
        ShellLog.Write("unhandled on the window thread", e.Exception)
        Try
            Dim owner As IWin32Window = Nothing
            If Application.OpenForms.Count > 0 Then owner = Application.OpenForms(0)
            ShellDialog.Problem(owner, Localization.T("shell_unhandled") & Environment.NewLine &
                                       Problems.Cause(e.Exception))
        Catch ex As Exception
            ShellLog.Write("showing the unhandled-exception notice", ex)
        End Try
    End Sub

    ' An exception on another thread. The process is ending and no window can be trusted from here,
    ' so the log is written and the one standalone notice this program has is shown.
    Private Sub OnUnhandledException(sender As Object, e As UnhandledExceptionEventArgs)
        ShellLog.Write("unhandled, the process is ending", TryCast(e.ExceptionObject, Exception))
        Try
            MessageBox.Show(Localization.T("shell_unhandled") & Environment.NewLine & Environment.NewLine &
                            Localization.T("shell_problem_logged"), Localization.T("shell_problem_title"),
                            MessageBoxButtons.OK, MessageBoxIcon.Error)
        Catch ex As Exception
            ShellLog.Write("showing the ending notice", ex)
        End Try
    End Sub

End Module
