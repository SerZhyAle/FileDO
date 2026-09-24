' The "Send logs" flow, one copy for every place that offers it: the About page, and the Send logs
' button of every problem dialog.
'
' Order matters here (APP-BEHAVIOUR rule 5, second half): the files are looked for first, and when
' there is nothing to send the user is told so - they are never asked to confirm an archive that
' would have been empty. The question that follows has a Cancel that Escape reaches (rule 1), and
' nothing leaves the machine: the zip is left on disk and the mail is sent by the user or not at
' all (rule 4).
Module LogSender

    Public Sub Run(owner As IWin32Window)
        Dim title = Localization.T("logs_title")

        If LogReport.CountAvailable() = 0 Then
            ShellDialog.Notice(owner, title, Localization.T("logs_none"))
            Return
        End If

        If ShellDialog.Ask(owner, title, Localization.T("logs_confirm"),
                           New String() {Localization.T("logs_btn_build"), Localization.T("shell_btn_cancel")},
                           1, 0) <> 0 Then
            Return
        End If

        Dim count As Integer = 0
        Dim archive As String = ""
        Dim busy = TryCast(owner, Control)
        Try
            If busy IsNot Nothing Then busy.Cursor = Cursors.WaitCursor
            archive = LogReport.BuildArchive(ShellSettings.Language(), count)
        Catch ex As Exception
            ShellLog.Write("send logs: building the archive", ex)
            If busy IsNot Nothing Then busy.Cursor = Cursors.Default
            If ShellDialog.Problem(owner, Localization.Format(Localization.T("logs_error"), Problems.Cause(ex)),
                                   Localization.T("shell_btn_retry"), offerLogs:=False) Then
                Run(owner)
            End If
            Return
        Finally
            If busy IsNot Nothing Then busy.Cursor = Cursors.Default
        End Try

        If archive = "" Then
            ShellDialog.Notice(owner, title, Localization.T("logs_none"))
            Return
        End If

        Dim failedSteps As New List(Of String)
        LogReport.RevealInExplorer(archive, failedSteps)
        LogReport.CopyPathToClipboard(archive, failedSteps)
        LogReport.OpenMailClient(archive, failedSteps)

        Dim msg As String = Localization.Format(Localization.T("logs_ready"), archive, count)
        If failedSteps.Count > 0 Then
            msg &= Environment.NewLine & Environment.NewLine & Localization.T("logs_partial")
            For Each key In failedSteps
                msg &= Environment.NewLine & Localization.T(key)
            Next
        End If
        ShellDialog.Notice(owner, title, msg)
    End Sub

End Module
