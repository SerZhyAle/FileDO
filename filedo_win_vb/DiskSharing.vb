Imports System.Threading.Tasks

' The sharing workflow uses the co-shipped CLI snapshot and command builders only.
Friend Class DiskSharingStep
    Public Action As DiskAction
    Public Options As DiskOptions
End Class

Friend Module DiskSharing
    ' One stop rule for the real workflow and its failure-injection tests.
    Friend Async Function RunSteps(steps As IList(Of DiskSharingStep), execute As Func(Of DiskSharingStep, Task(Of Boolean))) As Task(Of Boolean)
        For Each item In steps
            If Not Await execute(item) Then Return False
        Next
        Return True
    End Function
    Friend Function Plan(r As DiskRecord, o As DiskOptions, disableAuto As Boolean, openNow As Boolean) As List(Of DiskSharingStep)
        Dim steps As New List(Of DiskSharingStep)
        If r.AutoMount Then
            If Not disableAuto Then Return Nothing
            steps.Add(New DiskSharingStep With {.Action = DiskAction.AutoOff, .Options = New DiskOptions()})
        End If
        ' Clean unmount saves RAM through the existing CLI; no force/nosave path.
        If r.IsMounted Then steps.Add(New DiskSharingStep With {.Action = DiskAction.Unmount, .Options = New DiskOptions()})
        steps.Add(New DiskSharingStep With {.Action = DiskAction.ShareDisk, .Options = o})
        If openNow Then steps.Add(New DiskSharingStep With {.Action = DiskAction.OpenShared, .Options = New DiskOptions With {.HasCredential = o.HasCredential}})
        Return steps
    End Function

    Friend Function SameTarget(expected As DiskRecord, actual As DiskRecord) As Boolean
        Return actual IsNot Nothing AndAlso expected.ContainerId <> "" AndAlso
            String.Equals(expected.ContainerId, actual.ContainerId, StringComparison.OrdinalIgnoreCase) AndAlso
            String.Equals(expected.Path, actual.Path, StringComparison.OrdinalIgnoreCase) AndAlso Not actual.IsPartition AndAlso Not actual.IsImage
    End Function

    Friend Function BeforeStep(stepAction As DiskAction, expected As DiskRecord, actual As DiskRecord, s As DiskSnapshot) As String
        If s Is Nothing OrElse s.FMSAvailability <> "ready" Then Return "vd_share_unavailable"
        If s.Packaged Then Return "vd_share_store"
        If Not SameTarget(expected, actual) Then Return "vd_share_changed"
        If stepAction = DiskAction.Unmount Then
            If Not actual.IsMounted OrElse actual.Letter <> expected.Letter OrElse actual.IsShared OrElse actual.AutoMount Then Return "vd_share_changed"
        ElseIf stepAction = DiskAction.AutoOff Then
            If actual.IsShared Then Return "vd_share_changed"
        ElseIf stepAction = DiskAction.ShareDisk Then
            If actual.IsMounted OrElse actual.IsShared OrElse actual.AutoMount OrElse actual.FileState <> "ok" Then Return "vd_share_changed"
        Else
            Dim why = DiskStates.WhyNot(stepAction, actual, DiskStates.StateOf(actual, ""),
                New DiskContext With {.Packaged = s.Packaged, .FMSAvailability = s.FMSAvailability})
            If why <> "" Then Return why
        End If
        Return ""
    End Function
End Module

