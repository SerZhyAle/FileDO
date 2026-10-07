' The glyph of every Disk manager meaning, in one table (ICON-SET rules 1 and 5).
'
' The rail's Disks rows, the manager's toolbar, its menus and its detail buttons all show a meaning
' by asking this module, so one meaning is one picture wherever it appears and the two surfaces
' cannot drift apart. A meaning the vocabulary has a record for draws the catalog's file (vendored
' under assets/glyphs by assets/sync-icon-glyphs.ps1). Create, compact and auto-mount were the three
' that waited for a record, drawing a Segoe stand-in under the id FileDO proposed for each; ICON-SET
' 0.24 took them and they are vendored since 2026-10-07 (SP-0165), so no rail row waits any more
' (SelfTest's rail baseline is zero). Two meanings still have no record - autostart and create
' shortcut - and draw a stand-in on surfaces that are not the rail.
'
' A mount, a read-only mount, Mount as.. and Mount image.. are one meaning, "mount"; an unmount and an
' image unmount are "unmount"; Clone and Copy path are both "copy" - so they share a picture, which
' ICON-SET rule 1 asks for, not forbids.
Public Module DiskGlyphs

    Public ReadOnly CreateDisk As GlyphRef = GlyphRef.Vocabulary("action.create-disk")
    Public ReadOnly MountDisk As GlyphRef = GlyphRef.Vocabulary("action.mount-disk")
    Public ReadOnly UnmountDisk As GlyphRef = GlyphRef.Vocabulary("action.unmount-disk")
    Public ReadOnly CompactDisk As GlyphRef = GlyphRef.Vocabulary("action.compact")
    Public ReadOnly GrowDisk As GlyphRef = GlyphRef.Vocabulary("action.grow")
    Public ReadOnly FormatDisk As GlyphRef = GlyphRef.Vocabulary("action.format")
    Public ReadOnly SealDisk As GlyphRef = GlyphRef.Vocabulary("action.seal")
    Public ReadOnly ChangePassword As GlyphRef = GlyphRef.Vocabulary("action.change-password")
    Public ReadOnly AutoMount As GlyphRef = GlyphRef.Vocabulary("action.auto-mount")
    Public ReadOnly RememberName As GlyphRef = GlyphRef.Vocabulary("action.remember-name")
    ' The Autostart surface (SP-0080 5): the logon mounts and the shutdown guard in one place. The
    ' picture is the power button - the session's end is the guard's half of it.
    Public ReadOnly Autostart As GlyphRef = GlyphRef.Waiting("action.autostart", &HE7E8, "PowerButton")

    Public ReadOnly DiskContainer As GlyphRef = GlyphRef.Vocabulary("content.disk-container")

    ' The window's own controls, none of which is an action on a disk.
    Public ReadOnly Help As GlyphRef = GlyphRef.Vocabulary("app.help")
    Public ReadOnly More As GlyphRef = GlyphRef.Vocabulary("nav.more")
    Public ReadOnly CloseIt As GlyphRef = GlyphRef.Vocabulary("nav.close")
    Public ReadOnly ClearInput As GlyphRef = GlyphRef.Vocabulary("action.clear-input")
    Public ReadOnly OpenExternal As GlyphRef = GlyphRef.Vocabulary("nav.open-external")
    Public ReadOnly ShowDetails As GlyphRef = GlyphRef.Vocabulary("nav.expand")
    ' Ending a running task is action.cancel (ICON-SET PROPOSAL 2(c): media.stop is playback); emptying
    ' a whole list is action.clear-all.
    Public ReadOnly StopRun As GlyphRef = GlyphRef.Vocabulary("action.cancel")
    Public ReadOnly ClearQueue As GlyphRef = GlyphRef.Vocabulary("action.clear-all")

    ' The meaning an action shows. Every DiskAction has one - the self-test walks the enum.
    Public Function [For](a As DiskAction) As GlyphRef
        Select Case a
            Case DiskAction.ShareDisk : Return GlyphRef.Vocabulary("action.share")
            Case DiskAction.OpenShared : Return MountDisk
            Case DiskAction.CloseShared : Return UnmountDisk
            Case DiskAction.UnshareDisk : Return GlyphRef.Vocabulary("action.remove")
            Case DiskAction.ShareAutoOn, DiskAction.ShareAutoOff : Return Autostart
            Case DiskAction.NewDisk : Return CreateDisk
            Case DiskAction.AddToList : Return RememberName
            Case DiskAction.Mount, DiskAction.MountReadOnly, DiskAction.MountAs, DiskAction.MountImage : Return MountDisk
            Case DiskAction.Unmount, DiskAction.UnmountImage : Return UnmountDisk
            Case DiskAction.OpenDrive : Return OpenExternal
            Case DiskAction.SaveNow : Return GlyphRef.Vocabulary("action.save")
            Case DiskAction.Info : Return GlyphRef.Vocabulary("app.info")
            Case DiskAction.Verify, DiskAction.CheckVolume : Return GlyphRef.Vocabulary("action.verify")
            ' The meaning the catalog already has for putting a drive back in order (Recover on the rail).
            Case DiskAction.RepairVolume : Return GlyphRef.Vocabulary("action.recover-drive")
            Case DiskAction.AutoOn, DiskAction.AutoOff : Return AutoMount
            Case DiskAction.Autostart : Return Autostart
            Case DiskAction.Forget : Return GlyphRef.Vocabulary("action.remove")
            Case DiskAction.ShowInFolder : Return GlyphRef.Vocabulary("content.folder")
            Case DiskAction.CopyPath, DiskAction.Clone : Return GlyphRef.Vocabulary("action.copy")
            ' SP-0148: Adopt gives a found partition its name in the list - the meaning Add to list
            ' already draws. Image to file copies a partition disk byte for byte into a new .fdd:
            ' ICON-SET 0.26 gave it its own meaning, action.image-to-file, beside Export.
            Case DiskAction.Export : Return GlyphRef.Vocabulary("action.export")
            Case DiskAction.ImageToFile : Return GlyphRef.Vocabulary("action.image-to-file")
            Case DiskAction.Adopt : Return RememberName
            Case DiskAction.Compact : Return CompactDisk
            Case DiskAction.Grow : Return GrowDisk
            Case DiskAction.Seal : Return SealDisk
            Case DiskAction.ChangePassword : Return ChangePassword
            Case DiskAction.Format : Return FormatDisk
            Case DiskAction.Destroy : Return GlyphRef.Vocabulary("action.delete")
            Case DiskAction.Refresh : Return GlyphRef.Vocabulary("action.refresh")
        End Select
        Return Nothing
    End Function

    ' The catalog's canonical name of the meaning a control shows, for the accessible name of a
    ' control with a glyph and no caption (ICON-RENDER rule 8, APP-BEHAVIOUR rule 9): a localization
    ' key, present in the five tables.
    Public Function NameKey(g As GlyphRef) As String
        If g Is Nothing Then Return ""
        Select Case g.Meaning
            Case "app.settings" : Return "rail_job_settings"
            Case "app.help" : Return "vd_mgr_name_help"
            Case "nav.more" : Return "vd_mgr_btn_more"
            Case "nav.close" : Return "vd_mgr_name_close"
            Case "action.clear-input" : Return "vd_mgr_name_clear"
            Case "action.refresh" : Return "vd_mgr_btn_refresh"
            Case "nav.expand" : Return "vd_mgr_detail_show"
        End Select
        Return ""
    End Function

End Module
