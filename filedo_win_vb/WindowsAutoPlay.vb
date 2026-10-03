Imports Microsoft.Win32

' Windows' per-user AutoPlay switch. Only an explicit button click writes it.
' This changes media arrival actions, not FileDO's scheduled mount tasks.
Friend Module WindowsAutoPlay
    Private Const SettingsPath As String = "Software\Microsoft\Windows\CurrentVersion\Explorer\AutoplayHandlers"
    Friend Const SettingsUri As String = "ms-settings:autoplay"

    Friend Function Disable(owner As Form) As Boolean
        ' A Store package must use Windows settings, outside registry virtualization.
        If Packaging.IsPackaged() Then
            Links.Open(owner, SettingsUri)
            Return False
        End If
        Try
            Using key = Registry.CurrentUser.CreateSubKey(SettingsPath)
                key.SetValue("DisableAutoplay", 1, RegistryValueKind.DWord)
                If Not Object.Equals(key.GetValue("DisableAutoplay"), 1) Then
                    Throw New IO.IOException("Windows AutoPlay setting could not be confirmed.")
                End If
            End Using
            Return True
        Catch ex As Exception
            ShellLog.Write("disable Windows AutoPlay", ex)
            If ShellDialog.Problem(owner, Localization.Format(Localization.T("settings_autoplay_failed"), Problems.Cause(ex)),
                                   Localization.T("settings_autoplay_open")) Then
                Links.Open(owner, SettingsUri)
            End If
            Return False
        End Try
    End Function
End Module
