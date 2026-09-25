' The shell's own log: %LOCALAPPDATA%\FileDO\filedo_win.log.
'
' APP-BEHAVIOUR rule 6: an exception never reaches the user as text - it reaches this file, and the
' user gets a named cause and something to do about it. The file is what "Send logs" packs
' (LogReport), so a failure the user reports arrives with its detail even though the window never
' showed it.
'
' Writing here must never become a failure of its own: every path swallows its own errors, because a
' log that throws turns one problem into two. The file is capped and rolled over once, so a window
' left open for a month cannot fill a disk with it.
'
' `-debug` on the command line adds diagnostic lines (Debug) to the same file; without it only
' errors and the few lifecycle lines are written.
Imports System.IO

Module ShellLog

    Private Const FileName As String = "filedo_win.log"
    Private Const MaxBytes As Long = 1024L * 1024L

    Private ReadOnly gate As New Object()
    Private debugChecked As Boolean = False
    Private debugOn As Boolean = False

    ' The self-test's own log file, so the failures it provokes on purpose never reach the user's.
    Friend PathForTest As String = Nothing

    Public Function LogPath() As String
        If PathForTest IsNot Nothing Then Return PathForTest
        Return Path.Combine(Runner.GetAppDataDir(), FileName)
    End Function

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
                Try
                    Dim fi As New FileInfo(target)
                    If fi.Exists AndAlso fi.Length > MaxBytes Then
                        Dim older = target & ".1"
                        If File.Exists(older) Then File.Delete(older)
                        File.Move(target, older)
                    End If
                Catch
                End Try
                File.AppendAllText(target, DateTime.Now.ToString("yyyy-MM-dd HH:mm:ss.fff") & " " & level & " " &
                                   text & Environment.NewLine)
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
