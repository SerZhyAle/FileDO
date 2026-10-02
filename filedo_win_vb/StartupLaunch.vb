Imports Microsoft.Win32

' The sole writer of the user's logon entry. Never called during startup or by the installer.
Friend Module StartupLaunch
    Friend Const RunPath As String = "Software\Microsoft\Windows\CurrentVersion\Run"
    Friend Const ValueName As String = "FileDO"
    Friend ReadOnly Choices As String() = {"off", "shell", "manager", "tray"}

    Friend Function Normalize(choice As String) As String
        Return If(Choices.Contains(choice), choice, "off")
    End Function

    Friend Function Command(executable As String, choice As String) As String
        Select Case Normalize(choice)
            Case "shell" : Return """" & executable & """ --startup"
            Case "manager" : Return """" & executable & """ --startup --disks"
            Case "tray" : Return """" & executable & """ --startup --tray"
            Case Else : Return Nothing
        End Select
    End Function

    Friend Function SetChoice(choice As String, owner As Form) As Boolean
        If Packaging.IsPackaged() Then Return False
        Try
            Using key = Registry.CurrentUser.CreateSubKey(RunPath)
                Dim data = Command(Application.ExecutablePath, choice)
                If data Is Nothing Then
                    key.DeleteValue(ValueName, False)
                Else
                    key.SetValue(ValueName, data, RegistryValueKind.String)
                End If
            End Using
            ShellSettings.SetAutostart(choice)
            Return True
        Catch ex As Exception
            ShellLog.Write("change application startup", ex)
            ShellDialog.Problem(owner, Localization.Format(Localization.T("settings_startup_failed"), Problems.Cause(ex)))
            Return False
        End Try
    End Function

    Friend Function HiddenStart(args As String()) As Boolean
        Return args IsNot Nothing AndAlso args.Any(Function(a) String.Equals(a, "--startup", StringComparison.OrdinalIgnoreCase)) AndAlso
               args.Any(Function(a) String.Equals(a, "--tray", StringComparison.OrdinalIgnoreCase))
    End Function
End Module
