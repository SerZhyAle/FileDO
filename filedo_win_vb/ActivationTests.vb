Imports System.IO
Imports System.Threading

' APP-ACTIVATION 0.13, the focus-only profile (SP-0170): a second start signals the first copy with a
' registered window message, payload-less and bounded. These rows hold the three cases the contract
' names for it - a hung receiver, a start that carries a target, and a signal that must stay empty -
' on a stand-in window of this process. A receiver in another user's session cannot be built here;
' that case is the manual kit's (the mutex is session-local and a second account cannot open it).
Partial Module SelfTest

    ' A hidden top-level window on its own thread whose procedure can be made to stop answering.
    Private NotInheritable Class StandInWindow
        Inherits NativeWindow

        Friend Const AnswerMsg As Integer = &H8001
        Friend Const HangMsg As Integer = &H8002
        Friend Const QuitMsg As Integer = &H8003

        Friend Shared LastWParam As IntPtr = New IntPtr(-1)
        Friend Shared LastLParam As IntPtr = New IntPtr(-1)
        Private ReadOnly holdMs As Integer

        Friend Sub New(hold As Integer)
            holdMs = hold
            CreateHandle(New CreateParams())
        End Sub

        Protected Overrides Sub WndProc(ByRef m As Message)
            Select Case m.Msg
                Case AnswerMsg
                    LastWParam = m.WParam
                    LastLParam = m.LParam
                    m.Result = New IntPtr(1)
                    Return
                Case HangMsg
                    ' The window stops answering: the thread is inside its procedure and pumps nothing.
                    Thread.Sleep(holdMs)
                    m.Result = New IntPtr(1)
                    Return
                Case QuitMsg
                    Application.ExitThread()
                    Return
            End Select
            MyBase.WndProc(m)
        End Sub
    End Class

    Private Declare Auto Function PostStandInMessage Lib "user32" Alias "PostMessage" (hWnd As IntPtr, msg As Integer, wParam As IntPtr, lParam As IntPtr) As Boolean

    Private Sub CheckSecondStart()
        ' What a start is handed over for: a plain second start only. A file is what a start on a file
        ' is about, so it is never handed over - not by a second copy, not by the first.
        Dim targetPath = Path.Combine(Path.GetTempPath(), "filedo_selftest_target_" & Guid.NewGuid().ToString("N") & ".txt")
        Try
            File.WriteAllText(targetPath, "x")
            Dim target = Program.StartupTargetFrom(New String() {"filedo_win.exe", targetPath})
            Check("activation:file-target-is-a-target", target IsNot Nothing AndAlso String.Equals(target, Path.GetFullPath(targetPath), StringComparison.OrdinalIgnoreCase), target)
            Check("activation:file-target-not-handed-over", Not Program.WantsHandOver(target, False) AndAlso Not Program.WantsHandOver(target, True), "")
        Finally
            Try
                File.Delete(targetPath)
            Catch
            End Try
        End Try
        Check("activation:missing-file-is-no-target", Program.StartupTargetFrom(New String() {"filedo_win.exe", Path.Combine(Path.GetTempPath(), "filedo_selftest_absent_" & Guid.NewGuid().ToString("N"))}) Is Nothing, "")
        Check("activation:plain-second-start-handed-over", Program.WantsHandOver(Nothing, False), "")
        Check("activation:first-copy-not-handed-over", Not Program.WantsHandOver(Nothing, True), "")
        Check("activation:no-window-no-claim", Not AppHost.AskWindow(IntPtr.Zero, AppHost.ShowShellMessage, 100) AndAlso Not Program.FocusWindow(IntPtr.Zero), "")

        ' A receiver that answers, then one that hangs.
        Dim hwnd As IntPtr = IntPtr.Zero
        Dim ready As New ManualResetEventSlim(False)
        Dim worker As New Thread(Sub()
                                     Dim w As New StandInWindow(7000)
                                     hwnd = w.Handle
                                     ready.Set()
                                     Application.Run()
                                     w.DestroyHandle()
                                 End Sub)
        worker.SetApartmentState(ApartmentState.STA)
        worker.IsBackground = True
        worker.Start()
        If Not ready.Wait(5000) OrElse hwnd = IntPtr.Zero Then
            Check("activation:stand-in-window", False, "the stand-in window did not start")
            Return
        End If
        Try
            Check("activation:answering-receiver", AppHost.AskWindow(hwnd, StandInWindow.AnswerMsg, 1000), "")
            Check("activation:signal-is-payload-less", StandInWindow.LastWParam = IntPtr.Zero AndAlso StandInWindow.LastLParam = IntPtr.Zero,
                  StandInWindow.LastWParam.ToString() & "/" & StandInWindow.LastLParam.ToString())

            Dim hungSince = Stopwatch.StartNew()
            PostStandInMessage(hwnd, StandInWindow.HangMsg, IntPtr.Zero, IntPtr.Zero)
            Thread.Sleep(150)
            Dim ask = Stopwatch.StartNew()
            Dim answered = AppHost.AskWindow(hwnd, StandInWindow.AnswerMsg, 300)
            ask.Stop()
            Check("activation:hung-receiver-bounded", Not answered AndAlso ask.ElapsedMilliseconds < 1500, "answered=" & answered.ToString() & ", " & ask.ElapsedMilliseconds.ToString() & " ms")

            ' The system calls a window hung after five seconds without a message pumped.
            Dim wait = 5800 - CInt(hungSince.ElapsedMilliseconds)
            If wait > 0 Then Thread.Sleep(wait)
            Dim focus = Stopwatch.StartNew()
            Dim claimed = Program.FocusWindow(hwnd)
            focus.Stop()
            Check("activation:hung-window-not-claimed", Not claimed AndAlso focus.ElapsedMilliseconds < 1500, "claimed=" & claimed.ToString() & ", " & focus.ElapsedMilliseconds.ToString() & " ms")

            ' When the receiver comes back the same signal is answered: a hung start leaves nothing behind.
            Dim back = False
            Dim until = Stopwatch.StartNew()
            Do While until.ElapsedMilliseconds < 6000 AndAlso Not back
                back = AppHost.AskWindow(hwnd, StandInWindow.AnswerMsg, 500)
                If Not back Then Thread.Sleep(100)
            Loop
            Check("activation:receiver-recovers", back, "")
        Finally
            PostStandInMessage(hwnd, StandInWindow.QuitMsg, IntPtr.Zero, IntPtr.Zero)
            worker.Join(3000)
        End Try
    End Sub
End Module