Friend Class DiskShareDialog
    Inherits DiskSmallDialog
    Private ReadOnly root As TextBox
    Private ReadOnly ro As CheckBox
    Private ReadOnly openIt As CheckBox
    Private ReadOnly disableAuto As CheckBox
    Private ReadOnly record As DiskRecord

    Public Sub New(d As Dictionary(Of String, String), r As DiskRecord, owner As Control)
        MyBase.New(d, Tr(d, "vd_share_register"), Localization.Format(Tr(d, "vd_share_consent_fmt"), r.BaseName, r.Path), "vd_mgr_btn_continue", owner)
        record = r
        AddContent(NewNote(T("vd_share_root")))
        root = New TextBox With {.Text = r.BaseName, .Dock = DockStyle.Top, .MaxLength = 64, .Margin = PPad(0, 0, 0, 8)}
        root.AccessibleName = T("vd_share_root")
        AddContent(root)
        ro = New CheckBox With {.Text = T("vd_share_read_only"), .Checked = True, .Enabled = r.Profile <> "sealed", .AutoSize = True}
        AddContent(ro)
        openIt = New CheckBox With {.Text = T("vd_share_open_now"), .Checked = False, .AutoSize = True}
        AddContent(openIt)
        If r.IsMounted Then AddContent(NewNote(Localization.Format(T("vd_share_transfer_fmt"), r.Letter)))
        disableAuto = New CheckBox With {.Text = T("vd_share_disable_auto"), .Checked = False, .Visible = r.AutoMount, .AutoSize = True}
        AddContent(disableAuto)
        AddContent(NewNote(T("vd_share_policy")))
        AddHandler root.TextChanged, Sub() ValidateChoices()
        AddHandler disableAuto.CheckedChanged, Sub() ValidateChoices()
        Finish()
        okBtn.UseMnemonic = False
        AcceptButton = cancelBtn
        ActiveControl = cancelBtn
        ValidateChoices()
    End Sub

    Private Sub ValidateChoices()
        Dim name = root.Text.Trim()
        SetAcceptable(name.Length > 0 AndAlso name <> "." AndAlso name <> ".." AndAlso
            Not name.Any(Function(c) Char.IsControl(c) OrElse "/\:".Contains(c)) AndAlso (Not record.AutoMount OrElse disableAuto.Checked))
    End Sub

    Friend Function Options() As DiskOptions
        Return New DiskOptions With {.Name = root.Text.Trim(), .ReadOnly = ro.Checked OrElse record.Profile = "sealed"}
    End Function
    Friend ReadOnly Property OpenNow As Boolean
        Get
            Return openIt.Checked
        End Get
    End Property
    Friend ReadOnly Property DisableLocalAuto As Boolean
        Get
            Return disableAuto.Checked
        End Get
    End Property
End Class

