' Unit tests for ArgQuoting (SP-0006 section 7.2 / M1).
Public Module ArgQuotingTests

    Public Function RunTests() As Boolean
        Dim passed = True

        ' 1. Empty string
        If ArgQuoting.EscapeArg("") <> """""" Then
            passed = False
        End If

        ' 2. Simple token without special chars
        If ArgQuoting.EscapeArg("simple") <> "simple" Then
            passed = False
        End If

        ' 3. Token with spaces
        If ArgQuoting.EscapeArg("hello world") <> """hello world""" Then
            passed = False
        End If

        ' 4. Token with trailing backslash and spaces
        ' "C:\My Documents\" -> """C:\My Documents\\"""
        If ArgQuoting.EscapeArg("C:\My Documents\") <> """C:\My Documents\\""" Then
            passed = False
        End If

        ' 5. Token with internal quotes
        ' a"b -> """a\"b"""
        If ArgQuoting.EscapeArg("a""b") <> """a\""b""" Then
            passed = False
        End If

        ' 6. JoinArgs
        Dim joined = ArgQuoting.JoinArgs(New String() {"filedo", "C:\My Folder\", "speed", "100"})
        Dim expected = "filedo ""C:\My Folder\\"" speed 100"
        If joined <> expected Then
            passed = False
        End If

        ' 7. SplitArgs keeps a quoted path with a space in it as one argument.
        Dim split = ArgQuoting.SplitArgs("C: speed ""D:\My Folder\sub"" 100")
        If split.Count <> 4 OrElse split(2) <> "D:\My Folder\sub" Then
            passed = False
        End If

        ' 8. A round trip: whatever JoinArgs writes, SplitArgs reads back unchanged. This is the
        '    property that matters, because the expert page writes one and the runner reads the other.
        Dim original = New String() {"C:\My Documents\", "a""b", "plain", "two words"}
        Dim back = ArgQuoting.SplitArgs(ArgQuoting.JoinArgs(original))
        If back.Count <> original.Length Then
            passed = False
        Else
            For i As Integer = 0 To original.Length - 1
                If back(i) <> original(i) Then passed = False
            Next
        End If

        For Each c In NamedCases()
            If Not c.Value Then passed = False
        Next

        Return passed
    End Function

    ' The cases SP-0029 added, by name, so the self-test reports each one on its own row.
    Public Function NamedCases() As List(Of KeyValuePair(Of String, Boolean))
        Dim cases As New List(Of KeyValuePair(Of String, Boolean))
        Dim add = Sub(name As String, ok As Boolean) cases.Add(New KeyValuePair(Of String, Boolean)(name, ok))

        ' GUI-21: only a space or a tab separates arguments. A no-break space (and U+3000) is part
        ' of a name, as it is to filedo.exe.
        Dim nbsp = ArgQuoting.SplitArgs("C:\a" & ChrW(&HA0) & "b info")
        add("split:nbsp-is-not-a-separator", nbsp.Count = 2 AndAlso nbsp(0) = "C:\a" & ChrW(&HA0) & "b")
        Dim ideo = ArgQuoting.SplitArgs("x" & ChrW(&H3000) & "y")
        add("split:ideographic-space-is-not-a-separator", ideo.Count = 1)
        Dim tabbed = ArgQuoting.SplitArgs("a" & ControlChars.Tab & "b")
        add("split:tab-separates", tabbed.Count = 2)

        ' GUI-21: "" inside quotes is a literal quote and ends the quoted part (Go's readNextArg).
        Dim dq = ArgQuoting.SplitArgs("""a""""b"" c")
        add("split:doubled-quote-inside-quotes", dq.Count = 1 AndAlso dq(0) = "a""b c")
        Dim dq2 = ArgQuoting.SplitArgs("""a""""b""")
        add("split:doubled-quote-literal", dq2.Count = 1 AndAlso dq2(0) = "a""b")

        ' GUI-20: the copy for cmd.exe quotes cmd's characters, and survives cmd's own reading.
        add("cmd-copy:ampersand-quoted", ArgQuoting.EscapeArgForCmd("C:\R&D") = """C:\R&D""")
        For Each sample In New String() {"C:\R&D", "C:\100%\x", "C:\a\%b", "%PATH%", "x^y", "C:\My (x86)\", "a|b<c>d",
                                         "plain", "C:\dir with space\"}
            Dim pasted = CmdLineAsProgramSeesIt("filedo.exe " & ArgQuoting.EscapeArgForCmd(sample))
            Dim args = ArgQuoting.SplitArgs(pasted)
            add("cmd-copy:round-trip:" & sample, args.Count = 2 AndAlso args(1) = sample)
        Next
        Return cases
    End Function

    ' What cmd.exe hands a program for a line typed at its prompt, for the characters the copy
    ' escapes: a quote toggles cmd's quoting, and outside quotes a caret makes the next character
    ' literal and is removed. (%VAR% is left alone at the prompt when VAR does not exist, and the
    ' escaped form never names a variable that can: ^ and a quote are always inside the name.)
    Private Function CmdLineAsProgramSeesIt(line As String) As String
        Dim sb As New System.Text.StringBuilder()
        Dim quoted = False
        Dim i = 0
        While i < line.Length
            Dim c = line(i)
            If c = """"c Then
                quoted = Not quoted
                sb.Append(c)
            ElseIf c = "^"c AndAlso Not quoted AndAlso i + 1 < line.Length Then
                i += 1
                sb.Append(line(i))
            ElseIf Not quoted AndAlso "&|<>".IndexOf(c) >= 0 Then
                ' An unquoted separator would have ended the command here.
                Return sb.ToString()
            Else
                sb.Append(c)
            End If
            i += 1
        End While
        Return sb.ToString()
    End Function

End Module
