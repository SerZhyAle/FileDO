Imports System.IO

' Step 3 of the Backup and Restore pages (SP-0198): every option word `filedo backup` and
' `filedo restore` take, as one panel (cmd\filedo\backup_cmd.go, parseBackupArgs).
'
' The password is not here: it is the page's own credential block, the one the Protect pages use,
' because it is the one place a secret is handled and it must stay one (SP-0005 12). This panel only
' says whether the page needs it - a sealed backup, or a restore of a .fd-sec.
'
' Three answers a console would give are given on the page instead, because a run from the window
' has no console to answer on:
'
'   - A drive or share root is packed only with `root`, and the CLI asks before it packs one. The
'     page asks the same thing with its own checkbox, shown only when the source is a root, and the
'     run carries `y` for it - the prompt is skipped, never the checks (hard invariant 9).
'   - A restore into a folder that exists is suffix-or-cancel (SP-0198 D7). The console asks; the
'     page answers with `suffix`, on by default, so the window never ends in a question nobody saw.
'   - A restore that deletes its archive asks first on the console; the page asks with its checkbox
'     and carries `y`.
Public Class BackupOptionsPanel
    Inherits FlowLayoutPanel

    Public Event Changed()

    ' The density tokens in the order of the combo: "" lets the probe choose (SP-0198 3.4).
    Private Shared ReadOnly LevelTokens As String() = {"", "0", "1", "2", "3", "4", "5", "6", "7", "8", "9"}

    Private ReadOnly dict As Dictionary(Of String, String)
    Private verb As String = ""
    Private targetIsRoot As Boolean = False
    Private suspendEvents As Boolean = False

    Private noteLabel As Label
    Private levelLabel As Label
    Private levelCombo As ComboBox
    Private sealCheck As CheckBox
    Private keepZipCheck As CheckBox
    Private skipLinksCheck As CheckBox
    Private rootCheck As CheckBox
    Private rootNote As Label
    Private suffixCheck As CheckBox
    Private delCheck As CheckBox

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

    Private Sub BuildContent()
        noteLabel = New Label With {.AutoSize = True, .Margin = Ui.PxPad(Me, 0, 0, 0, 6)}

        levelLabel = New Label With {.Text = L("shell_bk_level"), .AutoSize = True, .Margin = Ui.PxPad(Me, 0, 4, 0, 2)}
        levelCombo = New ComboBox With {.DropDownStyle = ComboBoxStyle.DropDownList, .Width = Ui.Px(Me, 320), .Margin = Ui.PxPad(Me, 0, 0, 0, 4)}
        For Each token In LevelTokens
            levelCombo.Items.Add(LevelCaption(token))
        Next
        levelCombo.SelectedIndex = 0
        levelCombo.AccessibleName = L("shell_bk_level")
        AddHandler levelCombo.SelectedIndexChanged, Sub() Raise()

        sealCheck = NewCheck("shell_bk_sec")
        keepZipCheck = NewCheck("shell_bk_keep_zip")
        keepZipCheck.Margin = Ui.PxPad(Me, 18, 2, 0, 2)
        skipLinksCheck = NewCheck("shell_bk_skip_links")
        rootCheck = NewCheck("shell_bk_root")
        rootNote = New Label With {.Text = L("shell_bk_root_note"), .AutoSize = True, .Margin = Ui.PxPad(Me, 18, 0, 0, 4)}

        suffixCheck = NewCheck("shell_rs_suffix")
        delCheck = NewCheck("shell_rs_del")

        Controls.Add(noteLabel)
        Controls.Add(levelLabel)
        Controls.Add(levelCombo)
        Controls.Add(sealCheck)
        Controls.Add(keepZipCheck)
        Controls.Add(skipLinksCheck)
        Controls.Add(rootCheck)
        Controls.Add(rootNote)
        Controls.Add(suffixCheck)
        Controls.Add(delCheck)
    End Sub

    Private Function LevelCaption(token As String) As String
        Select Case token
            Case "" : Return L("shell_bk_level_auto")
            Case "0", "1", "6", "9" : Return L("shell_bk_level_" & token)
            Case Else : Return token
        End Select
    End Function

    Private Function NewCheck(key As String) As CheckBox
        Dim c As New BooleanOption With {.Text = L(key), .AutoSize = True, .Margin = Ui.PxPad(Me, 0, 2, 0, 2)}
        c.AccessibleName = L(key)
        AddHandler c.CheckedChanged, Sub() Raise()
        Return c
    End Function

    Private Sub Raise()
        If suspendEvents Then Return
        ApplyExclusions()
        RaiseEvent Changed()
    End Sub

    ' ---- the page's questions --------------------------------------------

    ' Shows the controls of this verb, and nothing else.
    Public Sub Configure(newVerb As String)
        verb = newVerb
        Dim backup = (verb = "backup")
        noteLabel.Text = If(backup, L("shell_bk_note"), L("shell_rs_note"))
        levelLabel.Visible = backup
        levelCombo.Visible = backup
        sealCheck.Visible = backup
        keepZipCheck.Visible = backup
        skipLinksCheck.Visible = backup
        suffixCheck.Visible = Not backup
        delCheck.Visible = Not backup
        ApplyRootShape()
        ApplyExclusions()
    End Sub

    ' Step 2 changed: a drive or share root is a different question (the `root` word, its own consent).
    Public Sub SetTarget(target As String)
        Dim wasRoot = targetIsRoot
        targetIsRoot = (verb = "backup") AndAlso IsRootPath(target)
        If wasRoot AndAlso Not targetIsRoot Then
            suspendEvents = True
            rootCheck.Checked = False
            suspendEvents = False
        End If
        ApplyRootShape()
    End Sub

    Private Sub ApplyRootShape()
        rootCheck.Visible = (verb = "backup") AndAlso targetIsRoot
        rootNote.Visible = rootCheck.Visible
    End Sub

    ' keep-zip needs sec; a delete after the restore needs `y`, which the CLI also reads as "restore
    ' beside an existing folder" - so the page shows suffix as on while delete is chosen.
    Private Sub ApplyExclusions()
        keepZipCheck.Enabled = sealCheck.Checked
        If Not sealCheck.Checked AndAlso keepZipCheck.Checked Then
            suspendEvents = True
            keepZipCheck.Checked = False
            suspendEvents = False
        End If
        If delCheck.Checked AndAlso Not suffixCheck.Checked Then
            suspendEvents = True
            suffixCheck.Checked = True
            suspendEvents = False
        End If
        suffixCheck.Enabled = Not delCheck.Checked
    End Sub

    Public Sub Reset()
        suspendEvents = True
        levelCombo.SelectedIndex = 0
        sealCheck.Checked = False
        keepZipCheck.Checked = False
        skipLinksCheck.Checked = False
        rootCheck.Checked = False
        suffixCheck.Checked = True
        delCheck.Checked = False
        targetIsRoot = False
        suspendEvents = False
        ApplyRootShape()
        ApplyExclusions()
    End Sub

    ' True for a sealed backup: the page shows the password twice.
    Public ReadOnly Property Sealed As Boolean
        Get
            Return verb = "backup" AndAlso sealCheck.Checked
        End Get
    End Property

    ' A restore that deletes its archive afterwards: a normal delete, so partly reversible, and the
    ' page carries the danger accent while it is chosen (the Protect pages' rule for dispositions).
    Public Function IsDestructiveNow() As Boolean
        Return verb = "restore" AndAlso delCheck.Checked
    End Function

    ' Why Run is not offered, as a string key, or "" when it is. Each is a refusal the CLI would give
    ' after the start; the page gives it before.
    Public Function BlockReasonKey(target As String, destination As String) As String
        If target = "" Then Return ""
        If verb = "restore" Then
            If Not (target.EndsWith(".zip", StringComparison.OrdinalIgnoreCase) OrElse
                    target.EndsWith(".fd-sec", StringComparison.OrdinalIgnoreCase)) Then Return "shell_rs_need_archive"
            Return ""
        End If
        If Not targetIsRoot Then Return ""
        If TargetPath.IsSystemVolumeRoot(target) Then Return "shell_bk_system_drive"
        If Not rootCheck.Checked Then Return "shell_bk_need_root"
        ' Without `to` a root's backup goes into the current folder, and filedo.exe started from this
        ' window runs in its own data folder - nobody would find the zip there.
        If destination = "" Then Return "shell_bk_root_needs_dest"
        Return ""
    End Function

    ' The option words, in the order the CLI documents them.
    Public Function ToArgs() As List(Of String)
        Dim args As New List(Of String)()
        If verb = "backup" Then
            Dim level = LevelTokens(Math.Max(levelCombo.SelectedIndex, 0))
            If level <> "" Then
                args.Add("level")
                args.Add(level)
            End If
            If sealCheck.Checked Then args.Add("sec")
            If sealCheck.Checked AndAlso keepZipCheck.Checked Then args.Add("keep-zip")
            If targetIsRoot AndAlso rootCheck.Checked Then
                args.Add("root")
                ' The consent the console asks after the estimate was given on the page.
                args.Add("y")
            End If
            If skipLinksCheck.Checked Then args.Add("skip-links")
        ElseIf verb = "restore" Then
            If suffixCheck.Checked Then args.Add("suffix")
            If delCheck.Checked Then
                args.Add("del")
                args.Add("y")
            End If
        End If
        Return args
    End Function

    ' A volume or share root by spelling: what the path is reduced to by Path.GetPathRoot. The CLI
    ' also knows a mounted folder by identity; that case it refuses with the reason after the start.
    Friend Shared Function IsRootPath(path As String) As Boolean
        If String.IsNullOrWhiteSpace(path) Then Return False
        Try
            Dim p = path.Trim()
            If p.Length = 2 AndAlso p(1) = ":"c Then Return True
            Dim root = IO.Path.GetPathRoot(p)
            If String.IsNullOrEmpty(root) Then Return False
            Return String.Equals(root.TrimEnd("\"c, "/"c), p.TrimEnd("\"c, "/"c), StringComparison.OrdinalIgnoreCase)
        Catch
            Return False
        End Try
    End Function

    Friend Sub SetForTest(sealed As Boolean, keepZip As Boolean, level As String, root As Boolean, skipLinks As Boolean,
                          suffix As Boolean, del As Boolean)
        suspendEvents = True
        sealCheck.Checked = sealed
        keepZipCheck.Checked = keepZip
        levelCombo.SelectedIndex = Math.Max(Array.IndexOf(LevelTokens, level), 0)
        rootCheck.Checked = root
        skipLinksCheck.Checked = skipLinks
        suffixCheck.Checked = suffix
        delCheck.Checked = del
        suspendEvents = False
        Raise()
    End Sub

    Public Sub WrapIn(card As Control, reserve As Integer)
        Ui.Wrap(noteLabel, card, reserve)
        Ui.Wrap(rootNote, card, reserve)
    End Sub

    Public Sub ApplyTheme()
        Dim p = Theme.Current
        BackColor = p.Surface
        ForeColor = p.Text
        noteLabel.Font = Theme.FontCaption()
        noteLabel.ForeColor = p.MutedText
        rootNote.Font = Theme.FontCaption()
        rootNote.ForeColor = p.Warning
        levelLabel.Font = Theme.FontBody()
        levelLabel.ForeColor = p.Text
        levelCombo.Font = Theme.FontBody()
        levelCombo.BackColor = p.Surface
        levelCombo.ForeColor = p.Text
        levelCombo.FlatStyle = FlatStyle.Flat
        For Each c As CheckBox In New CheckBox() {sealCheck, keepZipCheck, skipLinksCheck, rootCheck, suffixCheck, delCheck}
            c.Font = Theme.FontBody()
            c.BackColor = p.Surface
            c.ForeColor = p.Text
        Next
        delCheck.ForeColor = If(delCheck.Checked, p.Warning, p.Text)
        rootCheck.ForeColor = If(rootCheck.Checked, p.Warning, p.Text)
    End Sub

End Class
