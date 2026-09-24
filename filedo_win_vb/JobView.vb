Imports System.IO
Imports System.Threading
Imports System.Threading.Tasks

' One job, one page, top to bottom (SP-0006 section 5.1), and the page is numbered.
'
' The owner's flow is three questions in a fixed order: what to do, what to do it to, and with
' which parameters. Step 1 is the rail, so it is answered before this control is even shown; the
' page therefore opens at step 2 and carries the numbers in its headings, so that a person who has
' never seen the program knows where they are and what is still unanswered. The plan card (step 4)
' is where the three answers are read back in one sentence before anything is touched - which is
' principle 1 of the specification and the reason the numbers stop at "check and run" rather than
' at "run".
'
' Everything below the plan card is section 6: the run strip during, the result card after, and
' the raw output one toggle away and never in the way.
Public Class JobView
    Inherits UserControl

    Public Event OpenInCommandRequested(command As String)

    ' Raised on the UI thread when a run has ended and its result card is filled in. The window
    ' listens, because a close the user asked for while the run was active waits for this.
    Public Event RunFinished()

    Private ReadOnly runner As New Runner()
    Private job As JobDefinition

    ' A result that arrived while another view was in front. Choosing this job's row again shows it
    ' instead of resetting the page (APP-BEHAVIOUR rule 3: the outcome of a run is not lost).
    Private resultUnseen As Boolean = False

    ' What the wipe target's count found (T10): -1 until a count has finished.
    Private countedFiles As Long = -1
    Private countedFolders As Long = -1
    Private ReadOnly dict As Dictionary(Of String, String)
    Private ReadOnly tips As New ToolTip()

    Private rootPanel As TableLayoutPanel
    Private targetCard As ShellCard
    Private paramsCard As ShellCard
    Private planCard As ShellCard
    Private runStrip As ShellCard
    Private resultCard As ShellCard
    Private rawOutputDrawer As ShellCard

    ' Step 2 - what to work on
    Private targetHeader As Label
    Private targetLabel As Label
    Private targetCombo As ComboBox
    Private targetBrowseBtn As Button
    Private dropHintLabel As Label
    Private secondTargetLabel As Label
    Private secondTargetBox As TextBox
    Private secondTargetBrowseBtn As Button
    Private secondTargetRow As TableLayoutPanel

    ' Step 3 - parameters
    Private paramsHeader As Label
    Private presetRow As FlowLayoutPanel
    Private presetQuick As RadioButton
    Private presetThorough As RadioButton
    ' The third answer to "how much": the CLI takes any number of megabytes and any number of
    ' files, and a page with two presets and nothing else could only ask for two of them.
    Private presetCustom As RadioButton
    Private sizeRow As FlowLayoutPanel
    Private sizeLabel As Label
    Private sizeBox As TextBox
    Private optionsCheckAutoDel As CheckBox
    Private optionsCheckForce As CheckBox
    Private optionsCheckNoDel As CheckBox
    Private optionsCheckShort As CheckBox
    Private optionsCheckVerify As CheckBox
    Private optionsCheckNoHist As CheckBox
    Private optionsCheckHere As CheckBox
    Private optionsCheckProbeFix As CheckBox
    Private optionsCheckForceFormat As CheckBox

    ' The rest of step 3, one block per job that has one: the duplicate rule, the compare rule,
    ' the copy strategy and the whole option surface of `check`.
    Private extrasFlow As FlowLayoutPanel
    Private dupRow As FlowLayoutPanel
    Private dupRuleLabel As Label
    Private dupRuleCombo As ComboBox
    Private dupActionLabel As Label
    Private dupActionReport As RadioButton
    Private dupActionDelete As RadioButton
    Private dupActionMove As RadioButton
    Private dupMoveBox As TextBox
    Private dupMoveBrowseBtn As Button
    Private dupListCheck As CheckBox
    Private dupListBox As TextBox
    Private dupQuietCheck As CheckBox
    Private dupRuleNotice As Label
    Private cmpRow As FlowLayoutPanel
    Private cmpRuleLabel As Label
    Private cmpRuleCombo As ComboBox
    Private cmpRuleNotice As Label
    Private copyRow As FlowLayoutPanel
    Private copyStrategyLabel As Label
    Private copyStrategyCombo As ComboBox
    Private copyStrategyNotice As Label
    Private checkOptions As CheckOptionsPanel

    ' What step 3 holds for the job on screen. These are fields rather than the controls' own
    ' Visible property for the reason UpdateParamsVisibility already documents: a control reports
    ' Visible = False while any parent of it is hidden, and this page is built before it is shown.
    Private showsPresets As Boolean = False
    Private showsSize As Boolean = False
    Private showsDup As Boolean = False
    Private showsCompare As Boolean = False
    Private showsCopy As Boolean = False
    Private showsCheck As Boolean = False
    Private optionChecksShown As Boolean = False
    Private wipeConfirmBox As TextBox
    Private wipeConfirmLabel As Label
    Private wipeConfirmRow As FlowLayoutPanel
    Private blastRadiusLabel As Label
    Private blastRadiusSkipBtn As Button
    Private blastRadiusRow As FlowLayoutPanel
    Private countingTokenSource As CancellationTokenSource

    ' Step 3 for the secret files (SP-0005 S6) - the credential and the
    ' dispositions. They live in the same parameters card as everything else,
    ' because they are parameters; what makes them their own block of fields
    ' is that one of them is a secret and must never leave this window by any
    ' route the rest of the page uses.
    Private credRow As TableLayoutPanel
    Private credLabel As Label
    Private credBox As TextBox
    Private credConfirmLabel As Label
    Private credConfirmBox As TextBox
    Private credShowCheck As CheckBox
    Private credNoticeLabel As Label
    Private credHintLabel As Label
    Private fdsecRow As FlowLayoutPanel
    Private fdsecOriginalLabel As Label
    Private fdsecKeepRadio As RadioButton
    Private fdsecDelRadio As RadioButton
    Private fdsecWipeRadio As RadioButton
    Private fdsecRenameCheck As CheckBox
    Private fdsecDelContainerCheck As CheckBox
    Private fdsecKeepCopyCheck As CheckBox
    Private fdsecRwCheck As CheckBox
    Private fdsecNoticeLabel As Label

    ' Step 4 - the plan card
    Private planHeader As Label
    Private intentLabel As Label
    Private touchesLabel As Label
    Private reversibilityBadge As Label
    Private destructiveBadge As Label
    Private elevationBadge As Label
    Private redirectNoticeLabel As Label
    ' Why Run is not offered on the Wipe page when that is not obvious from the fields: the folder is
    ' empty, the location needs the console, or -y is not ticked (T10, T11).
    Private runBlockedLabel As Label
    Private commandLabel As Label
    Private commandBox As TextBox
    Private copyCmdBtn As Button
    Private openInCmdBtn As Button
    Private startBtn As Button

    ' The run strip
    Private stateBadge As Label
    Private stepLabel As Label
    Private progressBar As ProgressBar
    Private throughputLabel As Label
    Private timeLabel As Label
    Private stopBtn As Button
    Private toggleOutputBtn As Button
    Private runTimer As Windows.Forms.Timer

    ' The result card
    Private verdictBadge As Label
    Private verdictGlyph As Label
    Private shownVerdict As String = ""
    Private verdictReasonLabel As Label
    Private resultNumbersLabel As Label
    Private filesLeftLabel As Label
    Private reportsLabel As Label
    Private actionCleanBtn As Button
    Private actionOpenReportBtn As Button
    Private actionRunAgainBtn As Button

    ' The raw output
    Private rawOutputBox As TextBox
    Private copyOutputBtn As Button

    Private ReadOnly secondaryButtons As New List(Of Button)

    Public Sub New()
        dict = Localization.GetDict(ShellSettings.Language())
        DoubleBuffered = True
        Dock = DockStyle.Fill
        AutoScroll = True
        AllowDrop = True
        BuildLayout()
        HookRunnerEvents()
        AddHandler DragEnter, AddressOf JobView_DragEnter
        AddHandler DragDrop, AddressOf JobView_DragDrop
    End Sub

    Private Function L(key As String) As String
        Dim v As String = Nothing
        If dict IsNot Nothing AndAlso dict.TryGetValue(key, v) Then Return v
        Return key
    End Function

    Public Sub SetJob(jobDef As JobDefinition)
        ' The page never changes job under a running run: the window keeps the running page in
        ' front (ShellForm), and this is the backstop if anything else ever asks.
        If runner.IsActive Then Return
        Me.job = jobDef
        ResetView()
        PopulateTargets()
        UpdatePlanCard()
        ApplyTheme()
    End Sub

    ' ---- the run, as the window sees it -----------------------------------

    Public ReadOnly Property IsRunning As Boolean
        Get
            Return runner.IsActive
        End Get
    End Property

    ' The rail key of the job on this page, or "" when none is.
    Public ReadOnly Property CurrentJobId As String
        Get
            Return If(job Is Nothing, "", job.Id)
        End Get
    End Property

    Public ReadOnly Property HoldsRunOrUnseenResult As Boolean
        Get
            Return runner.IsActive OrElse resultUnseen
        End Get
    End Property

    Public Sub MarkResultSeen()
        resultUnseen = False
    End Sub

    ' The window's "Stop and close": the same request the Stop button makes.
    Public Sub RequestStopFromShell()
        If Not runner.IsActive Then Return
        StopBtn_Click(Me, EventArgs.Empty)
    End Sub

    ' The last resort of a close whose stop was not honoured: the process is ended outright.
    Public Sub ForceEnd()
        If runner.IsActive Then runner.ForceKill()
    End Sub

    Private Sub ResetView()
        If countingTokenSource IsNot Nothing Then
            countingTokenSource.Cancel()
            countingTokenSource = Nothing
        End If
        resultUnseen = False
        countedFiles = -1
        countedFolders = -1

        targetCard.Visible = True
        paramsCard.Visible = True
        planCard.Visible = True
        runStrip.Visible = False
        resultCard.Visible = False
        rawOutputDrawer.Visible = False

        wipeConfirmBox.Text = ""
        wipeConfirmRow.Visible = (job IsNot Nothing AndAlso job.Id = "rail_job_wipe")

        ' The credential does not survive a change of page: a password left in
        ' a box that is no longer the one in front of the user is a password
        ' that gets used by accident.
        credBox.Text = ""
        credConfirmBox.Text = ""
        credShowCheck.Checked = False
        fdsecKeepRadio.Checked = True
        fdsecRenameCheck.Checked = False
        fdsecDelContainerCheck.Checked = False
        fdsecKeepCopyCheck.Checked = False
        fdsecRwCheck.Checked = False
        ' The two mutual exclusions disable controls; a page that leaves one
        ' of them set would hand the next page a field nobody can type in.
        fdsecKeepCopyCheck.Enabled = True
        fdsecRwCheck.Enabled = True
        secondTargetBox.Enabled = True
        secondTargetBrowseBtn.Enabled = True

        ' Every parameter goes back to the CLI's own default when the page changes: a size left
        ' over from the last job, or a delete rule left over from the last folder, is the kind of
        ' thing that only shows up after the run.
        For Each c As CheckBox In New CheckBox() {
            optionsCheckAutoDel, optionsCheckNoDel, optionsCheckShort, optionsCheckVerify,
            optionsCheckHere, optionsCheckProbeFix,
            optionsCheckForceFormat, optionsCheckForce, optionsCheckNoHist,
            dupListCheck, dupQuietCheck}
            c.Checked = False
            c.Enabled = True
        Next
        sizeBox.Text = ""
        sizeBox.Enabled = True
        dupRuleCombo.SelectedIndex = 0
        dupActionReport.Checked = True
        dupMoveBox.Text = ""
        dupListBox.Text = "duplicates.lst"
        cmpRuleCombo.SelectedIndex = 0
        copyStrategyCombo.SelectedIndex = 0
        checkOptions.Reset()

        Dim pair = (job IsNot Nothing AndAlso job.TargetKind = JobDefinition.TargetType.SourceAndTarget)
        secondTargetRow.Visible = pair
        secondTargetLabel.Visible = pair
        blastRadiusRow.Visible = False
        ApplyJobShape()

        rawOutputBox.Clear()
        progressBar.Value = 0
        progressBar.Style = ProgressBarStyle.Continuous
        shownVerdict = ""
        stateBadge.Text = L("shell_state_idle")
        stepLabel.Text = ""
        throughputLabel.Text = ""
        timeLabel.Text = ""
        verdictReasonLabel.Visible = False
    End Sub

    ' What step 2 and step 3 ask comes from what step 1 answered.
    '
    ' The page used to ask every job for "drive, folder or file" and then open the one dialog that
    ' cannot pick a file - three words, two of them wrong for whatever job was on screen. A job
    ' that runs on a drive now says drive or folder and browses for a folder; a job that runs on
    ' one file says file and browses for a file. The size presets are shown only where the CLI
    ' actually takes a size, instead of standing on every page meaning nothing.
    Private Sub ApplyJobShape()
        If job Is Nothing Then Return

        Select Case job.TargetKind
            Case JobDefinition.TargetType.File
                targetLabel.Text = L("shell_lbl_file")
                targetBrowseBtn.Text = L("shell_btn_browse_file")
            Case JobDefinition.TargetType.Folder
                targetLabel.Text = L("ui_folder")
                targetBrowseBtn.Text = L("shell_btn_browse_folder")
            Case JobDefinition.TargetType.SourceAndTarget
                targetLabel.Text = L("ui_source")
                targetBrowseBtn.Text = L("shell_btn_browse_folder")
            Case Else
                targetLabel.Text = L("shell_lbl_target_drive_folder")
                targetBrowseBtn.Text = L("shell_btn_browse_folder")
        End Select

        Select Case job.DefaultVerb
            Case "speed"
                presetQuick.Text = L("shell_preset_quick_speed")
                presetThorough.Text = L("shell_preset_thorough_speed")
                presetCustom.Text = L("shell_preset_custom_speed")
                showsPresets = True
            Case "test"
                presetQuick.Text = L("shell_preset_quick_test")
                presetThorough.Text = L("shell_preset_thorough_test")
                presetCustom.Text = L("shell_preset_custom_test")
                showsPresets = True
            Case Else
                showsPresets = False
        End Select
        presetRow.Visible = showsPresets
        presetQuick.Checked = True

        ApplyOptionShape()
        ApplyFdsecShape()
        UpdateParamsVisibility()
    End Sub

    ' Which of step 3's controls this job actually has. One place, read off the CLI verb, so a
    ' control is on screen exactly when the command line below can carry it.
    Private Sub ApplyOptionShape()
        If job Is Nothing Then Return
        Dim verb = job.DefaultVerb

        optionsCheckAutoDel.Visible = (verb = "test" OrElse verb = "speed" OrElse verb = "fill")
        optionsCheckNoDel.Visible = (verb = "speed")
        optionsCheckShort.Visible = (verb = "info" OrElse verb = "speed")
        optionsCheckVerify.Visible = (verb = "fill")
        optionsCheckHere.Visible = (verb = "unsecure")
        optionsCheckProbeFix.Visible = (verb = "probe")
        optionsCheckForceFormat.Visible = (verb = "recover")
        ' -y belongs to wipe alone. A probe and a recover carry `yes` because their questions are
        ' answered on the page instead (UpdateParamsVisibility), and a secret-file page adds -y
        ' itself when a destructive disposition is chosen.
        optionsCheckForce.Visible = (verb = "wipe")
        optionsCheckNoHist.Visible = Not IsFdsec()
        optionChecksShown = AnyOptionCheckRequested(verb)

        showsDup = (verb = "cd")
        showsCompare = (verb = "compare")
        showsCopy = (verb = "copy")
        showsCheck = (verb = "check")
        dupRow.Visible = showsDup
        cmpRow.Visible = showsCompare
        copyRow.Visible = showsCopy
        checkOptions.Visible = showsCheck

        sizeLabel.Text = If(verb = "test", L("shell_lbl_params_count"), L("shell_lbl_params_size"))
        sizeBox.AccessibleName = sizeLabel.Text

        UpdateOptionExclusions()
        UpdateOptionNotices()
    End Sub

    ' The size field follows the third preset for the two jobs that have presets, and is simply
    ' the only way to say how big a fill file is for the one that has none.
    Private Sub UpdateSizeRowVisibility()
        If job Is Nothing Then Return
        Dim verb = job.DefaultVerb
        showsSize = (verb = "fill") OrElse
                    ((verb = "speed" OrElse verb = "test") AndAlso presetCustom.Checked)
        sizeRow.Visible = showsSize
    End Sub

    ' The verbs that own at least one of the shared option checkboxes. Kept next to the list that
    ' shows them, so the two cannot drift apart.
    Private Function AnyOptionCheckRequested(verb As String) As Boolean
        Select Case verb
            Case "test", "speed", "fill", "info", "unsecure", "probe", "recover", "wipe"
                Return True
            Case Else
                Return Not IsFdsec()
        End Select
    End Function

    ' The pairs the CLI would refuse, or would silently ignore, refused here instead.
    Private Sub UpdateOptionExclusions()
        If job Is Nothing Then Return

        If job.DefaultVerb = "speed" Then
            ' del and nodel answer the same question twice.
            optionsCheckAutoDel.Enabled = Not optionsCheckNoDel.Checked
            optionsCheckNoDel.Enabled = Not optionsCheckAutoDel.Checked
        Else
            optionsCheckAutoDel.Enabled = True
            optionsCheckNoDel.Enabled = True
        End If

        If job.DefaultVerb = "fill" Then
            ' `fill verify` is its own operation - it checks what a previous fill wrote and takes
            ' neither a size nor a delete.
            Dim verifying = optionsCheckVerify.Checked
            sizeBox.Enabled = Not verifying
            optionsCheckAutoDel.Enabled = Not verifying
        End If

        If job.DefaultVerb = "unsecure" Then
            ' `here` and `to <dest>` are two answers to "where does it go".
            secondTargetBox.Enabled = Not optionsCheckHere.Checked
            secondTargetBrowseBtn.Enabled = Not optionsCheckHere.Checked
            If optionsCheckHere.Checked Then secondTargetBox.Text = ""
        End If

        If job.DefaultVerb = "cd" Then
            dupMoveBox.Enabled = dupActionMove.Checked
            dupMoveBrowseBtn.Enabled = dupActionMove.Checked
            dupListBox.Enabled = dupListCheck.Checked
        End If

        UpdateSizeRowVisibility()
    End Sub

    ' Each rule picker says, in the user's language, what the token it just wrote actually does.
    Private Sub UpdateOptionNotices()
        If job Is Nothing Then Return

        If dupRuleCombo.SelectedIndex >= 0 Then
            dupRuleNotice.Text = L(CliRules.DupRuleKeys(dupRuleCombo.SelectedIndex))
        End If
        If cmpRuleCombo.SelectedIndex >= 0 Then
            cmpRuleNotice.Text = L(CliRules.CmpRuleKeys(cmpRuleCombo.SelectedIndex))
        End If
        If copyStrategyCombo.SelectedIndex >= 0 Then
            copyStrategyNotice.Text = L("op_" & CliRules.CopyVerbs(copyStrategyCombo.SelectedIndex))
        End If
    End Sub

    Private Sub PresetChanged()
        UpdateSizeRowVisibility()
        UpdatePlanCard()
    End Sub

    Private Sub OptionChanged()
        UpdateOptionExclusions()
        UpdateOptionNotices()
        ' Some of these controls carry the danger colour while they are on, so the page is
        ' re-themed the moment one is chosen rather than at the next page change.
        ApplyTheme()
        UpdatePlanCard()
    End Sub

    ' The three secret-file jobs, and which of the fields each one asks for.
    Private Function IsFdsec() As Boolean
        If job Is Nothing Then Return False
        Select Case job.DefaultVerb
            Case "secure", "unsecure", "reveal" : Return True
            Case Else : Return False
        End Select
    End Function

    Private Sub ApplyFdsecShape()
        Dim fdsec = IsFdsec()
        credRow.Visible = fdsec
        fdsecRow.Visible = fdsec
        If Not fdsec Then
            ' Both labels are shared with the jobs that had them first, so a
            ' page left without restoring them would relabel the next one.
            secondTargetLabel.Text = L("shell_lbl_dest")
            wipeConfirmLabel.Text = L("shell_wipe_confirm_hint")
            Return
        End If

        Dim verb = job.DefaultVerb
        ' Only secure asks twice: it is the one whose typo cannot be found out
        ' until the day the file is needed.
        credConfirmLabel.Visible = (verb = "secure")
        credConfirmBox.Visible = (verb = "secure")

        fdsecOriginalLabel.Visible = (verb = "secure")
        fdsecKeepRadio.Visible = (verb = "secure")
        fdsecDelRadio.Visible = (verb = "secure")
        fdsecWipeRadio.Visible = (verb = "secure")
        fdsecRenameCheck.Visible = (verb = "secure")
        fdsecDelContainerCheck.Visible = (verb = "unsecure")
        fdsecKeepCopyCheck.Visible = (verb = "reveal")
        fdsecRwCheck.Visible = (verb = "reveal")

        ' Where a result is written is a question for the two verbs that write
        ' an ordinary file. A reveal writes into its sandbox and nowhere else,
        ' which is the whole point of it.
        Dim wantsDest = (verb = "secure" OrElse verb = "unsecure")
        secondTargetLabel.Visible = wantsDest
        secondTargetRow.Visible = wantsDest
        secondTargetLabel.Text = L("shell_fdsec_dest")

        Select Case verb
            Case "reveal" : fdsecNoticeLabel.Text = L("shell_fdsec_reveal_where")
            Case "secure" : fdsecNoticeLabel.Text = L("shell_fdsec_secure_note")
            Case Else : fdsecNoticeLabel.Text = L("shell_fdsec_unsecure_note")
        End Select

        UpdateCredentialNotice()
        UpdateFdsecExclusions()
    End Sub

    ' Two pairs the CLI refuses, refused here instead of after the run: a
    ' random name and a named destination answer the same question twice, and
    ' -rw leaves no sandbox copy for -keep to keep.
    Private Sub UpdateFdsecExclusions()
        If Not IsFdsec() Then Return

        If job.DefaultVerb = "secure" Then
            Dim named = fdsecRenameCheck.Checked
            secondTargetBox.Enabled = Not named
            secondTargetBrowseBtn.Enabled = Not named
            If named Then secondTargetBox.Text = ""
        Else
            secondTargetBox.Enabled = True
            secondTargetBrowseBtn.Enabled = True
        End If

        If job.DefaultVerb = "reveal" Then
            fdsecKeepCopyCheck.Enabled = Not fdsecRwCheck.Checked
            fdsecRwCheck.Enabled = Not fdsecKeepCopyCheck.Checked
        End If
    End Sub

    Private Sub CredentialChanged()
        UpdateCredentialNotice()
        UpdatePlanCard()
    End Sub

    Private Sub FdsecOptionChanged()
        UpdateFdsecExclusions()
        UpdateParamsVisibility()
        ApplyTheme()
        UpdatePlanCard()
    End Sub

    ' The honest line under the password box (principle 2, invariant 10).
    ' It changes while the password is typed, because that is when it is worth
    ' reading - and it never calls an empty credential protection.
    Private Sub UpdateCredentialNotice()
        Dim pwd = credBox.Text
        If pwd.Length = 0 Then
            credNoticeLabel.Text = L("shell_cred_empty")
        ElseIf pwd.Length < 12 Then
            credNoticeLabel.Text = L("shell_cred_short")
        ElseIf job IsNot Nothing AndAlso job.DefaultVerb = "secure" AndAlso credConfirmBox.Text <> pwd Then
            credNoticeLabel.Text = L("shell_cred_mismatch")
        Else
            credNoticeLabel.Text = L("shell_cred_ok")
        End If
    End Sub

    ' A disposition, not a job, is what makes this page destructive: wipe
    ' overwrites the original and delete unlinks it, and neither is the
    ' default (Q9).
    Private Function FdsecIsDestructiveNow() As Boolean
        If Not IsFdsec() Then Return False
        If job.DefaultVerb = "secure" Then Return fdsecDelRadio.Checked OrElse fdsecWipeRadio.Checked
        If job.DefaultVerb = "unsecure" Then Return fdsecDelContainerCheck.Checked
        Return False
    End Function

    ' Typing WIPE is kept for the one disposition that overwrites bytes, and
    ' for no other - the shell's existing rule (SP-0006 section 8 item 1),
    ' applied where it earns its friction.
    Private Function FdsecNeedsTypedConfirm() As Boolean
        Return IsFdsec() AndAlso job.DefaultVerb = "secure" AndAlso fdsecWipeRadio.Checked
    End Function

    ' Step 2, answered from outside: a double-clicked container (SP-0005 9.4).
    Public Sub SetTarget(path As String)
        targetCombo.Text = path
        TargetChanged()
    End Sub

    ' What the plan card says will run. Read by the self-test, so that "the page writes the right
    ' command" is something a build can check rather than something a person clicks through.
    Public Function CurrentCommand() As String
        Return commandBox.Text
    End Function

    ' A heading with nothing under it is a question with no answers. Some jobs - info, the
    ' duplicate scan, compare, copy - take no parameters at all today, and for those the card is
    ' not shown empty: it is not shown, and the plan card takes its number so the page still
    ' counts 2, 3 rather than 2, (nothing), 4.
    '
    ' What the card holds is read off the job and not off the controls, because a control reports
    ' Visible = False while any parent of it is hidden - and this runs from SetJob, before the
    ' page is shown. Asking the controls would have hidden the card on every job, including the
    ' ones that do have parameters.
    Private Sub UpdateParamsVisibility()
        If job Is Nothing Then Return

        ' What the card holds is whatever ApplyOptionShape has just put on screen for this job.
        ' Every verb the shell runs now has at least one parameter of its own, but the question is
        ' still asked rather than assumed: a job added later with no parameters gets no empty card.
        Dim any = showsPresets OrElse showsSize OrElse showsDup OrElse showsCompare OrElse
                  showsCopy OrElse showsCheck OrElse job.IsDestructive OrElse IsFdsec() OrElse
                  optionChecksShown
        paramsCard.Visible = any
        planHeader.Text = If(any, L("shell_step4"), L("shell_step3_check"))

        ' The typed WIPE follows the disposition rather than the page, so it
        ' appears the moment "overwrite the original" is chosen and goes again
        ' when it is not.
        If IsFdsec() Then
            wipeConfirmLabel.Text = L("shell_fdsec_wipe_confirm")
            wipeConfirmRow.Visible = FdsecNeedsTypedConfirm()
            If Not wipeConfirmRow.Visible Then wipeConfirmBox.Text = ""
        End If

        ' The CLI asks a probe and a recover to have their target typed out on the console before
        ' either touches the volume (probe_windows.go). Started from this window there is no
        ' console to answer on, so the question is asked here and the run carries `yes`: the
        ' friction is kept where it was meant to be rather than dropped on the way.
        If NeedsDriveConfirm() Then
            wipeConfirmLabel.Text = L("shell_probe_confirm_hint")
            wipeConfirmRow.Visible = True
        End If
    End Sub

    Private Function NeedsDriveConfirm() As Boolean
        If job Is Nothing Then Return False
        Return job.DefaultVerb = "probe" OrElse job.DefaultVerb = "recover"
    End Function

    ' ---- layout ----------------------------------------------------------

    Private Sub BuildLayout()
        SuspendLayout()

        rootPanel = New TableLayoutPanel With {
            .Dock = DockStyle.Top,
            .AutoSize = True,
            .AutoSizeMode = AutoSizeMode.GrowAndShrink,
            .ColumnCount = 1,
            .RowCount = 6,
            .Padding = Ui.PxPad(Me, 0, 0, 0, 16),
            .Margin = New Padding(0)
        }
        rootPanel.ColumnStyles.Add(New ColumnStyle(SizeType.Percent, 100.0F))
        For i As Integer = 0 To 5
            rootPanel.RowStyles.Add(New RowStyle(SizeType.AutoSize))
        Next

        BuildTargetCard()
        BuildParamsCard()
        BuildPlanCard()
        BuildRunStrip()
        BuildResultCard()
        BuildRawOutputDrawer()

        rootPanel.Controls.Add(targetCard, 0, 0)
        rootPanel.Controls.Add(paramsCard, 0, 1)
        rootPanel.Controls.Add(planCard, 0, 2)
        rootPanel.Controls.Add(runStrip, 0, 3)
        rootPanel.Controls.Add(resultCard, 0, 4)
        rootPanel.Controls.Add(rawOutputDrawer, 0, 5)

        Controls.Add(rootPanel)
        ResumeLayout(True)
    End Sub

    Private Function NewCard() As ShellCard
        Return New ShellCard With {
            .Padding = Ui.PxPad(Me, 18, 16, 18, 16),
            .Margin = Ui.PxPad(Me, 0, 0, 0, 14)
        }
    End Function

    Private Function NewTable(columns As Integer, rows As Integer) As TableLayoutPanel
        Dim t As New TableLayoutPanel With {
            .Dock = DockStyle.Top,
            .AutoSize = True,
            .AutoSizeMode = AutoSizeMode.GrowAndShrink,
            .ColumnCount = columns,
            .RowCount = rows,
            .Margin = New Padding(0)
        }
        Return t
    End Function

    Private Sub BuildTargetCard()
        targetCard = NewCard()

        Dim table = NewTable(2, 5)
        table.ColumnStyles.Add(New ColumnStyle(SizeType.Percent, 100.0F))
        table.ColumnStyles.Add(New ColumnStyle(SizeType.AutoSize))

        targetHeader = Ui.StepHeader(Me, L("shell_step2"))
        table.Controls.Add(targetHeader, 0, 0)
        table.SetColumnSpan(targetHeader, 2)

        targetLabel = New Label With {.Text = L("shell_lbl_target"), .AutoSize = True, .Margin = Ui.PxPad(Me, 0, 0, 0, 2)}
        table.Controls.Add(targetLabel, 0, 1)
        table.SetColumnSpan(targetLabel, 2)

        targetCombo = New ComboBox With {
            .Dock = DockStyle.Fill,
            .DropDownStyle = ComboBoxStyle.DropDown,
            .Margin = Ui.PxPad(Me, 0, 2, 8, 4)
        }
        targetCombo.AccessibleName = L("shell_lbl_target")
        AddHandler targetCombo.TextChanged, Sub() TargetChanged()
        AddHandler targetCombo.SelectedIndexChanged, Sub() TargetChanged()

        targetBrowseBtn = New Button With {
            .Text = L("shell_btn_browse"),
            .AutoSize = True,
            .AutoSizeMode = AutoSizeMode.GrowAndShrink,
            .Margin = Ui.PxPad(Me, 0, 2, 0, 4)
        }
        AddHandler targetBrowseBtn.Click, AddressOf BrowseTarget_Click
        secondaryButtons.Add(targetBrowseBtn)

        table.Controls.Add(targetCombo, 0, 2)
        table.Controls.Add(targetBrowseBtn, 1, 2)

        ' The destination of a two-target job, labelled rather than left as a bare second box.
        secondTargetLabel = New Label With {.Text = L("shell_lbl_dest"), .AutoSize = True, .Margin = Ui.PxPad(Me, 0, 6, 0, 2), .Visible = False}
        table.Controls.Add(secondTargetLabel, 0, 3)
        table.SetColumnSpan(secondTargetLabel, 2)

        secondTargetRow = NewTable(2, 1)
        secondTargetRow.ColumnStyles.Add(New ColumnStyle(SizeType.Percent, 100.0F))
        secondTargetRow.ColumnStyles.Add(New ColumnStyle(SizeType.AutoSize))
        secondTargetRow.Margin = Ui.PxPad(Me, 0, 0, 0, 4)
        secondTargetRow.Visible = False

        secondTargetBox = New TextBox With {.Dock = DockStyle.Fill, .Margin = Ui.PxPad(Me, 0, 2, 8, 4)}
        secondTargetBox.AccessibleName = L("shell_lbl_dest")
        AddHandler secondTargetBox.TextChanged, Sub() UpdatePlanCard()

        secondTargetBrowseBtn = New Button With {
            .Text = L("shell_btn_browse_dest"),
            .AutoSize = True,
            .AutoSizeMode = AutoSizeMode.GrowAndShrink,
            .Margin = Ui.PxPad(Me, 0, 2, 0, 4)
        }
        AddHandler secondTargetBrowseBtn.Click, AddressOf BrowseSecondTarget_Click
        secondaryButtons.Add(secondTargetBrowseBtn)

        secondTargetRow.Controls.Add(secondTargetBox, 0, 0)
        secondTargetRow.Controls.Add(secondTargetBrowseBtn, 1, 0)
        table.Controls.Add(secondTargetRow, 0, 4)
        table.SetColumnSpan(secondTargetRow, 2)

        dropHintLabel = New Label With {.Text = L("shell_drop_hint"), .AutoSize = True, .Margin = Ui.PxPad(Me, 0, 6, 0, 0)}
        Dim hintHost = NewTable(1, 1)
        hintHost.ColumnStyles.Add(New ColumnStyle(SizeType.Percent, 100.0F))
        hintHost.Controls.Add(dropHintLabel, 0, 0)

        Dim wrapper = NewTable(1, 2)
        wrapper.ColumnStyles.Add(New ColumnStyle(SizeType.Percent, 100.0F))
        wrapper.Controls.Add(table, 0, 0)
        wrapper.Controls.Add(hintHost, 0, 1)

        targetCard.Controls.Add(wrapper)
        Ui.Wrap(dropHintLabel, targetCard, Ui.Px(Me, 36))
    End Sub

    Private Sub BuildParamsCard()
        paramsCard = NewCard()

        Dim table = NewTable(1, 9)
        table.ColumnStyles.Add(New ColumnStyle(SizeType.Percent, 100.0F))

        paramsHeader = Ui.StepHeader(Me, L("shell_step3"))
        table.Controls.Add(paramsHeader, 0, 0)

        ' Quick and Thorough are two answers to one question, and a pair of buttons says that
        ' badly: a button is something that happens once, and neither of them can show which one
        ' is in force. They are radio buttons, so the chosen answer is visible while it is chosen
        ' and the command line below changes with it.
        presetRow = New FlowLayoutPanel With {
            .Dock = DockStyle.Top,
            .AutoSize = True,
            .AutoSizeMode = AutoSizeMode.GrowAndShrink,
            .FlowDirection = FlowDirection.TopDown,
            .WrapContents = False,
            .Margin = Ui.PxPad(Me, 0, 0, 0, 4)
        }
        presetQuick = New RadioButton With {.Text = L("shell_preset_quick"), .AutoSize = True, .Checked = True, .Margin = Ui.PxPad(Me, 0, 2, 0, 2)}
        AddHandler presetQuick.CheckedChanged, Sub() PresetChanged()

        presetThorough = New RadioButton With {.Text = L("shell_preset_thorough"), .AutoSize = True, .Margin = Ui.PxPad(Me, 0, 2, 0, 2)}
        AddHandler presetThorough.CheckedChanged, Sub() PresetChanged()

        presetCustom = New RadioButton With {.Text = L("shell_preset_custom"), .AutoSize = True, .Margin = Ui.PxPad(Me, 0, 2, 0, 2)}
        AddHandler presetCustom.CheckedChanged, Sub() PresetChanged()

        presetRow.Controls.Add(presetQuick)
        presetRow.Controls.Add(presetThorough)
        presetRow.Controls.Add(presetCustom)
        table.Controls.Add(presetRow, 0, 1)

        ' The number itself - megabytes for speed and fill, a file count for test. It is one row
        ' rather than three, because it is one question asked of three jobs.
        sizeRow = New FlowLayoutPanel With {
            .Dock = DockStyle.Top,
            .AutoSize = True,
            .AutoSizeMode = AutoSizeMode.GrowAndShrink,
            .FlowDirection = FlowDirection.LeftToRight,
            .WrapContents = True,
            .Margin = Ui.PxPad(Me, 0, 2, 0, 2),
            .Visible = False
        }
        sizeLabel = New Label With {.Text = L("shell_lbl_params_size"), .AutoSize = True, .Margin = Ui.PxPad(Me, 0, 6, 8, 0)}
        sizeBox = New TextBox With {.Width = Ui.Px(Me, 90), .Margin = Ui.PxPad(Me, 0, 2, 0, 0)}
        sizeBox.AccessibleName = L("shell_lbl_params_size")
        AddHandler sizeBox.TextChanged, Sub() OptionChanged()
        sizeRow.Controls.Add(sizeLabel)
        sizeRow.Controls.Add(sizeBox)
        table.Controls.Add(sizeRow, 0, 2)

        BuildExtras()
        table.Controls.Add(extrasFlow, 0, 3)

        Dim optionFlow As New FlowLayoutPanel With {
            .Dock = DockStyle.Top,
            .AutoSize = True,
            .AutoSizeMode = AutoSizeMode.GrowAndShrink,
            .FlowDirection = FlowDirection.TopDown,
            .WrapContents = False,
            .Margin = Ui.PxPad(Me, 0, 6, 0, 4)
        }
        optionsCheckAutoDel = New CheckBox With {.Text = L("shell_opt_autodel"), .AutoSize = True, .Margin = Ui.PxPad(Me, 0, 2, 0, 2)}
        AddHandler optionsCheckAutoDel.CheckedChanged, Sub() UpdatePlanCard()

        optionsCheckForce = New CheckBox With {.Text = L("shell_opt_force"), .AutoSize = True, .Margin = Ui.PxPad(Me, 0, 2, 0, 2)}
        AddHandler optionsCheckForce.CheckedChanged, Sub() UpdatePlanCard()

        optionsCheckNoDel = NewOption("shell_opt_nodel")
        optionsCheckShort = NewOption("shell_opt_short")
        optionsCheckVerify = NewOption("shell_opt_verify")
        optionsCheckHere = NewOption("shell_opt_here")
        optionsCheckProbeFix = NewOption("shell_opt_probe_fix")
        optionsCheckForceFormat = NewOption("shell_opt_force_format")
        optionsCheckNoHist = NewOption("shell_opt_nohist")

        optionFlow.Controls.Add(optionsCheckAutoDel)
        optionFlow.Controls.Add(optionsCheckNoDel)
        optionFlow.Controls.Add(optionsCheckVerify)
        optionFlow.Controls.Add(optionsCheckShort)
        optionFlow.Controls.Add(optionsCheckHere)
        optionFlow.Controls.Add(optionsCheckProbeFix)
        optionFlow.Controls.Add(optionsCheckForceFormat)
        optionFlow.Controls.Add(optionsCheckForce)
        optionFlow.Controls.Add(optionsCheckNoHist)
        table.Controls.Add(optionFlow, 0, 4)

        blastRadiusRow = New FlowLayoutPanel With {
            .Dock = DockStyle.Top,
            .AutoSize = True,
            .AutoSizeMode = AutoSizeMode.GrowAndShrink,
            .FlowDirection = FlowDirection.LeftToRight,
            .WrapContents = True,
            .Margin = Ui.PxPad(Me, 0, 6, 0, 4),
            .Visible = False
        }
        blastRadiusLabel = New Label With {.Text = "", .AutoSize = True, .Margin = Ui.PxPad(Me, 0, 6, 8, 0)}
        blastRadiusSkipBtn = New Button With {.Text = L("shell_btn_skip_count"), .AutoSize = True, .AutoSizeMode = AutoSizeMode.GrowAndShrink}
        AddHandler blastRadiusSkipBtn.Click, AddressOf SkipCounting_Click
        secondaryButtons.Add(blastRadiusSkipBtn)
        blastRadiusRow.Controls.Add(blastRadiusLabel)
        blastRadiusRow.Controls.Add(blastRadiusSkipBtn)
        table.Controls.Add(blastRadiusRow, 0, 5)

        wipeConfirmRow = New FlowLayoutPanel With {
            .Dock = DockStyle.Top,
            .AutoSize = True,
            .AutoSizeMode = AutoSizeMode.GrowAndShrink,
            .FlowDirection = FlowDirection.LeftToRight,
            .WrapContents = True,
            .Margin = Ui.PxPad(Me, 0, 6, 0, 0),
            .Visible = False
        }
        wipeConfirmLabel = New Label With {.Text = L("shell_wipe_confirm_hint"), .AutoSize = True, .Margin = Ui.PxPad(Me, 0, 6, 8, 0)}
        wipeConfirmBox = New TextBox With {.Width = Ui.Px(Me, 120), .Margin = Ui.PxPad(Me, 0, 2, 8, 0)}
        wipeConfirmBox.AccessibleName = L("shell_wipe_confirm_hint")
        AddHandler wipeConfirmBox.TextChanged, Sub() UpdateStartButtonState()

        wipeConfirmRow.Controls.Add(wipeConfirmLabel)
        wipeConfirmRow.Controls.Add(wipeConfirmBox)
        table.Controls.Add(wipeConfirmRow, 0, 6)

        table.RowCount = 9
        BuildCredentialRow()
        BuildFdsecOptions()
        table.Controls.Add(credRow, 0, 7)
        table.Controls.Add(fdsecRow, 0, 8)

        paramsCard.Controls.Add(table)
        Ui.Wrap(dupRuleNotice, paramsCard, Ui.Px(Me, 36))
        Ui.Wrap(cmpRuleNotice, paramsCard, Ui.Px(Me, 36))
        Ui.Wrap(copyStrategyNotice, paramsCard, Ui.Px(Me, 36))
        Ui.Wrap(wipeConfirmLabel, paramsCard, Ui.Px(Me, 160))
        Ui.Wrap(credNoticeLabel, paramsCard, Ui.Px(Me, 36))
        Ui.Wrap(credHintLabel, paramsCard, Ui.Px(Me, 36))
        Ui.Wrap(fdsecNoticeLabel, paramsCard, Ui.Px(Me, 36))
    End Sub

    Private Function NewOption(key As String) As CheckBox
        Dim c As New CheckBox With {.Text = L(key), .AutoSize = True, .Margin = Ui.PxPad(Me, 0, 2, 0, 2), .Visible = False}
        AddHandler c.CheckedChanged, Sub() OptionChanged()
        Return c
    End Function

    ' The blocks that belong to one job each. They are built once and shown by ApplyJobShape, the
    ' same way the secret-file block already was.
    Private Sub BuildExtras()
        extrasFlow = New FlowLayoutPanel With {
            .Dock = DockStyle.Top,
            .AutoSize = True,
            .AutoSizeMode = AutoSizeMode.GrowAndShrink,
            .FlowDirection = FlowDirection.TopDown,
            .WrapContents = False,
            .Margin = New Padding(0)
        }

        BuildDuplicateRow()
        BuildCompareRow()
        BuildCopyRow()

        checkOptions = New CheckOptionsPanel() With {.Visible = False}
        AddHandler checkOptions.Changed,
            Sub()
                checkOptions.ApplyTheme()
                UpdatePlanCard()
            End Sub

        extrasFlow.Controls.Add(dupRow)
        extrasFlow.Controls.Add(cmpRow)
        extrasFlow.Controls.Add(copyRow)
        extrasFlow.Controls.Add(checkOptions)
    End Sub

    ' The duplicate scan asks two separate questions - which copy is the original, and what
    ' happens to the rest - and the CLI takes them as two independent words. The page asks them
    ' separately too, rather than folding them into one list of combinations.
    Private Sub BuildDuplicateRow()
        dupRow = New FlowLayoutPanel With {
            .Dock = DockStyle.Top,
            .AutoSize = True,
            .AutoSizeMode = AutoSizeMode.GrowAndShrink,
            .FlowDirection = FlowDirection.TopDown,
            .WrapContents = False,
            .Margin = Ui.PxPad(Me, 0, 2, 0, 4),
            .Visible = False
        }

        dupRuleLabel = New Label With {.Text = L("shell_dup_rule"), .AutoSize = True, .Margin = Ui.PxPad(Me, 0, 4, 0, 2)}
        dupRuleCombo = New ComboBox With {.DropDownStyle = ComboBoxStyle.DropDownList, .Width = Ui.Px(Me, 240), .Margin = Ui.PxPad(Me, 0, 0, 0, 2)}
        For i = 0 To CliRules.DupRuleTokens.Length - 1
            dupRuleCombo.Items.Add(If(CliRules.DupRuleTokens(i) = "", L("shell_dup_rule_default"), CliRules.DupRuleTokens(i)))
        Next
        dupRuleCombo.SelectedIndex = 0
        dupRuleCombo.AccessibleName = L("shell_dup_rule")
        AddHandler dupRuleCombo.SelectedIndexChanged, Sub() OptionChanged()

        dupRuleNotice = New Label With {.Text = L("cdrule_default"), .AutoSize = True, .Margin = Ui.PxPad(Me, 0, 0, 0, 6)}

        dupActionLabel = New Label With {.Text = L("shell_dup_action"), .AutoSize = True, .Margin = Ui.PxPad(Me, 0, 4, 0, 2)}
        dupActionReport = New RadioButton With {.Text = L("shell_dup_report"), .AutoSize = True, .Checked = True, .Margin = Ui.PxPad(Me, 0, 2, 0, 2)}
        dupActionDelete = New RadioButton With {.Text = L("shell_dup_delete"), .AutoSize = True, .Margin = Ui.PxPad(Me, 0, 2, 0, 2)}
        dupActionMove = New RadioButton With {.Text = L("dup_move"), .AutoSize = True, .Margin = Ui.PxPad(Me, 0, 2, 0, 2)}
        For Each r As RadioButton In New RadioButton() {dupActionReport, dupActionDelete, dupActionMove}
            AddHandler r.CheckedChanged, Sub() OptionChanged()
        Next

        Dim moveHost As New FlowLayoutPanel With {
            .AutoSize = True,
            .AutoSizeMode = AutoSizeMode.GrowAndShrink,
            .FlowDirection = FlowDirection.LeftToRight,
            .WrapContents = False,
            .Margin = Ui.PxPad(Me, 18, 0, 0, 4)
        }
        dupMoveBox = New TextBox With {.Width = Ui.Px(Me, 260), .Margin = Ui.PxPad(Me, 0, 2, 8, 0), .Enabled = False}
        dupMoveBox.AccessibleName = L("dup_move")
        AddHandler dupMoveBox.TextChanged, Sub() UpdatePlanCard()
        dupMoveBrowseBtn = New Button With {.Text = L("shell_btn_browse_folder"), .AutoSize = True, .AutoSizeMode = AutoSizeMode.GrowAndShrink, .Enabled = False}
        AddHandler dupMoveBrowseBtn.Click,
            Sub()
                Using dlg As New FolderBrowserDialog()
                    dlg.Description = L("shell_dlg_select_dest")
                    If dlg.ShowDialog(FindForm()) = DialogResult.OK Then dupMoveBox.Text = dlg.SelectedPath
                End Using
            End Sub
        secondaryButtons.Add(dupMoveBrowseBtn)
        moveHost.Controls.Add(dupMoveBox)
        moveHost.Controls.Add(dupMoveBrowseBtn)

        ' `list <file>` writes the duplicate groups to a file the CLI can be handed back later
        ' (`cd from list <file>`), which is the whole point of scanning a slow drive once.
        dupListCheck = New CheckBox With {.Text = L("shell_dup_list"), .AutoSize = True, .Margin = Ui.PxPad(Me, 0, 6, 0, 2)}
        AddHandler dupListCheck.CheckedChanged, Sub() OptionChanged()

        Dim listHost As New FlowLayoutPanel With {
            .AutoSize = True,
            .AutoSizeMode = AutoSizeMode.GrowAndShrink,
            .FlowDirection = FlowDirection.LeftToRight,
            .WrapContents = False,
            .Margin = Ui.PxPad(Me, 18, 0, 0, 4)
        }
        dupListBox = New TextBox With {.Width = Ui.Px(Me, 260), .Text = "duplicates.lst", .Margin = Ui.PxPad(Me, 0, 2, 8, 0), .Enabled = False}
        dupListBox.AccessibleName = L("ui_list")
        AddHandler dupListBox.TextChanged, Sub() UpdatePlanCard()
        listHost.Controls.Add(dupListBox)

        dupQuietCheck = New CheckBox With {.Text = L("shell_dup_quiet"), .AutoSize = True, .Margin = Ui.PxPad(Me, 0, 4, 0, 2)}
        AddHandler dupQuietCheck.CheckedChanged, Sub() OptionChanged()

        dupRow.Controls.Add(dupRuleLabel)
        dupRow.Controls.Add(dupRuleCombo)
        dupRow.Controls.Add(dupRuleNotice)
        dupRow.Controls.Add(dupActionLabel)
        dupRow.Controls.Add(dupActionReport)
        dupRow.Controls.Add(dupActionDelete)
        dupRow.Controls.Add(dupActionMove)
        dupRow.Controls.Add(moveHost)
        dupRow.Controls.Add(dupListCheck)
        dupRow.Controls.Add(listHost)
        dupRow.Controls.Add(dupQuietCheck)
    End Sub

    Private Sub BuildCompareRow()
        cmpRow = New FlowLayoutPanel With {
            .Dock = DockStyle.Top,
            .AutoSize = True,
            .AutoSizeMode = AutoSizeMode.GrowAndShrink,
            .FlowDirection = FlowDirection.TopDown,
            .WrapContents = False,
            .Margin = Ui.PxPad(Me, 0, 2, 0, 4),
            .Visible = False
        }

        cmpRuleLabel = New Label With {.Text = L("cmp_label"), .AutoSize = True, .Margin = Ui.PxPad(Me, 0, 4, 0, 2)}
        cmpRuleCombo = New ComboBox With {.DropDownStyle = ComboBoxStyle.DropDownList, .Width = Ui.Px(Me, 260), .Margin = Ui.PxPad(Me, 0, 0, 0, 2)}
        For i = 0 To CliRules.CmpRuleTokens.Length - 1
            cmpRuleCombo.Items.Add(If(CliRules.CmpRuleTokens(i) = "", L("shell_cmp_rule_none"), CliRules.CmpRuleTokens(i)))
        Next
        cmpRuleCombo.SelectedIndex = 0
        cmpRuleCombo.AccessibleName = L("cmp_label")
        AddHandler cmpRuleCombo.SelectedIndexChanged, Sub() OptionChanged()

        cmpRuleNotice = New Label With {.Text = L("cmprule_none"), .AutoSize = True, .Margin = Ui.PxPad(Me, 0, 0, 0, 2)}

        cmpRow.Controls.Add(cmpRuleLabel)
        cmpRow.Controls.Add(cmpRuleCombo)
        cmpRow.Controls.Add(cmpRuleNotice)
    End Sub

    Private Sub BuildCopyRow()
        copyRow = New FlowLayoutPanel With {
            .Dock = DockStyle.Top,
            .AutoSize = True,
            .AutoSizeMode = AutoSizeMode.GrowAndShrink,
            .FlowDirection = FlowDirection.TopDown,
            .WrapContents = False,
            .Margin = Ui.PxPad(Me, 0, 2, 0, 4),
            .Visible = False
        }

        copyStrategyLabel = New Label With {.Text = L("shell_copy_strategy"), .AutoSize = True, .Margin = Ui.PxPad(Me, 0, 4, 0, 2)}
        copyStrategyCombo = New ComboBox With {.DropDownStyle = ComboBoxStyle.DropDownList, .Width = Ui.Px(Me, 200), .Margin = Ui.PxPad(Me, 0, 0, 0, 2)}
        copyStrategyCombo.Items.AddRange(CliRules.CopyVerbs.Cast(Of Object)().ToArray())
        copyStrategyCombo.SelectedIndex = 0
        copyStrategyCombo.AccessibleName = L("shell_copy_strategy")
        AddHandler copyStrategyCombo.SelectedIndexChanged, Sub() OptionChanged()

        copyStrategyNotice = New Label With {.Text = L("op_copy"), .AutoSize = True, .Margin = Ui.PxPad(Me, 0, 0, 0, 2)}

        copyRow.Controls.Add(copyStrategyLabel)
        copyRow.Controls.Add(copyStrategyCombo)
        copyRow.Controls.Add(copyStrategyNotice)
    End Sub

    ' The credential field (SP-0005 7, 12).
    '
    ' Three things about it are not decoration. It is masked, with a show
    ' toggle, because a password typed in front of somebody is the ordinary
    ' case in an office. Secure asks twice, because a typo there locks the
    ' data behind a password the owner never meant - and the CLI's own prompt
    ' does the same. And the line under it says what an empty password is
    ' worth, at the moment it is being typed rather than in a manual: an empty
    ' credential is obfuscation and nothing else, and every surface says so
    ' (principle 2).
    Private Sub BuildCredentialRow()
        credRow = NewTable(3, 5)
        credRow.ColumnStyles.Add(New ColumnStyle(SizeType.Percent, 100.0F))
        credRow.ColumnStyles.Add(New ColumnStyle(SizeType.AutoSize))
        credRow.ColumnStyles.Add(New ColumnStyle(SizeType.AutoSize))
        credRow.Margin = Ui.PxPad(Me, 0, 6, 0, 0)
        credRow.Visible = False

        credLabel = New Label With {.Text = L("shell_lbl_password"), .AutoSize = True, .Margin = Ui.PxPad(Me, 0, 4, 0, 2)}
        credRow.Controls.Add(credLabel, 0, 0)
        credRow.SetColumnSpan(credLabel, 3)

        credBox = New TextBox With {.Dock = DockStyle.Fill, .UseSystemPasswordChar = True, .Margin = Ui.PxPad(Me, 0, 2, 8, 4)}
        credBox.AccessibleName = L("shell_lbl_password")
        AddHandler credBox.TextChanged, Sub() CredentialChanged()
        credRow.Controls.Add(credBox, 0, 1)

        credShowCheck = New CheckBox With {.Text = L("shell_cred_show"), .AutoSize = True, .Margin = Ui.PxPad(Me, 0, 4, 0, 4)}
        AddHandler credShowCheck.CheckedChanged,
            Sub()
                credBox.UseSystemPasswordChar = Not credShowCheck.Checked
                credConfirmBox.UseSystemPasswordChar = Not credShowCheck.Checked
            End Sub
        credRow.Controls.Add(credShowCheck, 1, 1)

        credConfirmLabel = New Label With {.Text = L("shell_cred_confirm"), .AutoSize = True, .Margin = Ui.PxPad(Me, 0, 6, 0, 2)}
        credRow.Controls.Add(credConfirmLabel, 0, 2)
        credRow.SetColumnSpan(credConfirmLabel, 3)

        credConfirmBox = New TextBox With {.Dock = DockStyle.Fill, .UseSystemPasswordChar = True, .Margin = Ui.PxPad(Me, 0, 2, 8, 4)}
        credConfirmBox.AccessibleName = L("shell_cred_confirm")
        AddHandler credConfirmBox.TextChanged, Sub() CredentialChanged()
        credRow.Controls.Add(credConfirmBox, 0, 3)

        credNoticeLabel = New Label With {.Text = L("shell_cred_empty"), .AutoSize = True, .Margin = Ui.PxPad(Me, 0, 4, 0, 2)}
        credRow.Controls.Add(credNoticeLabel, 0, 4)
        credRow.SetColumnSpan(credNoticeLabel, 3)
    End Sub

    ' The dispositions - what happens to the original, to the container, and
    ' to the revealed copy. One control per CLI option, in the CLI's own
    ' words, so that the command line in the plan card below reads as the
    ' answer to what was just clicked.
    Private Sub BuildFdsecOptions()
        fdsecRow = New FlowLayoutPanel With {
            .Dock = DockStyle.Top,
            .AutoSize = True,
            .AutoSizeMode = AutoSizeMode.GrowAndShrink,
            .FlowDirection = FlowDirection.TopDown,
            .WrapContents = False,
            .Margin = Ui.PxPad(Me, 0, 4, 0, 0),
            .Visible = False
        }

        fdsecOriginalLabel = New Label With {.Text = L("shell_fdsec_original"), .AutoSize = True, .Margin = Ui.PxPad(Me, 0, 6, 0, 2)}
        fdsecKeepRadio = New RadioButton With {.Text = L("shell_fdsec_keep"), .AutoSize = True, .Checked = True, .Margin = Ui.PxPad(Me, 0, 2, 0, 2)}
        fdsecDelRadio = New RadioButton With {.Text = L("shell_fdsec_del"), .AutoSize = True, .Margin = Ui.PxPad(Me, 0, 2, 0, 2)}
        fdsecWipeRadio = New RadioButton With {.Text = L("shell_fdsec_wipe"), .AutoSize = True, .Margin = Ui.PxPad(Me, 0, 2, 0, 2)}
        For Each r As RadioButton In New RadioButton() {fdsecKeepRadio, fdsecDelRadio, fdsecWipeRadio}
            AddHandler r.CheckedChanged, Sub() FdsecOptionChanged()
        Next

        fdsecRenameCheck = New CheckBox With {.Text = L("shell_fdsec_rename"), .AutoSize = True, .Margin = Ui.PxPad(Me, 0, 6, 0, 2)}
        AddHandler fdsecRenameCheck.CheckedChanged, Sub() FdsecOptionChanged()

        fdsecDelContainerCheck = New CheckBox With {.Text = L("shell_fdsec_del_container"), .AutoSize = True, .Margin = Ui.PxPad(Me, 0, 6, 0, 2)}
        AddHandler fdsecDelContainerCheck.CheckedChanged, Sub() FdsecOptionChanged()

        fdsecKeepCopyCheck = New CheckBox With {.Text = L("shell_fdsec_keep_copy"), .AutoSize = True, .Margin = Ui.PxPad(Me, 0, 6, 0, 2)}
        AddHandler fdsecKeepCopyCheck.CheckedChanged, Sub() FdsecOptionChanged()

        fdsecRwCheck = New CheckBox With {.Text = L("shell_fdsec_rw"), .AutoSize = True, .Margin = Ui.PxPad(Me, 0, 2, 0, 2)}
        AddHandler fdsecRwCheck.CheckedChanged, Sub() FdsecOptionChanged()

        fdsecNoticeLabel = New Label With {.Text = L("shell_fdsec_reveal_where"), .AutoSize = True, .Margin = Ui.PxPad(Me, 0, 8, 0, 2)}

        fdsecRow.Controls.Add(fdsecOriginalLabel)
        fdsecRow.Controls.Add(fdsecKeepRadio)
        fdsecRow.Controls.Add(fdsecDelRadio)
        fdsecRow.Controls.Add(fdsecWipeRadio)
        fdsecRow.Controls.Add(fdsecRenameCheck)
        fdsecRow.Controls.Add(fdsecDelContainerCheck)
        fdsecRow.Controls.Add(fdsecKeepCopyCheck)
        fdsecRow.Controls.Add(fdsecRwCheck)
        fdsecRow.Controls.Add(fdsecNoticeLabel)

        credHintLabel = New Label With {.Text = L("shell_cred_out_of_sight"), .AutoSize = True, .Margin = Ui.PxPad(Me, 0, 6, 0, 0)}
        fdsecRow.Controls.Add(credHintLabel)
    End Sub

    Private Sub BuildPlanCard()
        planCard = NewCard()

        Dim table = NewTable(1, 9)
        table.ColumnStyles.Add(New ColumnStyle(SizeType.Percent, 100.0F))

        planHeader = Ui.StepHeader(Me, L("shell_step4"))
        intentLabel = New Label With {.AutoSize = True, .Margin = Ui.PxPad(Me, 0, 0, 0, 6)}
        touchesLabel = New Label With {.AutoSize = True, .Margin = Ui.PxPad(Me, 0, 0, 0, 6)}

        Dim badgeRow As New FlowLayoutPanel With {
            .AutoSize = True,
            .AutoSizeMode = AutoSizeMode.GrowAndShrink,
            .WrapContents = True,
            .Dock = DockStyle.Top,
            .Margin = Ui.PxPad(Me, 0, 0, 0, 6)
        }
        reversibilityBadge = New Label With {.AutoSize = True, .Padding = Ui.PxPad(Me, 8, 3, 8, 3), .Margin = Ui.PxPad(Me, 0, 0, 8, 0)}
        destructiveBadge = New Label With {.AutoSize = True, .Padding = Ui.PxPad(Me, 8, 3, 8, 3), .Margin = Ui.PxPad(Me, 0, 0, 8, 0), .Visible = False}
        elevationBadge = New Label With {.AutoSize = True, .Padding = Ui.PxPad(Me, 8, 3, 8, 3), .Margin = Ui.PxPad(Me, 0, 0, 8, 0), .Visible = False}
        badgeRow.Controls.Add(reversibilityBadge)
        badgeRow.Controls.Add(destructiveBadge)
        badgeRow.Controls.Add(elevationBadge)

        redirectNoticeLabel = New Label With {
            .Text = L("shell_redirect_notice"),
            .AutoSize = True,
            .Margin = Ui.PxPad(Me, 0, 0, 0, 6),
            .Visible = False
        }

        commandLabel = New Label With {.Text = L("shell_lbl_command"), .AutoSize = True, .Margin = Ui.PxPad(Me, 0, 4, 0, 2)}
        commandBox = New TextBox With {
            .Dock = DockStyle.Fill,
            .ReadOnly = True,
            .Multiline = True,
            .WordWrap = True,
            .ScrollBars = ScrollBars.Vertical,
            .Height = Ui.Px(Me, 46),
            .Margin = Ui.PxPad(Me, 0, 0, 0, 8)
        }
        commandBox.AccessibleName = L("shell_lbl_command")

        Dim btnRow As New FlowLayoutPanel With {
            .AutoSize = True,
            .AutoSizeMode = AutoSizeMode.GrowAndShrink,
            .WrapContents = True,
            .Dock = DockStyle.Top,
            .Margin = Ui.PxPad(Me, 0, 4, 0, 0)
        }
        startBtn = New Button With {
            .Text = L("shell_btn_run_cmd"),
            .AutoSize = True,
            .AutoSizeMode = AutoSizeMode.GrowAndShrink,
            .Padding = Ui.PxPad(Me, 18, 7, 18, 7),
            .Margin = Ui.PxPad(Me, 0, 0, 10, 0)
        }
        AddHandler startBtn.Click, AddressOf StartBtn_Click
        AddHandler startBtn.EnabledChanged, Sub(s, e) StyleStartBtn()

        copyCmdBtn = New Button With {.Text = L("shell_btn_copy_cmd"), .AutoSize = True, .AutoSizeMode = AutoSizeMode.GrowAndShrink, .Margin = Ui.PxPad(Me, 0, 0, 8, 0)}
        AddHandler copyCmdBtn.Click, AddressOf CopyCmdBtn_Click
        secondaryButtons.Add(copyCmdBtn)

        openInCmdBtn = New Button With {.Text = L("shell_btn_open_in_cmd"), .AutoSize = True, .AutoSizeMode = AutoSizeMode.GrowAndShrink, .Margin = Ui.PxPad(Me, 0, 0, 8, 0)}
        AddHandler openInCmdBtn.Click, AddressOf OpenInCmdBtn_Click
        secondaryButtons.Add(openInCmdBtn)

        btnRow.Controls.Add(startBtn)
        btnRow.Controls.Add(copyCmdBtn)
        btnRow.Controls.Add(openInCmdBtn)

        runBlockedLabel = New Label With {
            .Text = "",
            .AutoSize = True,
            .Margin = Ui.PxPad(Me, 0, 0, 0, 6),
            .Visible = False
        }

        table.Controls.Add(planHeader, 0, 0)
        table.Controls.Add(intentLabel, 0, 1)
        table.Controls.Add(touchesLabel, 0, 2)
        table.Controls.Add(badgeRow, 0, 3)
        table.Controls.Add(redirectNoticeLabel, 0, 4)
        table.Controls.Add(runBlockedLabel, 0, 5)
        table.Controls.Add(commandLabel, 0, 6)
        table.Controls.Add(commandBox, 0, 7)
        table.Controls.Add(btnRow, 0, 8)

        planCard.Controls.Add(table)

        Ui.Wrap(intentLabel, planCard, Ui.Px(Me, 36))
        Ui.Wrap(touchesLabel, planCard, Ui.Px(Me, 36))
        Ui.Wrap(redirectNoticeLabel, planCard, Ui.Px(Me, 36))
        Ui.Wrap(runBlockedLabel, planCard, Ui.Px(Me, 36))
    End Sub

    Private Sub BuildRunStrip()
        runStrip = NewCard()
        runStrip.Visible = False

        Dim table = NewTable(2, 4)
        table.ColumnStyles.Add(New ColumnStyle(SizeType.Percent, 100.0F))
        table.ColumnStyles.Add(New ColumnStyle(SizeType.AutoSize))

        Dim headerRow As New FlowLayoutPanel With {
            .AutoSize = True,
            .AutoSizeMode = AutoSizeMode.GrowAndShrink,
            .WrapContents = True,
            .Dock = DockStyle.Top,
            .Margin = Ui.PxPad(Me, 0, 0, 0, 6)
        }
        stateBadge = New Label With {.AutoSize = True, .Padding = Ui.PxPad(Me, 8, 3, 8, 3), .Margin = Ui.PxPad(Me, 0, 0, 8, 0)}
        stepLabel = New Label With {.AutoSize = True, .Margin = Ui.PxPad(Me, 0, 4, 0, 0)}
        headerRow.Controls.Add(stateBadge)
        headerRow.Controls.Add(stepLabel)

        progressBar = New ProgressBar With {.Dock = DockStyle.Fill, .Height = Ui.Px(Me, 18), .Margin = Ui.PxPad(Me, 0, 4, 8, 6)}
        progressBar.AccessibleName = L("shell_state_running")

        stopBtn = New Button With {.Text = L("shell_btn_stop"), .AutoSize = True, .AutoSizeMode = AutoSizeMode.GrowAndShrink, .Margin = Ui.PxPad(Me, 0, 0, 0, 6)}
        AddHandler stopBtn.Click, AddressOf StopBtn_Click

        Dim metaRow As New FlowLayoutPanel With {
            .AutoSize = True,
            .AutoSizeMode = AutoSizeMode.GrowAndShrink,
            .WrapContents = True,
            .Dock = DockStyle.Top,
            .Margin = Ui.PxPad(Me, 0, 0, 0, 4)
        }
        throughputLabel = New Label With {.AutoSize = True, .Margin = Ui.PxPad(Me, 0, 0, 16, 0)}
        timeLabel = New Label With {.AutoSize = True, .Margin = Ui.PxPad(Me, 0, 0, 16, 0)}
        metaRow.Controls.Add(throughputLabel)
        metaRow.Controls.Add(timeLabel)

        toggleOutputBtn = New Button With {.Text = L("shell_btn_show_output"), .AutoSize = True, .AutoSizeMode = AutoSizeMode.GrowAndShrink}
        AddHandler toggleOutputBtn.Click, AddressOf ToggleOutputBtn_Click
        secondaryButtons.Add(toggleOutputBtn)

        table.Controls.Add(headerRow, 0, 0)
        table.SetColumnSpan(headerRow, 2)
        table.Controls.Add(progressBar, 0, 1)
        table.Controls.Add(stopBtn, 1, 1)
        table.Controls.Add(metaRow, 0, 2)
        table.Controls.Add(toggleOutputBtn, 0, 3)

        runStrip.Controls.Add(table)
        Ui.Wrap(stepLabel, runStrip, Ui.Px(Me, 140))
    End Sub

    Private Sub BuildResultCard()
        resultCard = NewCard()
        resultCard.Visible = False

        Dim table = NewTable(1, 6)
        table.ColumnStyles.Add(New ColumnStyle(SizeType.Percent, 100.0F))

        Dim vRow As New FlowLayoutPanel With {
            .AutoSize = True,
            .AutoSizeMode = AutoSizeMode.GrowAndShrink,
            .WrapContents = True,
            .Dock = DockStyle.Top,
            .Margin = Ui.PxPad(Me, 0, 0, 0, 8)
        }
        ' The glyph is decoration beside the badge, which carries the verdict's word: it has no
        ' accessible role, so a screen reader reads the verdict once, not a glyph and then the word
        ' (APP-BEHAVIOUR rule 9).
        verdictGlyph = New Label With {.AutoSize = True, .Margin = Ui.PxPad(Me, 0, 4, 6, 0)}
        verdictGlyph.AccessibleRole = AccessibleRole.None
        verdictBadge = New Label With {.AutoSize = True, .Padding = Ui.PxPad(Me, 10, 4, 10, 4)}
        vRow.Controls.Add(verdictGlyph)
        vRow.Controls.Add(verdictBadge)

        verdictReasonLabel = New Label With {.AutoSize = True, .Margin = Ui.PxPad(Me, 0, 0, 0, 8), .Visible = False}
        resultNumbersLabel = New Label With {.AutoSize = True, .Margin = Ui.PxPad(Me, 0, 0, 0, 6)}
        filesLeftLabel = New Label With {.AutoSize = True, .Margin = Ui.PxPad(Me, 0, 0, 0, 6)}
        reportsLabel = New Label With {.AutoSize = True, .Margin = Ui.PxPad(Me, 0, 0, 0, 8)}

        Dim actRow As New FlowLayoutPanel With {
            .AutoSize = True,
            .AutoSizeMode = AutoSizeMode.GrowAndShrink,
            .WrapContents = True,
            .Dock = DockStyle.Top
        }
        actionCleanBtn = New Button With {.Text = L("shell_btn_clean_files"), .AutoSize = True, .AutoSizeMode = AutoSizeMode.GrowAndShrink, .Margin = Ui.PxPad(Me, 0, 0, 8, 0), .Visible = False}
        AddHandler actionCleanBtn.Click, AddressOf CleanTestFiles_Click
        secondaryButtons.Add(actionCleanBtn)

        actionOpenReportBtn = New Button With {.Text = L("shell_btn_open_report"), .AutoSize = True, .AutoSizeMode = AutoSizeMode.GrowAndShrink, .Margin = Ui.PxPad(Me, 0, 0, 8, 0), .Visible = False}
        AddHandler actionOpenReportBtn.Click, AddressOf OpenLatestReport_Click
        secondaryButtons.Add(actionOpenReportBtn)

        actionRunAgainBtn = New Button With {.Text = L("shell_btn_run_again"), .AutoSize = True, .AutoSizeMode = AutoSizeMode.GrowAndShrink, .Margin = Ui.PxPad(Me, 0, 0, 8, 0)}
        AddHandler actionRunAgainBtn.Click, Sub() ResetView()
        secondaryButtons.Add(actionRunAgainBtn)

        actRow.Controls.Add(actionCleanBtn)
        actRow.Controls.Add(actionOpenReportBtn)
        actRow.Controls.Add(actionRunAgainBtn)

        table.Controls.Add(vRow, 0, 0)
        table.Controls.Add(verdictReasonLabel, 0, 1)
        table.Controls.Add(resultNumbersLabel, 0, 2)
        table.Controls.Add(filesLeftLabel, 0, 3)
        table.Controls.Add(reportsLabel, 0, 4)
        table.Controls.Add(actRow, 0, 5)

        resultCard.Controls.Add(table)

        Ui.Wrap(verdictReasonLabel, resultCard, Ui.Px(Me, 36))
        Ui.Wrap(resultNumbersLabel, resultCard, Ui.Px(Me, 36))
        Ui.Wrap(filesLeftLabel, resultCard, Ui.Px(Me, 36))
        Ui.Wrap(reportsLabel, resultCard, Ui.Px(Me, 36))
    End Sub

    Private Sub BuildRawOutputDrawer()
        rawOutputDrawer = NewCard()
        rawOutputDrawer.Visible = False

        Dim table = NewTable(1, 2)
        table.ColumnStyles.Add(New ColumnStyle(SizeType.Percent, 100.0F))

        rawOutputBox = New TextBox With {
            .Dock = DockStyle.Fill,
            .Multiline = True,
            .ReadOnly = True,
            .ScrollBars = ScrollBars.Both,
            .Height = Ui.Px(Me, 200),
            .Margin = Ui.PxPad(Me, 0, 0, 0, 8)
        }
        rawOutputBox.AccessibleName = L("shell_btn_show_output")

        copyOutputBtn = New Button With {.Text = L("shell_btn_copy_output"), .AutoSize = True, .AutoSizeMode = AutoSizeMode.GrowAndShrink}
        AddHandler copyOutputBtn.Click, Sub() Ui.CopyText(ShellDialog.OwnerOf(Me), rawOutputBox.Text)
        secondaryButtons.Add(copyOutputBtn)

        table.Controls.Add(rawOutputBox, 0, 0)
        table.Controls.Add(copyOutputBtn, 0, 1)

        rawOutputDrawer.Controls.Add(table)
    End Sub

    ' ---- the runner ------------------------------------------------------

    Private Sub HookRunnerEvents()
        AddHandler runner.OutputLineReceived,
            Sub(line, isErr)
                PostToUi(Sub()
                                rawOutputBox.AppendText(line & Environment.NewLine)
                            End Sub)
            End Sub

        AddHandler runner.StepChanged,
            Sub(name, desc)
                PostToUi(Sub()
                                stepLabel.Text = If(String.IsNullOrEmpty(desc), name, name & ": " & desc)
                            End Sub)
            End Sub

        AddHandler runner.ProgressReported,
            Sub(p)
                PostToUi(Sub() OnProgress(p))
            End Sub

        AddHandler runner.FindingReported,
            Sub(fType, msg, details)
                PostToUi(Sub()
                                rawOutputBox.AppendText("[" & fType & "] " & msg & Environment.NewLine)
                            End Sub)
            End Sub

        AddHandler runner.NoteReported,
            Sub(msg)
                PostToUi(Sub()
                                rawOutputBox.AppendText("[note] " & msg & Environment.NewLine)
                            End Sub)
            End Sub
    End Sub

    ' One progress event, drawn by the rule both run pages share (RunProgress, T6).
    Private Sub OnProgress(p As EventStream.ProgressInfo)
        RunProgress.Apply(progressBar, p)

        If p.SpeedBps > 0 Then
            Dim mbps = p.SpeedBps / (1024.0 * 1024.0)
            throughputLabel.Text = mbps.ToString("F1") & " MB/s"
        End If

        If Not String.IsNullOrEmpty(p.Message) Then
            stepLabel.Text = p.Message
        End If
    End Sub

    ' ---- seams for SelfTest.vb -------------------------------------------

    Friend Sub FeedProgressForTest(p As EventStream.ProgressInfo)
        OnProgress(p)
    End Sub

    Friend ReadOnly Property ProgressBarForTest As ProgressBar
        Get
            Return progressBar
        End Get
    End Property

    Friend Sub ShowResultForTest(verdict As String)
        ShowResultCard(New Runner.RunResult With {.Verdict = verdict, .ExitCode = 1, .Duration = TimeSpan.Zero})
    End Sub

    Friend ReadOnly Property VerdictBadgeForTest As Label
        Get
            Return verdictBadge
        End Get
    End Property

    ' The plan card's reason line and whether Run is offered, for the Wipe page's rules (T10, T11).
    Friend Function RunStateForTest(ByRef reason As String) As Boolean
        reason = If(runBlockedLabel.Visible, runBlockedLabel.Text, "")
        Return startBtn.Enabled
    End Function

    Friend Sub SetWipeInputsForTest(typed As String, yes As Boolean)
        optionsCheckForce.Checked = yes
        wipeConfirmBox.Text = typed
        UpdatePlanCard()
    End Sub

    ' A finished count, as StartBlastRadiusCount would report it.
    Friend Sub SetCountForTest(files As Long, folders As Long)
        If countingTokenSource IsNot Nothing Then
            countingTokenSource.Cancel()
            countingTokenSource = Nothing
        End If
        CountFinished(files, folders, 0)
    End Sub

    ' ---- step 2 - the target ---------------------------------------------

    ' Drives carry their label, file system, size and free space on the row, because a letter alone
    ' is not enough to tell two external disks apart - and picking the wrong one is the whole risk
    ' the Erase group is about (SP-0006 section 5.3).
    Private Sub PopulateTargets()
        targetCombo.Items.Clear()
        If job Is Nothing Then Return
        If job.TargetKind <> JobDefinition.TargetType.Drive Then
            targetCombo.Text = ""
            Return
        End If

        Try
            For Each d In DriveInfo.GetDrives()
                If d.IsReady Then
                    Dim sizeGb = d.TotalSize / (1024.0 * 1024.0 * 1024.0)
                    Dim freeGb = d.TotalFreeSpace / (1024.0 * 1024.0 * 1024.0)
                    Dim label = If(String.IsNullOrEmpty(d.VolumeLabel), d.DriveType.ToString(), d.VolumeLabel)
                    targetCombo.Items.Add(Localization.Format(L("shell_drive_row_fmt"),
                                                              d.Name, label, d.DriveFormat, sizeGb, freeGb))
                Else
                    targetCombo.Items.Add(d.Name)
                End If
            Next
        Catch ex As Exception
            ' A drive that vanishes while it is listed; the list keeps what it had, and the box
            ' still takes a typed path.
            ShellLog.Write("list drives", ex)
        End Try

        If targetCombo.Items.Count > 0 Then targetCombo.SelectedIndex = 0
    End Sub

    Private Function GetRawTarget() As String
        Dim text = targetCombo.Text.Trim()
        If String.IsNullOrEmpty(text) Then Return ""
        ' A drive row reads "E:\ (label, NTFS, ..)" - the command wants the first two characters.
        If text.Length >= 3 AndAlso text(1) = ":"c AndAlso text.Contains("(") Then
            Return text.Substring(0, 2)
        End If
        Return text
    End Function

    Private Sub TargetChanged()
        UpdatePlanCard()
        If job Is Nothing OrElse Not job.IsDestructive Then Return
        ' A location this page will not wipe (a drive root, TEMP..) is not counted either: counting
        ' a whole drive to tell the user a number for a run that is not offered is work for nothing.
        If job.DefaultVerb = "wipe" AndAlso WipeSafety.DangerKey(GetRawTarget()) <> "" Then
            If countingTokenSource IsNot Nothing Then
                countingTokenSource.Cancel()
                countingTokenSource = Nothing
            End If
            countedFiles = -1
            countedFolders = -1
            blastRadiusRow.Visible = False
            Return
        End If
        StartBlastRadiusCount()
    End Sub

    ' Dropping a file or a folder on the page answers step 2 in one gesture (section 5.3).
    Private Sub JobView_DragEnter(sender As Object, e As DragEventArgs)
        If e.Data.GetDataPresent(DataFormats.FileDrop) Then
            e.Effect = DragDropEffects.Link
        Else
            e.Effect = DragDropEffects.None
        End If
    End Sub

    Private Sub JobView_DragDrop(sender As Object, e As DragEventArgs)
        Try
            Dim paths = TryCast(e.Data.GetData(DataFormats.FileDrop), String())
            If paths Is Nothing OrElse paths.Length = 0 Then Return
            targetCombo.Text = paths(0)
            If paths.Length > 1 AndAlso secondTargetRow.Visible Then secondTargetBox.Text = paths(1)
        Catch
        End Try
    End Sub

    ' ---- step 3 - the parameters -----------------------------------------

    Private Sub StartBlastRadiusCount()
        If countingTokenSource IsNot Nothing Then
            countingTokenSource.Cancel()
        End If

        countingTokenSource = New CancellationTokenSource()
        Dim token = countingTokenSource.Token
        Dim target = GetRawTarget()

        countedFiles = -1
        countedFolders = -1
        blastRadiusRow.Visible = True
        UpdateParamsVisibility()
        blastRadiusLabel.Text = L("shell_blast_counting")
        UpdatePlanCard()

        ' Files and folders both: a folder holding only empty folders is not "nothing to delete",
        ' and saying so would let it be wiped without the typed WIPE (T10).
        Task.Run(
            Sub()
                Try
                    If Not Directory.Exists(target) Then
                        PostToUi(Sub() CountNotFinished(token))
                        Return
                    End If

                    Dim files As Long = 0
                    Dim folders As Long = 0
                    Dim bytes As Long = 0
                    Dim di As New DirectoryInfo(target)
                    For Each entry In di.EnumerateFileSystemInfos("*", SearchOption.AllDirectories)
                        If token.IsCancellationRequested Then Return
                        Dim fi = TryCast(entry, FileInfo)
                        If fi IsNot Nothing Then
                            files += 1
                            bytes += fi.Length
                        Else
                            folders += 1
                        End If
                    Next

                    PostToUi(Sub()
                                 If Not token.IsCancellationRequested Then CountFinished(files, folders, bytes)
                             End Sub)
                Catch ex As Exception
                    ' A folder the user may not read (a drive's Recycle Bin) is an ordinary answer here:
                    ' the page says "not counted", and the detail is for a -debug log only.
                    ShellLog.Debug("count the target: " & ex.GetType().Name)
                    PostToUi(Sub() CountNotFinished(token))
                End Try
            End Sub, token)
    End Sub

    ' Hands work from another thread - the runner's events, the target count - to the page. A page
    ' that has no window yet (the self-test builds pages it never shows) or no longer has one (the
    ' window closed while Windows was shutting down) simply does not get it: an event posted to a
    ' destroyed control would end the process.
    Private Sub PostToUi(work As Action)
        If IsDisposed OrElse Not IsHandleCreated Then Return
        Try
            BeginInvoke(work)
        Catch ex As InvalidOperationException
            ' The window closed between the check and the call; the count has nobody to tell.
        End Try
    End Sub

    Private Sub CountFinished(files As Long, folders As Long, bytes As Long)
        countedFiles = files
        countedFolders = folders
        If files = 0 AndAlso folders = 0 Then
            blastRadiusLabel.Text = L("shell_blast_nothing")
        Else
            blastRadiusLabel.Text = Localization.Format(L("shell_blast_result_fmt"), files, bytes / (1024.0 * 1024.0), folders)
        End If
        UpdatePlanCard()
    End Sub

    Private Sub CountNotFinished(token As CancellationToken)
        If token.IsCancellationRequested Then Return
        countedFiles = -1
        countedFolders = -1
        blastRadiusLabel.Text = L("shell_blast_not_counted")
        UpdatePlanCard()
    End Sub

    ' A wipe target whose count finished at no files and no folders: there is nothing to confirm and
    ' nothing to run (APP-BEHAVIOUR rule 5). A target not counted, or not yet counted, keeps the
    ' typed WIPE exactly as before (SP-0006 section 8 item 1).
    Private Function WipeTargetIsEmpty() As Boolean
        Return job IsNot Nothing AndAlso job.DefaultVerb = "wipe" AndAlso countedFiles = 0 AndAlso countedFolders = 0
    End Function

    Private Sub SkipCounting_Click(sender As Object, e As EventArgs)
        If countingTokenSource IsNot Nothing Then
            countingTokenSource.Cancel()
            countingTokenSource = Nothing
        End If
        countedFiles = -1
        countedFolders = -1
        blastRadiusLabel.Text = L("shell_blast_not_counted")
        UpdatePlanCard()
    End Sub

    ' ---- step 4 - the plan -----------------------------------------------

    Private Sub UpdatePlanCard()
        If job Is Nothing Then Return

        Dim target = GetRawTarget()
        Dim isSysDrive = target.StartsWith("C:", StringComparison.OrdinalIgnoreCase)

        intentLabel.Text = L(job.PurposeKey)
        touchesLabel.Text = Localization.Format(L("shell_touches_fmt"), If(target = "", L("shell_target_none"), target))
        ' A secret-file page states the reversibility of what is actually
        ' about to happen, not of the job in the abstract: "secure and keep"
        ' is reversible, "secure and overwrite the original" is not, and the
        ' badge would be a lie in one of the two if it never moved.
        Dim reversibility = job.Reversibility
        If IsFdsec() Then
            If fdsecWipeRadio.Checked Then
                reversibility = "permanent"
            ElseIf FdsecIsDestructiveNow() Then
                reversibility = "partly reversible"
            End If
        End If
        reversibilityBadge.Text = L("shell_rev_" & reversibility.Replace(" ", "_"))

        destructiveBadge.Visible = job.IsDestructive OrElse FdsecIsDestructiveNow()
        destructiveBadge.Text = L("shell_badge_destructive")
        elevationBadge.Visible = job.NeedsElevation
        elevationBadge.Text = L("shell_badge_elevation")

        redirectNoticeLabel.Visible = isSysDrive AndAlso
            (job.DefaultVerb = "speed" OrElse job.DefaultVerb = "fill" OrElse job.DefaultVerb = "test")

        Dim cmdArgs = BuildCommandArgs()
        commandBox.Text = "filedo.exe " & ArgQuoting.JoinArgs(cmdArgs)

        startBtn.Text = Localization.Format(L("shell_btn_run_action"), L(job.LabelKey))
        tips.SetToolTip(startBtn, commandBox.Text)

        UpdateStartButtonState()
    End Sub

    ' The plan card's reason line: shown with the reason Run is not offered, when the reason is one
    ' the fields above do not already make plain.
    Private Sub ShowRunBlocked(reason As String)
        runBlockedLabel.Text = reason
        runBlockedLabel.Visible = (reason <> "")
    End Sub

    Private Sub UpdateStartButtonState()
        ShowRunBlocked("")
        If job Is Nothing Then
            startBtn.Enabled = False
            Return
        End If

        Dim target = GetRawTarget()
        If String.IsNullOrEmpty(target) Then
            startBtn.Enabled = False
            tips.SetToolTip(startBtn, L("shell_need_target"))
            If job.Id = "rail_job_wipe" Then wipeConfirmRow.Visible = True
            Return
        End If

        If job.TargetKind = JobDefinition.TargetType.SourceAndTarget AndAlso
           String.IsNullOrEmpty(secondTargetBox.Text.Trim()) Then
            startBtn.Enabled = False
            tips.SetToolTip(startBtn, L("shell_need_dest"))
            Return
        End If

        ' A parameter that the CLI would reject, or that names nothing, stops the run here rather
        ' than a minute later in the output box.
        If Not ParametersComplete() Then
            startBtn.Enabled = False
            Return
        End If

        ' The Wipe page, in the order its questions are answered.
        If job.Id = "rail_job_wipe" Then
            ' A drive or share root, a junction or the system TEMP folder is asked about twice on
            ' a console - WIPE, then the path - whatever -y says, and a run from this window has no
            ' console to answer on. It would end cancelled after the user had typed WIPE here, so
            ' it is not offered: the reason says where it can be done, and the command can be
            ' copied from the box below (APP-BEHAVIOUR rules 5 and 6; SP-0014 T11).
            Dim danger = WipeSafety.DangerKey(target)
            If danger <> "" Then
                startBtn.Enabled = False
                wipeConfirmRow.Visible = False
                ShowRunBlocked(Localization.Format(L("shell_wipe_needs_console"), L(danger)))
                tips.SetToolTip(startBtn, runBlockedLabel.Text)
                Return
            End If

            ' A folder counted empty has nothing to confirm (T10).
            If WipeTargetIsEmpty() Then
                startBtn.Enabled = False
                wipeConfirmRow.Visible = False
                ShowRunBlocked(L("shell_blast_nothing"))
                tips.SetToolTip(startBtn, runBlockedLabel.Text)
                Return
            End If
            wipeConfirmRow.Visible = True

            ' Typing WIPE stays typing WIPE. A checkbox is not equivalent and is not offered in its
            ' place (SP-0006 section 8 item 1). And -y is needed as well: filedo.exe asks for WIPE
            ' again on its console, which a run from this window does not have, so without -y it
            ' would end cancelled after the user had already typed the word here. The owner's call
            ' (SP-0014 D2) is to keep that console question and say so, rather than have the page
            ' pass -y on its own.
            If Not optionsCheckForce.Checked Then
                startBtn.Enabled = False
                ShowRunBlocked(L("shell_wipe_needs_y"))
                tips.SetToolTip(startBtn, runBlockedLabel.Text)
                Return
            End If
            startBtn.Enabled = (wipeConfirmBox.Text.Trim() = "WIPE")
            If Not startBtn.Enabled Then tips.SetToolTip(startBtn, L("shell_wipe_confirm_hint"))
            Return
        End If

        ' The drive the CLI would have made the user type on a console, typed here instead.
        If NeedsDriveConfirm() Then
            Dim typed = wipeConfirmBox.Text.Trim().TrimEnd("\"c)
            startBtn.Enabled = String.Equals(typed, target.TrimEnd("\"c), StringComparison.OrdinalIgnoreCase)
            If Not startBtn.Enabled Then tips.SetToolTip(startBtn, L("shell_probe_confirm_hint"))
            Return
        End If

        If IsFdsec() Then
            ' An empty password is allowed - it is a documented choice, not a
            ' mistake (R3), and the line above the button says what it buys.
            ' What is not allowed is a secure whose two boxes differ, or an
            ' overwrite that was never typed out.
            If job.DefaultVerb = "secure" AndAlso credConfirmBox.Text <> credBox.Text Then
                startBtn.Enabled = False
                tips.SetToolTip(startBtn, L("shell_cred_mismatch"))
                Return
            End If
            If FdsecNeedsTypedConfirm() AndAlso wipeConfirmBox.Text.Trim() <> "WIPE" Then
                startBtn.Enabled = False
                tips.SetToolTip(startBtn, L("shell_fdsec_wipe_confirm"))
                Return
            End If
            startBtn.Enabled = True
            Return
        End If

        startBtn.Enabled = True
    End Sub

    ' A flat button keeps its fill when disabled, so a run that is waiting on a field (the typed
    ' WIPE, the password pair) looked live and just ignored clicks. Disabled is drawn muted.
    Private Sub StyleStartBtn()
        If startBtn Is Nothing Then Return
        Dim p = Theme.Current
        If startBtn.Enabled Then
            Ui.StyleButton(startBtn, p.Accent, p.AccentText, p.Accent)
        Else
            Ui.StyleButton(startBtn, p.SurfaceAlt, p.TextDisabled, p.Border)
        End If
    End Sub

    ' True when step 3 has been answered well enough to run. Each failure sets the tooltip that
    ' says which field is the problem, because a disabled button with no reason on it is the
    ' complaint the shell exists to avoid.
    Private Function ParametersComplete() As Boolean
        If showsSize AndAlso sizeBox.Enabled Then
            Dim v = sizeBox.Text.Trim()
            If showsPresets AndAlso presetCustom.Checked AndAlso v = "" Then
                tips.SetToolTip(startBtn, L("shell_need_size"))
                Return False
            End If
            If v <> "" AndAlso Not IsNumeric(v) Then
                tips.SetToolTip(startBtn, L("shell_bad_number"))
                Return False
            End If
        End If

        If showsDup Then
            If dupActionMove.Checked AndAlso dupMoveBox.Text.Trim() = "" Then
                tips.SetToolTip(startBtn, L("shell_need_move_dir"))
                Return False
            End If
            If dupListCheck.Checked AndAlso dupListBox.Text.Trim() = "" Then
                tips.SetToolTip(startBtn, L("shell_need_list_file"))
                Return False
            End If
        End If

        If showsCheck AndAlso checkOptions.HasInvalidNumber() Then
            tips.SetToolTip(startBtn, L("shell_bad_number"))
            Return False
        End If

        Return True
    End Function

    Private Function BuildCommandArgs() As List(Of String)
        Dim args As New List(Of String)()
        Dim target = GetRawTarget()
        Dim hasTarget = Not String.IsNullOrEmpty(target)

        Select Case job.DefaultVerb
            Case "test", "speed", "fill"
                If hasTarget Then args.Add(target)
                args.Add(job.DefaultVerb)
                ' Quick, Thorough and Custom are the CLI's own size argument, written out: speed
                ' takes megabytes ("max" is the 10 GB file), test takes a file count, and fill
                ' takes the size of one test file. The number is always written, even when it is
                ' the default, so that a trailing option can never be read as the size.
                If job.DefaultVerb = "speed" Then
                    args.Add(SizeArgument("100", "max"))
                ElseIf job.DefaultVerb = "test" Then
                    args.Add(SizeArgument("100", "1000"))
                ElseIf optionsCheckVerify.Checked Then
                    ' `fill verify` checks what an earlier fill wrote; it takes nothing else.
                    args.Add("verify")
                Else
                    Dim mb = sizeBox.Text.Trim()
                    args.Add(If(mb = "", "100", mb))
                End If
                If optionsCheckAutoDel.Checked AndAlso Not optionsCheckVerify.Checked Then args.Add("del")
                If optionsCheckNoDel.Checked AndAlso job.DefaultVerb = "speed" Then args.Add("nodel")
                If optionsCheckShort.Checked AndAlso job.DefaultVerb = "speed" Then args.Add("short")

            Case "info"
                If hasTarget Then args.Add(target)
                ' `short` is not an option on top of `info` - it is the other word in its place
                ' (command_handlers.go), and writing both would ask for the long report.
                args.Add(If(optionsCheckShort.Checked, "short", "info"))

            Case "clean"
                If hasTarget Then args.Add(target)
                args.Add("clean")

            Case "probe"
                If hasTarget Then args.Add(target)
                args.Add("probe")
                ' `yes` says the console questions were answered here, on the page, by typing
                ' the drive out - not that they were skipped.
                args.Add("yes")
                If optionsCheckProbeFix.Checked Then args.Add("fix")

            Case "recover"
                If hasTarget Then args.Add(target)
                args.Add("recover")
                args.Add("yes")
                If optionsCheckForceFormat.Checked Then args.Add("format")

            Case "check"
                args.Add("check")
                If hasTarget Then args.Add(target)
                args.AddRange(checkOptions.ToArgs())

            Case "cd"
                If hasTarget Then args.Add(target)
                args.Add("cd")
                args.AddRange(DuplicateOptionArgs())

            Case "compare"
                args.Add("compare")
                If hasTarget Then args.Add(target)
                Dim dest = secondTargetBox.Text.Trim()
                If Not String.IsNullOrEmpty(dest) Then args.Add(dest)
                ' "del old target" is three words to the CLI, and it is three arguments here.
                Dim rule = CliRules.CmpRuleTokens(Math.Max(cmpRuleCombo.SelectedIndex, 0))
                If rule <> "" Then args.AddRange(rule.Split(" "c))

            Case "copy"
                args.Add(CliRules.CopyVerbs(Math.Max(copyStrategyCombo.SelectedIndex, 0)))
                If hasTarget Then args.Add(target)
                Dim dest = secondTargetBox.Text.Trim()
                If Not String.IsNullOrEmpty(dest) Then args.Add(dest)

            Case "wipe"
                If hasTarget Then args.Add(target)
                args.Add("wipe")
                If optionsCheckForce.Checked Then args.Add("-y")

            Case "secure", "unsecure", "reveal"
                If hasTarget Then args.Add(target)
                args.Add(job.DefaultVerb)
                args.AddRange(FdsecOptionArgs())
                ' The credential goes by name, never by value: pe:VAR tells
                ' filedo.exe to read it out of the environment this window
                ' gives the child process. What lands in the command box, in
                ' the run report and in history.json is therefore this word -
                ' the password itself is on no command line, where the process
                ' list of every process of this user could read it (SP-0005
                ' 5.4, 12).
                args.Add("pe:" & CredentialEnvName)
        End Select

        ' `nohist` is read wherever it appears in the line (main.go), and it is the one option
        ' every job shares: it keeps this run out of history.json.
        If Not IsFdsec() AndAlso optionsCheckNoHist.Checked Then args.Add("nohist")

        Return args
    End Function

    ' The size the run will use: the custom field when it was chosen and filled in, and the
    ' preset's own number otherwise.
    Private Function SizeArgument(quick As String, thorough As String) As String
        If presetCustom.Checked Then
            Dim v = sizeBox.Text.Trim()
            If v <> "" Then Return v
        End If
        Return If(presetThorough.Checked, thorough, quick)
    End Function

    ' The duplicate scan's options, in the order the CLI documents them. Order does not matter to
    ' fileduplicates.ParseArguments, but a command line that reads the same way twice does.
    Private Function DuplicateOptionArgs() As List(Of String)
        Dim args As New List(Of String)()

        Dim rule = CliRules.DupRuleTokens(Math.Max(dupRuleCombo.SelectedIndex, 0))
        If rule <> "" Then args.Add(rule)

        If dupActionDelete.Checked Then
            args.Add("del")
        ElseIf dupActionMove.Checked Then
            Dim dir = dupMoveBox.Text.Trim()
            If dir <> "" Then
                args.Add("move")
                args.Add(dir)
            End If
        End If

        If dupListCheck.Checked Then
            Dim path = dupListBox.Text.Trim()
            If path <> "" Then
                args.Add("list")
                args.Add(path)
            End If
        End If

        If dupQuietCheck.Checked Then args.Add("quiet")
        Return args
    End Function

    ' CredentialEnvName is the variable the password travels in. It is set on
    ' the child process alone and dies with it.
    Private Const CredentialEnvName As String = "FILEDO_SHELL_CRED"

    ' One control, one CLI option, in the order the CLI documents them.
    Private Function FdsecOptionArgs() As List(Of String)
        Dim args As New List(Of String)()
        If Not IsFdsec() Then Return args

        Select Case job.DefaultVerb
            Case "secure"
                If fdsecDelRadio.Checked Then args.Add("del")
                If fdsecWipeRadio.Checked Then args.Add("wipe")
                If fdsecRenameCheck.Checked Then args.Add("rename")
                Dim dest = secondTargetBox.Text.Trim()
                If Not fdsecRenameCheck.Checked AndAlso dest <> "" Then
                    args.Add("to")
                    args.Add(dest)
                End If

            Case "unsecure"
                If fdsecDelContainerCheck.Checked Then args.Add("del")
                Dim dest = secondTargetBox.Text.Trim()
                If dest <> "" Then
                    args.Add("to")
                    args.Add(dest)
                End If

            Case "reveal"
                If fdsecRwCheck.Checked Then args.Add("-rw")
                If fdsecKeepCopyCheck.Checked Then args.Add("-keep")
        End Select

        ' -y skips the console prompt and nothing else: the read-back verify,
        ' the overwrite guard and every other safety check still run (hard
        ' invariant 9). The prompt is skipped because it cannot be answered -
        ' this run has no console - and because the question was already asked
        ' here, in a window, before anything was touched.
        If FdsecIsDestructiveNow() Then args.Add("-y")
        Return args
    End Function

    ' ---- buttons ---------------------------------------------------------

    Private Sub BrowseTarget_Click(sender As Object, e As EventArgs)
        If job IsNot Nothing AndAlso job.TargetKind = JobDefinition.TargetType.File Then
            Using dlg As New OpenFileDialog()
                dlg.Title = L("shell_dlg_select_file")
                If dlg.ShowDialog(FindForm()) = DialogResult.OK Then targetCombo.Text = dlg.FileName
            End Using
            Return
        End If

        Using dlg As New FolderBrowserDialog()
            dlg.Description = L("shell_dlg_select_folder")
            If dlg.ShowDialog(FindForm()) = DialogResult.OK Then targetCombo.Text = dlg.SelectedPath
        End Using
    End Sub

    Private Sub BrowseSecondTarget_Click(sender As Object, e As EventArgs)
        Using dlg As New FolderBrowserDialog()
            dlg.Description = L("shell_dlg_select_dest")
            If dlg.ShowDialog(FindForm()) = DialogResult.OK Then secondTargetBox.Text = dlg.SelectedPath
        End Using
    End Sub

    Private Sub CopyCmdBtn_Click(sender As Object, e As EventArgs)
        Ui.CopyText(ShellDialog.OwnerOf(Me), commandBox.Text)
    End Sub

    Private Sub OpenInCmdBtn_Click(sender As Object, e As EventArgs)
        RaiseEvent OpenInCommandRequested(commandBox.Text)
    End Sub

    Private Async Sub StartBtn_Click(sender As Object, e As EventArgs)
        If runner.IsActive Then Return

        targetCard.Visible = False
        paramsCard.Visible = False
        planCard.Visible = False
        runStrip.Visible = True
        resultCard.Visible = False

        stateBadge.Text = L("shell_state_running")
        stepLabel.Text = L("shell_step_starting")
        RunProgress.Begin(progressBar)
        stopBtn.Enabled = True

        ' During a reveal the strip is not a progress bar with a cancel button
        ' on it: the run lasts exactly as long as the plaintext copy exists,
        ' and this button is the visible "remove it now" that SP-0005 8.2
        ' makes load-bearing. A reveal started from a window has no console to
        ' press Enter in, so without the button on screen nothing would end
        ' the copy's life but the next FileDO start.
        Dim revealing = (IsFdsec() AndAlso job.DefaultVerb = "reveal" AndAlso Not fdsecRwCheck.Checked)
        stopBtn.Text = If(revealing, L("shell_btn_remove_copy"), L("shell_btn_stop"))
        If revealing Then stepLabel.Text = L("shell_fdsec_reveal_running")

        Dim startTime = DateTime.Now
        runTimer = New Windows.Forms.Timer With {.Interval = 500}
        AddHandler runTimer.Tick, Sub()
                                      Dim el = DateTime.Now - startTime
                                      timeLabel.Text = L("shell_elapsed") & ": " & el.ToString("mm\:ss")
                                  End Sub
        runTimer.Start()

        Dim args = BuildCommandArgs()
        Dim env As Dictionary(Of String, String) = Nothing
        If IsFdsec() Then
            env = New Dictionary(Of String, String) From {{CredentialEnvName, credBox.Text}}
        End If
        Dim res = Await runner.ExecuteAsync(args, envVars:=env, elevate:=job.NeedsElevation)

        runTimer.Stop()
        runTimer.Dispose()
        runTimer = Nothing

        ShowResultCard(res)
        ' Another view was in front when the run ended: the result waits on this page until its
        ' row is chosen again, rather than being reset away.
        resultUnseen = Not Visible
        RaiseEvent RunFinished()
    End Sub

    ' Stop is a request, not a kill: the CLI is asked to end the way Ctrl+C ends it, so its own
    ' cleanup still runs (SP-0006 section 6.2 and 7.5).
    Private Sub StopBtn_Click(sender As Object, e As EventArgs)
        runner.RequestStop()
        stateBadge.Text = L("shell_state_stopping")
        ' The same channel says two different things depending on what is
        ' running: end this operation cleanly, or - for a reveal - the copy
        ' can go now. filedo.exe reads the stop file either way.
        Dim revealing = (IsFdsec() AndAlso job.DefaultVerb = "reveal")
        stepLabel.Text = If(revealing, L("shell_fdsec_removing_copy"), L("shell_stop_requested"))
        stopBtn.Enabled = False
    End Sub

    Private Sub ToggleOutputBtn_Click(sender As Object, e As EventArgs)
        rawOutputDrawer.Visible = Not rawOutputDrawer.Visible
        toggleOutputBtn.Text = If(rawOutputDrawer.Visible, L("shell_btn_hide_output"), L("shell_btn_show_output"))
    End Sub

    ' ---- the result ------------------------------------------------------

    Private Sub ShowResultCard(res As Runner.RunResult)
        runStrip.Visible = False
        resultCard.Visible = True

        verdictBadge.Text = L("shell_verdict_" & res.Verdict.ToLowerInvariant().Replace(" ", "_"))

        ' Every verdict carries a word and a glyph as well as a colour (section 11 item 8). The
        ' glyph is the verdict's state glyph in the state's colour (Theme.VerdictGlyph, SP-0016
        ' T2); it is decoration beside the badge that says the word. The colours are painted by
        ' PaintVerdict, which ApplyTheme calls too - so a theme switch repaints them (T2).
        shownVerdict = res.Verdict
        verdictGlyph.Text = Theme.Glyph(Theme.VerdictGlyph(res.Verdict))
        PaintVerdict(Theme.Current)

        ' "Could not verify" is a real answer, and it says why (principle 3).
        If String.IsNullOrEmpty(res.Reason) Then
            verdictReasonLabel.Visible = False
        Else
            verdictReasonLabel.Visible = True
            verdictReasonLabel.Text = L(res.Reason)
        End If

        Dim numStr = Localization.Format(L("shell_result_summary_fmt"), res.Duration.ToString("mm\:ss"), res.ExitCode)
        If res.ResultInfo IsNot Nothing AndAlso res.ResultInfo.Numbers IsNot Nothing Then
            For Each kvp In res.ResultInfo.Numbers
                numStr &= Environment.NewLine & kvp.Key & ": " & kvp.Value.ToString()
            Next
        End If
        resultNumbersLabel.Text = numStr

        ' What was left on disk, always, and never as a footnote (section 6.3 item 3).
        Dim filesLeft = If(res.ResultInfo IsNot Nothing, res.ResultInfo.FilesLeft, Nothing)
        If filesLeft IsNot Nothing AndAlso filesLeft.Count > 0 Then
            filesLeftLabel.Visible = True
            filesLeftLabel.Text = L("shell_files_left_warning") & Environment.NewLine & String.Join(Environment.NewLine, filesLeft)
            actionCleanBtn.Visible = True
        Else
            filesLeftLabel.Visible = False
            actionCleanBtn.Visible = False
        End If

        If ShellSettings.HistoryEnabled() Then
            reportsLabel.Text = Localization.Format(L("shell_reports_dir_fmt"), Runner.GetReportsDir())
            actionOpenReportBtn.Visible = True
        Else
            reportsLabel.Text = L("shell_history_off")
            actionOpenReportBtn.Visible = False
        End If
    End Sub

    ' The verdict's colours, one function of (verdict, palette) for both the result path and a theme
    ' change (APP-STYLE section 3).
    Private Sub PaintVerdict(p As Theme.Palette)
        verdictGlyph.ForeColor = Theme.VerdictColor(shownVerdict, p)
        verdictBadge.BackColor = Theme.VerdictBack(shownVerdict, p)
        verdictBadge.ForeColor = Theme.VerdictFore(shownVerdict, p)
    End Sub

    Private Sub CleanTestFiles_Click(sender As Object, e As EventArgs)
        Dim cleanJob = JobCatalogue.GetJob("rail_job_clean")
        If cleanJob IsNot Nothing Then SetJob(cleanJob)
    End Sub

    Private Sub OpenLatestReport_Click(sender As Object, e As EventArgs)
        Dim reportsDir As String
        Try
            reportsDir = Runner.GetReportsDir()
        Catch ex As Exception
            ShellLog.Write("find the reports folder", ex)
            ShellDialog.Problem(ShellDialog.OwnerOf(Me), Problems.Cause(ex))
            Return
        End Try
        Ui.OpenFolder(ShellDialog.OwnerOf(Me), reportsDir)
    End Sub

    ' ---- theming ---------------------------------------------------------

    Public Sub ApplyTheme()
        Dim p = Theme.Current
        BackColor = p.Background
        ForeColor = p.Text

        For Each c As ShellCard In New ShellCard() {targetCard, paramsCard, planCard, runStrip, resultCard, rawOutputDrawer}
            c.BackColor = p.Surface
            c.BorderColour = p.Border
            c.Invalidate()
        Next

        ' The Erase group is visually distinct, and by more than a colour: it keeps an accent bar
        ' on the card's edge (section 8 item 3).
        Dim destructive = (job IsNot Nothing AndAlso job.IsDestructive) OrElse FdsecIsDestructiveNow()
        paramsCard.Accented = destructive
        paramsCard.AccentColour = p.Danger
        planCard.Accented = destructive
        planCard.AccentColour = p.Danger

        For Each h As Label In New Label() {targetHeader, paramsHeader, planHeader}
            h.Font = Theme.FontSubtitle()
            h.ForeColor = p.Accent
        Next

        targetLabel.Font = Theme.FontBody()
        targetLabel.ForeColor = p.Text
        secondTargetLabel.Font = Theme.FontBody()
        secondTargetLabel.ForeColor = p.Text
        dropHintLabel.Font = Theme.FontCaption()
        dropHintLabel.ForeColor = p.MutedText

        targetCombo.BackColor = p.Surface
        targetCombo.ForeColor = p.Text
        targetCombo.FlatStyle = FlatStyle.Flat
        secondTargetBox.BackColor = p.Surface
        secondTargetBox.ForeColor = p.Text
        secondTargetBox.BorderStyle = BorderStyle.FixedSingle

        For Each r As RadioButton In New RadioButton() {presetQuick, presetThorough, presetCustom,
                                                        dupActionReport, dupActionDelete, dupActionMove}
            r.Font = Theme.FontBody()
            r.ForeColor = p.Text
            r.BackColor = p.Surface
        Next
        dupActionDelete.ForeColor = If(dupActionDelete.Checked, p.Danger, p.Text)

        For Each cb As CheckBox In New CheckBox() {optionsCheckAutoDel, optionsCheckNoDel,
                                                   optionsCheckShort, optionsCheckVerify, optionsCheckHere,
                                                   optionsCheckNoHist,
                                                   dupListCheck, dupQuietCheck}
            cb.Font = Theme.FontBody()
            cb.BackColor = p.Surface
            cb.ForeColor = p.Text
        Next

        ' The three that change what a drive looks like afterwards say so in the danger colour
        ' once they are on - a quick format and a repaired volume are not undone by unchecking.
        For Each cb As CheckBox In New CheckBox() {optionsCheckProbeFix, optionsCheckForceFormat}
            cb.Font = Theme.FontBody()
            cb.BackColor = p.Surface
            cb.ForeColor = If(cb.Checked, p.Danger, p.Text)
        Next
        optionsCheckForce.Font = Theme.FontBody()
        optionsCheckForce.ForeColor = If(destructive, p.Danger, p.Text)

        For Each lb As Label In New Label() {sizeLabel, dupRuleLabel, dupActionLabel, cmpRuleLabel, copyStrategyLabel}
            lb.Font = Theme.FontBody()
            lb.ForeColor = p.Text
        Next
        For Each lb As Label In New Label() {dupRuleNotice, cmpRuleNotice, copyStrategyNotice}
            lb.Font = Theme.FontCaption()
            lb.ForeColor = p.MutedText
        Next
        For Each cb As ComboBox In New ComboBox() {dupRuleCombo, cmpRuleCombo, copyStrategyCombo}
            cb.Font = Theme.FontBody()
            cb.BackColor = p.Surface
            cb.ForeColor = p.Text
            cb.FlatStyle = FlatStyle.Flat
        Next
        For Each tb As TextBox In New TextBox() {sizeBox, dupMoveBox, dupListBox}
            tb.Font = Theme.FontBody()
            tb.BackColor = p.Surface
            tb.ForeColor = p.Text
            tb.BorderStyle = BorderStyle.FixedSingle
        Next
        ' Same rule as the check panel's: a size that is not a number is shown as one.
        Dim sizeText = sizeBox.Text.Trim()
        If sizeText <> "" AndAlso Not IsNumeric(sizeText) Then sizeBox.ForeColor = p.Danger

        checkOptions.ApplyTheme()

        ' The secret-file fields. The notice under the password is the one
        ' label on this page whose colour carries meaning: it warns while the
        ' credential is empty or short, and stops warning when it is neither.
        For Each lb As Label In New Label() {credLabel, credConfirmLabel, fdsecOriginalLabel}
            lb.Font = Theme.FontBody()
            lb.ForeColor = p.Text
        Next
        For Each tb As TextBox In New TextBox() {credBox, credConfirmBox}
            tb.BackColor = p.Surface
            tb.ForeColor = p.Text
            tb.BorderStyle = BorderStyle.FixedSingle
        Next
        For Each r As RadioButton In New RadioButton() {fdsecKeepRadio, fdsecDelRadio, fdsecWipeRadio}
            r.Font = Theme.FontBody()
            r.BackColor = p.Surface
            r.ForeColor = p.Text
        Next
        fdsecDelRadio.ForeColor = If(fdsecDelRadio.Checked, p.Warning, p.Text)
        fdsecWipeRadio.ForeColor = If(fdsecWipeRadio.Checked, p.Danger, p.Text)
        For Each cb As CheckBox In New CheckBox() {credShowCheck, fdsecRenameCheck, fdsecDelContainerCheck, fdsecKeepCopyCheck, fdsecRwCheck}
            cb.Font = Theme.FontBody()
            cb.BackColor = p.Surface
            cb.ForeColor = p.Text
        Next
        fdsecDelContainerCheck.ForeColor = If(fdsecDelContainerCheck.Checked, p.Warning, p.Text)

        credNoticeLabel.Font = Theme.FontBodyStrong()
        credNoticeLabel.ForeColor = If(credNoticeLabel.Text = L("shell_cred_ok"), p.MutedText, p.Warning)
        For Each lb As Label In New Label() {credHintLabel, fdsecNoticeLabel}
            lb.Font = Theme.FontCaption()
            lb.ForeColor = p.MutedText
        Next

        blastRadiusLabel.Font = Theme.FontBody()
        blastRadiusLabel.ForeColor = p.Text
        wipeConfirmLabel.Font = Theme.FontBodyStrong()
        wipeConfirmLabel.ForeColor = p.Danger
        wipeConfirmBox.BackColor = p.Surface
        wipeConfirmBox.ForeColor = p.Text
        wipeConfirmBox.BorderStyle = BorderStyle.FixedSingle

        intentLabel.Font = Theme.FontBody()
        intentLabel.ForeColor = p.Text
        touchesLabel.Font = Theme.FontBodyStrong()
        touchesLabel.ForeColor = p.Text

        reversibilityBadge.Font = Theme.FontCaption()
        reversibilityBadge.BackColor = p.SurfaceAlt
        reversibilityBadge.ForeColor = p.Text
        destructiveBadge.Font = Theme.FontCaption()
        destructiveBadge.BackColor = p.Danger
        destructiveBadge.ForeColor = p.AccentText
        elevationBadge.Font = Theme.FontCaption()
        elevationBadge.BackColor = p.Warning
        elevationBadge.ForeColor = p.AccentText

        redirectNoticeLabel.Font = Theme.FontBodyStrong()
        redirectNoticeLabel.ForeColor = p.Warning
        runBlockedLabel.Font = Theme.FontBodyStrong()
        runBlockedLabel.ForeColor = p.Warning

        commandLabel.Font = Theme.FontCaption()
        commandLabel.ForeColor = p.MutedText
        commandBox.Font = Theme.FontMono()
        commandBox.BackColor = p.SurfaceAlt
        commandBox.ForeColor = p.Text
        commandBox.BorderStyle = BorderStyle.FixedSingle

        startBtn.Font = Theme.FontBodyStrong()
        StyleStartBtn()
        Ui.StyleButton(stopBtn, p.Danger, p.AccentText, p.Danger)
        stopBtn.Font = Theme.FontBodyStrong()

        For Each b In secondaryButtons
            b.Font = Theme.FontBody()
            Ui.StyleButton(b, p.SurfaceAlt, p.Text, p.Border)
        Next

        stateBadge.Font = Theme.FontCaption()
        stateBadge.BackColor = p.Accent
        stateBadge.ForeColor = p.AccentText

        stepLabel.Font = Theme.FontBody()
        stepLabel.ForeColor = p.Text
        throughputLabel.Font = Theme.FontBodyStrong()
        throughputLabel.ForeColor = p.Text
        timeLabel.Font = Theme.FontBody()
        timeLabel.ForeColor = p.MutedText

        verdictGlyph.Font = Theme.FontGlyph()
        verdictBadge.Font = Theme.FontSubtitle()
        PaintVerdict(p)
        verdictReasonLabel.Font = Theme.FontBody()
        verdictReasonLabel.ForeColor = p.MutedText

        resultNumbersLabel.Font = Theme.FontBody()
        resultNumbersLabel.ForeColor = p.Text
        filesLeftLabel.Font = Theme.FontBodyStrong()
        filesLeftLabel.ForeColor = p.Danger
        reportsLabel.Font = Theme.FontCaption()
        reportsLabel.ForeColor = p.MutedText

        rawOutputBox.Font = Theme.FontMono()
        rawOutputBox.BackColor = p.SurfaceAlt
        rawOutputBox.ForeColor = p.Text
        rawOutputBox.BorderStyle = BorderStyle.FixedSingle

        Invalidate(True)
    End Sub

End Class
