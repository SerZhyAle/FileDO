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
'   - Every exception nothing else caught is logged and shown as a cause and actions, never as the
'     stock WinForms dialog with a stack trace in it (APP-BEHAVIOUR rule 6).
'   - A second copy hands the desktop back to the one already open - unless it was started on a
'     file, which is what that start was about.
Imports System.Threading

Module Program

    Private Declare Function SetForegroundWindow Lib "user32" (hWnd As IntPtr) As Boolean
    Private Declare Function ShowWindow Lib "user32" (hWnd As IntPtr, nCmdShow As Integer) As Boolean
    Private Const SW_RESTORE As Integer = 9

    <STAThread>
    Public Sub Main()
        Application.EnableVisualStyles()
        Application.SetCompatibleTextRenderingDefault(False)

        If Environment.GetCommandLineArgs().Contains("--selftest") Then
            Environment.Exit(SelfTest.Run())
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
        If target Is Nothing AndAlso HandOverToRunningCopy() Then Return

        ShellLog.Debug("start: " & String.Join(" ", Environment.GetCommandLineArgs().Skip(1).ToArray()))

        Dim shell As ShellForm = Nothing
        Try
            shell = New ShellForm(target)
        Catch ex As Exception
            ' The shell could not be built. There is no window to own the message, so it stands on
            ' its own - and it still says what happened and what can be done, not the exception.
            ShellLog.Write("the window could not open", ex)
            ShellDialog.Problem(Nothing, Localization.Format(Localization.T("shell_open_failed"), Problems.Cause(ex)))
            Return
        End Try
        Application.Run(shell)
    End Sub

    ' The file the program was started on, if it was started on one.
    '
    ' Explorer hands a file to this program as a bare path (SP-0005 9.4), and the switches this
    ' program understands are all prefixed, so "the first argument that is not a switch and is a
    ' file on disk" needs no new grammar. Anything else is left to the shell's own opening page.
    Friend Function StartupTarget() As String
        Dim args = Environment.GetCommandLineArgs()
        For i As Integer = 1 To args.Length - 1
            Dim a = args(i)
            If String.IsNullOrWhiteSpace(a) Then Continue For
            If a.StartsWith("-") OrElse a.StartsWith("/") Then Continue For
            Try
                If IO.File.Exists(a) Then Return a
            Catch
            End Try
        Next
        Return Nothing
    End Function

    Private Function HandOverToRunningCopy() As Boolean
        Try
            Dim me_ = Process.GetCurrentProcess()
            For Each p In Process.GetProcessesByName(me_.ProcessName)
                If p.Id <> me_.Id AndAlso p.MainWindowHandle <> IntPtr.Zero Then
                    ShowWindow(p.MainWindowHandle, SW_RESTORE)
                    SetForegroundWindow(p.MainWindowHandle)
                    Return True
                End If
            Next
        Catch ex As Exception
            ' Not finding the other copy only means a second window opens.
            ShellLog.Write("single-instance check", ex)
        End Try
        Return False
    End Function

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
