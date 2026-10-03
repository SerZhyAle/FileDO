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
        Next
    End Sub
End Module
