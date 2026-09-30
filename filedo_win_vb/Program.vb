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
    Private Declare Function ShowWindow Lib "user32" (hWnd As IntPtr, nCmdShow As Integer) As Boolean
    Private Declare Function IsIconic Lib "user32" (hWnd As IntPtr) As Boolean
    Private Const SW_RESTORE As Integer = 9

    ' SHELL-12: the single-instance claim, held for the life of the process. A named mutex in the
    ' session namespace, because two quick starts both find no window by process name and both open.
    Private Const InstanceMutexName As String = "Local\FileDO.Shell"
    Private instanceMutex As Mutex

    ' How long a start that lost the claim waits for the first copy's window to exist.
    Private Const HandOverWaitMs As Integer = 5000

    <STAThread>
    Public Sub Main()
        Application.EnableVisualStyles()
        Application.SetCompatibleTextRenderingDefault(False)

        ' `--write-menu-icons <folder>` writes the Explorer icons (MenuIcons.vb) and exits: 0 when
        ' every file was written, 1 otherwise. No window, like --selftest.
        Dim argv = Environment.GetCommandLineArgs()
        Dim iconsAt = Array.IndexOf(argv, "--write-menu-icons")
        If iconsAt >= 0 Then
            Dim code = 1
            Try
                If iconsAt + 1 < argv.Length AndAlso Glyphs.Problems().Count = 0 Then
                    For Each written In MenuIcons.WriteAll(argv(iconsAt + 1))
                        Console.WriteLine(written)
                    Next
                    code = 0
                End If
            Catch ex As Exception
                ShellLog.Write("write-menu-icons", ex)
            End Try
            Environment.Exit(code)
            Return
        End If

        ' `--capture-screens <folder>` renders the guide screenshots (Capture.vb) and exits: 0 when
        ' every file was written, 1 otherwise. It builds windows but writes no setting.
        Dim captureAt = Array.IndexOf(argv, "--capture-screens")
        If captureAt >= 0 Then
            Environment.Exit(If(captureAt + 1 < argv.Length, Capture.Run(argv(captureAt + 1)), 1))
            Return
        End If

        If Environment.GetCommandLineArgs().Contains("--selftest") Then
            ' SHELL-15: whatever happens inside, the gate ends with a log and a code - a crash is a
            ' FAIL, never a process that dies with nothing written.
            Dim code As Integer
            Try
                code = SelfTest.Run()
            Catch ex As Exception
                code = SelfTest.WriteCrash(ex)
            End Try
            Environment.Exit(code)
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
        Dim target = StartupTarget()
        ' `--disks` starts straight into the Disk Manager, with no shell window (SP-0063 4.2); a
        ' second one while this program runs brings the running copy's manager forward (D1).
        Dim disksOnly = target Is Nothing AndAlso WantsDiskManager(Environment.GetCommandLineArgs())
        Dim first = ClaimSingleInstance()
        If target Is Nothing AndAlso Not first Then
            If disksOnly AndAlso AppHost.HandOverDisks() Then Return
            ' A plain start asks the running copy for its shell window - which it opens if the copy was
            ' started as the Disk Manager alone (the Start menu's FileDO entry after its Disk Manager
            ' entry). A copy that does not listen (an older one) is focused as before.
            If Not disksOnly AndAlso AppHost.HandOverShell() Then Return
            If HandOverToRunningCopy() Then Return
        End If

        ' A start on a `.fdd` (SP-0004 6.4, T6.18) is answered before any window exists: a container
        ' that is mounted already opens its drive in Explorer and nothing else, which is what a user
        ' means by opening it - no window flashes up only to point elsewhere.
        Dim diskDecision As DiskRoute.Decision = Nothing
        If target IsNot Nothing AndAlso DiskRoute.IsContainer(target) Then
            diskDecision = DiskStartDecision(target, Environment.GetCommandLineArgs())
            If diskDecision.Action = DiskRoute.RouteAction.OpenDrive Then
                Ui.OpenFolder(Nothing, diskDecision.Letter & "\")
                Return
            End If
        End If

        ' SHELL-02: the retention the Settings page promises - reports 30 days, run files 7 - is
        ' kept here, once per start, off the window's thread.
        StartSweep()

        ShellLog.Debug("start: " & StartLogLine(Environment.GetCommandLineArgs().Skip(1).ToArray()))

        ' The process's windows - the shell, the Disk Manager - live in one AppHost, which ends the
        ' process when the last of them has closed (SP-0063 D1).
        Dim host As AppHost = Nothing
        Try
            If disksOnly Then
                host = New AppHost(Nothing, True)
            Else
                host = New AppHost(New ShellForm(target, diskDecision), False)
            End If
        Catch ex As Exception
            ' The window could not be built. There is no window to own the message, so it stands on
            ' its own - and it still says what happened and what can be done, not the exception.
            ShellLog.Write("the window could not open", ex)
            ShellDialog.Problem(Nothing, Localization.Format(Localization.T("shell_open_failed"), Problems.Cause(ex)))
            Return
        End Try
        Application.Run(host)
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
        Dim mode = DiskRoute.ModeFrom(args)
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

    ' A start that lost the claim waits for the first copy's window - it may still be opening - and
    ' brings it forward. When none appears in time, this start opens its own rather than nothing.
    Private Function HandOverToRunningCopy() As Boolean
        Dim deadline = DateTime.Now.AddMilliseconds(HandOverWaitMs)
        Do
            Dim hwnd = RunningCopyWindow()
            If hwnd <> IntPtr.Zero Then
                If IsIconic(hwnd) Then ShowWindow(hwnd, SW_RESTORE)
                SetForegroundWindow(hwnd)
                Return True
            End If
            If DateTime.Now >= deadline Then Exit Do
            Thread.Sleep(100)
        Loop
        Return False
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
