Partial Module SelfTest
    Private Sub CheckDiskShareManager()
        Dim r As New DiskRecord With {.Name = "work", .Path = "C:\fixture\work.fdd", .ContainerId = "fixture", .Registered = True,
            .Letter = "D:", .ServerAlive = True, .AutoMount = True, .Profile = "ram", .Protection = DiskProtection.Obfuscated}
        Dim o As New DiskOptions With {.Name = "Media", .ReadOnly = True}
        Check("disk-share-manager:cancel-auto-policy", DiskSharing.Plan(r, o, False, True) Is Nothing, "")
        Dim plan = DiskSharing.Plan(r, o, True, True)
        Check("disk-share-manager:clean-transfer", plan.Select(Function(x) x.Action).SequenceEqual(
            New DiskAction() {DiskAction.AutoOff, DiskAction.Unmount, DiskAction.ShareDisk, DiskAction.OpenShared}), "")
        For failAt = 0 To plan.Count - 1
            Dim calls As Integer = 0
            Dim failure = failAt
            Dim completed = DiskSharing.RunSteps(plan, Function(item)
                Dim ok = calls <> failure
                calls += 1
                Return Task.FromResult(ok)
            End Function).GetAwaiter().GetResult()
            Check("disk-share-manager:stops-at-failure:" & failure.ToString(), Not completed AndAlso calls = failure + 1, "")
        Next
        Dim detach = DiskStates.QuickCommand(DiskAction.Unmount, r, Nothing)
        Check("disk-share-manager:no-force-no-loss", Not detach.Contains("force") AndAlso Not detach.Contains("nosave"), "")
        Dim snap As New DiskSnapshot With {.FMSAvailability = "unavailable"}
        Check("disk-share-manager:no-worker-no-mutation", DiskSharing.BeforeStep(DiskAction.AutoOff, r, r, snap) = "vd_share_unavailable", "")
        snap.FMSAvailability = "ready"
        Check("disk-share-manager:policy-still-on", DiskSharing.BeforeStep(DiskAction.Unmount, r, r, snap) = "vd_share_changed", "")
        r.AutoMount = False
        Dim changed As New DiskRecord With {.ContainerId = "other", .Path = r.Path, .Letter = "D:"}
        Check("disk-share-manager:drive-reused", DiskSharing.BeforeStep(DiskAction.Unmount, r, changed, snap) = "vd_share_changed", "")
        Check("disk-share-manager:mounted-cannot-register", DiskSharing.BeforeStep(DiskAction.ShareDisk, r, r, snap) = "vd_share_changed", "")
        Dim ctx As New DiskContext With {.FMSAvailability = "ready"}
        r.Letter = ""
        r.[Shared] = True
        r.Holder = "unknown"
        r.SharedState = "unknown"
        For Each action In {DiskAction.OpenShared, DiskAction.CloseShared, DiskAction.UnshareDisk, DiskAction.ShareAutoOn, DiskAction.ShareAutoOff}
            Check("disk-share-manager:unknown:" & action.ToString(), DiskStates.WhyNot(action, r, DiskRowState.NotMounted, ctx) = "vd_share_unknown", "")
        Next
        r.Holder = "none"
        r.SharedState = "closed"
        Check("disk-share-manager:closed-can-open", DiskStates.WhyNot(DiskAction.OpenShared, r, DiskRowState.NotMounted, ctx) = "", "")
        r.Carrier = "partition"
        Check("disk-share-manager:partition-refuses", DiskStates.WhyNot(DiskAction.ShareDisk, r, DiskRowState.NotMounted, ctx) = "vd_share_partition", "")
        r.Carrier = "file"
        ctx.Packaged = True
        Check("disk-share-manager:store-hidden", DiskStates.HiddenInBuild(DiskAction.ShareDisk, ctx), "")
        r.Profile = "sealed"
        Dim sealedArgs = DiskStates.QuickCommand(DiskAction.ShareDisk, r, New DiskOptions With {.Name = "Media"})
        Check("disk-share-manager:sealed-ro", sealedArgs.Contains("ro"), "")
        Dim openArgs = DiskStates.QuickCommand(DiskAction.OpenShared, r, New DiskOptions With {.HasCredential = True})
        Check("disk-share-manager:password-only-env", openArgs.SequenceEqual(New String() {"vd", "open", r.Path, "pe:FILEDO_SHELL_CRED"}), "")
        Dim autoArgs = DiskStates.QuickCommand(DiskAction.ShareAutoOn, r, New DiskOptions With {.HasCredential = True})
        Check("disk-share-manager:explicit-storage-consent", autoArgs.Contains("consent") AndAlso autoArgs.Contains("pe:FILEDO_SHELL_CRED"), "")
        Dim removeArgs = DiskStates.QuickCommand(DiskAction.UnshareDisk, r, Nothing)
        Check("disk-share-manager:unshare-no-force", removeArgs.SequenceEqual(New String() {"vd", "share", r.Path, "off"}), "")
        For Each lang In Localization.Languages
            Dim d = Localization.GetDict(lang)
            For Each key In Localization.OwnKeysForTest("en").Where(Function(k) k.StartsWith("vd_share_"))
                Check("disk-share-manager:locale:" & lang & ":" & key, Localization.OwnKeysForTest(lang).Contains(key), "")
            Next
            Using dlg As New DiskShareDialog(d, r, Nothing)
                Check("disk-share-manager:dialog-safe:" & lang, dlg.AcceptButton Is dlg.CancelButton AndAlso dlg.Options().ReadOnly AndAlso Not dlg.OpenNow, "")
            End Using
            ' Read-only is the owner's choice, not a default: a plain disk opens the dialog writable, a sealed one cannot be.
            Dim plain As New DiskRecord With {.Name = "work", .Path = r.Path, .ContainerId = r.ContainerId, .Profile = "plain", .Carrier = "file"}
            Using dlg As New DiskShareDialog(d, plain, Nothing)
                Check("disk-share-manager:dialog-writable-default:" & lang, Not dlg.Options().ReadOnly, "")
            End Using
        Next
    End Sub

    ' The row's state in words when the disk is published in FMS, and the ending sentence of a chkdsk run.
    Private Sub CheckDiskFmsState()
        Dim keys = {"vd_mgr_state_fms_open_main", "vd_mgr_state_fms_open", "vd_mgr_state_fms_closed", "vd_mgr_state_fms_opening",
                    "vd_mgr_state_fms_closing", "vd_mgr_state_fms_locked", "vd_mgr_state_fms_failed", "vd_mgr_state_fms_unknown",
                    "vd_help_state_fms", "vd_mgr_done_chkdsk_clean_fmt", "vd_mgr_done_repair_clean_fmt", "vd_mgr_done_repair_fixed_fmt"}
        For Each lang In Localization.Languages
            For Each key In keys
                Check("disk-fms-state:locale:" & lang & ":" & key, Localization.OwnKeysForTest(lang).Contains(key), "")
            Next
        Next
        Dim ui = Localization.GetDict("en")
        Dim plain As New DiskRecord With {.Name = "work", .Path = "C:\fixture\work.fdd", .ContainerId = "fixture", .Registered = True, .Carrier = "file"}
        Dim shared1 As New DiskRecord With {.Name = "work", .Path = "C:\fixture\work.fdd", .ContainerId = "fixture", .Registered = True, .Carrier = "file",
                                            .[Shared] = True, .Holder = "none", .SharedState = "closed"}
        Dim notMounted = ui("vd_mgr_state_not_mounted")
        Check("disk-fms-state:not-shared-unchanged", DiskStates.StateText(plain, DiskRowState.NotMounted, "", ui) = notMounted, "")
        Check("disk-fms-state:closed-tail", DiskStates.StateText(shared1, DiskRowState.NotMounted, "", ui) = notMounted & " - " & ui("vd_mgr_state_fms_closed"), "")
        shared1.SharedState = "open"
        shared1.Holder = "fms-service"
        Check("disk-fms-state:open-is-the-state", DiskStates.StateText(shared1, DiskRowState.NotMounted, "", ui) = ui("vd_mgr_state_fms_open_main"), "")
        Check("disk-fms-state:open-tail-when-unclean", DiskStates.StateText(shared1, DiskRowState.Unclean, "", ui) = ui("vd_mgr_state_unclean") & " - " & ui("vd_mgr_state_fms_open"), "")
        Check("disk-fms-state:mounted-tail", DiskStates.StateText(shared1, DiskRowState.Mounted, "", ui) = ui("vd_mgr_state_mounted") & " - " & ui("vd_mgr_state_fms_open"), "")
        shared1.SharedState = "something-new"
        Check("disk-fms-state:unknown-word", DiskStates.StateText(shared1, DiskRowState.NotMounted, "", ui) = notMounted & " - " & ui("vd_mgr_state_fms_unknown"), "")
        Check("disk-fms-state:busy-and-missing-say-their-own", DiskStates.StateText(shared1, DiskRowState.Busy, "unmount", ui) = ui("vd_mgr_state_busy_unmount") AndAlso
                                                               DiskStates.StateText(shared1, DiskRowState.Missing, "", ui) = ui("vd_mgr_state_missing"), "")
        Check("disk-fms-state:chkdsk-sentences",
              DiskStates.ChkdskDoneKey(DiskAction.CheckVolume, 0) = "vd_mgr_done_chkdsk_clean_fmt" AndAlso
              DiskStates.ChkdskDoneKey(DiskAction.RepairVolume, 0) = "vd_mgr_done_repair_clean_fmt" AndAlso
              DiskStates.ChkdskDoneKey(DiskAction.RepairVolume, 2) = "vd_mgr_done_repair_clean_fmt" AndAlso
              DiskStates.ChkdskDoneKey(DiskAction.RepairVolume, 1) = "vd_mgr_done_repair_fixed_fmt" AndAlso
              DiskStates.ChkdskDoneKey(DiskAction.CheckVolume, 1) = "" AndAlso DiskStates.ChkdskDoneKey(DiskAction.RepairVolume, 3) = "" AndAlso
              DiskStates.ChkdskDoneKey(DiskAction.Verify, 0) = "", "")
        Dim res As New Runner.RunResult With {.ResultInfo = New EventStream.ResultInfo With {.Numbers = New Dictionary(Of String, Object) From {{"chkdsk_exit", 1.0R}}}}
        Dim code As Integer
        Check("disk-fms-state:chkdsk-exit-read", DiskManagerForm.ChkdskExitOf(res, code) AndAlso code = 1 AndAlso
                                                 Not DiskManagerForm.ChkdskExitOf(New Runner.RunResult(), code) AndAlso Not DiskManagerForm.ChkdskExitOf(Nothing, code), "")
    End Sub
End Module
