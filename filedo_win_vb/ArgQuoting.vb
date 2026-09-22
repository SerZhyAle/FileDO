' Command-line argument quoting following Windows CommandLineToArgvW rules (SP-0006 section 7.2).
' .NET Framework 4.8 lacks ProcessStartInfo.ArgumentList, so this helper correctly escapes arguments
' for CreateProcess without risking command injection or broken paths with spaces/quotes.
Public Module ArgQuoting

    Public Function EscapeArg(arg As String) As String
        If String.IsNullOrEmpty(arg) Then
            Return """"""
        End If

        ' If the argument does not contain spaces, tabs, newlines or quotes or trailing backslashes,
        ' it doesn't strictly need quotes.
        Dim needsQuotes = False
        For Each ch As Char In arg
            If Char.IsWhiteSpace(ch) OrElse ch = """"c Then
                needsQuotes = True
                Exit For
            End If
        Next

        If Not needsQuotes Then
            Return arg
        End If

        Dim sb As New System.Text.StringBuilder()
        sb.Append(""""c)

        Dim backslashCount = 0
        For i As Integer = 0 To arg.Length - 1
            Dim c = arg(i)
            If c = "\"c Then
                backslashCount += 1
            ElseIf c = """"c Then
                ' 2N+1 backslashes followed by quote
                sb.Append(New String("\"c, backslashCount * 2 + 1))
                sb.Append(""""c)
                backslashCount = 0
            Else
                If backslashCount > 0 Then
                    sb.Append(New String("\"c, backslashCount))
                    backslashCount = 0
                End If
                sb.Append(c)
            End If
        Next

        ' 2N backslashes before the closing quote
        If backslashCount > 0 Then
            sb.Append(New String("\"c, backslashCount * 2))
        End If

        sb.Append(""""c)
        Return sb.ToString()
    End Function

    ' The inverse: one command line back into the argument list Windows would hand a program.
    '
    ' The expert page lets a person edit the command as text, and that text then has to become the
    ' list the runner passes. Splitting on spaces - which is the obvious thing to write - breaks the
    ' first path with a space in it and silently runs a different command than the one on screen,
    ' which is exactly what section 7.1 records as today's defect. The rules here are
    ' CommandLineToArgvW's: a quote toggles quoting, 2N backslashes before a quote are N backslashes
    ' and the quote is special, 2N+1 are N backslashes and a literal quote.
    Public Function SplitArgs(commandLine As String) As List(Of String)
        Dim result As New List(Of String)()
        If String.IsNullOrEmpty(commandLine) Then Return result

        Dim current As New System.Text.StringBuilder()
        Dim inQuotes = False
        Dim have = False
        Dim backslashes = 0

        For i As Integer = 0 To commandLine.Length - 1
            Dim c = commandLine(i)
            If c = "\"c Then
                backslashes += 1
                Continue For
            End If

            If c = """"c Then
                current.Append(New String("\"c, backslashes \ 2))
                If (backslashes Mod 2) = 1 Then
                    current.Append(""""c)
                Else
                    inQuotes = Not inQuotes
                End If
                backslashes = 0
                have = True
                Continue For
            End If

            If backslashes > 0 Then
                current.Append(New String("\"c, backslashes))
                backslashes = 0
                have = True
            End If

            If Char.IsWhiteSpace(c) AndAlso Not inQuotes Then
                If have Then
                    result.Add(current.ToString())
                    current.Clear()
                    have = False
                End If
            Else
                current.Append(c)
                have = True
            End If
        Next

        If backslashes > 0 Then
            current.Append(New String("\"c, backslashes))
            have = True
        End If
        If have Then result.Add(current.ToString())

        Return result
    End Function

    Public Function JoinArgs(args As IEnumerable(Of String)) As String
        If args Is Nothing Then Return ""
        Dim escapedList As New List(Of String)()
        For Each a In args
            escapedList.Add(EscapeArg(a))
        Next
        Return String.Join(" ", escapedList)
    End Function

End Module
