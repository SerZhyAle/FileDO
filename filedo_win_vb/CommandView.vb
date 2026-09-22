Imports System.IO

' The expert page (SP-0006 section 10, D2): the builder that exists today, kept rather than deleted,
' and moved onto the runner so its output lands in the page instead of in a console window.
'
' It follows the same numbered flow as a job page - what to do, what to work on, with which
' parameters - because the owner's flow does not change just because the user is an expert. What is
' different is the last field: the command line itself is editable, which is the whole point of the
' page and the reason the shell can never model fewer things than the CLI can do.
'
' The order is load-bearing, and it used to be wrong here. The page asked for the target first and
' the operation second, which meant it asked "drive, folder or file?" before it could know which of
' the three the answer was allowed to be - and then offered a folder picker whatever the answer
' was. The operation is step 1, and every field under it - the label, the drive list, which
' pickers are offered, what the extra-parameter box is for - is chosen from that answer.
'
' The page carries credential-bearing verbs too - secure, unsecure, reveal and the two fdsec
' inspections - and it carries them the only way an editable command line safely can: the password
' is typed into a masked field of its own and never into the line. What the line holds is
' `pe:FILEDO_SHELL_CRED`, the name of the variable the child process is given, so copying the
' command, logging it or reading it over a shoulder gives nothing away (principle 6, SP-0005 12).
' A user who types `p:<password>` into the line by hand has chosen that; the page never does.
Public Class CommandView
    Inherits UserControl

    Private ReadOnly dict As Dictionary(Of String, String)
    Private ReadOnly runner As New Runner()
    Private ReadOnly tips As New ToolTip()
    Private ReadOnly secondaryButtons As New List(Of Button)

    Private root As TableLayoutPanel
    Private inputCard As ShellCard
    Private runCard As ShellCard

    Private opHeader As Label
    Private opLabel As Label
    Private opCombo As ComboBox
    Private opHintLabel As Label

    Private targetHeader As Label
    Private targetLabel As Label
    Private targetCombo As ComboBox
    Private browseFolderBtn As Button
    Private browseFileBtn As Button

    Private paramsHeader As Label
    Private paramLabel As Label
    Private paramBox As TextBox
    Private flagsLabel As Label
    Private flagsFlow As FlowLayoutPanel
    Private flagCheckShort As CheckBox
    Private flagCheckDel As CheckBox
    Private flagCheckNoDel As CheckBox
    Private flagCheckMax As CheckBox
    Private flagCheckVerify As CheckBox
    Private flagCheckYes As CheckBox
    Private flagCheckFix As CheckBox
    Private flagCheckFormat As CheckBox
    Private flagCheckAllUsers As CheckBox
    Private flagCheckNoHist As CheckBox
    Private flagCheckForce As CheckBox
    Private flagCheckWipe As CheckBox
    Private flagCheckRename As CheckBox
    Private flagCheckHere As CheckBox
    Private flagCheckRw As CheckBox
    Private flagCheckKeep As CheckBox

    ' The rule pickers, one per operation that has rules, and the whole option surface of `check`.
    ' The builder is the page that must never be able to say less than the CLI can do, so what it
    ' offers here is the same list the job pages offer - written once per page, from the same CLI.
    Private rulesRow As FlowLayoutPanel
    Private dupRuleLabel As Label
    Private dupRuleCombo As ComboBox
    Private dupActionLabel As Label
    Private dupActionCombo As ComboBox
    Private dupMoveBox As TextBox
    Private dupMoveBrowseBtn As Button
    Private dupListLabel As Label
    Private dupListBox As TextBox
    Private dupQuietCheck As CheckBox
    Private cmpRuleLabel As Label
    Private cmpRuleCombo As ComboBox
    Private ruleNotice As Label
    Private checkOptions As CheckOptionsPanel

    ' The secret-file credential (SP-0005 7, 12). The page carries the five fdsec verbs, and the
    ' password is the reason they were kept off it at first: an editable command line will hold
    ' whatever is typed into it. So the password is not typed into it. It is masked here, typed
    ' twice for secure, and handed to the child process in an environment variable - the command
    ' line, the run report and history.json see `pe:FILEDO_SHELL_CRED` and nothing else, exactly
    ' as the Protect pages do it.
    Private credRow As TableLayoutPanel
    Private credLabel As Label
    Private credBox As TextBox
    Private credShowCheck As CheckBox
    Private credConfirmLabel As Label
    Private credConfirmBox As TextBox
    Private credNoticeLabel As Label
    Private fdsecNoticeLabel As Label

    Private runHeader As Label
    Private cmdLabel As Label
    Private cmdLineBox As TextBox
    Private runBtn As Button
    Private stopBtn As Button
    Private copyBtn As Button
    Private verdictLabel As Label
    Private outputBox As TextBox

    Public Sub New()
        dict = Localization.GetDict(ShellSettings.Language())
        DoubleBuffered = True
        Dock = DockStyle.Fill
        BuildLayout()
        PopulateControls()
        HookRunner()
        ApplyTheme()
    End Sub

    Private Function L(key As String) As String
        Dim v As String = Nothing
        If dict IsNot Nothing AndAlso dict.TryGetValue(key, v) Then Return v
        Return key
    End Function

    Private Sub BuildLayout()
        SuspendLayout()

        root = New TableLayoutPanel With {
            .Dock = DockStyle.Fill,
            .ColumnCount = 1,
            .RowCount = 2,
            .Padding = Ui.PxPad(Me, 0, 0, 0, 16),
            .Margin = New Padding(0)
        }
        root.ColumnStyles.Add(New ColumnStyle(SizeType.Percent, 100.0F))
        root.RowStyles.Add(New RowStyle(SizeType.AutoSize))
        root.RowStyles.Add(New RowStyle(SizeType.Percent, 100.0F))

        BuildInputCard()
        BuildRunCard()

        root.Controls.Add(inputCard, 0, 0)
        root.Controls.Add(runCard, 0, 1)
        Controls.Add(root)

        ResumeLayout(True)
    End Sub

    Private Sub BuildInputCard()
        inputCard = New ShellCard With {
            .Padding = Ui.PxPad(Me, 18, 16, 18, 16),
            .Margin = Ui.PxPad(Me, 0, 0, 0, 14)
        }

        Dim t As New TableLayoutPanel With {
            .Dock = DockStyle.Top,
            .AutoSize = True,
            .AutoSizeMode = AutoSizeMode.GrowAndShrink,
            .ColumnCount = 3,
            .RowCount = 9,
            .Margin = New Padding(0)
        }
        t.ColumnStyles.Add(New ColumnStyle(SizeType.Percent, 100.0F))
        t.ColumnStyles.Add(New ColumnStyle(SizeType.AutoSize))
        t.ColumnStyles.Add(New ColumnStyle(SizeType.AutoSize))

        ' Step 1 - the operation, and what it does in one line.
        opHeader = Ui.StepHeader(Me, L("shell_step1"))
        t.Controls.Add(opHeader, 0, 0)
        t.SetColumnSpan(opHeader, 3)

        opLabel = New Label With {.Text = L("ui_operation"), .AutoSize = True, .Margin = Ui.PxPad(Me, 0, 0, 0, 2)}
        t.Controls.Add(opLabel, 0, 1)
        t.SetColumnSpan(opLabel, 3)

        opCombo = New ComboBox With {.DropDownStyle = ComboBoxStyle.DropDownList, .Width = Ui.Px(Me, 260), .Margin = Ui.PxPad(Me, 0, 2, 0, 4)}
        opCombo.AccessibleName = L("ui_operation")
        AddHandler opCombo.SelectedIndexChanged, Sub() ApplyOperationShape()
        t.Controls.Add(opCombo, 0, 2)
        t.SetColumnSpan(opCombo, 3)

        opHintLabel = New Label With {.Text = "", .AutoSize = True, .Margin = Ui.PxPad(Me, 0, 0, 0, 10)}
        t.Controls.Add(opHintLabel, 0, 3)
        t.SetColumnSpan(opHintLabel, 3)

        ' Step 2 - what it runs on. The label and the pickers come from step 1.
        targetHeader = Ui.StepHeader(Me, L("shell_step2"))
        t.Controls.Add(targetHeader, 0, 4)
        t.SetColumnSpan(targetHeader, 3)

        targetLabel = New Label With {.Text = L("shell_lbl_target"), .AutoSize = True, .Margin = Ui.PxPad(Me, 0, 0, 0, 2)}
        t.Controls.Add(targetLabel, 0, 5)
        t.SetColumnSpan(targetLabel, 3)

        targetCombo = New ComboBox With {.Dock = DockStyle.Fill, .DropDownStyle = ComboBoxStyle.DropDown, .Margin = Ui.PxPad(Me, 0, 2, 8, 6)}
        targetCombo.AccessibleName = L("shell_lbl_target")
        AddHandler targetCombo.TextChanged, Sub() BuildCommandLine()

        browseFolderBtn = New Button With {.Text = L("shell_btn_browse_folder"), .AutoSize = True, .AutoSizeMode = AutoSizeMode.GrowAndShrink, .Margin = Ui.PxPad(Me, 0, 2, 8, 6)}
        AddHandler browseFolderBtn.Click, Sub()
                                              Using dlg As New FolderBrowserDialog()
                                                  dlg.Description = L("shell_dlg_select_folder")
                                                  If dlg.ShowDialog() = DialogResult.OK Then targetCombo.Text = dlg.SelectedPath
                                              End Using
                                          End Sub
        secondaryButtons.Add(browseFolderBtn)

        ' The file picker is offered wherever the CLI takes a file, instead of being named in a
        ' label and then missing from the page.
        browseFileBtn = New Button With {.Text = L("shell_btn_browse_file"), .AutoSize = True, .AutoSizeMode = AutoSizeMode.GrowAndShrink, .Margin = Ui.PxPad(Me, 0, 2, 0, 6)}
        AddHandler browseFileBtn.Click, Sub()
                                            Using dlg As New OpenFileDialog()
                                                dlg.Title = L("shell_dlg_select_file")
                                                If dlg.ShowDialog() = DialogResult.OK Then targetCombo.Text = dlg.FileName
                                            End Using
                                        End Sub
        secondaryButtons.Add(browseFileBtn)

        t.Controls.Add(targetCombo, 0, 6)
        t.Controls.Add(browseFolderBtn, 1, 6)
        t.Controls.Add(browseFileBtn, 2, 6)

        ' Step 3 - the rest of the line.
        paramsHeader = Ui.StepHeader(Me, L("shell_step3"))
        paramsHeader.Margin = Ui.PxPad(Me, 0, 10, 0, 8)
        t.Controls.Add(paramsHeader, 0, 7)
        t.SetColumnSpan(paramsHeader, 3)

        paramLabel = New Label With {.Text = L("shell_lbl_params"), .AutoSize = True, .Margin = Ui.PxPad(Me, 0, 0, 0, 2)}
        t.Controls.Add(paramLabel, 0, 8)
        t.SetColumnSpan(paramLabel, 3)

        Dim bottom As New TableLayoutPanel With {
            .Dock = DockStyle.Top,
            .AutoSize = True,
            .AutoSizeMode = AutoSizeMode.GrowAndShrink,
            .ColumnCount = 1,
            .RowCount = 3,
            .Margin = New Padding(0)
        }
        bottom.ColumnStyles.Add(New ColumnStyle(SizeType.Percent, 100.0F))

        paramBox = New TextBox With {.Dock = DockStyle.Fill, .Margin = Ui.PxPad(Me, 0, 2, 0, 8)}
        paramBox.AccessibleName = L("shell_lbl_params")
        AddHandler paramBox.TextChanged, Sub() BuildCommandLine()

        flagsLabel = New Label With {.Text = L("ui_flags"), .AutoSize = True, .Margin = Ui.PxPad(Me, 0, 0, 0, 2)}

        flagsFlow = New FlowLayoutPanel With {
            .Dock = DockStyle.Top,
            .AutoSize = True,
            .AutoSizeMode = AutoSizeMode.GrowAndShrink,
            .WrapContents = True,
            .Margin = Ui.PxPad(Me, 0, 2, 0, 0)
        }
        flagCheckShort = NewFlag("short", "fl_short")
        flagCheckDel = NewFlag("del", "fl_del")
        flagCheckNoDel = NewFlag("nodel", "fl_nodel")
        flagCheckMax = NewFlag("max", "fl_max")
        flagCheckVerify = NewFlag("verify", "fl_verify")
        flagCheckYes = NewFlag("yes", "fl_yes")
        flagCheckFix = NewFlag("fix", "fl_fix")
        flagCheckFormat = NewFlag("format", "fl_format")
        flagCheckAllUsers = NewFlag("-all-users", "fl_all_users")
        flagCheckNoHist = NewFlag("nohist", "fl_nohist")
        flagCheckWipe = NewFlag("wipe", "fl_fdsec_wipe")
        flagCheckRename = NewFlag("rename", "fl_rename")
        flagCheckHere = NewFlag("here", "fl_here")
        flagCheckRw = NewFlag("-rw", "fl_rw")
        flagCheckKeep = NewFlag("-keep", "fl_keep")
        flagCheckForce = NewFlag("-y / --force", "fl_force")
        For Each c As CheckBox In New CheckBox() {flagCheckShort, flagCheckDel, flagCheckNoDel,
                                                  flagCheckMax, flagCheckVerify, flagCheckYes,
                                                  flagCheckFix, flagCheckFormat, flagCheckAllUsers,
                                                  flagCheckWipe, flagCheckRename, flagCheckHere,
                                                  flagCheckRw, flagCheckKeep,
                                                  flagCheckNoHist, flagCheckForce}
            flagsFlow.Controls.Add(c)
        Next

        BuildRulesRow()
        BuildCredentialRow()
        checkOptions = New CheckOptionsPanel() With {.Visible = False}
        AddHandler checkOptions.Changed,
            Sub()
                checkOptions.ApplyTheme()
                BuildCommandLine()
            End Sub

        bottom.RowCount = 7
        bottom.Controls.Add(paramBox, 0, 0)
        bottom.Controls.Add(flagsLabel, 0, 1)
        bottom.Controls.Add(flagsFlow, 0, 2)
        bottom.Controls.Add(rulesRow, 0, 3)
        bottom.Controls.Add(credRow, 0, 4)
        bottom.Controls.Add(fdsecNoticeLabel, 0, 5)
        bottom.Controls.Add(checkOptions, 0, 6)

        Dim wrapper As New TableLayoutPanel With {
            .Dock = DockStyle.Top,
            .AutoSize = True,
            .AutoSizeMode = AutoSizeMode.GrowAndShrink,
            .ColumnCount = 1,
            .RowCount = 2,
            .Margin = New Padding(0)
        }
        wrapper.ColumnStyles.Add(New ColumnStyle(SizeType.Percent, 100.0F))
        wrapper.Controls.Add(t, 0, 0)
        wrapper.Controls.Add(bottom, 0, 1)

        inputCard.Controls.Add(wrapper)
        Ui.Wrap(opHintLabel, inputCard, Ui.Px(Me, 36))
        Ui.Wrap(ruleNotice, inputCard, Ui.Px(Me, 36))
        Ui.Wrap(credNoticeLabel, inputCard, Ui.Px(Me, 36))
        Ui.Wrap(fdsecNoticeLabel, inputCard, Ui.Px(Me, 36))
    End Sub

    Private Function NewFlag(text As String, helpKey As String) As CheckBox
        Dim c As New CheckBox With {.Text = text, .AutoSize = True, .Margin = Ui.PxPad(Me, 0, 2, 16, 2), .Visible = False}
        c.AccessibleName = text
        tips.SetToolTip(c, L(helpKey))
        AddHandler c.CheckedChanged, Sub() BuildCommandLine()
        Return c
    End Function

    ' The duplicate rule, the duplicate action and the compare rule. All three write CLI tokens,
    ' so the combos show the tokens and the line under them says what the chosen one does.
    Private Sub BuildRulesRow()
        rulesRow = New FlowLayoutPanel With {
            .Dock = DockStyle.Top,
            .AutoSize = True,
            .AutoSizeMode = AutoSizeMode.GrowAndShrink,
            .FlowDirection = FlowDirection.LeftToRight,
            .WrapContents = True,
            .Margin = Ui.PxPad(Me, 0, 8, 0, 0),
            .Visible = False
        }

        dupRuleLabel = New Label With {.Text = L("shell_dup_rule"), .AutoSize = True, .Margin = Ui.PxPad(Me, 0, 6, 6, 0)}
        dupRuleCombo = New ComboBox With {.DropDownStyle = ComboBoxStyle.DropDownList, .Width = Ui.Px(Me, 150), .Margin = Ui.PxPad(Me, 0, 2, 16, 2)}
        For Each token In CliRules.DupRuleTokens
            dupRuleCombo.Items.Add(If(token = "", L("shell_dup_rule_default"), token))
        Next
        dupRuleCombo.SelectedIndex = 0
        dupRuleCombo.AccessibleName = L("shell_dup_rule")
        AddHandler dupRuleCombo.SelectedIndexChanged, Sub() RuleChanged()

        dupActionLabel = New Label With {.Text = L("shell_dup_action"), .AutoSize = True, .Margin = Ui.PxPad(Me, 0, 6, 6, 0)}
        dupActionCombo = New ComboBox With {.DropDownStyle = ComboBoxStyle.DropDownList, .Width = Ui.Px(Me, 150), .Margin = Ui.PxPad(Me, 0, 2, 16, 2)}
        dupActionCombo.Items.AddRange(New Object() {L("shell_dup_report"), "del", "move"})
        dupActionCombo.SelectedIndex = 0
        dupActionCombo.AccessibleName = L("shell_dup_action")
        AddHandler dupActionCombo.SelectedIndexChanged, Sub() RuleChanged()

        dupMoveBox = New TextBox With {.Width = Ui.Px(Me, 170), .Margin = Ui.PxPad(Me, 0, 2, 6, 2), .Enabled = False}
        dupMoveBox.AccessibleName = L("dup_move")
        AddHandler dupMoveBox.TextChanged, Sub() BuildCommandLine()

        dupMoveBrowseBtn = New Button With {.Text = L("shell_btn_browse_folder"), .AutoSize = True, .AutoSizeMode = AutoSizeMode.GrowAndShrink, .Margin = Ui.PxPad(Me, 0, 2, 16, 2), .Enabled = False}
        AddHandler dupMoveBrowseBtn.Click,
            Sub()
                Using dlg As New FolderBrowserDialog()
                    dlg.Description = L("shell_dlg_select_dest")
                    If dlg.ShowDialog() = DialogResult.OK Then dupMoveBox.Text = dlg.SelectedPath
                End Using
            End Sub
        secondaryButtons.Add(dupMoveBrowseBtn)

        dupListLabel = New Label With {.Text = L("ui_list"), .AutoSize = True, .Margin = Ui.PxPad(Me, 0, 6, 6, 0)}
        dupListBox = New TextBox With {.Width = Ui.Px(Me, 150), .Margin = Ui.PxPad(Me, 0, 2, 16, 2)}
        dupListBox.AccessibleName = L("ui_list")
        AddHandler dupListBox.TextChanged, Sub() BuildCommandLine()

        dupQuietCheck = New CheckBox With {.Text = "quiet", .AutoSize = True, .Margin = Ui.PxPad(Me, 0, 4, 16, 2)}
        tips.SetToolTip(dupQuietCheck, L("shell_dup_quiet"))
        AddHandler dupQuietCheck.CheckedChanged, Sub() BuildCommandLine()

        cmpRuleLabel = New Label With {.Text = L("cmp_label"), .AutoSize = True, .Margin = Ui.PxPad(Me, 0, 6, 6, 0)}
        cmpRuleCombo = New ComboBox With {.DropDownStyle = ComboBoxStyle.DropDownList, .Width = Ui.Px(Me, 190), .Margin = Ui.PxPad(Me, 0, 2, 16, 2)}
        For Each token In CliRules.CmpRuleTokens
            cmpRuleCombo.Items.Add(If(token = "", L("shell_cmp_rule_none"), token))
        Next
        cmpRuleCombo.SelectedIndex = 0
        cmpRuleCombo.AccessibleName = L("cmp_label")
        AddHandler cmpRuleCombo.SelectedIndexChanged, Sub() RuleChanged()

        ruleNotice = New Label With {.Text = "", .AutoSize = True, .Margin = Ui.PxPad(Me, 0, 6, 0, 0)}

        For Each c As Control In New Control() {dupRuleLabel, dupRuleCombo, dupActionLabel, dupActionCombo,
                                                dupMoveBox, dupMoveBrowseBtn, dupListLabel, dupListBox,
                                                dupQuietCheck, cmpRuleLabel, cmpRuleCombo, ruleNotice}
            rulesRow.Controls.Add(c)
        Next
    End Sub

    ' The password field, masked, with the same honest line under it that the Protect pages carry:
    ' an empty credential is obfuscation and nothing else. Secure asks twice, because a typo there
    ' locks the data behind a password nobody meant to set.
    Private Sub BuildCredentialRow()
        credRow = New TableLayoutPanel With {
            .Dock = DockStyle.Top,
            .AutoSize = True,
            .AutoSizeMode = AutoSizeMode.GrowAndShrink,
            .ColumnCount = 2,
            .RowCount = 5,
            .Margin = Ui.PxPad(Me, 0, 8, 0, 0),
            .Visible = False
        }
        credRow.ColumnStyles.Add(New ColumnStyle(SizeType.Percent, 100.0F))
        credRow.ColumnStyles.Add(New ColumnStyle(SizeType.AutoSize))

        credLabel = New Label With {.Text = L("shell_lbl_password"), .AutoSize = True, .Margin = Ui.PxPad(Me, 0, 4, 0, 2)}
        credRow.Controls.Add(credLabel, 0, 0)
        credRow.SetColumnSpan(credLabel, 2)

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
        credRow.SetColumnSpan(credConfirmLabel, 2)

        credConfirmBox = New TextBox With {.Dock = DockStyle.Fill, .UseSystemPasswordChar = True, .Margin = Ui.PxPad(Me, 0, 2, 8, 4)}
        credConfirmBox.AccessibleName = L("shell_cred_confirm")
        AddHandler credConfirmBox.TextChanged, Sub() CredentialChanged()
        credRow.Controls.Add(credConfirmBox, 0, 3)

        credNoticeLabel = New Label With {.Text = L("shell_cred_empty"), .AutoSize = True, .Margin = Ui.PxPad(Me, 0, 4, 0, 2)}
        credRow.Controls.Add(credNoticeLabel, 0, 4)
        credRow.SetColumnSpan(credNoticeLabel, 2)

        ' Two things this page has to say out loud and the job pages do not: where the password
        ' goes, and what happens to a destructive option whose question cannot be answered from a
        ' window. UpdateFdsecNotice picks which one is on screen.
        fdsecNoticeLabel = New Label With {.Text = "", .AutoSize = True, .Margin = Ui.PxPad(Me, 0, 6, 0, 0), .Visible = False}
    End Sub

    Private Sub CredentialChanged()
        UpdateFdsecNotice()
        ApplyTheme()
        UpdateRunButtonState()
    End Sub

    ' The line under the password box, and the warning about an unanswerable question.
    Private Sub UpdateFdsecNotice()
        Dim op = CurrentOp()
        If Not IsFdsecOp(op) Then
            fdsecNoticeLabel.Visible = False
            Return
        End If

        Dim pwd = credBox.Text
        If pwd.Length = 0 Then
            credNoticeLabel.Text = L("shell_cred_empty")
        ElseIf pwd.Length < 12 Then
            credNoticeLabel.Text = L("shell_cred_short")
        ElseIf op = "secure" AndAlso credConfirmBox.Text <> pwd Then
            credNoticeLabel.Text = L("shell_cred_mismatch")
        Else
            credNoticeLabel.Text = L("shell_cred_ok")
        End If

        ' A wipe or a delete of the original is confirmed on a console, and a run started from
        ' this window has none: without -y the CLI keeps the original and says so. That is the
        ' safe outcome, not a broken one - but it must not be a surprise.
        Dim unanswerable = (op = "secure") AndAlso (flagCheckWipe.Checked OrElse flagCheckDel.Checked) AndAlso Not flagCheckForce.Checked
        If unanswerable Then
            fdsecNoticeLabel.Text = L("shell_cmd_fdsec_needs_y")
            fdsecNoticeLabel.Visible = True
        Else
            fdsecNoticeLabel.Text = L("shell_cred_out_of_sight")
            fdsecNoticeLabel.Visible = True
        End If
    End Sub

    ' Run is refused for the one mistake this page can catch: a secure whose two boxes differ.
    ' Everything else - an empty password included - is a documented choice.
    Private Sub UpdateRunButtonState()
        If runner.IsActive Then Return
        Dim op = CurrentOp()
        Dim mismatched = (op = "secure") AndAlso (credConfirmBox.Text <> credBox.Text)
        runBtn.Enabled = Not mismatched
        If mismatched Then tips.SetToolTip(runBtn, L("shell_cred_mismatch")) Else tips.SetToolTip(runBtn, "")
    End Sub

    Private Sub RuleChanged()
        Dim moving = (dupActionCombo.SelectedIndex = 2)
        dupMoveBox.Enabled = moving
        dupMoveBrowseBtn.Enabled = moving

        Select Case CurrentOp()
            Case "cd" : ruleNotice.Text = L(CliRules.DupRuleKeys(Math.Max(dupRuleCombo.SelectedIndex, 0)))
            Case "compare" : ruleNotice.Text = L(CliRules.CmpRuleKeys(Math.Max(cmpRuleCombo.SelectedIndex, 0)))
            Case Else : ruleNotice.Text = ""
        End Select

        BuildCommandLine()
    End Sub

    Private Sub BuildRunCard()
        runCard = New ShellCard With {
            .Dock = DockStyle.Fill,
            .AutoSize = False,
            .Padding = Ui.PxPad(Me, 18, 16, 18, 16),
            .Margin = New Padding(0)
        }

        Dim t As New TableLayoutPanel With {
            .Dock = DockStyle.Fill,
            .ColumnCount = 1,
            .RowCount = 6,
            .Margin = New Padding(0)
        }
        t.ColumnStyles.Add(New ColumnStyle(SizeType.Percent, 100.0F))
        t.RowStyles.Add(New RowStyle(SizeType.AutoSize))
        t.RowStyles.Add(New RowStyle(SizeType.AutoSize))
        t.RowStyles.Add(New RowStyle(SizeType.AutoSize))
        t.RowStyles.Add(New RowStyle(SizeType.AutoSize))
        t.RowStyles.Add(New RowStyle(SizeType.AutoSize))
        t.RowStyles.Add(New RowStyle(SizeType.Percent, 100.0F))

        runHeader = Ui.StepHeader(Me, L("shell_step4"))
        cmdLabel = New Label With {.Text = L("shell_lbl_command_editable"), .AutoSize = True, .Margin = Ui.PxPad(Me, 0, 0, 0, 2)}

        cmdLineBox = New TextBox With {.Dock = DockStyle.Fill, .Margin = Ui.PxPad(Me, 0, 2, 0, 8)}
        cmdLineBox.AccessibleName = L("shell_lbl_command_editable")

        Dim btnFlow As New FlowLayoutPanel With {
            .Dock = DockStyle.Top,
            .AutoSize = True,
            .AutoSizeMode = AutoSizeMode.GrowAndShrink,
            .WrapContents = True,
            .Margin = Ui.PxPad(Me, 0, 0, 0, 8)
        }
        runBtn = New Button With {
            .Text = L("shell_btn_run_cmd"),
            .AutoSize = True,
            .AutoSizeMode = AutoSizeMode.GrowAndShrink,
            .Padding = Ui.PxPad(Me, 16, 6, 16, 6),
            .Margin = Ui.PxPad(Me, 0, 0, 10, 0)
        }
        AddHandler runBtn.Click, AddressOf RunBtn_Click

        stopBtn = New Button With {.Text = L("shell_btn_stop"), .AutoSize = True, .AutoSizeMode = AutoSizeMode.GrowAndShrink, .Margin = Ui.PxPad(Me, 0, 0, 8, 0), .Enabled = False}
        ' A red button that cannot be pressed is the loudest thing on a page where nothing is
        ' running. Stop looks like Stop while there is something to stop, and like any other
        ' inactive control the rest of the time.
        AddHandler stopBtn.EnabledChanged, Sub() StyleStopButton()
        AddHandler stopBtn.Click, Sub()
                                      runner.RequestStop()
                                      stopBtn.Enabled = False
                                      verdictLabel.Text = L("shell_stop_requested")
                                  End Sub

        copyBtn = New Button With {.Text = L("shell_btn_copy_cmd"), .AutoSize = True, .AutoSizeMode = AutoSizeMode.GrowAndShrink}
        AddHandler copyBtn.Click, Sub()
                                      If Not String.IsNullOrEmpty(cmdLineBox.Text) Then Clipboard.SetText(cmdLineBox.Text)
                                  End Sub
        secondaryButtons.Add(copyBtn)

        btnFlow.Controls.Add(runBtn)
        btnFlow.Controls.Add(stopBtn)
        btnFlow.Controls.Add(copyBtn)

        verdictLabel = New Label With {.Text = "", .AutoSize = True, .Margin = Ui.PxPad(Me, 0, 0, 0, 6)}

        outputBox = New TextBox With {
            .Dock = DockStyle.Fill,
            .Multiline = True,
            .ReadOnly = True,
            .ScrollBars = ScrollBars.Both,
            .Margin = New Padding(0)
        }
        outputBox.AccessibleName = L("shell_btn_show_output")

        t.Controls.Add(runHeader, 0, 0)
        t.Controls.Add(cmdLabel, 0, 1)
        t.Controls.Add(cmdLineBox, 0, 2)
        t.Controls.Add(btnFlow, 0, 3)
        t.Controls.Add(verdictLabel, 0, 4)
        t.Controls.Add(outputBox, 0, 5)

        runCard.Controls.Add(t)
        Ui.Wrap(verdictLabel, runCard, Ui.Px(Me, 36))
    End Sub

    ' Every operation filedo.exe takes that can be written on a command line without a password.
    '
    ' The list used to hold ten of them, which made the expert page the one place in the shell
    ' that could do LESS than the CLI - probe, recover, the six named copy strategies, the batch
    ' runner, the history dump and the Explorer registration were all missing, while their
    ' explanations were already translated and sitting in Localization.vb.
    '
    ' The five secret-file verbs are here too, with the password kept out of the line: the field
    ' below is masked and what the command carries is `pe:FILEDO_SHELL_CRED`, the variable the
    ' child process is given. The page is still the one place that must not model less than the
    ' CLI can do, and "this verb exists but not here" was the last place where it did.
    Private Shared ReadOnly Operations As String() = {
        "info", "speed", "test", "fill", "clean", "probe", "recover",
        "cd", "compare", "check",
        "copy", "fastcopy", "synccopy", "balanced", "maxcopy", "smartcopy", "safecopy",
        "wipe", "secure", "unsecure", "reveal", "fdsec info", "fdsec verify",
        "from", "hist", "fdsec register", "fdsec unregister", "help"
    }

    ' The verbs that take a password, and the variable it travels in.
    Private Const CredentialEnvName As String = "FILEDO_SHELL_CRED"

    Private Shared Function IsFdsecOp(op As String) As Boolean
        Return Array.IndexOf(New String() {"secure", "unsecure", "reveal",
                                           "fdsec info", "fdsec verify"}, op) >= 0
    End Function

    ' The verbs written "<target> <verb>"; everything else in Operations is verb-first.
    Private Shared ReadOnly TargetFirstOps As String() = {
        "info", "speed", "test", "fill", "clean", "probe", "recover", "cd", "wipe",
        "secure", "unsecure", "reveal"
    }

    ' The verbs that take no target at all.
    Private Shared ReadOnly NoTargetOps As String() = {
        "hist", "help", "fdsec register", "fdsec unregister"
    }

    Private Sub PopulateControls()
        opCombo.Items.Clear()
        opCombo.Items.AddRange(Operations.Cast(Of Object)().ToArray())
        opCombo.SelectedIndex = 0
        ApplyOperationShape()
    End Sub

    Private Function CurrentOp() As String
        If opCombo.SelectedItem Is Nothing Then Return ""
        Return opCombo.SelectedItem.ToString()
    End Function

    ' Which of the three kinds of target the chosen operation actually accepts. The CLI is the
    ' authority for both lists, and the page says only what the CLI will take.
    Private Shared Function TakesDrive(op As String) As Boolean
        Return Array.IndexOf(New String() {"info", "speed", "test", "fill", "clean", "cd",
                                           "probe", "recover", "wipe"}, op) >= 0
    End Function

    Private Shared Function TakesFile(op As String) As Boolean
        ' `from` takes a list file, and every copy strategy copies a single file just as happily
        ' as a folder - so the file picker is offered wherever the CLI accepts one.
        Return Array.IndexOf(New String() {"info", "check", "from",
                                           "copy", "fastcopy", "synccopy", "balanced",
                                           "maxcopy", "smartcopy", "safecopy"}, op) >= 0 OrElse
               IsFdsecOp(op)
    End Function

    Private Shared Function IsCopyOp(op As String) As Boolean
        Return Array.IndexOf(CliRules.CopyVerbs, op) >= 0
    End Function

    Private Shared Function TakesNoTarget(op As String) As Boolean
        Return Array.IndexOf(NoTargetOps, op) >= 0
    End Function

    ' Step 1 has been answered, so every field below it can now say something true.
    Private Sub ApplyOperationShape()
        Dim op = CurrentOp()
        If op = "" Then Return

        opHintLabel.Text = L("op_" & op.Replace(" ", "_"))

        ' The drive list belongs to the operations that run on a drive. For the others it is not
        ' greyed out, it is simply not there - an empty list is an honest answer to "which drive",
        ' and the pickers below take over.
        If TakesDrive(op) Then
            If targetCombo.Items.Count = 0 Then
                Try
                    For Each d In DriveInfo.GetDrives()
                        If d.IsReady Then targetCombo.Items.Add(d.Name.Substring(0, 2))
                    Next
                Catch
                End Try
            End If
            If targetCombo.Text.Trim() = "" AndAlso targetCombo.Items.Count > 0 Then targetCombo.SelectedIndex = 0
        Else
            targetCombo.Items.Clear()
        End If

        ' An operation with no target has no step 2 to answer: history, the full help and the two
        ' Explorer registrations run on nothing at all.
        Dim needsTarget = Not TakesNoTarget(op)
        targetLabel.Visible = needsTarget
        targetCombo.Visible = needsTarget
        browseFolderBtn.Visible = needsTarget
        browseFileBtn.Visible = needsTarget AndAlso TakesFile(op)

        Select Case op
            Case "compare"
                targetLabel.Text = L("ui_source")
            Case "wipe", "cd"
                targetLabel.Text = L("ui_folder")
            Case "info", "check"
                targetLabel.Text = L("shell_lbl_target")
            Case "from"
                targetLabel.Text = L("ui_list")
            Case Else
                If IsCopyOp(op) Then
                    targetLabel.Text = L("ui_source")
                ElseIf IsFdsecOp(op) Then
                    targetLabel.Text = L("shell_lbl_file")
                Else
                    targetLabel.Text = L("shell_lbl_target_drive_folder")
                End If
        End Select

        Select Case op
            Case "speed", "fill"
                paramLabel.Text = L("shell_lbl_params_size")
            Case "test"
                paramLabel.Text = L("shell_lbl_params_count")
            Case "compare"
                paramLabel.Text = L("ui_dest")
            Case Else
                If IsCopyOp(op) Then
                    paramLabel.Text = L("ui_dest")
                ElseIf op = "secure" OrElse op = "unsecure" Then
                    ' The box becomes the CLI's `to <dest>` for the two verbs that write a file.
                    paramLabel.Text = L("shell_fdsec_dest")
                Else
                    paramLabel.Text = L("shell_lbl_params")
                End If
        End Select

        ApplyFlagShape(op)

        targetCombo.AccessibleName = targetLabel.Text
        paramBox.AccessibleName = paramLabel.Text

        RuleChanged()
    End Sub

    ' Which flags and which rule pickers this operation actually has. The page shows the ones the
    ' CLI would accept and hides the rest, rather than offering four flags to twenty-three verbs.
    Private Sub ApplyFlagShape(op As String)
        flagCheckShort.Visible = Array.IndexOf(New String() {"info", "speed", "cd"}, op) >= 0
        ' `del` is two options with one name: the test files of a speed/test/fill run, and the
        ' original (or the container) of a secret-file run.
        flagCheckDel.Visible = Array.IndexOf(New String() {"speed", "test", "fill",
                                                           "secure", "unsecure"}, op) >= 0
        flagCheckNoDel.Visible = (op = "speed")
        flagCheckMax.Visible = (op = "speed")
        flagCheckVerify.Visible = (op = "fill")
        flagCheckYes.Visible = (op = "probe" OrElse op = "recover")
        flagCheckFix.Visible = (op = "probe")
        flagCheckFormat.Visible = (op = "recover")
        flagCheckAllUsers.Visible = (op = "fdsec register")
        flagCheckWipe.Visible = (op = "secure")
        flagCheckRename.Visible = (op = "secure")
        flagCheckHere.Visible = (op = "unsecure")
        flagCheckRw.Visible = (op = "reveal")
        flagCheckKeep.Visible = (op = "reveal")
        ' -y belongs to wipe and to the secret-file verbs, where it is what lets a delete or an
        ' overwrite go through without a console to confirm it on.
        flagCheckForce.Visible = (op = "wipe") OrElse (op = "secure") OrElse (op = "unsecure")
        flagCheckNoHist.Visible = (op <> "help")
        flagsLabel.Visible = AnyFlagVisible()

        ' The password block: shown for the five verbs that need one, and the second box only for
        ' the one whose typo cannot be found out until the day the file is wanted back.
        Dim fdsec = IsFdsecOp(op)
        credRow.Visible = fdsec
        credConfirmLabel.Visible = (op = "secure")
        credConfirmBox.Visible = (op = "secure")
        If Not fdsec Then
            credBox.Text = ""
            credConfirmBox.Text = ""
            credShowCheck.Checked = False
        End If
        UpdateFdsecNotice()
        UpdateRunButtonState()

        Dim dup = (op = "cd")
        Dim cmp = (op = "compare")
        dupRuleLabel.Visible = dup
        dupRuleCombo.Visible = dup
        dupActionLabel.Visible = dup
        dupActionCombo.Visible = dup
        dupMoveBox.Visible = dup
        dupMoveBrowseBtn.Visible = dup
        dupListLabel.Visible = dup
        dupListBox.Visible = dup
        dupQuietCheck.Visible = dup
        cmpRuleLabel.Visible = cmp
        cmpRuleCombo.Visible = cmp
        ruleNotice.Visible = dup OrElse cmp
        rulesRow.Visible = dup OrElse cmp
        checkOptions.Visible = (op = "check")
    End Sub

    Private Function AnyFlagVisible() As Boolean
        For Each c As Control In flagsFlow.Controls
            If c.Visible Then Return True
        Next
        Return False
    End Function

    ' "Open in Command" hands over the exact command a job page would have run (section 10 item 2).
    Public Sub SetCommand(cmd As String)
        cmdLineBox.Text = cmd
    End Sub

    ' The three below exist for SelfTest.vb: with twenty-three operations on one page, "every one
    ' of them writes a command and explains itself in every locale" is a claim that should be
    ' checked by a build rather than by opening the dropdown.
    Public Function OperationCount() As Integer
        Return opCombo.Items.Count
    End Function

    Public Function SelectOperation(index As Integer) As String
        opCombo.SelectedIndex = index
        Return CurrentOp()
    End Function

    Public Function CurrentCommandLine() As String
        Return cmdLineBox.Text
    End Function

    Public Function CurrentHint() As String
        Return opHintLabel.Text
    End Function

    ' Also for SelfTest.vb: it types a password in and then checks that the command line does not
    ' contain it. That the password stays off the line is the whole reason these five verbs could
    ' be put on this page at all, so it is checked by the build and not by reading the code.
    Public Sub SetCredentialForTest(value As String)
        credBox.Text = value
        credConfirmBox.Text = value
    End Sub

    ' The CLI has two argument orders, not one: the target verbs read "<target> <op>", while the
    ' copy family, compare, check and the batch runner are verb-first. The page used to write the
    ' first shape for all of them, so the expert page produced a command the CLI would refuse for
    ' three of its ten operations.
    Private Sub BuildCommandLine()
        Dim target = targetCombo.Text.Trim()
        Dim op = CurrentOp()
        Dim param = paramBox.Text.Trim()

        Dim args As New List(Of String)()
        If IsFdsecOp(op) Then
            cmdLineBox.Text = "filedo.exe " & ArgQuoting.JoinArgs(FdsecArgs(op, target, param))
            Return
        End If

        If TakesNoTarget(op) Then
            ' "fdsec register" is two words to this page and two arguments to the CLI.
            args.AddRange(op.Split(" "c))
        ElseIf Array.IndexOf(TargetFirstOps, op) >= 0 Then
            If Not String.IsNullOrEmpty(target) Then args.Add(target)
            If Not String.IsNullOrEmpty(op) Then args.Add(op)
        Else
            If Not String.IsNullOrEmpty(op) Then args.Add(op)
            If Not String.IsNullOrEmpty(target) Then args.Add(target)
        End If

        ' `fill verify` is its own operation and takes no size, so the size box is not written
        ' into a line that would then be refused.
        If Not (op = "fill" AndAlso flagCheckVerify.Checked) Then
            If Not String.IsNullOrEmpty(param) Then args.Add(param)
        End If

        If flagCheckVerify.Visible AndAlso flagCheckVerify.Checked Then args.Add("verify")
        If flagCheckMax.Visible AndAlso flagCheckMax.Checked Then args.Add("max")
        If flagCheckDel.Visible AndAlso flagCheckDel.Checked Then args.Add("del")
        If flagCheckNoDel.Visible AndAlso flagCheckNoDel.Checked Then args.Add("nodel")
        If flagCheckShort.Visible AndAlso flagCheckShort.Checked Then args.Add("short")
        If flagCheckYes.Visible AndAlso flagCheckYes.Checked Then args.Add("yes")
        If flagCheckFix.Visible AndAlso flagCheckFix.Checked Then args.Add("fix")
        If flagCheckFormat.Visible AndAlso flagCheckFormat.Checked Then args.Add("format")
        If flagCheckAllUsers.Visible AndAlso flagCheckAllUsers.Checked Then args.Add("-all-users")
        If flagCheckForce.Visible AndAlso flagCheckForce.Checked Then args.Add("-y")

        Select Case op
            Case "cd" : args.AddRange(DuplicateArgs())
            Case "compare"
                Dim rule = CliRules.CmpRuleTokens(Math.Max(cmpRuleCombo.SelectedIndex, 0))
                If rule <> "" Then args.AddRange(rule.Split(" "c))
            Case "check" : args.AddRange(checkOptions.ToArgs())
        End Select

        If flagCheckNoHist.Visible AndAlso flagCheckNoHist.Checked Then args.Add("nohist")

        cmdLineBox.Text = "filedo.exe " & ArgQuoting.JoinArgs(args)
    End Sub

    ' A secret-file command: the verb, its dispositions, and the credential by name.
    '
    ' `pe:` is what makes this page safe to have these verbs on at all. The CLI requires an
    ' explicit credential source whenever any option is present (fdsec_cmd.go), and this is the
    ' one source that names a place rather than a secret - so the line can be copied, logged and
    ' read over a shoulder without giving anything away.
    Private Function FdsecArgs(op As String, target As String, param As String) As List(Of String)
        Dim args As New List(Of String)()

        If op.StartsWith("fdsec ") Then
            args.AddRange(op.Split(" "c))
            If Not String.IsNullOrEmpty(target) Then args.Add(target)
        Else
            If Not String.IsNullOrEmpty(target) Then args.Add(target)
            args.Add(op)
        End If

        If flagCheckDel.Visible AndAlso flagCheckDel.Checked Then args.Add("del")
        If flagCheckWipe.Visible AndAlso flagCheckWipe.Checked Then args.Add("wipe")
        If flagCheckRename.Visible AndAlso flagCheckRename.Checked Then args.Add("rename")
        If flagCheckHere.Visible AndAlso flagCheckHere.Checked Then args.Add("here")
        If flagCheckRw.Visible AndAlso flagCheckRw.Checked Then args.Add("-rw")
        If flagCheckKeep.Visible AndAlso flagCheckKeep.Checked Then args.Add("-keep")

        ' The parameter box is `to <dest>` for the two verbs that write an ordinary file, and
        ' `rename` already answers the same question, so the two are not written together.
        If (op = "secure" OrElse op = "unsecure") AndAlso param <> "" AndAlso Not flagCheckRename.Checked Then
            args.Add("to")
            args.Add(param)
        End If

        If flagCheckForce.Visible AndAlso flagCheckForce.Checked Then args.Add("-y")
        args.Add("pe:" & CredentialEnvName)
        If flagCheckNoHist.Checked Then args.Add("nohist")
        Return args
    End Function

    Private Function DuplicateArgs() As List(Of String)
        Dim args As New List(Of String)()

        Dim rule = CliRules.DupRuleTokens(Math.Max(dupRuleCombo.SelectedIndex, 0))
        If rule <> "" Then args.Add(rule)

        Select Case dupActionCombo.SelectedIndex
            Case 1
                args.Add("del")
            Case 2
                Dim dir = dupMoveBox.Text.Trim()
                If dir <> "" Then
                    args.Add("move")
                    args.Add(dir)
                End If
        End Select

        Dim listPath = dupListBox.Text.Trim()
        If listPath <> "" Then
            args.Add("list")
            args.Add(listPath)
        End If

        If dupQuietCheck.Checked Then args.Add("quiet")
        Return args
    End Function

    Private Sub HookRunner()
        AddHandler runner.OutputLineReceived,
            Sub(line, isErr)
                BeginInvoke(Sub() outputBox.AppendText(line & Environment.NewLine))
            End Sub
        AddHandler runner.NoteReported,
            Sub(msg)
                BeginInvoke(Sub() outputBox.AppendText("[note] " & msg & Environment.NewLine))
            End Sub
    End Sub

    Private Async Sub RunBtn_Click(sender As Object, e As EventArgs)
        If runner.IsActive Then Return

        outputBox.Clear()
        verdictLabel.Text = L("shell_state_running")
        verdictLabel.ForeColor = Theme.Current.Accent
        runBtn.Enabled = False
        stopBtn.Enabled = True

        Dim cmd = cmdLineBox.Text.Trim()
        If cmd.StartsWith("filedo.exe ", StringComparison.OrdinalIgnoreCase) Then
            cmd = cmd.Substring("filedo.exe ".Length).Trim()
        End If

        ' A reveal's run lasts exactly as long as the plaintext copy does, so the button that
        ' ends it says what it ends (SP-0005 8.2) - the same wording the Protect page uses.
        Dim revealing = (CurrentOp() = "reveal") AndAlso Not flagCheckRw.Checked
        stopBtn.Text = If(revealing, L("shell_btn_remove_copy"), L("shell_btn_stop"))

        ' The password is put on the child process's environment and nowhere else. It is read off
        ' the line rather than off the operation, so a hand-edited command that still names the
        ' variable keeps working - and one that does not, gets nothing.
        Dim env As Dictionary(Of String, String) = Nothing
        If cmd.Contains("pe:" & CredentialEnvName) Then
            env = New Dictionary(Of String, String) From {{CredentialEnvName, credBox.Text}}
        End If

        ' The text on screen becomes the argument list by the same rules Windows would use, so a
        ' quoted path with a space in it stays one argument (ArgQuoting.SplitArgs).
        Dim res = Await runner.ExecuteAsync(ArgQuoting.SplitArgs(cmd), envVars:=env)

        runBtn.Enabled = True
        stopBtn.Enabled = False
        stopBtn.Text = L("shell_btn_stop")
        ShowVerdict(res)
    End Sub

    Private Sub ShowVerdict(res As Runner.RunResult)
        Dim p = Theme.Current
        Dim word = L("shell_verdict_" & res.Verdict.ToLowerInvariant().Replace(" ", "_"))
        Dim line = Theme.Glyph(VerdictGlyph(res.Verdict)) & " " & word & " - " &
                   String.Format(L("shell_result_summary_fmt"), res.Duration.ToString("mm\:ss"), res.ExitCode)
        If Not String.IsNullOrEmpty(res.Reason) Then line &= Environment.NewLine & L(res.Reason)

        verdictLabel.Text = line
        Select Case res.Verdict.ToLowerInvariant()
            Case "passed", "done" : verdictLabel.ForeColor = p.Success
            Case "failed" : verdictLabel.ForeColor = p.Danger
            Case "stopped" : verdictLabel.ForeColor = p.Warning
            Case Else : verdictLabel.ForeColor = p.MutedText
        End Select
    End Sub

    Private Sub StyleStopButton()
        If stopBtn Is Nothing Then Return
        Dim p = Theme.Current
        If stopBtn.Enabled Then
            Ui.StyleButton(stopBtn, p.Danger, p.AccentText, p.Danger)
        Else
            Ui.StyleButton(stopBtn, p.SurfaceAlt, p.MutedText, p.Border)
        End If
    End Sub

    Private Shared Function VerdictGlyph(verdict As String) As String
        Select Case verdict.ToLowerInvariant()
            Case "passed", "done" : Return ChrW(&HE73E)
            Case "failed" : Return ChrW(&HE711)
            Case "stopped" : Return ChrW(&HE71A)
            Case Else : Return ChrW(&HE9CE)
        End Select
    End Function

    Public Sub ApplyTheme()
        Dim p = Theme.Current
        BackColor = p.Background
        ForeColor = p.Text

        For Each c As ShellCard In New ShellCard() {inputCard, runCard}
            c.BackColor = p.Surface
            c.BorderColour = p.Border
            c.Invalidate()
        Next

        For Each h As Label In New Label() {opHeader, targetHeader, paramsHeader, runHeader}
            h.Font = Theme.FontSubtitle()
            h.ForeColor = p.Accent
        Next

        For Each lbl As Label In New Label() {targetLabel, opLabel, paramLabel, flagsLabel, cmdLabel}
            lbl.Font = Theme.FontBody()
            lbl.ForeColor = p.Text
        Next

        opHintLabel.Font = Theme.FontBody()
        opHintLabel.ForeColor = p.MutedText

        For Each c As ComboBox In New ComboBox() {targetCombo, opCombo}
            c.Font = Theme.FontBody()
            c.BackColor = p.SurfaceAlt
            c.ForeColor = p.Text
            c.FlatStyle = FlatStyle.Flat
        Next

        paramBox.Font = Theme.FontBody()
        paramBox.BackColor = p.SurfaceAlt
        paramBox.ForeColor = p.Text
        paramBox.BorderStyle = BorderStyle.FixedSingle

        For Each c As Control In flagsFlow.Controls
            c.Font = Theme.FontBody()
            c.ForeColor = p.Text
        Next
        ' The three that skip a prompt or reformat a volume keep the danger colour, on or off:
        ' they are the flags whose consequence outlives the run.
        For Each c As CheckBox In New CheckBox() {flagCheckForce, flagCheckYes, flagCheckFormat}
            c.ForeColor = p.Danger
        Next

        For Each lbl As Label In New Label() {dupRuleLabel, dupActionLabel, cmpRuleLabel, dupListLabel}
            lbl.Font = Theme.FontBody()
            lbl.ForeColor = p.Text
        Next
        ruleNotice.Font = Theme.FontCaption()
        ruleNotice.ForeColor = p.MutedText
        For Each c As ComboBox In New ComboBox() {dupRuleCombo, dupActionCombo, cmpRuleCombo}
            c.Font = Theme.FontBody()
            c.BackColor = p.SurfaceAlt
            c.ForeColor = p.Text
            c.FlatStyle = FlatStyle.Flat
        Next
        For Each tb As TextBox In New TextBox() {dupMoveBox, dupListBox}
            tb.Font = Theme.FontBody()
            tb.BackColor = p.SurfaceAlt
            tb.ForeColor = p.Text
            tb.BorderStyle = BorderStyle.FixedSingle
        Next
        dupQuietCheck.Font = Theme.FontBody()
        dupQuietCheck.ForeColor = p.Text
        checkOptions.ApplyTheme()

        ' The secret-file block. The notice under the password is the one label here whose colour
        ' carries meaning: it warns while the credential is empty, short or mistyped.
        For Each lbl As Label In New Label() {credLabel, credConfirmLabel}
            lbl.Font = Theme.FontBody()
            lbl.ForeColor = p.Text
        Next
        For Each tb As TextBox In New TextBox() {credBox, credConfirmBox}
            tb.Font = Theme.FontBody()
            tb.BackColor = p.SurfaceAlt
            tb.ForeColor = p.Text
            tb.BorderStyle = BorderStyle.FixedSingle
        Next
        credShowCheck.Font = Theme.FontBody()
        credShowCheck.ForeColor = p.Text
        credNoticeLabel.Font = Theme.FontBodyStrong()
        credNoticeLabel.ForeColor = If(credNoticeLabel.Text = L("shell_cred_ok"), p.MutedText, p.Warning)
        fdsecNoticeLabel.Font = Theme.FontCaption()
        fdsecNoticeLabel.ForeColor = If(fdsecNoticeLabel.Text = L("shell_cmd_fdsec_needs_y"), p.Warning, p.MutedText)
        flagCheckWipe.ForeColor = If(flagCheckWipe.Checked, p.Danger, p.Text)

        cmdLineBox.Font = Theme.FontMono()
        cmdLineBox.BackColor = p.SurfaceAlt
        cmdLineBox.ForeColor = p.Text
        cmdLineBox.BorderStyle = BorderStyle.FixedSingle

        outputBox.Font = Theme.FontMono()
        outputBox.BackColor = p.SurfaceAlt
        outputBox.ForeColor = p.Text
        outputBox.BorderStyle = BorderStyle.FixedSingle

        verdictLabel.Font = Theme.FontBodyStrong()

        runBtn.Font = Theme.FontBodyStrong()
        Ui.StyleButton(runBtn, p.Accent, p.AccentText, p.Accent)
        stopBtn.Font = Theme.FontBodyStrong()
        StyleStopButton()

        For Each b In secondaryButtons
            b.Font = Theme.FontBody()
            Ui.StyleButton(b, p.SurfaceAlt, p.Text, p.Border)
        Next

        Invalidate(True)
    End Sub

End Class
