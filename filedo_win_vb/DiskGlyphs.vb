' The glyph of every Disk manager meaning, in one table (ICON-SET rules 1 and 5).
'
' The rail's Disks rows, the manager's toolbar, its menus and its detail buttons all show a meaning
' by asking this module, so one meaning is one picture wherever it appears and the two surfaces
' cannot drift apart. A meaning the vocabulary has a record for draws the catalog's file (vendored
' under assets/glyphs by assets/sync-icon-glyphs.ps1). Ten meanings have no record yet - create,
' mount, unmount, compact, grow, format, seal, change password, auto-mount, remember a name; they draw
' a Segoe stand-in under the id FileDO proposed for each (the catalog's
' PROPOSAL-2026-09-30-filedo-disk-meanings.md), which is what SelfTest's rail baseline counts, and the
' number goes down as each record lands - never up.
'
' A mount, a read-only mount, Mount as.. and Mount image.. are one meaning, "mount"; an unmount and an
' image unmount are "unmount"; Clone and Copy path are both "copy" - so they share a picture, which
' ICON-SET rule 1 asks for, not forbids.
Public Module DiskGlyphs

    Public ReadOnly CreateDisk As GlyphRef = GlyphRef.Waiting("action.create-disk", &HE710, "Add")
    Public ReadOnly MountDisk As GlyphRef = GlyphRef.Waiting("action.mount-disk", &HEDA2, "HardDrive")
    Public ReadOnly UnmountDisk As GlyphRef = GlyphRef.Waiting("action.unmount-disk", &HE738, "Remove")
    Public ReadOnly CompactDisk As GlyphRef = GlyphRef.Waiting("action.compact", &HE73F, "BackToWindow")
    Public ReadOnly GrowDisk As GlyphRef = GlyphRef.Waiting("action.grow", &HE740, "FullScreen")
    Public ReadOnly FormatDisk As GlyphRef = GlyphRef.Waiting("action.format", &HE75C, "EraseTool")
    Public ReadOnly SealDisk As GlyphRef = GlyphRef.Waiting("action.seal", &HE72E, "Lock")
    Public ReadOnly ChangePassword As GlyphRef = GlyphRef.Waiting("action.change-password", &HE8D7, "Permissions")
    Public ReadOnly AutoMount As GlyphRef = GlyphRef.Waiting("action.auto-mount", &HE823, "Recent")
    Public ReadOnly RememberName As GlyphRef = GlyphRef.Waiting("action.remember-name", &HE8EC, "Tag")

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
            Case DiskAction.NewDisk : Return CreateDisk
            Case DiskAction.AddToList : Return RememberName
            Case DiskAction.Mount, DiskAction.MountReadOnly, DiskAction.MountAs, DiskAction.MountImage : Return MountDisk
            Case DiskAction.Unmount, DiskAction.UnmountImage : Return UnmountDisk
            Case DiskAction.OpenDrive : Return OpenExternal
            Case DiskAction.SaveNow : Return GlyphRef.Vocabulary("action.save")
            Case DiskAction.Info : Return GlyphRef.Vocabulary("app.info")
            Case DiskAction.Verify : Return GlyphRef.Vocabulary("action.verify")
            Case DiskAction.AutoOn, DiskAction.AutoOff : Return AutoMount
            Case DiskAction.Forget : Return GlyphRef.Vocabulary("action.remove")
            Case DiskAction.ShowInFolder : Return GlyphRef.Vocabulary("content.folder")
            Case DiskAction.CopyPath, DiskAction.Clone : Return GlyphRef.Vocabulary("action.copy")
            Case DiskAction.Export : Return GlyphRef.Vocabulary("action.export")
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
