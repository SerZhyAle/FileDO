' The shell's own log: %LOCALAPPDATA%\FileDO\filedo_win.log.
'
' APP-BEHAVIOUR rule 6: an exception never reaches the user as text - it reaches this file, and the
' user gets a named cause and something to do about it. The file is what "Send logs" packs
' (LogReport), so a failure the user reports arrives with its detail even though the window never
' showed it.
'
' Writing here must never become a failure of its own: every path swallows its own errors, because a
' log that throws turns one problem into two. Each process keeps a bounded session log;
' startup prunes closed sessions while active processes keep their files open against deletion.
'
' `-debug` on the command line adds diagnostic lines (Debug) to the same file; without it only
' errors and the few lifecycle lines are written.
Imports System.IO

Module ShellLog

    Private Const FileName As String = "filedo_win.log"
    Private Const MaxBytes As Long = 16L * 1024L * 1024L
    Private sessionPath As String = Nothing
    Private sessionLease As FileStream = Nothing

    Private ReadOnly gate As New Object()
    Private debugChecked As Boolean = False
    Private debugOn As Boolean = False

    ' The self-test's own log file, so the failures it provokes on purpose never reach the user's.
    Friend PathForTest As String = Nothing

    Public Function LogPath() As String
        If PathForTest IsNot Nothing Then Return PathForTest
        SyncLock gate
            If sessionPath Is Nothing Then
                Dim dir = Runner.GetAppDataDir()
                Directory.CreateDirectory(dir)
                sessionPath = Path.Combine(dir, "filedo_win.session-" & DateTime.UtcNow.ToString("yyyyMMdd-HHmmss-fffffff") & "-" & Guid.NewGuid().ToString("N") & ".log")
                ' Held for this process's lifetime. Other starts cannot remove a live session.
                Using created As New FileStream(sessionPath, FileMode.CreateNew, FileAccess.Write, FileShare.ReadWrite)
                End Using
                sessionLease = New FileStream(sessionPath, FileMode.Open, FileAccess.Read, FileShare.ReadWrite)
                PruneSessions(dir)
            End If
            Return sessionPath
        End SyncLock
    End Function

    Friend Sub PruneSessions(dir As String)
        Dim logs = Directory.GetFiles(dir, "filedo_win.session-*.log").OrderByDescending(Function(f) Path.GetFileName(f)).ToArray()
        For Each f In logs.Skip(10)
            Try
                ' The active reader lease refuses this open. Pruning is never by process-name guessing.
                Using lease As New FileStream(f, FileMode.Open, FileAccess.Read, FileShare.None)
                End Using
                File.Delete(f)
            Catch ex As IOException
            Catch ex As UnauthorizedAccessException
            End Try
        Next
    End Sub

    Friend Sub CompactLog(target As String, ceiling As Long, headBytes As Integer, tailBytes As Integer)
        Using stream As New FileStream(target, FileMode.Open, FileAccess.ReadWrite, FileShare.ReadWrite)
            Dim length = stream.Length
            If length <= ceiling Then Return
            Dim head(headBytes - 1) As Byte
            Dim tail(tailBytes - 1) As Byte
            ReadFully(stream, head)
            stream.Position = length - tailBytes
            ReadFully(stream, tail)
            Dim marker = System.Text.Encoding.UTF8.GetBytes(Environment.NewLine &
                "[Diag] LOG COMPACTED | dropped_middle_bytes=" & (length - headBytes - tailBytes).ToString() &
                " | kept_head_bytes=" & headBytes.ToString() & " | kept_tail_bytes=" & tailBytes.ToString() & Environment.NewLine)
            stream.Position = 0
            stream.Write(head, 0, head.Length)
            stream.Write(marker, 0, marker.Length)
            stream.Write(tail, 0, tail.Length)
            stream.SetLength(stream.Position)
            stream.Flush()
        End Using
    End Sub

    Private Sub ReadFully(stream As Stream, bytes As Byte())
        Dim used = 0
        While used < bytes.Length
            Dim n = stream.Read(bytes, used, bytes.Length - used)
            If n = 0 Then Throw New EndOfStreamException()
            used += n
        End While
    End Sub

    Public Function DebugEnabled() As Boolean
        If Not debugChecked Then
            Try
                debugOn = Environment.GetCommandLineArgs().Contains("-debug")
            Catch
                debugOn = False
            End Try
            debugChecked = True
        End If
        Return debugOn
    End Function

    ' A caught exception, with where it was caught. The full text - type, message and stack - is
    ' the log's, never the screen's.
    Public Sub Write(context As String, ex As Exception)
        If ex Is Nothing Then
            Append("ERROR", context)
        Else
            Append("ERROR", context & ": " & ex.ToString())
        End If
    End Sub

    Public Sub Info(line As String)
        Append("INFO", line)
    End Sub

    Public Sub Debug(line As String)
        If DebugEnabled() Then Append("DEBUG", line)
    End Sub

    ' SHELL-06: SyncLock only orders this process's threads. Two windows are two processes - a
    ' double-clicked container opens its own - and without a gate between them one could rotate
    ' the file while the other appended to it, or both could append at once and one line be lost.
    ' The gate is a named mutex in the session; a writer that cannot get it in two seconds writes
    ' anyway, because a log line late is better than a log that stops.
    Private Const CrossProcessName As String = "Local\FileDO.ShellLog"
    Private Const CrossProcessWaitMs As Integer = 2000
    Private crossProcess As Threading.Mutex = Nothing
    Private crossProcessTried As Boolean = False

    Private Function CrossProcessGate() As Threading.Mutex
        If Not crossProcessTried Then
            crossProcessTried = True
            Try
                crossProcess = New Threading.Mutex(False, CrossProcessName)
            Catch
                crossProcess = Nothing
            End Try
        End If
        Return crossProcess
    End Function

    Private Sub Append(level As String, text As String)
        SyncLock gate
            Dim m = CrossProcessGate()
            Dim held = False
            Try
                If m IsNot Nothing Then
                    Try
                        held = m.WaitOne(CrossProcessWaitMs)
                    Catch ex As Threading.AbandonedMutexException
                        ' The other window ended while it held the gate; the gate is this one's now.
                        held = True
                    End Try
                End If
                Dim target = LogPath()
                File.AppendAllText(target, DateTime.Now.ToString("yyyy-MM-dd HH:mm:ss.fff") & " " & level & " " &
                                   text & Environment.NewLine)
                CompactLog(target, MaxBytes, 1024 * 1024, 7 * 1024 * 1024)
            Catch
            Finally
                If held Then
                    Try
                        m.ReleaseMutex()
                    Catch
                    End Try
                End If
            End Try
        End SyncLock
    End Sub

End Module
