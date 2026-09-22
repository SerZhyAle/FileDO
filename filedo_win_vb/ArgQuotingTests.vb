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

        Return passed
    End Function

End Module
