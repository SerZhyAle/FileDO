Imports System.IO
Imports System.IO.Pipes
Imports System.Runtime.InteropServices
Imports System.Security.AccessControl
Imports System.Security.Principal
Imports System.Text
Imports System.Threading

' The command pipe of the running filedo_win.exe (APP-COMMANDS rule 7, first bullet; APP-ACTIVATION rule 3).
' One request line in, one response line out, on the same connection, then the connection closes.
'
' - Only the first copy of the session listens (Program.vb starts it after it won the single-instance claim);
'   a window opened on a file is its own process and never does.
' - The pipe admits the current user and refuses the network (rule 15). An elevated copy refuses a caller
'   that is not elevated (reason `elevation`).
' - One command at a time, in the order they arrive (rule 10): the pipe has one instance, so a second caller
'   waits in its connect bound and is answered busy by its own side.
' - The listener runs on a background thread of its own and survives every connection (APP-ACTIVATION
'   rule 3): a malformed, empty, oversized or abandoned request is answered or dropped, never fatal.
' - The work runs on the window's thread. A window thread that does not take the command within the start
'   bound - a hung window, a nested loop that pumps nothing - is answered busy, and the command it would
'   have run is not run later (rule 10, case C5).
Friend NotInheritable Class CommandListener

    Friend Shared Current As CommandListener

    ' How long the window's thread has to start a command before the answer is busy. Inside the delivery
    ' bound of rule 8 (1 s) as the caller counts it.
    Friend Const StartBoundMs As Integer = 800
    ' How long a connected caller has to send its line, and to read the answer before the pipe is reset.
    Friend Const ReadBoundMs As Integer = 2000

    Private ReadOnly pipeName As String
    Private ReadOnly manifest As CommandManifest
    Private ReadOnly run As Func(Of CommandRequest, CommandResponse)
    Private ReadOnly invoker As Control
    Private ReadOnly worker As Thread
    Private ReadOnly idle As New ManualResetEventSlim(True)
    Private stopping As Integer
    Private server As NamedPipeServerStream

    ' The listener of this process, started once the window program is the first copy.
    Friend Shared Sub Start()
        If Current IsNot Nothing Then Return
        Try
            Current = New CommandListener(AppCommands.PipeName(), AppCommands.Manifest(), AddressOf AppCommands.Execute)
        Catch ex As Exception
            ' Without it a command from outside answers "not running"; the window works as before.
            ShellLog.Write("listen for commands", ex)
        End Try
    End Sub

    ' Lets the answer being written reach its caller, then stops listening. Called after the message loop ended.
    Friend Shared Sub Shutdown(waitMs As Integer)
        Dim l = Current
        Current = Nothing
        If l IsNot Nothing Then l.StopListening(waitMs)
    End Sub

    ' Made on the window's thread: `invoker` is the window-thread handle the work is posted to.
    Friend Sub New(name As String, m As CommandManifest, work As Func(Of CommandRequest, CommandResponse))
        pipeName = name
        manifest = m
        run = work
        invoker = New Control()
        Dim h = invoker.Handle
        server = NewServer()
        worker = New Thread(AddressOf Listen) With {.IsBackground = True, .Name = "FileDO command pipe"}
        worker.Start()
    End Sub

    Friend Sub StopListening(waitMs As Integer)
        idle.Wait(Math.Max(0, waitMs))
        Interlocked.Exchange(stopping, 1)
        ' A throwaway connection unblocks a WaitForConnection in progress (APP-ACTIVATION rule 3, clean shutdown).
        Try
            Using nudge As New NamedPipeClientStream(".", pipeName, PipeDirection.InOut)
                nudge.Connect(100)
            End Using
        Catch ex As Exception
            ' Nobody was waiting, or the pipe is gone already.
        End Try
        worker.Join(500)
        DropServer()
    End Sub

    ' The current user, read and write; the network, never (APP-COMMANDS rule 15).
    Private Function NewServer() As NamedPipeServerStream
        Dim security As New PipeSecurity()
        Using id = WindowsIdentity.GetCurrent()
            security.AddAccessRule(New PipeAccessRule(id.User, PipeAccessRights.FullControl, AccessControlType.Allow))
        End Using
        security.AddAccessRule(New PipeAccessRule(New SecurityIdentifier(WellKnownSidType.NetworkSid, Nothing), PipeAccessRights.FullControl, AccessControlType.Deny))
        Return New NamedPipeServerStream(pipeName, PipeDirection.InOut, 1, PipeTransmissionMode.Byte, PipeOptions.Asynchronous, 4096, 4096, security)
    End Function

    Private Sub DropServer()
        Dim s = Interlocked.Exchange(server, Nothing)
        If s Is Nothing Then Return
        Try
            s.Dispose()
        Catch ex As Exception
            ShellLog.Write("close the command pipe", ex)
        End Try
    End Sub

    Private Sub Listen()
        Dim failures = 0
        While Interlocked.CompareExchange(stopping, 0, 0) = 0
            Try
                If server Is Nothing Then server = NewServer()
                server.WaitForConnection()
                If Interlocked.CompareExchange(stopping, 0, 0) <> 0 Then Exit While
                idle.Reset()
                Dim keep As Boolean
                Try
                    keep = Serve(server)
                Finally
                    idle.Set()
                End Try
                If keep Then
                    Try
                        server.Disconnect()
                    Catch ex As Exception
                        ' A connection the caller broke cannot always be disconnected: the instance is made again.
                        DropServer()
                    End Try
                Else
                    DropServer()
                End If
                failures = 0
            Catch ex As Exception
                ' One connection's failure is that connection's: the pipe is made again and the loop goes on.
                If Interlocked.CompareExchange(stopping, 0, 0) <> 0 Then Exit While
                ShellLog.Write("command pipe", ex)
                DropServer()
                failures += 1
                Thread.Sleep(Math.Min(5000, 100 * failures))
            End Try
        End While
    End Sub

    ' One connection: read the line, answer it, wait for the caller to take the answer. False when the pipe
    ' instance has to be made again (a read or a write that did not finish).
    Private Function Serve(pipe As NamedPipeServerStream) As Boolean
        Dim callerPid = ClientProcessId(pipe)
        Dim line As String = Nothing
        Dim tooLarge = False
        Dim got = ReadLine(pipe, line, tooLarge)
        If got = ReadOutcome.Abandoned Then Return False
        If got = ReadOutcome.Empty Then Return True

        Dim request As CommandRequest = Nothing
        Dim response As CommandResponse
        If tooLarge Then
            response = CommandResponse.Refused(10, "too-large", Localization.T("appcmd_too_large"))
        Else
            response = AppCommands.ReadRequest(line, manifest, request)
        End If
        If response Is Nothing AndAlso ElevatedOver(callerPid) Then
            response = CommandResponse.Refused(12, "elevation", Localization.T("appcmd_elevation"))
        End If
        If response Is Nothing Then response = AppCommands.Gate(request.Spec, request.Legacy)
        If response Is Nothing Then response = RunOnWindowThread(request)
        If request IsNot Nothing Then response.RequestId = request.RequestId

        Try
            ShellLog.Info(AppCommands.LogLine(request, callerPid, response))
        Catch ex As Exception
            ShellLog.Write("log a command", ex)
        End Try
        Return WriteAnswer(pipe, response)
    End Function

    Private Enum ReadOutcome
        Line
        Empty
        Abandoned
    End Enum

    ' Up to the first newline, at most 64 KiB (rule 3); a caller that sends nothing in time is dropped.
    Private Function ReadLine(pipe As NamedPipeServerStream, ByRef line As String, ByRef tooLarge As Boolean) As ReadOutcome
        Dim buf(4095) As Byte
        Dim acc As New MemoryStream()
        Dim deadline = Environment.TickCount + ReadBoundMs
        Do
            Dim left = deadline - Environment.TickCount
            If left <= 0 Then Return ReadOutcome.Abandoned
            Dim n As Integer
            Try
                Dim t = pipe.ReadAsync(buf, 0, buf.Length)
                If Not t.Wait(left) Then Return ReadOutcome.Abandoned
                n = t.Result
            Catch ex As Exception
                ' The caller left mid-line.
                Return ReadOutcome.Abandoned
            End Try
            If n <= 0 Then
                If acc.Length = 0 Then Return ReadOutcome.Empty
                Exit Do
            End If
            Dim nl = Array.IndexOf(buf, CByte(10), 0, n)
            If nl >= 0 Then
                acc.Write(buf, 0, nl)
                Exit Do
            End If
            acc.Write(buf, 0, n)
            If acc.Length > AppCommands.MaxRequestBytes Then
                tooLarge = True
                Return ReadOutcome.Line
            End If
        Loop
        If acc.Length > AppCommands.MaxRequestBytes Then
            tooLarge = True
            Return ReadOutcome.Line
        End If
        line = Encoding.UTF8.GetString(acc.ToArray()).TrimEnd(ChrW(13))
        Return ReadOutcome.Line
    End Function

    ' Writes the answer and waits for the caller to close its end, so the line is read before the pipe is
    ' disconnected (a disconnect discards what the caller has not read yet).
    Private Function WriteAnswer(pipe As NamedPipeServerStream, response As CommandResponse) As Boolean
        Dim line = response.ToLine()
        ' Item L: a response line is at most 64 KiB of UTF-8; a non-paged verb past it fails response-too-large (exit 1).
        If Encoding.UTF8.GetByteCount(line) > AppCommands.MaxRequestBytes Then
            Dim oversized = CommandResponse.Failed(1, "response-too-large", Localization.T("appcmd_response_too_large"))
            oversized.RequestId = response.RequestId
            line = oversized.ToLine()
        End If
        Dim bytes = Encoding.UTF8.GetBytes(line & vbLf)
        Try
            Dim w = pipe.WriteAsync(bytes, 0, bytes.Length)
            If Not w.Wait(ReadBoundMs) Then Return False
            pipe.Flush()
        Catch ex As Exception
            ' The caller left before its answer: nothing to deliver, the pipe is made again.
            Return False
        End Try
        Try
            Dim probe(0) As Byte
            Dim r = pipe.ReadAsync(probe, 0, 1)
            If Not r.Wait(ReadBoundMs) Then Return False
        Catch ex As Exception
            ' A broken pipe here is the caller closing its end after reading: what was waited for.
        End Try
        Return True
    End Function

    ' How a call on the window's thread ended.
    Private Enum WindowCall
        Finished
        ' The window's thread did not start it inside the start bound (or has ended); it will not run it later.
        NotTaken
        ' Started, and still going when the finish bound passed.
        NotFinished
        ' It threw; the cause is in the shell log.
        Faulted
    End Enum

    ' `work` on the window's thread, which must start it inside the start bound or not at all (rule 10, case C5): a
    ' call given up on does nothing when the thread comes to it later. `value` is what it returned, when Finished.
    Private Function OnWindowThread(Of T)(work As Func(Of T), label As String, finishBoundMs As Integer, ByRef value As T) As WindowCall
        Dim started As New ManualResetEventSlim(False)
        Dim finished As New ManualResetEventSlim(False)
        Dim claim = 0 ' 0 open, 1 given up by the listener, 2 taken by the window's thread
        Dim outcome As T = Nothing
        Dim faulted = False
        Dim job As MethodInvoker =
            Sub()
                If Interlocked.CompareExchange(claim, 2, 0) <> 0 Then Return
                started.Set()
                Try
                    outcome = work()
                Catch ex As Exception
                    ShellLog.Write("command " & label, ex)
                    faulted = True
                Finally
                    finished.Set()
                End Try
            End Sub
        Try
            invoker.BeginInvoke(job)
        Catch ex As Exception
            ' The window's thread has ended: the program is closing.
            Return WindowCall.NotTaken
        End Try
        If Not started.Wait(StartBoundMs) Then
            If Interlocked.CompareExchange(claim, 1, 0) = 0 Then Return WindowCall.NotTaken
        End If
        If Not finished.Wait(finishBoundMs) Then Return WindowCall.NotFinished
        If faulted Then Return WindowCall.Faulted
        value = outcome
        Return WindowCall.Finished
    End Function

    ' The answer for a call that did not finish; Nothing when it did.
    Private Shared Function Unfinished(state As WindowCall) As CommandResponse
        Select Case state
            Case WindowCall.NotTaken : Return CommandResponse.Busy(Localization.T("appcmd_busy"))
            Case WindowCall.NotFinished : Return CommandResponse.Failed(2, "timeout", Localization.T("appcmd_timeout"))
            Case WindowCall.Faulted : Return CommandResponse.Failed(1, "error", Localization.T("appcmd_failed"))
        End Select
        Return Nothing
    End Function

    ' The work on the window's thread, inside the start bound. A command that answers with something still to come
    ' (PendingCommand) is waited for here, on the listener's own thread.
    Private Function RunOnWindowThread(request As CommandRequest) As CommandResponse
        Dim clock = Diagnostics.Stopwatch.StartNew()
        Dim result As CommandResponse = Nothing
        Dim failure = Unfinished(OnWindowThread(Function() run(request), request.Command, request.Spec.CompletionMs + 1000, result))
        If failure IsNot Nothing Then Return failure
        If result Is Nothing Then Return CommandResponse.Failed(1, "error", Localization.T("appcmd_failed"))
        If result.Pending IsNot Nothing Then result = AwaitPending(request, result.Pending, clock)
        Return result
    End Function

    ' The bound of the wait is the verb's own `timeoutMs` less this margin, counted from when the command was taken:
    ' the caller counts its bound from its own write, a little earlier, and gets the answer from the program rather than
    ' its own silence (SP-0218 A6).
    Friend Const WaitMarginMs As Integer = 3000
    ' The looks at the thing waited for are this far apart, and it must hold at two of them in a row, so the pages
    ' have shown what the runs left before the answer goes out.
    Friend Const SettleMs As Integer = 100

    ' Asks `Poll` on the window's thread, each time inside the start bound, until it has held at two looks running or the
    ' bound passes; then `Done` or `TimedOut` is asked, also on the window's thread. Nothing here blocks that thread.
    Private Function AwaitPending(request As CommandRequest, pending As PendingCommand, clock As Diagnostics.Stopwatch) As CommandResponse
        Dim bound = Math.Max(1000, request.Spec.CompletionMs - WaitMarginMs)
        Dim held = 0
        Do
            Dim ended = False
            If OnWindowThread(pending.Poll, request.Command, StartBoundMs + 1000, ended) = WindowCall.Finished AndAlso ended Then
                held += 1
                If held >= 2 Then Return Conclude(request, pending.Done)
            Else
                held = 0
            End If
            If clock.ElapsedMilliseconds >= bound Then Exit Do
            Thread.Sleep(SettleMs)
        Loop
        Return Conclude(request, pending.TimedOut)
    End Function

    ' The closing answer of a waiting command, made on the window's thread.
    Private Function Conclude(request As CommandRequest, answer As Func(Of CommandResponse)) As CommandResponse
        Dim result As CommandResponse = Nothing
        Dim failure = Unfinished(OnWindowThread(answer, request.Command, request.Spec.CompletionMs, result))
        If failure IsNot Nothing Then Return failure
        If result Is Nothing Then Return CommandResponse.Failed(1, "error", Localization.T("appcmd_failed"))
        Return result
    End Function

    ' ---- who is asking -------------------------------------------------------

    <DllImport("kernel32.dll", SetLastError:=True)>
    Private Shared Function GetNamedPipeClientProcessId(pipe As IntPtr, ByRef clientProcessId As UInteger) As Boolean
    End Function

    <DllImport("kernel32.dll", SetLastError:=True)>
    Private Shared Function OpenProcess(access As UInteger, inherit As Boolean, pid As UInteger) As IntPtr
    End Function

    <DllImport("advapi32.dll", SetLastError:=True)>
    Private Shared Function OpenProcessToken(process As IntPtr, access As UInteger, ByRef token As IntPtr) As Boolean
    End Function

    <DllImport("advapi32.dll", SetLastError:=True)>
    Private Shared Function GetTokenInformation(token As IntPtr, infoClass As Integer, ByRef info As Integer, length As Integer, ByRef returned As Integer) As Boolean
    End Function

    <DllImport("kernel32.dll")>
    Private Shared Function CloseHandle(handle As IntPtr) As Boolean
    End Function

    Private Const ProcessQueryLimitedInformation As UInteger = &H1000UI
    Private Const TokenQuery As UInteger = &H8UI
    Private Const TokenElevationClass As Integer = 20

    Private Shared Function ClientProcessId(pipe As NamedPipeServerStream) As Integer
        Try
            Dim pid As UInteger
            If GetNamedPipeClientProcessId(pipe.SafePipeHandle.DangerousGetHandle(), pid) Then Return CInt(pid)
        Catch ex As Exception
            ShellLog.Write("read the caller of a command", ex)
        End Try
        Return 0
    End Function

    ' True when this process is elevated and the caller is not (or cannot be read): APP-COMMANDS rule 11, code 12.
    Friend Shared Function ElevatedOver(callerPid As Integer) As Boolean
        Using self = Process.GetCurrentProcess()
            If Not IsElevated(self.Id) Then Return False
        End Using
        Return callerPid = 0 OrElse Not IsElevated(callerPid)
    End Function

    Friend Shared Function IsElevated(pid As Integer) As Boolean
        Dim proc = OpenProcess(ProcessQueryLimitedInformation, False, CUInt(pid))
        If proc = IntPtr.Zero Then Return False
        Try
            Dim token As IntPtr = IntPtr.Zero
            If Not OpenProcessToken(proc, TokenQuery, token) Then Return False
            Try
                Dim elevated = 0
                Dim returned = 0
                If Not GetTokenInformation(token, TokenElevationClass, elevated, 4, returned) Then Return False
                Return elevated <> 0
            Finally
                CloseHandle(token)
            End Try
        Finally
            CloseHandle(proc)
        End Try
    End Function

End Class
