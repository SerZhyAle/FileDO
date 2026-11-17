' What the pages remember between sessions (ShellSettings "remembered input", the Command page, Ui.ChooseFolder):
' the lists keep the newest first and stay within their limit, junk is refused, a run kept out of history or
' holding a literal password adds nothing, run history off remembers nothing, Forget empties everything, and the
' Command page puts back its operation, target and destination. Rows are named `memory:`;
' FILEDO_SELFTEST_ONLY=memory runs just these. They run on the self-test's own value store, never the registry.
Partial Public Module SelfTest

    Private Sub CheckInputMemory()
        Dim saved = ShellSettings.ValuesForTest
        ShellSettings.ValuesForTest = New Dictionary(Of String, Object)()
        Try
            CheckRememberedLists()
            CheckRememberedGate()
            CheckCommandPageMemory()
        Finally
            ShellSettings.ValuesForTest = saved
        End Try
    End Sub

    Private Sub CheckRememberedLists()
        Dim destRole = ShellSettings.DestinationRole
        Dim targetRole = ShellSettings.TargetRole
        ShellSettings.RememberPath(destRole, "E:\Bak")
        ShellSettings.RememberPath(destRole, "F:\Other")
        ShellSettings.RememberPath(destRole, "e:\bak")
        Dim dest = ShellSettings.RecentPaths(destRole)
        Check("memory:newest-first", dest.Count = 2 AndAlso dest(0) = "e:\bak" AndAlso dest(1) = "F:\Other", String.Join("|", dest.ToArray()))
        Check("memory:roles-apart", ShellSettings.RecentPaths(targetRole).Count = 0, "")

        For i = 1 To ShellSettings.RememberedLimit + 5
            ShellSettings.RememberCommand("filedo.exe C: info " & i.ToString())
        Next
        Dim lines = ShellSettings.RecentCommands()
        Check("memory:limit",
              lines.Count = ShellSettings.RememberedLimit AndAlso lines(0).EndsWith((ShellSettings.RememberedLimit + 5).ToString(), StringComparison.Ordinal),
              lines.Count.ToString())

        ShellSettings.RememberPath(targetRole, "")
        ShellSettings.RememberPath(targetRole, "a" & vbLf & "b")
        Check("memory:junk-refused", ShellSettings.RecentPaths(targetRole).Count = 0, "")

        ShellSettings.SetCommandPageState("Op", "backup")
        Check("memory:state-kept", ShellSettings.CommandPageState("Op") = "backup", ShellSettings.CommandPageState("Op"))
        ShellSettings.ForgetRemembered()
        Check("memory:forget",
              ShellSettings.RecentCommands().Count = 0 AndAlso ShellSettings.RecentPaths(destRole).Count = 0 AndAlso
              ShellSettings.CommandPageState("Op") = "", "")
    End Sub

    ' A run is remembered unless it is kept out of history, names a literal password, or run history is off.
    Private Sub CheckRememberedGate()
        Check("memory:gate:plain", ShellSettings.MayRemember({"backup", "D:\Photos", "to", "E:\Bak"}), "")
        Check("memory:gate:nohist", Not ShellSettings.MayRemember({"backup", "D:\Photos", "nohist"}), "")
        Check("memory:gate:no_history", Not ShellSettings.MayRemember({"backup", "D:\Photos", "no_history"}), "")
        Check("memory:gate:password", Not ShellSettings.MayRemember({"D:\a.txt", "secure", "p:hunter2"}), "")
        Check("memory:gate:password-variable", ShellSettings.MayRemember({"D:\a.txt", "secure", "pe:FILEDO_SHELL_CRED"}), "")
        Check("memory:gate:drive-p", ShellSettings.MayRemember({"p:\dir\a.fd-sec", "unsecure"}), "")
        ShellSettings.SetHistoryEnabled(False)
        Check("memory:gate:history-off", Not ShellSettings.MayRemember({"backup", "D:\Photos"}), "")
        ShellSettings.RememberPath(ShellSettings.TargetRole, "D:\Photos")
        Check("memory:history-off-no-path", ShellSettings.RecentPaths(ShellSettings.TargetRole).Count = 0, "")
        ShellSettings.SetHistoryEnabled(True)
    End Sub

    ' The Command page: a destination has a folder picker and a size does not; what the page was left on comes
    ' back in a new page; a run adds its line, target and destination to the lists, and a line with a password
    ' adds nothing; the remembered targets are offered in the target list.
    Private Sub CheckCommandPageMemory()
        Dim view As CommandView = Nothing
        Dim again As CommandView = Nothing
        Dim third As CommandView = Nothing
        Try
            view = New CommandView()
            view.ChooseOperation("backup")
            Check("memory:page:picker-on-destination", view.DestinationPickerShownForTest, "")
            view.ChooseOperation("speed")
            Check("memory:page:no-picker-on-size", Not view.DestinationPickerShownForTest, "")

            view.ChooseOperation("backup")
            view.SetTargetForTest("D:\Photos")
            view.SetParameterForTest("E:\Bak")
            view.RememberState()
            again = New CommandView()
            Dim line = again.CurrentCommandLine()
            Check("memory:page:restored",
                  again.CurrentOperationForTest() = "backup" AndAlso line.Contains("D:\Photos") AndAlso line.Contains("E:\Bak"),
                  again.CurrentOperationForTest() & " | " & line)

            Dim shown = view.CurrentCommandLine()
            view.RememberRunForTest(shown)
            Check("memory:page:run-remembered",
                  ShellSettings.RecentCommands().Contains(shown) AndAlso
                  ShellSettings.RecentPaths(ShellSettings.DestinationRole).Contains("E:\Bak") AndAlso
                  ShellSettings.RecentPaths(ShellSettings.TargetRole).Contains("D:\Photos"), shown)
            view.RememberRunForTest("filedo.exe D:\a.txt secure p:hunter2")
            Check("memory:page:password-not-remembered",
                  Not ShellSettings.RecentCommands().Any(Function(c) c.Contains("hunter2")), "")

            third = New CommandView()
            third.ChooseOperation("copy")
            Check("memory:page:targets-offered", third.TargetChoicesForTest.Contains("D:\Photos"), String.Join("|", third.TargetChoicesForTest.ToArray()))
        Catch ex As Exception
            Check("memory:page", False, ex.GetType().Name & ": " & ex.Message)
        Finally
            If view IsNot Nothing Then view.Dispose()
            If again IsNot Nothing Then again.Dispose()
            If third IsNot Nothing Then third.Dispose()
        End Try
    End Sub

End Module
