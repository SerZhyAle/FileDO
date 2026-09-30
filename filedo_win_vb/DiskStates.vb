' The Disk Manager's decisions, as pure functions (SP-0063 sections 5.4 and 6.1): which state a
' row is in, what that state is called, which glyph it shows, and whether an action applies to a
' row - and if not, why. Nothing here touches a window or starts filedo.exe, so the self-test holds
' every (state x profile x protection x packaged x transport) combination without either.

' One state per row, in the order of spec 5.4: the first that holds wins.
Public Enum DiskRowState
    Busy          ' S1  an operation started from this window runs on the disk
    ServerGone    ' S2  mounted, block server not alive
    Unsaved       ' S3  ram, mounted, dirty bytes > 0
    Mounted       ' S4  mounted, server alive (read-only is said in words)
    Image         ' S5  a foreign image
    Missing       ' S6  registered, the file is gone
    Different     ' S7  registered, another container is at the path
    Unreadable    ' S8  the header cannot be read
    Unclean       ' S9  not mounted, not closed cleanly
    NotMounted    ' S10 everything else
End Enum

' Every action of spec 6.1 plus the two that are not about a row.
Public Enum DiskAction
    Mount
    MountReadOnly
    MountAs
    Unmount
    UnmountImage
    OpenDrive
    SaveNow
    Info
    Verify
    AutoOn
    AutoOff
    AddToList
    Forget
    ShowInFolder
    CopyPath
    NewDisk
    MountImage
    Export
    Compact
    Grow
    Seal
    Clone
    ChangePassword
    Format
    Destroy
    Refresh
End Enum

' How an action runs (spec 7): a filedo.exe run from the manager, something with no filedo.exe at
' all, or the existing job page in the shell - destructive ones set apart in every menu.
Public Enum DiskActionKind
    Quick
    Local
    Delegated
    Destructive
End Enum

' What the window knows about the machine, beside the rows.
Public Class DiskContext
    Public Property Packaged As Boolean = False
    Public Property TransportReady As Boolean = True
    Public Property TransportReason As String = ""
End Class