Partial Public Class DiskManagerForm
    ' Shares the snapshot read lane with polling. No second status child is started.
    Private Async Function FreshSharingSnapshot() As Task(Of DiskSnapshot)
        While reading AndAlso Not IsDisposed
            Await Task.Delay(100)
        End While
        If IsDisposed Then Return Nothing
        reading = True
        Try
            Dim res = Await Task.Run(AddressOf ReadOnce)
            If IsDisposed Then Return Nothing
            ApplyRead(res.Snapshot, res.Problem)
            Return res.Snapshot
        Finally
            reading = False
            If readAgain AndAlso Not IsDisposed Then RequestRead()
        End Try
    End Function

    Private Sub SharingUnavailable(s As DiskSnapshot)
        Dim reason = If(s Is Nothing, "unknown", s.FMSAvailability)
        Dim key = If(New String() {"not-capable", "untrusted", "communication"}.Contains(reason), "vd_share_avail_" & reason, "vd_share_avail_unknown")
        Dim answer = ShellDialog.Ask(Me, L("vd_share_register"), LText(key),
            New String() {L("vd_share_install"), L("vd_mgr_act_refresh"), L("shell_btn_cancel")}, 2, 0)
        If answer = 0 Then Links.Open(Me, Links.FMSInstall)
        If answer = 1 Then RequestRead()
    End Sub

    Private Async Sub PrepareSharing(a As DiskAction, selected As DiskRecord)
        Dim op As DiskOp = Nothing
        Dim env As Dictionary(Of String, String) = Nothing
        busy(selected.Key) = "queued"
        RefreshView()
        Try
            Dim s = Await FreshSharingSnapshot()
            If IsDisposed Then Return
            If s Is Nothing OrElse s.FMSAvailability <> "ready" Then
                SharingUnavailable(s)
                Return
            End If
            Dim r = s.Disks.FirstOrDefault(Function(d) d.Key = selected.Key)
            If Not DiskSharing.SameTarget(selected, r) Then
                ShellDialog.Notice(Me, L("vd_share_register"), L("vd_share_changed"))
                Return
            End If
            Dim why = DiskStates.WhyNot(a, r, DiskStates.StateOf(r, ""), Context())
            If why <> "" Then
                ShellDialog.Notice(Me, L(DiskStates.LabelKey(a, r)), L(why))
                Return
            End If
            Dim steps As List(Of DiskSharingStep)
            Dim needCredential As Boolean
            If a = DiskAction.ShareDisk Then
                Using dlg As New DiskShareDialog(dict, r, Me)
                    If dlg.ShowDialog(Me) <> DialogResult.OK Then Return
                    steps = DiskSharing.Plan(r, dlg.Options(), dlg.DisableLocalAuto, dlg.OpenNow)
                    needCredential = dlg.OpenNow AndAlso r.Protection = DiskProtection.Encrypted
                    Dim confirmation = Localization.Format(LText("vd_share_confirm_fmt"), dlg.Options().Name,
                        L(If(dlg.Options().ReadOnly, "vd_share_access_ro", "vd_share_access_rw")),
                        Localization.Format(LText("vd_share_consent_fmt"), r.BaseName, r.Path))
                    If r.IsMounted Then confirmation &= Environment.NewLine & Localization.Format(LText("vd_share_transfer_fmt"), r.Letter)
                    If r.AutoMount Then confirmation &= Environment.NewLine & L("vd_share_disable_auto")
                    If dlg.OpenNow Then confirmation &= Environment.NewLine & L("vd_share_open_now")
                    If Ask("vd_share_register", confirmation, "vd_share_register") <> 0 Then Return
                End Using
            Else
                If a = DiskAction.ShareAutoOn Then
                    If Ask("vd_share_auto_on", LText("vd_share_auto_consent"), "vd_mgr_btn_turn_on") <> 0 Then Return
                ElseIf a = DiskAction.UnshareDisk OrElse a = DiskAction.CloseShared Then
                    If Ask(DiskStates.LabelKey(a, r), LText("vd_share_close_consent"), DiskStates.LabelKey(a, r)) <> 0 Then Return
                End If
                steps = New List(Of DiskSharingStep) From {New DiskSharingStep With {.Action = a, .Options = New DiskOptions()}}
                needCredential = DiskStates.AsksPassword(a, r)
            End If
            If steps Is Nothing Then Return
            If needCredential Then
                Using password As New DiskPasswordDialog(dict, r.BaseName, Me)
                    If password.ShowDialog(Me) <> DialogResult.OK Then Return
                    env = New Dictionary(Of String, String) From {{DiskCommands.CredentialEnvName, password.TakePassword()}}
                End Using
                steps.Last().Options.HasCredential = True
            End If
            op = New DiskOp With {.Action = a, .Record = r, .Env = env, .SharingSteps = steps}
            If a = DiskAction.ShareDisk Then ShellLog.Info("disk sharing prior policy: container=" & r.ContainerId &
                "; local auto=" & r.AutoMount.ToString() & "; drive=" & r.Letter & "; manual keep ends with clean unmount")
            env = Nothing
        Catch ex As Exception
            ShellLog.Write("prepare disk sharing", ex)
            If Not IsDisposed Then ShellDialog.Problem(Me, L("vd_share_unavailable"), L("vd_mgr_act_refresh"))
        Finally
            If env IsNot Nothing Then env.Clear()
            busy.Remove(selected.Key)
            If Not IsDisposed Then RefreshView()
        End Try
        If op IsNot Nothing Then
            If IsDisposed Then
                If op.Env IsNot Nothing Then op.Env.Clear()
            Else
                Enqueue(op)
            End If
        End If
    End Sub

    Private Async Function ExecuteSharing(op As DiskOp) As Task(Of Runner.RunResult)
        Dim result As Runner.RunResult = Nothing
        Dim completed = Await DiskSharing.RunSteps(op.SharingSteps, Async Function(stepItem)
            op.SharingFailure = DiskStates.LabelKey(stepItem.Action, op.Record)
            Dim s = Await FreshSharingSnapshot()
            If IsDisposed Then
                result = Nothing
                Return False
            End If
            Dim actual = If(s Is Nothing, Nothing, s.Disks.FirstOrDefault(Function(r) r.Key = op.Record.Key))
            Dim why = DiskSharing.BeforeStep(stepItem.Action, op.Record, actual, s)
            If why <> "" Then
                op.SharingFailure = why
                result = Nothing
                Return False
            End If
            busy(op.Record.Key) = DiskStates.VerbOf(stepItem.Action)
            RefreshView()
            op.Run.Dispose()
            op.Run = New Runner()
            Dim args = DiskStates.QuickCommand(stepItem.Action, actual, stepItem.Options)
            result = Await op.Run.ExecuteAsync(args, envVars:=If(stepItem.Options.HasCredential, op.Env, Nothing))
            If result Is Nothing OrElse result.ExitCode <> 0 OrElse (result.Verdict <> "Done" AndAlso result.Verdict <> "Passed") Then Return False
            op.CompletedSharing.Add(L(DiskStates.LabelKey(stepItem.Action, actual)))
            Return True
        End Function)
        If completed Then op.SharingFailure = ""
        Return result
    End Function
End Class
