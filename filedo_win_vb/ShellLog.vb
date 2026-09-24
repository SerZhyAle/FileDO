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

    Public Function LogPath() As String
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

    Private Sub Append(level As String, text As String)
        SyncLock gate
            Try
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
            End Try
        End SyncLock
    End Sub

End Module
