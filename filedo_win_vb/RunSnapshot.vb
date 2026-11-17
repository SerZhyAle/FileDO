Imports System.Text.RegularExpressions

' SP-0218 / APP-COMMANDS 0.1: the runs of the window program as one list. A run lives in one of three places - the
' Job page and the Command page of the main window (each a Runner with its own events file and stop file) and the
' Disk Manager's running operations and queue - and none of them kept a list. Each place answers its own entries on
' the window's thread (`Runs`); AppHost joins them. `filedo job list` shows the list, `filedo status` counts it, and
' `job stop` and `quit --stop-jobs` wait on it.
'
' An entry says where the run is, its console verb, whether it runs or waits, the name of its current step and how
' far it is. It carries no path and no name of the person's files (APP-COMMANDS rule 13, class `safe`): the verb comes
' from a closed list, the step is a short word or empty, the percent a number or none.
Friend NotInheritable Class RunInfo
    Friend Place As String = ""
    Friend Verb As String = ""
    Friend State As String = ""
    Friend StepName As String = ""
    ' 0 to 100, or -1 when the run has not said how much there is to do.
    Friend Percent As Integer = -1
End Class

Friend Module RunSnapshot

    Friend Const WhereShell As String = "shell"
    Friend Const WhereManager As String = "manager"
    Friend Const StateRunning As String = "running"
    Friend Const StateQueued As String = "queued"

    ' Whether the `--where` option (all, shell or manager; empty is all) takes in a window.
    Friend Function Includes(whereOption As String, windowWhere As String) As Boolean
        Return String.IsNullOrEmpty(whereOption) OrElse String.Equals(whereOption, "all", StringComparison.Ordinal) OrElse
               String.Equals(whereOption, windowWhere, StringComparison.Ordinal)
    End Function

    ' The runs that are in progress now; a queued operation is listed and is not counted (status.runningJobs).
    Friend Function RunningCount(runs As IEnumerable(Of RunInfo)) As Integer
        If runs Is Nothing Then Return 0
        Return runs.Count(Function(r) String.Equals(r.State, StateRunning, StringComparison.Ordinal))
    End Function

    ' A word the answer may carry: a letter, then letters, digits, `.`, `_` and `-`, at most 40. Anything else - a path, a
    ' sentence, a name of the person's - becomes `fallback`.
    Friend Function SafeToken(word As String, fallback As String) As String
        If word IsNot Nothing AndAlso Regex.IsMatch(word, "^[A-Za-z][A-Za-z0-9._-]{0,39}$") Then Return word
        Return fallback
    End Function

    ' The `jobs` array of the answer: one object per entry, `percent` null when the run has not said.
    Friend Function ToData(runs As IEnumerable(Of RunInfo)) As List(Of Object)
        Dim list As New List(Of Object)()
        If runs Is Nothing Then Return list
        For Each r In runs
            Dim percent As Object = Nothing
            If r.Percent >= 0 Then percent = r.Percent
            list.Add(New Dictionary(Of String, Object) From {
                {"where", r.Place}, {"verb", r.Verb}, {"state", r.State}, {"step", r.StepName}, {"percent", percent}})
        Next
        Return list
    End Function

End Module