Public Module DiskStates

    ' ---- the state -----------------------------------------------------------

    ' busyVerb is the verb an operation from this window runs (or waits to run) on the row, "" for
    ' none.
    Public Function StateOf(r As DiskRecord, busyVerb As String) As DiskRowState
        If Not String.IsNullOrEmpty(busyVerb) Then Return DiskRowState.Busy
        If r.IsImage Then Return DiskRowState.Image
        If r.IsMounted Then
            If Not r.ServerAlive Then Return DiskRowState.ServerGone
            If r.HasRam AndAlso r.RamDirty > 0 Then Return DiskRowState.Unsaved
            Return DiskRowState.Mounted
        End If
        Select Case r.FileState
            Case "missing" : Return DiskRowState.Missing
            Case "different" : Return DiskRowState.Different
            Case "unreadable" : Return DiskRowState.Unreadable
        End Select
        If r.Clean.HasValue AndAlso Not r.Clean.Value Then Return DiskRowState.Unclean
        Return DiskRowState.NotMounted
    End Function

    ' The state in words (principle 1: never colour alone), with the one most important detail -
    ' the console's own words where it already says them (vdList, vdStatus).
    Public Function StateText(r As DiskRecord, state As DiskRowState, busyVerb As String, dict As Dictionary(Of String, String)) As String
        Select Case state
            Case DiskRowState.Busy : Return T(dict, BusyKey(busyVerb))
            Case DiskRowState.ServerGone : Return T(dict, "vd_mgr_state_server_gone")
            Case DiskRowState.Unsaved : Return Localization.Format(T(dict, "vd_mgr_state_unsaved_fmt"), SizeText(r.RamDirty))
            Case DiskRowState.Mounted : Return T(dict, If(r.ReadOnly, "vd_mgr_state_mounted_ro", "vd_mgr_state_mounted"))
            Case DiskRowState.Image : Return T(dict, "vd_mgr_state_image")
            Case DiskRowState.Missing : Return T(dict, "vd_mgr_state_missing")
            Case DiskRowState.Different : Return T(dict, "vd_mgr_state_different")
            Case DiskRowState.Unreadable : Return T(dict, "vd_mgr_state_unreadable")
            Case DiskRowState.Unclean : Return T(dict, "vd_mgr_state_unclean")
        End Select
        Return T(dict, "vd_mgr_state_not_mounted")
    End Function

    ' The word of an operation in progress, by its verb; "queued" is an operation waiting its turn.
    Public Function BusyKey(verb As String) As String
        Select Case verb
            Case "mount" : Return "vd_mgr_state_busy_mount"
            Case "unmount" : Return "vd_mgr_state_busy_unmount"
            Case "verify" : Return "vd_mgr_state_busy_verify"
            Case "save" : Return "vd_mgr_state_busy_save"
            Case "queued" : Return "vd_mgr_state_queued"
        End Select
        Return "vd_mgr_state_busy_other"
    End Function

    ' The state's glyph (ICON-SET vocabulary): ok, warning or error in the state's tone, and none
    ' for a disk at rest or one being worked on - the word says those, and no vocabulary meaning
    ' says "in progress".
    Private ReadOnly OkGlyph As GlyphRef = GlyphRef.Vocabulary("status.ok")
    Private ReadOnly WarningGlyph As GlyphRef = GlyphRef.Vocabulary("status.warning")
    Private ReadOnly ErrorGlyph As GlyphRef = GlyphRef.Vocabulary("status.error")

    Public Function GlyphOf(state As DiskRowState) As GlyphRef
        Select Case state
            Case DiskRowState.Mounted, DiskRowState.Image : Return OkGlyph
            Case DiskRowState.Unsaved, DiskRowState.Missing, DiskRowState.Different, DiskRowState.Unclean : Return WarningGlyph
            Case DiskRowState.ServerGone, DiskRowState.Unreadable : Return ErrorGlyph
        End Select
        Return Nothing
    End Function

    Friend Function ToneOf(state As DiskRowState, p As Theme.Palette) As Color
        Select Case state
            Case DiskRowState.Mounted, DiskRowState.Image : Return p.StateOk
            Case DiskRowState.Unsaved, DiskRowState.Missing, DiskRowState.Different, DiskRowState.Unclean : Return p.StateWarning
            Case DiskRowState.ServerGone, DiskRowState.Unreadable : Return p.StateError
        End Select
        Return p.MutedText
    End Function

    ' The Protection column: one of two words and nothing in between (principle 3), or "-".
    Public Function ProtectionText(r As DiskRecord, dict As Dictionary(Of String, String)) As String
        If r.IsImage Then Return "-"
        Select Case r.Protection
            Case DiskProtection.Obfuscated : Return T(dict, "vd_mgr_prot_obfuscated")
            Case DiskProtection.Encrypted : Return T(dict, "vd_mgr_prot_encrypted")
        End Select
        Return "-"
    End Function

    Public Function ProfileText(r As DiskRecord) As String
        If r.IsImage Then Return r.ImageFormat
        Return r.Profile
    End Function

    ' A size as the console prints one (vdSize): whole TiB or GiB when it is one, MiB otherwise.
    Public Function SizeText(n As Long) As String
        If n <= 0 Then Return "0 MiB"
        Const GiB As Long = 1L << 30
        Const TiB As Long = 1L << 40
        If n >= TiB AndAlso n Mod TiB = 0 Then Return (n \ TiB).ToString() & " TiB"
        If n >= GiB AndAlso n Mod GiB = 0 Then Return (n \ GiB).ToString() & " GiB"
        If n >= GiB Then Return (n / CDbl(GiB)).ToString("0.#", Globalization.CultureInfo.InvariantCulture) & " GiB"
        Return Math.Max(1L, n >> 20).ToString() & " MiB"
    End Function

    ' The default order (spec 4.1): mounted first by letter, then the rest by name.
    Public Function DefaultOrder(a As DiskRecord, b As DiskRecord) As Integer
        If a.IsMounted <> b.IsMounted Then Return If(a.IsMounted, -1, 1)
        If a.IsMounted Then
            Dim c = String.Compare(a.Letter, b.Letter, StringComparison.OrdinalIgnoreCase)
            If c <> 0 Then Return c
        End If
        Dim n = String.Compare(a.BaseName, b.BaseName, StringComparison.CurrentCultureIgnoreCase)
        If n <> 0 Then Return n
        Return String.Compare(a.Path, b.Path, StringComparison.OrdinalIgnoreCase)
    End Function

    ' ---- the actions ---------------------------------------------------------

    Public Function KindOf(a As DiskAction) As DiskActionKind
        Select Case a
            Case DiskAction.OpenDrive, DiskAction.ShowInFolder, DiskAction.CopyPath, DiskAction.Refresh
                Return DiskActionKind.Local
            Case DiskAction.NewDisk, DiskAction.Export, DiskAction.Compact, DiskAction.Grow, DiskAction.Seal,
                 DiskAction.Clone, DiskAction.ChangePassword
                Return DiskActionKind.Delegated
            Case DiskAction.Format, DiskAction.Destroy
                Return DiskActionKind.Destructive
        End Select
        Return DiskActionKind.Quick
    End Function

    ' The verb of the console an action runs, and "" for one that runs none.
    Public Function VerbOf(a As DiskAction) As String
        Select Case a
            Case DiskAction.Mount, DiskAction.MountReadOnly, DiskAction.MountAs, DiskAction.MountImage : Return "mount"
            Case DiskAction.Unmount, DiskAction.UnmountImage : Return "unmount"
            Case DiskAction.SaveNow : Return "save"
            Case DiskAction.Info : Return "info"
            Case DiskAction.Verify : Return "verify"
            Case DiskAction.AutoOn, DiskAction.AutoOff : Return "auto"
            Case DiskAction.AddToList : Return "add"
            Case DiskAction.Forget : Return "forget"
            Case DiskAction.NewDisk : Return "new"
            Case DiskAction.Export : Return "export"
            Case DiskAction.Compact : Return "compact"
            Case DiskAction.Grow : Return "grow"
            Case DiskAction.Seal : Return "seal"
            Case DiskAction.Clone : Return "clone"
            Case DiskAction.ChangePassword : Return "pass"
            Case DiskAction.Format : Return "format"
            Case DiskAction.Destroy : Return "destroy"
        End Select
        Return ""
    End Function

    ' The Disks job page a delegated action opens (spec 7.3): the page that builds that line stays
    ' the only one that does.
    Public Function JobKeyOf(a As DiskAction) As String
        If KindOf(a) <> DiskActionKind.Delegated AndAlso KindOf(a) <> DiskActionKind.Destructive Then Return ""
        Return "rail_job_vd_" & VerbOf(a)
    End Function

    ' Whether an action's run asks Windows for consent (mount, unmount, the flush of a save, the
    ' scheduled task of auto) - these run one at a time (D4). Reads run beside them.
    Public Function Elevates(a As DiskAction) As Boolean
        Return DiskCommands.NeedsTransport(VerbOf(a))
    End Function

    ' Runs one after another with the elevating ones: everything that changes something. Info and
    ' verify only read, and may run beside them (D4).
    Public Function Serial(a As DiskAction) As Boolean
        Return a <> DiskAction.Info AndAlso a <> DiskAction.Verify
    End Function

    ' The actions that are not about a row: a new disk, an image from a file, a fresh read.
    Public Function IsGlobal(a As DiskAction) As Boolean
        Return a = DiskAction.NewDisk OrElse a = DiskAction.MountImage OrElse a = DiskAction.Refresh
    End Function

    ' The actions that apply to a selection of more than one row (spec 6.2: Ctrl/Shift-click).
    Public Function AppliesToMany(a As DiskAction) As Boolean
        Select Case a
            Case DiskAction.Mount, DiskAction.MountReadOnly, DiskAction.Unmount, DiskAction.UnmountImage,
                 DiskAction.OpenDrive, DiskAction.SaveNow, DiskAction.Info, DiskAction.Verify, DiskAction.Forget
                Return True
        End Select
        Return False
    End Function

    ' The label key of an action in the toolbar, the menu and the detail pane.
    Public Function LabelKey(a As DiskAction, r As DiskRecord) As String
        Select Case a
            Case DiskAction.Mount : Return "vd_mgr_act_mount"
            Case DiskAction.MountReadOnly : Return "vd_mgr_act_mount_ro"
            Case DiskAction.MountAs : Return "vd_mgr_act_mount_as"
            Case DiskAction.Unmount : Return "vd_mgr_act_unmount"
            Case DiskAction.UnmountImage : Return "vd_mgr_act_unmount_image"
            Case DiskAction.OpenDrive : Return "vd_mgr_act_open"
            Case DiskAction.SaveNow : Return "vd_mgr_act_save"
            Case DiskAction.Info : Return "vd_mgr_act_info"
            Case DiskAction.Verify : Return "vd_mgr_act_verify"
            Case DiskAction.AutoOn : Return "vd_mgr_act_auto_on"
            Case DiskAction.AutoOff : Return "vd_mgr_act_auto_off"
            Case DiskAction.AddToList : Return "vd_mgr_act_add"
            Case DiskAction.Forget : Return "vd_mgr_act_forget"
            Case DiskAction.ShowInFolder : Return "vd_mgr_act_show_folder"
            Case DiskAction.CopyPath : Return "vd_mgr_act_copy_path"
            Case DiskAction.NewDisk : Return "vd_mgr_act_new"
            Case DiskAction.MountImage : Return "vd_mgr_act_mount_image"
            Case DiskAction.Export : Return "vd_mgr_act_export"
            Case DiskAction.Compact : Return "vd_mgr_act_compact"
            Case DiskAction.Grow : Return "vd_mgr_act_grow"
            Case DiskAction.Seal : Return "vd_mgr_act_seal"
            Case DiskAction.Clone : Return "vd_mgr_act_clone"
            Case DiskAction.ChangePassword : Return "vd_mgr_act_pass"
            Case DiskAction.Format : Return "vd_mgr_act_format"
            Case DiskAction.Destroy : Return "vd_mgr_act_destroy"
        End Select
        Return "vd_mgr_act_refresh"
    End Function

    ' Why an action does not apply to one row now, as a localization key - or "" when it does
    ' (principle 2: a disabled action says why). The rules are the console's own, checked here so
    ' the window never offers what the console would refuse (spec 7.4 of SP-0004).
    Public Function WhyNot(a As DiskAction, r As DiskRecord, state As DiskRowState, ctx As DiskContext) As String
        If ctx Is Nothing Then ctx = New DiskContext()

        ' The actions that are not about a row.
        Select Case a
            Case DiskAction.NewDisk, DiskAction.Refresh
                Return ""
            Case DiskAction.MountImage
                Return If(ctx.Packaged, "vd_packaged", "")
        End Select
        If r Is Nothing Then Return "vd_mgr_why_no_selection"

        ' Copying the path and showing the file do nothing to the disk.
        Select Case a
            Case DiskAction.CopyPath
                Return ""
            Case DiskAction.ShowInFolder
                Return If(r.FileState = "missing" AndAlso Not r.IsImage, "vd_mgr_why_missing", "")
        End Select

        ' A disk with an operation running accepts no second one (spec 7.1).
        If state = DiskRowState.Busy Then Return "vd_mgr_why_busy"

        ' What a foreign image is: only a drive to open and to detach.
        If r.IsImage Then
            Select Case a
                Case DiskAction.UnmountImage
                    Return If(ctx.Packaged, "vd_packaged", "")
                Case DiskAction.OpenDrive
                    Return ""
            End Select
            Return "vd_mgr_why_image"
        End If

        Dim mounted = r.IsMounted
        Select Case a
            Case DiskAction.Mount, DiskAction.MountReadOnly, DiskAction.MountAs
                If ctx.Packaged Then Return "vd_packaged"
                If mounted Then Return "vd_mgr_why_already_mounted"
                Dim fw = FileWhy(state)
                If fw <> "" Then Return fw
                Return TransportWhy(ctx)

            Case DiskAction.Unmount
                If ctx.Packaged Then Return "vd_packaged"
                If Not mounted Then Return "vd_block_not_mounted"
                Return ""

            Case DiskAction.UnmountImage
                Return "vd_mgr_why_not_image"

            Case DiskAction.OpenDrive
                If Not mounted Then Return "vd_block_not_mounted"
                If state = DiskRowState.ServerGone Then Return "vd_mgr_why_server_gone"
                Return ""

            Case DiskAction.SaveNow
                If ctx.Packaged Then Return "vd_packaged"
                If Not mounted Then Return "vd_block_not_mounted"
                If state = DiskRowState.ServerGone Then Return "vd_mgr_why_server_gone"
                If Not r.IsRam Then Return "vd_mgr_why_not_ram"
                If r.ReadOnly Then Return "vd_mgr_why_read_only"
                Return ""

            Case DiskAction.Info
                If state = DiskRowState.Missing Then Return "vd_mgr_why_missing"
                Return ""

            Case DiskAction.Verify
                If mounted Then Return "vd_mgr_why_mounted"
                Return FileWhy(state)

            Case DiskAction.AutoOn
                If ctx.Packaged Then Return "vd_packaged"
                If Not r.Registered Then Return "vd_mgr_why_not_registered"
                If r.AutoMount Then Return "vd_mgr_why_auto_on_already"
                Dim fw = FileWhy(state)
                If fw <> "" Then Return fw
                ' The console refuses it (FDD-BEHAVIOUR 7 rule 9): no credential is stored anywhere.
                If r.Protection = DiskProtection.Encrypted Then Return "vd_mgr_why_encrypted_auto"
                Return ""

            Case DiskAction.AutoOff
                If ctx.Packaged Then Return "vd_packaged"
                If Not r.Registered Then Return "vd_mgr_why_not_registered"
                If Not r.AutoMount Then Return "vd_mgr_why_auto_off_already"
                Return ""

            Case DiskAction.AddToList
                If r.Registered Then Return "vd_mgr_why_registered"
                Return FileWhy(state)

            Case DiskAction.Forget
                If Not r.Registered Then Return "vd_mgr_why_not_registered"
                Return ""

            Case DiskAction.Export, DiskAction.Seal, DiskAction.Clone
                Return FileWhy(state)

            Case DiskAction.Compact, DiskAction.Grow
                If mounted Then Return "vd_mgr_why_mounted"
                Return FileWhy(state)

            Case DiskAction.ChangePassword
                If mounted Then Return "vd_mgr_why_mounted"
                Dim fw = FileWhy(state)
                If fw <> "" Then Return fw
                If r.Protection = DiskProtection.Obfuscated Then Return "vd_block_pass_obfuscated"
                Return ""

            Case DiskAction.Format
                If ctx.Packaged Then Return "vd_packaged"
                If mounted Then Return "vd_mgr_why_mounted"
                Return FileWhy(state)

            Case DiskAction.Destroy
                If mounted Then Return "vd_mgr_why_mounted"
                If state = DiskRowState.Missing Then Return "vd_mgr_why_missing"
                Return ""
        End Select
        Return ""
    End Function

    ' An action this build can never run is not offered at all: APP-BEHAVIOUR rule 11, "a control that
    ' cannot work in this build is hidden, not disabled". The Store build cannot mount (class 6), so
    ' it shows no mount, unmount, save-now, auto-mount or format - each of which WhyNot refuses with
    ' vd_packaged - and keeps the read path: info, verify, export, the list and its names. What is only
    ' unavailable now (a service that is off) stays a disabled control that says why (principle 2).
    Public Function HiddenInBuild(a As DiskAction, ctx As DiskContext) As Boolean
        If ctx Is Nothing OrElse Not ctx.Packaged Then Return False
        Select Case a
            Case DiskAction.Mount, DiskAction.MountReadOnly, DiskAction.MountAs, DiskAction.MountImage,
                 DiskAction.Unmount, DiskAction.UnmountImage, DiskAction.SaveNow,
                 DiskAction.AutoOn, DiskAction.AutoOff, DiskAction.Format
                Return True
        End Select
        Return False
    End Function

    ' Why the file itself stands in the way: gone, another container, or a header that does not read.
    Private Function FileWhy(state As DiskRowState) As String
        Select Case state
            Case DiskRowState.Missing : Return "vd_mgr_why_missing"
            Case DiskRowState.Different : Return "vd_mgr_why_different"
            Case DiskRowState.Unreadable : Return "vd_mgr_why_unreadable"
        End Select
        Return ""
    End Function

    ' Why the transport refuses a mount now. A service manager that could not be asked is not a
    ' refusal: the mount tries, and the console says what it met.
    Public Function TransportWhy(ctx As DiskContext) As String
        If ctx.Packaged Then Return "vd_packaged"
        If ctx.TransportReady Then Return ""
        Select Case ctx.TransportReason
            Case "initiator_missing" : Return "vd_mgr_why_initiator_missing"
            Case "initiator_disabled" : Return "vd_mgr_why_initiator_disabled"
        End Select
        Return ""
    End Function

    ' The same question for a selection: "" when the action applies to every row, the rows' shared
    ' reason when none allows it for the same one, and "not every selected disk" otherwise.
    Public Function WhyNotAll(a As DiskAction, rows As IList(Of DiskRecord), states As IList(Of DiskRowState), ctx As DiskContext) As String
        ' The actions that are not about a row do not care what is selected.
        If IsGlobal(a) OrElse rows Is Nothing OrElse rows.Count = 0 Then Return WhyNot(a, Nothing, DiskRowState.NotMounted, ctx)
        If rows.Count > 1 AndAlso Not AppliesToMany(a) Then Return "vd_mgr_why_one_at_a_time"
        Dim first As String = Nothing
        Dim anyOk = False
        For i = 0 To rows.Count - 1
            Dim why = WhyNot(a, rows(i), states(i), ctx)
            If why = "" Then
                anyOk = True
            ElseIf first Is Nothing Then
                first = why
            ElseIf first <> why Then
                first = "vd_mgr_why_mixed"
            End If
        Next
        If first Is Nothing Then Return ""
        Return If(anyOk, "vd_mgr_why_mixed", first)
    End Function

    ' A double-click and Enter (spec 6.2, D3): a disk at rest is mounted, a mounted one is opened -
    ' never unmounted, since a double-click must not take a volume away from running programs - and
    ' anything else only selects. Nothing destructive is ever reached this way.
    Public Function DefaultAction(state As DiskRowState) As DiskAction?
        Select Case state
            Case DiskRowState.NotMounted, DiskRowState.Unclean : Return DiskAction.Mount
            Case DiskRowState.Mounted, DiskRowState.Unsaved, DiskRowState.Image : Return DiskAction.OpenDrive
        End Select
        Return Nothing
    End Function

    ' The console line of a quick action (principle 4): built by DiskCommands.Build, the function the
    ' jobs use, and never assembled here. A mount by path, an unmount and a save by the letter the
    ' snapshot is sure of, forget by the registry name - and auto by the container's path, which the
    ' console takes for its registered name as the Auto page's line does, and which history keeps
    ' readable where a bare name after "off" is screened as a possible password.
    Public Function QuickCommand(a As DiskAction, r As DiskRecord, o As DiskOptions) As List(Of String)
        If o Is Nothing Then o = New DiskOptions()
        o.Protection = r.Protection
        Select Case a
            Case DiskAction.Mount, DiskAction.MountAs
                Return DiskCommands.Build("mount", r.Path, o)
            Case DiskAction.MountReadOnly
                o.ReadOnly = True
                Return DiskCommands.Build("mount", r.Path, o)
            Case DiskAction.MountImage
                o.Protection = DiskProtection.Unknown
                o.HasCredential = False
                Return DiskCommands.Build("mount", r.Path, o)
            Case DiskAction.Unmount, DiskAction.UnmountImage
                Return DiskCommands.Build("unmount", r.Letter, New DiskOptions())
            Case DiskAction.SaveNow
                Return DiskCommands.Build("save", r.Letter, New DiskOptions())
            Case DiskAction.Info
                Return DiskCommands.Build("info", r.Path, New DiskOptions())
            Case DiskAction.Verify
                Return DiskCommands.Build("verify", r.Path, o)
            Case DiskAction.AutoOn
                Return DiskCommands.Build("auto", r.Path, New DiskOptions With {.AutoOn = True})
            Case DiskAction.AutoOff
                Return DiskCommands.Build("auto", r.Path, New DiskOptions With {.AutoOn = False})
            Case DiskAction.AddToList
                Return DiskCommands.Build("add", r.Path, New DiskOptions With {.Remember = True, .Name = o.Name})
            Case DiskAction.Forget
                Return DiskCommands.Build("add", r.Name, New DiskOptions With {.Remember = False})
        End Select
        Return Nothing
    End Function

    ' Whether a quick action asks for the password first: the verb takes one and the container is
    ' encrypted. An obfuscated container is never asked for one (spec 7.1).
    Public Function AsksPassword(a As DiskAction, r As DiskRecord) As Boolean
        If r Is Nothing OrElse r.IsImage OrElse r.Protection <> DiskProtection.Encrypted Then Return False
        Select Case a
            Case DiskAction.Mount, DiskAction.MountReadOnly, DiskAction.MountAs, DiskAction.Verify
                Return True
        End Select
        Return False
    End Function

    ' The name `vd add .. as <name>` accepts (vdNameSpelling in cmd\filedo\vdisk_registry.go): letters,
    ' digits, - and _, up to 40, starting with a letter or a digit, and not a drive letter.
    Private ReadOnly NameSpelling As New Text.RegularExpressions.Regex("^[A-Za-z0-9][A-Za-z0-9_-]{0,39}$",
                                                                     Text.RegularExpressions.RegexOptions.CultureInvariant)

    Public Function IsUsableName(name As String) As Boolean
        Dim n = If(name, "").Trim()
        Return NameSpelling.IsMatch(n) AndAlso Not TargetPath.IsDriveToken(n)
    End Function

    Private Function T(dict As Dictionary(Of String, String), key As String) As String
        Dim v As String = Nothing
        If dict IsNot Nothing AndAlso dict.TryGetValue(key, v) Then Return v
        Return key
    End Function

End Module
