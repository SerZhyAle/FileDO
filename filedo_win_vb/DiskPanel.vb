Imports System.IO

' Step 3 of every Disks page (SP-0004 P6, spec 7.1), as one control the job page hosts - the way
' CheckOptionsPanel holds the whole option surface of `check`.
'
' Three rules shape it:
'
'   What a choice costs is printed beside the choice (T6.24). The profile radios on Create carry
'   the sentence of the profile chosen - what `ram` risks, what `vault` costs if the password is
'   lost - and a Seal page says that a sealed copy is read-only for good. An empty password is
'   called obfuscation, never encryption, on every page that can take one.
'
'   A credential never leaves this control by any route but the child's environment (T6.21). The
'   command names FILEDO_SHELL_CRED - and, for `pass`, FILEDO_SHELL_CRED_NEW - and the values are
'   handed to the runner as a dictionary that dies with the run.
'
'   The page refuses what the console would refuse (spec 7.4): a mounted container is not formatted,
'   destroyed, grown or compacted from here, a packaged build is not offered a mount, and a
'   container already mounted is not mounted twice. The reason is the line above Run.
Public Class DiskOptionsPanel
    Inherits FlowLayoutPanel

    Public Event Changed()

    ' The typed words of the destructive answers (the shell's typed confirmation, SP-0006 8.1).
    Friend Const FormatWord As String = "FORMAT"
    Friend Const DestroyWord As String = "DESTROY"
    Friend Const DiscardWord As String = "DISCARD"

    Private Const ShortPassword As Integer = 12

    Private ReadOnly dict As Dictionary(Of String, String)
    Private verb As String = ""
    Private suspendEvents As Boolean = False

    ' What `info` said about the container in step 2: Nothing while no container is named, and a
    ' fact sheet with Read = False while it is being read or could not be.
    Private knownFacts As ContainerFacts = Nothing
    Private factsChecking As Boolean = False

    Private factsLabel As Label
    Private noteLabel As Label
    Private elevationLabel As Label

    Private sizeRow As FlowLayoutPanel
    Private sizeLabel As Label
    Private sizeBox As TextBox

    Private profileRow As FlowLayoutPanel
    Private profileLabel As Label
    Private profilePlain As RadioButton
    Private profileFast As RadioButton
    Private profileRam As RadioButton
    Private profileVault As RadioButton
    Private profileNotice As Label

    Private labelRow As FlowLayoutPanel
    Private volLabelLabel As Label
    Private volLabelBox As TextBox

    Private fsRow As FlowLayoutPanel
    Private fsLabel As Label
    Private fsCombo As ComboBox

    Private roCheck As CheckBox
    Private noscanCheck As CheckBox
    Private letterRow As FlowLayoutPanel
    Private letterLabel As Label
    Private letterCombo As ComboBox

    Private forceCheck As CheckBox
    Private nosaveCheck As CheckBox
    Private wipeCheck As CheckBox

    Private exportRow As FlowLayoutPanel
    Private exportRaw As RadioButton
    Private exportVhd As RadioButton

    Private destRow As FlowLayoutPanel
    Private destLabel As Label
    Private destBox As TextBox
    Private destBrowseBtn As Button

    Private nopassCheck As CheckBox
    Private nopassNotice As Label

    Private autoRow As FlowLayoutPanel
    Private autoOnRadio As RadioButton
    Private autoOffRadio As RadioButton

    Private nameRow As FlowLayoutPanel
    Private rememberRadio As RadioButton
    Private forgetRadio As RadioButton
    Private nameLabel As Label
    Private nameBox As TextBox

    Private listRow As FlowLayoutPanel
    Private listRadio As RadioButton
    Private statusRadio As RadioButton

    Private credBlock As FlowLayoutPanel
    Private credLabel As Label
    Private credBox As TextBox
    Private credShowCheck As CheckBox
    Private credConfirmLabel As Label
    Private credConfirmBox As TextBox
    Private newCredLabel As Label
    Private newCredBox As TextBox
    Private newCredConfirmLabel As Label
    Private newCredConfirmBox As TextBox
    Private credNotice As Label
    Private credHint As Label

    Private confirmRow As FlowLayoutPanel
    Private confirmLabel As Label
    Private confirmBox As TextBox

    Private ReadOnly buttons As New List(Of Button)

    Public Sub New()
        dict = Localization.GetDict(ShellSettings.Language())
        Dock = DockStyle.Top
        AutoSize = True
        AutoSizeMode = AutoSizeMode.GrowAndShrink
        FlowDirection = FlowDirection.TopDown
        WrapContents = False
        Margin = Ui.PxPad(Me, 0, 2, 0, 4)
        Visible = False
        BuildContent()
    End Sub

    Private Function L(key As String) As String
        Dim v As String = Nothing
        If dict IsNot Nothing AndAlso dict.TryGetValue(key, v) Then Return v
        Return key
    End Function

    ' ---- layout ----------------------------------------------------------

    Private Function NewRow(Optional topDown As Boolean = False) As FlowLayoutPanel
        Return New FlowLayoutPanel With {
            .AutoSize = True,
            .AutoSizeMode = AutoSizeMode.GrowAndShrink,
            .FlowDirection = If(topDown, FlowDirection.TopDown, FlowDirection.LeftToRight),
            .WrapContents = Not topDown,
            .Margin = Ui.PxPad(Me, 0, 2, 0, 2)
        }
    End Function

    Private Function NewLabel(key As String) As Label
        Return New Label With {.Text = L(key), .AutoSize = True, .Margin = Ui.PxPad(Me, 0, 6, 8, 0)}
    End Function

    Private Function NewNote() As Label
        Return New Label With {.Text = "", .AutoSize = True, .Margin = Ui.PxPad(Me, 0, 2, 0, 6)}
    End Function

    Private Function NewBox(nameKey As String, width As Integer, Optional secret As Boolean = False) As TextBox
        Dim b As New TextBox With {.Width = Ui.Px(Me, width), .Margin = Ui.PxPad(Me, 0, 2, 8, 2), .UseSystemPasswordChar = secret}
        b.AccessibleName = L(nameKey)
        AddHandler b.TextChanged, Sub() OnChanged()
        Return b
    End Function

    Private Function NewCheck(key As String) As CheckBox
        Dim c As New CheckBox With {.Text = L(key), .AutoSize = True, .Margin = Ui.PxPad(Me, 0, 2, 0, 2)}
        AddHandler c.CheckedChanged, Sub() OnChanged()
        Return c
    End Function

    Private Function NewRadio(key As String) As RadioButton
        Dim r As New RadioButton With {.Text = L(key), .AutoSize = True, .Margin = Ui.PxPad(Me, 0, 2, 12, 2)}
        AddHandler r.CheckedChanged, Sub() OnChanged()
        Return r
    End Function

    Private Sub BuildContent()
        factsLabel = NewNote()
        factsLabel.Margin = Ui.PxPad(Me, 0, 0, 0, 8)
        noteLabel = NewNote()
        elevationLabel = NewNote()

        ' Size - Create and Grow.
        sizeRow = NewRow()
        sizeLabel = NewLabel("vd_lbl_size")
        sizeBox = NewBox("vd_lbl_size", 110)
        sizeRow.Controls.Add(sizeLabel)
        sizeRow.Controls.Add(sizeBox)

        ' Profile - Create. The four answers stand one under another with the sentence of the
        ' chosen one below them (T6.24).
        profileRow = NewRow(True)
        profileLabel = NewLabel("vd_lbl_profile")
        profilePlain = NewRadio("vd_profile_plain")
        profileFast = NewRadio("vd_profile_fast")
        profileRam = NewRadio("vd_profile_ram")
        profileVault = NewRadio("vd_profile_vault")
        profileNotice = NewNote()
        profileRow.Controls.Add(profileLabel)
        profileRow.Controls.Add(profilePlain)
        profileRow.Controls.Add(profileFast)
        profileRow.Controls.Add(profileRam)
        profileRow.Controls.Add(profileVault)
        profileRow.Controls.Add(profileNotice)

        labelRow = NewRow()
        volLabelLabel = NewLabel("vd_lbl_label")
        volLabelBox = NewBox("vd_lbl_label", 200)
        labelRow.Controls.Add(volLabelLabel)
        labelRow.Controls.Add(volLabelBox)

        fsRow = NewRow()
        fsLabel = NewLabel("vd_lbl_fs")
        fsCombo = New ComboBox With {.DropDownStyle = ComboBoxStyle.DropDownList, .Width = Ui.Px(Me, 110), .Margin = Ui.PxPad(Me, 0, 2, 8, 2)}
        fsCombo.Items.AddRange(New Object() {"ntfs", "exfat"})
        fsCombo.SelectedIndex = 0
        fsCombo.AccessibleName = L("vd_lbl_fs")
        AddHandler fsCombo.SelectedIndexChanged, Sub() OnChanged()
        fsRow.Controls.Add(fsLabel)
        fsRow.Controls.Add(fsCombo)

        ' Mount.
        roCheck = NewCheck("vd_opt_ro")
        noscanCheck = NewCheck("vd_opt_noscan")
        letterRow = NewRow()
        letterLabel = NewLabel("vd_lbl_letter")
        letterCombo = New ComboBox With {.DropDownStyle = ComboBoxStyle.DropDownList, .Width = Ui.Px(Me, 150), .Margin = Ui.PxPad(Me, 0, 2, 8, 2)}
        letterCombo.AccessibleName = L("vd_lbl_letter")
        AddHandler letterCombo.SelectedIndexChanged, Sub() OnChanged()
        letterRow.Controls.Add(letterLabel)
        letterRow.Controls.Add(letterCombo)

        ' Unmount and Destroy.
        forceCheck = NewCheck("vd_opt_force")
        nosaveCheck = NewCheck("vd_opt_nosave")
        wipeCheck = NewCheck("vd_opt_wipe")

        ' Export.
        exportRow = NewRow(True)
        exportRaw = NewRadio("vd_export_raw")
        exportVhd = NewRadio("vd_export_vhd")
        exportRow.Controls.Add(NewLabel("vd_lbl_export_form"))
        exportRow.Controls.Add(exportRaw)
        exportRow.Controls.Add(exportVhd)

        ' The new file of Export, Seal and Clone.
        destRow = NewRow()
        destLabel = NewLabel("vd_lbl_dest")
        destBox = NewBox("vd_lbl_dest", 320)
        destBrowseBtn = New Button With {.Text = L("shell_btn_browse_file"), .AutoSize = True, .AutoSizeMode = AutoSizeMode.GrowAndShrink, .Margin = Ui.PxPad(Me, 0, 1, 0, 2)}
        AddHandler destBrowseBtn.Click, AddressOf BrowseDest_Click
        buttons.Add(destBrowseBtn)
        destRow.Controls.Add(destLabel)
        destRow.Controls.Add(destBox)
        destRow.Controls.Add(destBrowseBtn)

        nopassCheck = NewCheck("vd_opt_nopass")
        nopassNotice = NewNote()

        ' Auto-mount.
        autoRow = NewRow(True)
        autoOnRadio = NewRadio("vd_auto_on")
        autoOffRadio = NewRadio("vd_auto_off")
        autoRow.Controls.Add(autoOnRadio)
        autoRow.Controls.Add(autoOffRadio)

        ' Remember or forget.
        nameRow = NewRow(True)
        rememberRadio = NewRadio("vd_remember")
        forgetRadio = NewRadio("vd_forget")
        Dim nameInner = NewRow()
        nameLabel = NewLabel("vd_lbl_name")
        nameBox = NewBox("vd_lbl_name", 180)
        nameInner.Controls.Add(nameLabel)
        nameInner.Controls.Add(nameBox)
        nameRow.Controls.Add(rememberRadio)
        nameRow.Controls.Add(nameInner)
        nameRow.Controls.Add(forgetRadio)

        ' The list.
        listRow = NewRow(True)
        listRadio = NewRadio("vd_list_registered")
        statusRadio = NewRadio("vd_list_mounted")
        listRow.Controls.Add(listRadio)
        listRow.Controls.Add(statusRadio)

        BuildCredentialBlock()

        confirmRow = NewRow()
        confirmRow.Margin = Ui.PxPad(Me, 0, 6, 0, 0)
        confirmLabel = NewLabel("vd_confirm_format")
        confirmBox = NewBox("vd_confirm_format", 120)
        confirmRow.Controls.Add(confirmLabel)
        confirmRow.Controls.Add(confirmBox)

        For Each c As Control In New Control() {
            factsLabel, sizeRow, profileRow, labelRow, fsRow, roCheck, noscanCheck, letterRow,
            forceCheck, nosaveCheck, wipeCheck, exportRow, destRow, nopassCheck, nopassNotice,
            autoRow, nameRow, listRow, credBlock, noteLabel, elevationLabel, confirmRow}
            Controls.Add(c)
        Next
    End Sub

    ' The credential, masked with a show toggle, and the line under it that says what the password
    ' is worth as it is typed (spec 7.2, the same words the console uses).
    Private Sub BuildCredentialBlock()
        credBlock = NewRow(True)
        credBlock.Margin = Ui.PxPad(Me, 0, 6, 0, 0)

        credLabel = NewLabel("shell_lbl_password")
        credBox = NewBox("shell_lbl_password", 300, secret:=True)
        credShowCheck = New CheckBox With {.Text = L("shell_cred_show"), .AutoSize = True, .Margin = Ui.PxPad(Me, 0, 2, 0, 2)}
        AddHandler credShowCheck.CheckedChanged,
            Sub()
                For Each b In New TextBox() {credBox, credConfirmBox, newCredBox, newCredConfirmBox}
                    b.UseSystemPasswordChar = Not credShowCheck.Checked
                Next
            End Sub
        credConfirmLabel = NewLabel("shell_cred_confirm")
        credConfirmBox = NewBox("shell_cred_confirm", 300, secret:=True)
        newCredLabel = NewLabel("vd_lbl_new_password")
        newCredBox = NewBox("vd_lbl_new_password", 300, secret:=True)
        newCredConfirmLabel = NewLabel("vd_lbl_new_password_again")
        newCredConfirmBox = NewBox("vd_lbl_new_password_again", 300, secret:=True)
        credNotice = NewNote()
        credHint = NewNote()
        credHint.Text = L("shell_cred_out_of_sight")

        For Each c As Control In New Control() {
            credLabel, credBox, credConfirmLabel, credConfirmBox, newCredLabel, newCredBox,
            newCredConfirmLabel, newCredConfirmBox, credShowCheck, credNotice, credHint}
            credBlock.Controls.Add(c)
        Next
    End Sub

    ' The labels that can run long wrap inside the card they are in.
    Public Sub WrapIn(card As Control, reserve As Integer)
        For Each lb In New Label() {factsLabel, noteLabel, elevationLabel, profileNotice, nopassNotice, credNotice, credHint}
            Ui.Wrap(lb, card, reserve)
        Next
    End Sub

    ' ---- the page's questions --------------------------------------------

    ' Shows the controls this verb has, and nothing else.
    Public Sub Configure(verbOfJob As String)
        suspendEvents = True
        verb = If(verbOfJob, "")

        sizeRow.Visible = (verb = "new" OrElse verb = "grow")
        sizeLabel.Text = If(verb = "grow", L("vd_lbl_new_size"), L("vd_lbl_size"))
        sizeBox.AccessibleName = sizeLabel.Text
        profileRow.Visible = (verb = "new")
        labelRow.Visible = (verb = "new" OrElse verb = "format")
        fsRow.Visible = (verb = "format")
        roCheck.Visible = (verb = "mount")
        noscanCheck.Visible = (verb = "mount")
        letterRow.Visible = (verb = "mount")
        forceCheck.Visible = (verb = "unmount")
        nosaveCheck.Visible = (verb = "unmount")
        wipeCheck.Visible = (verb = "destroy")
        exportRow.Visible = (verb = "export")
        destRow.Visible = (verb = "export" OrElse verb = "seal" OrElse verb = "clone")
        nopassCheck.Visible = (verb = "seal" OrElse verb = "clone")
        autoRow.Visible = (verb = "auto")
        nameRow.Visible = (verb = "add")
        listRow.Visible = (verb = "list")
        elevationLabel.Visible = DiskCommands.NeedsTransport(verb)
        elevationLabel.Text = If(verb = "auto", L("vd_auto_elevation_note"), L("vd_elevation_note"))

        credLabel.Text = If(verb = "pass", L("vd_lbl_old_password"), L("shell_lbl_password"))
        credBox.AccessibleName = credLabel.Text
        credConfirmLabel.Visible = (verb = "new")
        credConfirmBox.Visible = (verb = "new")
        newCredLabel.Visible = (verb = "pass")
        newCredBox.Visible = (verb = "pass")
        newCredConfirmLabel.Visible = (verb = "pass")
        newCredConfirmBox.Visible = (verb = "pass")

        If verb = "mount" Then FillLetters()

        suspendEvents = False
        Refresh_()
    End Sub

    ' Every answer back to the console's own default, and every password box emptied: a password
    ' left in a box that is no longer the one in front of the user is used by accident.
    Public Sub Reset()
        suspendEvents = True
        knownFacts = Nothing
        factsChecking = False
        For Each b In New TextBox() {sizeBox, volLabelBox, destBox, nameBox, credBox, credConfirmBox,
                                    newCredBox, newCredConfirmBox, confirmBox}
            b.Text = ""
        Next
        For Each c In New CheckBox() {roCheck, noscanCheck, forceCheck, nosaveCheck, wipeCheck, nopassCheck, credShowCheck}
            c.Checked = False
        Next
        profilePlain.Checked = True
        exportRaw.Checked = True
        autoOnRadio.Checked = True
        rememberRadio.Checked = True
        listRadio.Checked = True
        fsCombo.SelectedIndex = 0
        If letterCombo.Items.Count > 0 Then letterCombo.SelectedIndex = 0
        suspendEvents = False
        Refresh_()
    End Sub

    ' A preset asked for from outside - the list's "turn off auto-mount", a --mount-ro start.
    Public Sub ApplyPreset(preset As String)
        Select Case If(preset, "")
            Case "off" : autoOffRadio.Checked = True
            Case "ro" : roCheck.Checked = True
            Case "mounted" : statusRadio.Checked = True
        End Select
    End Sub

    ' The drive letters a mount may ask for: the first free one (the default), then every letter
    ' no drive holds now.
    Private Sub FillLetters()
        Dim keep = If(letterCombo.SelectedIndex > 0, CStr(letterCombo.SelectedItem), "")
        letterCombo.Items.Clear()
        letterCombo.Items.Add(L("vd_letter_auto"))
        Dim used As New HashSet(Of Char)()
        Try
            For Each d In DriveInfo.GetDrives()
                If d.Name.Length > 0 Then used.Add(Char.ToUpperInvariant(d.Name(0)))
            Next
        Catch ex As Exception
            ShellLog.Write("list drive letters", ex)
        End Try
        For c = AscW("D"c) To AscW("Z"c)
            Dim ch = ChrW(c)
            If Not used.Contains(ch) Then letterCombo.Items.Add(ch & ":")
        Next
        Dim at = If(keep = "", 0, letterCombo.Items.IndexOf(keep))
        letterCombo.SelectedIndex = Math.Max(0, at)
    End Sub

    ' ---- what the container is -------------------------------------------

    Public Sub ClearFacts()
        knownFacts = Nothing
        factsChecking = False
        Refresh_()
    End Sub

    Public Sub SetFactsChecking(path As String)
        knownFacts = ContainerFacts.Unknown(path)
        factsChecking = True
        Refresh_()
    End Sub

    Public Sub SetFacts(f As ContainerFacts)
        knownFacts = f
        factsChecking = False
        Refresh_()
    End Sub

    Public ReadOnly Property Facts As ContainerFacts
        Get
            Return knownFacts
        End Get
    End Property

    Private ReadOnly Property Protection As DiskProtection
        Get
            If knownFacts Is Nothing OrElse Not knownFacts.Read Then Return DiskProtection.Unknown
            Return knownFacts.Protection
        End Get
    End Property

    ' The container in step 2, in words: obfuscated or encrypted - never one called the other
    ' (spec 7.1) - whether it is mounted now, and whether it was closed cleanly.
    Private Function FactsText() As String
        If knownFacts Is Nothing Then Return ""
        If factsChecking Then Return L("vd_facts_checking")
        If Not knownFacts.Read Then Return L("vd_facts_unknown")
        Dim lines As New List(Of String)()
        lines.Add(If(knownFacts.Protection = DiskProtection.Obfuscated, L("vd_facts_obfuscated"), L("vd_facts_encrypted")))
        If knownFacts.Profile <> "" Then lines.Add(Localization.Format(L("vd_facts_profile_fmt"), knownFacts.Profile))
        If knownFacts.IsMounted Then
            lines.Add(Localization.Format(L("vd_facts_mounted_fmt"), knownFacts.MountedLetter))
        ElseIf knownFacts.Clean.HasValue AndAlso Not knownFacts.Clean.Value Then
            lines.Add(Localization.Format(L("vd_facts_unclean_fmt"), If(knownFacts.LastGoodSave = "", "-", knownFacts.LastGoodSave)))
        End If
        Return String.Join(Environment.NewLine, lines.ToArray())
    End Function

    Private Function FactsWarn() As Boolean
        If knownFacts Is Nothing OrElse Not knownFacts.Read Then Return False
        Return knownFacts.Protection = DiskProtection.Obfuscated OrElse
               (Not knownFacts.IsMounted AndAlso knownFacts.Clean.HasValue AndAlso Not knownFacts.Clean.Value)
    End Function

    ' ---- the answers -----------------------------------------------------

    Private Function CredentialShown() As Boolean
        If Not DiskCommands.TakesCredential(verb) Then Return False
        If verb = "new" Then Return True
        Return Protection <> DiskProtection.Obfuscated
    End Function

    Public ReadOnly Property Credential As String
        Get
            Return If(CredentialShown(), credBox.Text, "")
        End Get
    End Property

    Public Function ToOptions() As DiskOptions
        Dim profile = "plain"
        If profileFast.Checked Then profile = "fast"
        If profileRam.Checked Then profile = "ram"
        If profileVault.Checked Then profile = "vault"
        Return New DiskOptions With {
            .Size = sizeBox.Text,
            .Profile = profile,
            .Label = volLabelBox.Text,
            .FileSystem = CStr(If(fsCombo.SelectedItem, "ntfs")),
            .ReadOnly = roCheck.Checked,
            .NoScan = noscanCheck.Checked,
            .Letter = If(letterCombo.SelectedIndex > 0, CStr(letterCombo.SelectedItem), ""),
            .Force = forceCheck.Checked,
            .NoSave = nosaveCheck.Checked,
            .Wipe = wipeCheck.Checked,
            .NoPass = nopassCheck.Checked,
            .ExportForm = If(exportVhd.Checked, "vhd", "raw"),
            .Dest = ResolvedDest(),
            .AutoOn = autoOnRadio.Checked,
            .Remember = rememberRadio.Checked,
            .Name = nameBox.Text,
            .ShowMounted = statusRadio.Checked,
            .HasCredential = CredentialShown() AndAlso credBox.Text <> "",
            .HasNewCredential = (verb = "pass" AndAlso newCredBox.Text <> ""),
            .Protection = Protection
        }
    End Function

    Private Function ResolvedDest() As String
        Dim t = destBox.Text.Trim()
        If t = "" Then Return ""
        Return If(TargetPath.Resolve(t), t)
    End Function

    ' The child's environment: the password under FILEDO_SHELL_CRED, the new one of `pass` under
    ' FILEDO_SHELL_CRED_NEW, and nothing when the line names neither.
    Public Function EnvironmentForRun() As Dictionary(Of String, String)
        Dim o = ToOptions()
        Dim env As New Dictionary(Of String, String)()
        If o.HasCredential Then env(DiskCommands.CredentialEnvName) = credBox.Text
        If o.HasNewCredential Then env(DiskCommands.NewCredentialEnvName) = newCredBox.Text
        Return If(env.Count = 0, Nothing, env)
    End Function

    Public Function IsDestructiveNow(jobIsDestructive As Boolean) As Boolean
        Return jobIsDestructive OrElse (verb = "unmount" AndAlso nosaveCheck.Checked)
    End Function

    Public Function ReversibilityNow(ofJob As String) As String
        If verb = "unmount" AndAlso nosaveCheck.Checked Then Return "permanent"
        Return ofJob
    End Function

    Private Function ConfirmWord() As String
        Select Case verb
            Case "format" : Return FormatWord
            Case "destroy" : Return DestroyWord
            Case "unmount" : Return If(nosaveCheck.Checked, DiscardWord, "")
        End Select
        Return ""
    End Function

    ' Why Run is not offered for this target and these answers, or "" when it is. The target has
    ' already been checked for being there and being a full path (JobView).
    Public Function BlockReason(target As String) As String
        Dim t = If(target, "").Trim()

        ' What step 2 names.
        If verb = "new" Then
            If Not DiskCommands.IsFddPath(t) Then Return L("vd_need_fdd")
            If FileExistsQuietly(t) Then Return L("vd_new_exists")
        ElseIf DiskCommands.ActsOnContainer(verb) Then
            Dim letterOk = (verb = "unmount" AndAlso TargetPath.IsDriveToken(t.TrimEnd("\"c)))
            If Not letterOk AndAlso Not DiskCommands.IsFddPath(t) Then Return L("vd_need_fdd")
        End If

        ' What this build can do.
        If DiskCommands.NeedsTransport(verb) AndAlso Packaging.IsPackaged() Then Return L("vd_packaged")

        ' What the container is.
        If knownFacts IsNot Nothing AndAlso knownFacts.Read Then
            If verb = "mount" AndAlso knownFacts.IsMounted Then Return Localization.Format(L("vd_block_already_mounted_fmt"), knownFacts.MountedLetter)
            If DiskCommands.RefusedWhileMounted(verb) AndAlso knownFacts.IsMounted Then Return Localization.Format(L("vd_block_mounted_fmt"), knownFacts.MountedLetter)
            If (verb = "unmount" OrElse verb = "save") AndAlso Not knownFacts.IsMounted Then Return L("vd_block_not_mounted")
            If verb = "pass" AndAlso knownFacts.Protection = DiskProtection.Obfuscated Then Return L("vd_block_pass_obfuscated")
        End If

        ' The parameters.
        If verb = "new" OrElse verb = "grow" Then
            If Not DiskCommands.IsSize(sizeBox.Text) Then Return L("vd_need_size")
        End If
        If destRow.Visible OrElse verb = "export" OrElse verb = "seal" OrElse verb = "clone" Then
            Dim d = destBox.Text.Trim()
            If d = "" Then Return L("vd_need_dest")
            If TargetPath.Resolve(d) Is Nothing Then Return L("shell_target_not_absolute")
            If (verb = "seal" OrElse verb = "clone") AndAlso Not DiskCommands.IsFddPath(d) Then Return L("vd_need_fdd_dest")
            If String.Equals(ResolvedDest(), t, StringComparison.OrdinalIgnoreCase) Then Return L("vd_dest_is_source")
            If FileExistsQuietly(ResolvedDest()) Then Return L("vd_dest_exists")
        End If
        If verb = "add" AndAlso rememberRadio.Checked AndAlso nameBox.Text.Trim().Contains(" ") Then Return L("vd_name_one_word")

        ' The credentials.
        If verb = "new" Then
            If profileVault.Checked AndAlso credBox.Text = "" Then Return L("vd_cred_vault_needs")
            If credBox.Text <> credConfirmBox.Text Then Return L("shell_cred_mismatch")
        ElseIf verb = "pass" Then
            If credBox.Text = "" Then Return L("vd_cred_needed_old")
            If newCredBox.Text = "" Then Return L("vd_pass_empty_new")
            If newCredBox.Text <> newCredConfirmBox.Text Then Return L("shell_cred_mismatch")
        ElseIf DiskCommands.TakesCredential(verb) AndAlso Protection = DiskProtection.Encrypted AndAlso credBox.Text = "" Then
            Return L("vd_cred_needed")
        End If

        ' The typed word of a destructive answer.
        Dim word = ConfirmWord()
        If word <> "" AndAlso confirmBox.Text.Trim() <> word Then Return confirmLabel.Text
        Return ""
    End Function

    Private Shared Function FileExistsQuietly(path As String) As Boolean
        Try
            Return Not String.IsNullOrEmpty(path) AndAlso (File.Exists(path) OrElse Directory.Exists(path))
        Catch
            Return False
        End Try
    End Function

    ' ---- keeping the page in step ----------------------------------------

    Private Sub OnChanged()
        If suspendEvents Then Return
        Refresh_()
        RaiseEvent Changed()
    End Sub

    ' Every notice, every visibility that follows an answer rather than the verb.
    Private Sub Refresh_()
        factsLabel.Text = FactsText()
        factsLabel.Visible = (factsLabel.Text <> "")

        noteLabel.Text = L("vd_note_" & If(verb = "", "list", verb))
        If verb = "auto" Then noteLabel.Text = If(autoOffRadio.Checked, L("vd_auto_off_note"), L("vd_auto_on_note"))
        If verb = "add" Then noteLabel.Text = If(forgetRadio.Checked, L("vd_forget_note"), L("vd_remember_note"))
        If verb = "export" AndAlso exportVhd.Checked Then noteLabel.Text &= " " & L("vd_export_vhd_note")

        If profileRam.Checked Then
            profileNotice.Text = L("vd_profile_ram_note")
        ElseIf profileVault.Checked Then
            profileNotice.Text = L("vd_profile_vault_note")
        ElseIf profileFast.Checked Then
            profileNotice.Text = L("vd_profile_fast_note")
        Else
            profileNotice.Text = L("vd_profile_plain_note")
        End If

        nopassNotice.Visible = nopassCheck.Visible AndAlso nopassCheck.Checked
        nopassNotice.Text = L("vd_nopass_note")

        nameBox.Enabled = rememberRadio.Checked
        nameLabel.Enabled = rememberRadio.Checked

        credBlock.Visible = CredentialShown()
        credNotice.Text = CredentialNoticeText()
        credNotice.Visible = (credNotice.Text <> "")

        Dim word = ConfirmWord()
        confirmRow.Visible = (word <> "")
        Select Case verb
            Case "format" : confirmLabel.Text = L("vd_confirm_format")
            Case "destroy" : confirmLabel.Text = L("vd_confirm_destroy")
            Case Else : confirmLabel.Text = L("vd_confirm_nosave")
        End Select
        confirmBox.AccessibleName = confirmLabel.Text
        If word = "" Then confirmBox.Text = ""

        ApplyTheme()
    End Sub

    ' The honest line under the password (spec 7.2): what an empty one is, what a short one is
    ' worth, and that there is no recovery.
    Private Function CredentialNoticeText() As String
        Select Case verb
            Case "new"
                If credBox.Text = "" Then Return If(profileVault.Checked, L("vd_cred_vault_needs"), L("vd_cred_empty_new"))
                If credConfirmBox.Text <> credBox.Text Then Return L("shell_cred_mismatch")
                If credBox.Text.Length < ShortPassword Then Return L("shell_cred_short")
                Return L("vd_cred_ok_new")
            Case "pass"
                If newCredBox.Text = "" Then Return L("vd_pass_empty_new")
                If newCredConfirmBox.Text <> newCredBox.Text Then Return L("shell_cred_mismatch")
                If newCredBox.Text.Length < ShortPassword Then Return L("shell_cred_short")
                Return L("vd_pass_ok")
        End Select
        If credBox.Text <> "" Then Return ""
        Return If(Protection = DiskProtection.Encrypted, L("vd_cred_needed"), L("vd_cred_unknown"))
    End Function

    Private Function NoticeWarns() As Boolean
        Select Case credNotice.Text
            Case L("vd_cred_ok_new"), L("vd_pass_ok"), L("vd_cred_unknown") : Return False
        End Select
        Return True
    End Function

    Private Sub BrowseDest_Click(sender As Object, e As EventArgs)
        Using dlg As New SaveFileDialog()
            dlg.OverwritePrompt = False
            dlg.Title = L("vd_lbl_dest")
            If verb = "export" Then
                dlg.Filter = If(exportVhd.Checked, DiskCommands.FileFilter(L("vd_filter_vhd"), "*.vhd", L("vd_filter_all")), DiskCommands.FileFilter(L("vd_filter_img"), "*.img", L("vd_filter_all")))
            Else
                dlg.Filter = DiskCommands.FileFilter(L("vd_filter_fdd"), "*.fdd", L("vd_filter_all"))
            End If
            If dlg.ShowDialog(FindForm()) = DialogResult.OK Then destBox.Text = dlg.FileName
        End Using
    End Sub

    ' ---- theming ---------------------------------------------------------

    ' A font or a colour is set only when it differs from the one the control has: every assignment
    ' re-lays out the panel's nested rows, and this runs on each keystroke.
    Private Shared Sub SetFont(c As Control, f As Font)
        If Not c.Font.Equals(f) Then c.Font = f
    End Sub

    Private Shared Sub SetColours(c As Control, fore As Color)
        If c.ForeColor <> fore Then c.ForeColor = fore
    End Sub

    Private Shared Sub SetColours(c As Control, fore As Color, back As Color)
        SetColours(c, fore)
        If c.BackColor <> back Then c.BackColor = back
    End Sub

    Public Sub ApplyTheme()
        Dim p = Theme.Current
        Dim body = Theme.FontBody()
        Dim caption = Theme.FontCaption()
        Dim strong = Theme.FontBodyStrong()
        SuspendLayout()
        Try
            SetColours(Me, p.Text, p.Surface)

            For Each c As Control In AllChildren(Me)
                If TypeOf c Is Label Then
                    SetFont(c, body)
                    SetColours(c, p.Text)
                ElseIf TypeOf c Is CheckBox OrElse TypeOf c Is RadioButton Then
                    SetFont(c, body)
                    SetColours(c, p.Text, p.Surface)
                ElseIf TypeOf c Is TextBox Then
                    SetFont(c, body)
                    SetColours(c, p.Text, p.Surface)
                    Dim tb = DirectCast(c, TextBox)
                    If tb.BorderStyle <> BorderStyle.FixedSingle Then tb.BorderStyle = BorderStyle.FixedSingle
                ElseIf TypeOf c Is ComboBox Then
                    SetFont(c, body)
                    SetColours(c, p.Text, p.Surface)
                    Dim cb = DirectCast(c, ComboBox)
                    If cb.FlatStyle <> FlatStyle.Flat Then cb.FlatStyle = FlatStyle.Flat
                End If
            Next

            For Each lb In New Label() {noteLabel, elevationLabel, profileNotice, credHint}
                SetFont(lb, caption)
                SetColours(lb, p.MutedText)
            Next
            SetFont(factsLabel, strong)
            SetColours(factsLabel, If(FactsWarn(), p.Warning, p.Text))
            SetFont(nopassNotice, strong)
            SetColours(nopassNotice, p.Warning)
            If profileRam.Checked OrElse profileVault.Checked Then SetColours(profileNotice, p.Warning)
            SetFont(credNotice, strong)
            SetColours(credNotice, If(NoticeWarns(), p.Warning, p.MutedText))

            ' The answers that lose data say so in the danger colour while they are on.
            For Each cb In New CheckBox() {nosaveCheck, wipeCheck, forceCheck}
                SetColours(cb, If(cb.Checked, p.Danger, p.Text))
            Next
            SetFont(confirmLabel, strong)
            SetColours(confirmLabel, p.Danger)

            Dim sizeText = sizeBox.Text.Trim()
            If sizeText <> "" AndAlso Not DiskCommands.IsSize(sizeText) Then SetColours(sizeBox, p.Danger)

            For Each b In buttons
                SetFont(b, body)
                Ui.StyleButton(b, p.SurfaceAlt, p.Text, p.Border)
            Next
        Finally
            ResumeLayout(True)
        End Try
    End Sub

    Private Iterator Function AllChildren(root As Control) As IEnumerable(Of Control)
        For Each c As Control In root.Controls
            Yield c
            For Each inner In AllChildren(c)
                Yield inner
            Next
        Next
    End Function

    ' ---- seams for SelfTest.vb -------------------------------------------

    ' Fills the page's answers from a DiskOptions, as a user would click them; the passwords are
    ' typed into their boxes (both boxes of a pair get the same text).
    Friend Sub SetForTest(o As DiskOptions, password As String, newPassword As String, typed As String)
        suspendEvents = True
        sizeBox.Text = o.Size
        profilePlain.Checked = (o.Profile = "plain")
        profileFast.Checked = (o.Profile = "fast")
        profileRam.Checked = (o.Profile = "ram")
        profileVault.Checked = (o.Profile = "vault")
        volLabelBox.Text = o.Label
        fsCombo.SelectedIndex = If(o.FileSystem = "exfat", 1, 0)
        roCheck.Checked = o.ReadOnly
        noscanCheck.Checked = o.NoScan
        If o.Letter <> "" Then
            If Not letterCombo.Items.Contains(o.Letter) Then letterCombo.Items.Add(o.Letter)
            letterCombo.SelectedItem = o.Letter
        ElseIf letterCombo.Items.Count > 0 Then
            letterCombo.SelectedIndex = 0
        End If
        forceCheck.Checked = o.Force
        nosaveCheck.Checked = o.NoSave
        wipeCheck.Checked = o.Wipe
        nopassCheck.Checked = o.NoPass
        exportVhd.Checked = (o.ExportForm = "vhd")
        exportRaw.Checked = (o.ExportForm <> "vhd")
        destBox.Text = o.Dest
        autoOnRadio.Checked = o.AutoOn
        autoOffRadio.Checked = Not o.AutoOn
        rememberRadio.Checked = o.Remember
        forgetRadio.Checked = Not o.Remember
        nameBox.Text = o.Name
        statusRadio.Checked = o.ShowMounted
        listRadio.Checked = Not o.ShowMounted
        credBox.Text = If(password, "")
        credConfirmBox.Text = If(password, "")
        newCredBox.Text = If(newPassword, "")
        newCredConfirmBox.Text = If(newPassword, "")
        confirmBox.Text = If(typed, "")
        suspendEvents = False
        Refresh_()
        RaiseEvent Changed()
    End Sub

    Friend Sub SetNewConfirmForTest(text As String)
        newCredConfirmBox.Text = text
    End Sub

    Friend ReadOnly Property ProfileNoticeForTest As String
        Get
            Return profileNotice.Text
        End Get
    End Property

    Friend ReadOnly Property CredentialShownForTest As Boolean
        Get
            Return CredentialShown()
        End Get
    End Property

    Friend ReadOnly Property ReadOnlyCheckedForTest As Boolean
        Get
            Return roCheck.Checked
        End Get
    End Property

End Class

' What a Disks run leaves on its result card: the list as a table (spec 7.1), the drive a mount
' just attached, one click to open it, and the rows' own next steps - unmount, turn auto-mount off -
' which open the page that does them rather than doing them behind the user's back.
'
' The list is the snapshot of `vd status json` (SP-0063 8.1), read by DiskSnapshot: the registered
' containers, or what is mounted, are two views of that one document - no line of human text is
' parsed for a column. "Open in Disk manager" takes the same rows to the window that keeps them true.
Public Class DiskResultPanel
    Inherits TableLayoutPanel

    ' A page to open, on a target, with a preset ("off" for auto-mount).
    Public Event JobRequested(key As String, target As String, preset As String)
    ' The Disk Manager, asked for from under the table.
    Public Event ManagerRequested()

    Private ReadOnly dict As Dictionary(Of String, String)
    Private messageLabel As Label
    Private table As ListView
    Private buttonRow As FlowLayoutPanel
    Private openDriveBtn As Button
    Private unmountBtn As Button
    Private autoOffBtn As Button
    Private managerBtn As Button
    Private ReadOnly buttons As New List(Of Button)
    Private rows As New List(Of DiskRecord)
    Private mountedLetter As String = ""
    Private showingList As Boolean = False

    Public Sub New()
        dict = Localization.GetDict(ShellSettings.Language())
        Dock = DockStyle.Top
        AutoSize = True
        AutoSizeMode = AutoSizeMode.GrowAndShrink
        ColumnCount = 1
        RowCount = 3
        Margin = New Padding(0)
        ColumnStyles.Add(New ColumnStyle(SizeType.Percent, 100.0F))
        Visible = False

        messageLabel = New Label With {.AutoSize = True, .Margin = Ui.PxPad(Me, 0, 0, 0, 6), .Visible = False}
        table = New ListView With {
            .View = View.Details,
            .FullRowSelect = True,
            .HideSelection = False,
            .MultiSelect = False,
            .Anchor = AnchorStyles.Left Or AnchorStyles.Right,
            .Height = Ui.Px(Me, 180),
            .Margin = Ui.PxPad(Me, 0, 0, 0, 8),
            .Visible = False
        }
        table.AccessibleName = L("vd_table_name")
        AddHandler table.SelectedIndexChanged, Sub() UpdateButtons()

        buttonRow = New FlowLayoutPanel With {.AutoSize = True, .AutoSizeMode = AutoSizeMode.GrowAndShrink, .WrapContents = True, .Dock = DockStyle.Top, .Margin = Ui.PxPad(Me, 0, 0, 0, 8)}
        openDriveBtn = NewButton("vd_btn_open_drive")
        AddHandler openDriveBtn.Click, AddressOf OpenDrive_Click
        unmountBtn = NewButton("vd_btn_unmount_row")
        AddHandler unmountBtn.Click, Sub() RequestForSelected("rail_job_vd_unmount", "")
        autoOffBtn = NewButton("vd_btn_auto_off_row")
        AddHandler autoOffBtn.Click, Sub() RequestForSelected("rail_job_vd_auto", "off")
        managerBtn = NewButton("vd_btn_open_manager")
        AddHandler managerBtn.Click, Sub() RaiseEvent ManagerRequested()
        buttonRow.Controls.Add(openDriveBtn)
        buttonRow.Controls.Add(unmountBtn)
        buttonRow.Controls.Add(autoOffBtn)
        buttonRow.Controls.Add(managerBtn)

        Controls.Add(messageLabel, 0, 0)
        Controls.Add(table, 0, 1)
        Controls.Add(buttonRow, 0, 2)
    End Sub

    Private Function L(key As String) As String
        Dim v As String = Nothing
        If dict IsNot Nothing AndAlso dict.TryGetValue(key, v) Then Return v
        Return key
    End Function

    Private Function NewButton(key As String) As Button
        Dim b As New Button With {.Text = L(key), .AutoSize = True, .AutoSizeMode = AutoSizeMode.GrowAndShrink, .Margin = Ui.PxPad(Me, 0, 0, 8, 0), .Visible = False}
        buttons.Add(b)
        Return b
    End Function

    Public Sub WrapIn(card As Control, reserve As Integer)
        Ui.Wrap(messageLabel, card, reserve)
    End Sub

    Public Sub Clear()
        Visible = False
        rows.Clear()
        table.Items.Clear()
        table.Visible = False
        messageLabel.Visible = False
        mountedLetter = ""
        showingList = False
        UpdateButtons()
    End Sub

    ' Fills the card after a Disks run: the table for the list, the drive for a mount.
    Public Sub ShowResult(verb As String, showMounted As Boolean, res As Runner.RunResult)
        Clear()
        If res Is Nothing Then Return
        Dim output = If(res.Output, "")
        If verb = "list" AndAlso res.ExitCode = 0 Then
            showingList = True
            Dim problem As String = ""
            Dim snap = DiskSnapshot.Parse(output, problem)
            If snap IsNot Nothing Then
                rows = snap.Disks.Where(Function(d) If(showMounted, d.IsMounted, d.Registered)).ToList()
                rows.Sort(AddressOf DiskStates.DefaultOrder)
            End If
            FillTable(showMounted)
            Dim message = ""
            If snap Is Nothing Then
                message = L(problem)
            ElseIf rows.Count = 0 Then
                message = L(If(showMounted, "vd_list_none_mounted", "vd_list_none_registered"))
            End If
            messageLabel.Text = message
            messageLabel.Visible = (message <> "")
            table.Visible = (rows.Count > 0)
            Visible = True
        ElseIf verb = "mount" AndAlso res.ExitCode = 0 Then
            mountedLetter = DiskCommands.MountedLetterIn(output)
            Visible = (mountedLetter <> "")
        End If
        UpdateButtons()
    End Sub

    Private Sub FillTable(showMounted As Boolean)
        table.BeginUpdate()
        table.Columns.Clear()
        table.Items.Clear()
        If showMounted Then
            table.Columns.Add(L("vd_col_drive"), Ui.Px(Me, 60))
            table.Columns.Add(L("vd_col_file"), Ui.Px(Me, 320))
            table.Columns.Add(L("vd_col_state"), Ui.Px(Me, 360))
            For Each r In rows
                Dim it As New ListViewItem(r.Letter)
                it.SubItems.Add(r.Path)
                it.SubItems.Add(StateText(r))
                table.Items.Add(it)
            Next
        Else
            table.Columns.Add(L("vd_col_name"), Ui.Px(Me, 120))
            table.Columns.Add(L("vd_col_profile"), Ui.Px(Me, 70))
            table.Columns.Add(L("vd_col_size"), Ui.Px(Me, 80))
            table.Columns.Add(L("vd_col_file"), Ui.Px(Me, 280))
            table.Columns.Add(L("vd_col_state"), Ui.Px(Me, 240))
            For Each r In rows
                Dim it As New ListViewItem(r.Name)
                it.SubItems.Add(r.Profile)
                it.SubItems.Add(If(r.LogicalSize > 0, DiskStates.SizeText(r.LogicalSize), "-"))
                it.SubItems.Add(r.Path)
                it.SubItems.Add(StateText(r))
                table.Items.Add(it)
            Next
        End If
        table.EndUpdate()
    End Sub

    ' The row's state in the Disk Manager's words, with the letter and the auto-mount task beside it.
    Private Function StateText(r As DiskRecord) As String
        Dim text = DiskStates.StateText(r, DiskStates.StateOf(r, ""), "", dict)
        If r.IsMounted Then text = r.Letter & "  " & text
        If r.AutoMount Then text &= "; " & L("vd_list_auto_on")
        Return text
    End Function

    Private Function SelectedRow() As DiskRecord
        If table.SelectedIndices.Count = 0 Then Return Nothing
        Dim i = table.SelectedIndices(0)
        Return If(i >= 0 AndAlso i < rows.Count, rows(i), Nothing)
    End Function

    Private Sub UpdateButtons()
        Dim r = SelectedRow()
        If showingList Then
            openDriveBtn.Text = L("vd_btn_open_drive")
            openDriveBtn.Visible = True
            openDriveBtn.Enabled = (r IsNot Nothing AndAlso r.Letter <> "")
            unmountBtn.Visible = True
            unmountBtn.Enabled = (r IsNot Nothing AndAlso r.Letter <> "")
            autoOffBtn.Visible = True
            autoOffBtn.Enabled = (r IsNot Nothing AndAlso r.AutoMount AndAlso r.Path <> "")
            managerBtn.Visible = True
        Else
            openDriveBtn.Visible = (mountedLetter <> "")
            openDriveBtn.Enabled = True
            openDriveBtn.Text = Localization.Format(L("vd_btn_open_drive_fmt"), mountedLetter)
            unmountBtn.Visible = False
            autoOffBtn.Visible = False
            managerBtn.Visible = False
        End If
    End Sub

    Private Sub OpenDrive_Click(sender As Object, e As EventArgs)
        Dim letter = mountedLetter
        If showingList Then
            Dim r = SelectedRow()
            If r Is Nothing Then Return
            letter = r.Letter
        End If
        If letter = "" Then Return
        Ui.OpenFolder(ShellDialog.OwnerOf(Me), letter & "\")
    End Sub

    ' Unmount by the drive letter where there is one: it is what the listing is sure of.
    Private Sub RequestForSelected(key As String, preset As String)
        Dim r = SelectedRow()
        If r Is Nothing Then Return
        Dim target = If(key = "rail_job_vd_unmount" AndAlso r.Letter <> "", r.Letter, r.Path)
        RaiseEvent JobRequested(key, target, preset)
    End Sub

    Public Sub ApplyTheme()
        Dim p = Theme.Current
        BackColor = p.Surface
        messageLabel.Font = Theme.FontBody()
        messageLabel.ForeColor = p.Text
        table.Font = Theme.FontBody()
        table.BackColor = p.Surface
        table.ForeColor = p.Text
        table.BorderStyle = BorderStyle.FixedSingle
        For Each b In buttons
            b.Font = Theme.FontBody()
            Ui.StyleButton(b, p.SurfaceAlt, p.Text, p.Border)
        Next
    End Sub

    ' ---- seams for SelfTest.vb -------------------------------------------

    Friend ReadOnly Property RowCountForTest As Integer
        Get
            Return rows.Count
        End Get
    End Property

    Friend ReadOnly Property MessageForTest As String
        Get
            Return If(messageLabel.Visible, messageLabel.Text, "")
        End Get
    End Property

    Friend ReadOnly Property OpenDriveTextForTest As String
        Get
            Return If(openDriveBtn.Visible, openDriveBtn.Text, "")
        End Get
    End Property

End Class
