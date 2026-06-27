Imports System.IO
Imports System.Linq
Imports System.Diagnostics
Imports Microsoft.Win32

Public Class MainForm
    Private debugMode As Boolean = False
    Private logWriter As StreamWriter = Nothing
    Private updating As Boolean = True   ' suppress event churn while we set controls programmatically

    Private currentLang As String = "en"
    Private dict As Dictionary(Of String, String) = Localization.GetDict("en")

    ' Every operation the CLI understands. The combo item text IS the CLI token.
    Private ReadOnly operations As String() = {
        "info", "speed", "test", "fill", "clean", "cd",
        "copy", "fastcopy", "synccopy", "balanced", "maxcopy", "smartcopy", "safecopy",
        "wipe", "compare", "check", "probe", "recover", "from", "hist", "help"
    }

    Private ReadOnly copyFamily As String() = {
        "copy", "fastcopy", "synccopy", "balanced", "maxcopy", "smartcopy", "safecopy"
    }

    Private ReadOnly targetOps As String() = {
        "info", "speed", "test", "fill", "clean", "cd", "wipe", "probe", "recover"
    }

    Private ReadOnly compareDelRules As String() = {
        "(no delete)", "del source", "del target", "del old", "del new",
        "del small", "del big", "del old target", "del new source",
        "del small source", "del big target"
    }

    Private Sub MainForm_Load(sender As Object, e As EventArgs) Handles MyBase.Load
        CheckDebugMode()
        InitializeForm()
    End Sub

    Private Sub CheckDebugMode()
        Dim args As String() = Environment.GetCommandLineArgs()
        debugMode = args.Contains("-debug")
        If debugMode Then
            Try
                logWriter = New StreamWriter("filedo_win_debug.log", False)
                LogMessage("=== FileDO VB.NET GUI Start ===")
            Catch ex As Exception
                MessageBox.Show("Failed to create log file: " & ex.Message)
            End Try
        End If
    End Sub

    Private Sub InitializeForm()
        updating = True

        cmbLang.Items.Clear()
        cmbLang.Items.AddRange(Localization.LanguageNames.Cast(Of Object)().ToArray())

        cmbOperation.Items.Clear()
        cmbOperation.Items.AddRange(operations.Cast(Of Object)().ToArray())
        cmbOperation.SelectedItem = "info"

        cmbCompareDel.Items.Clear()
        cmbCompareDel.Items.AddRange(compareDelRules.Cast(Of Object)().ToArray())
        cmbCompareDel.SelectedIndex = 0

        PopulateDrives()

        chkDevice.Checked = True
        rbNew.Checked = True
        txtSize.Text = "100"

        currentLang = LoadLang()
        Dim li = Array.IndexOf(Localization.Languages, currentLang)
        cmbLang.SelectedIndex = If(li >= 0, li, 0)

        updating = False
        ApplyLanguage()
        RefreshAll()
        LogMessage("Form initialized")
    End Sub

    Private Sub PopulateDrives()
        cmbDrive.Items.Clear()
        Try
            For Each dv As DriveInfo In DriveInfo.GetDrives()
                cmbDrive.Items.Add(dv.Name.TrimEnd("\"c)) ' "C:\" -> "C:"
            Next
        Catch
        End Try
        Dim sys = SystemDriveLetter()
        If cmbDrive.Items.Contains(sys) Then
            cmbDrive.SelectedItem = sys
        ElseIf cmbDrive.Items.Count > 0 Then
            cmbDrive.SelectedIndex = 0
        Else
            cmbDrive.Text = sys
        End If
    End Sub

    ' ---- helpers ----------------------------------------------------------

    Private Function L(key As String) As String
        Dim v As String = Nothing
        If dict IsNot Nothing AndAlso dict.TryGetValue(key, v) Then Return v
        Return key
    End Function

    Private Function CurrentOp() As String
        If cmbOperation.SelectedItem Is Nothing Then Return "info"
        Return cmbOperation.SelectedItem.ToString()
    End Function

    Private Function IsCopyFamily(op As String) As Boolean
        Return copyFamily.Contains(op)
    End Function

    Private Function IsTwoPath(op As String) As Boolean
        Return IsCopyFamily(op) OrElse op = "compare"
    End Function

    Private Function UsesTargetType(op As String) As Boolean
        Return targetOps.Contains(op)
    End Function

    Private Function GetSelectedTarget() As String
        If chkDevice.Checked Then Return "device"
        If chkFolder.Checked Then Return "folder"
        If chkNetwork.Checked Then Return "network"
        If chkFile.Checked Then Return "file"
        Return ""
    End Function

    Private Function SystemDriveLetter() As String
        Dim s = Environment.GetEnvironmentVariable("SystemDrive")
        If String.IsNullOrEmpty(s) Then s = "C:"
        Return s.TrimEnd("\"c)
    End Function

    ' Path 1: the drive combo when it is showing (device), else the text box.
    Private Function CurrentPath1() As String
        If cmbDrive.Visible Then Return cmbDrive.Text.Trim()
        Return txtPath.Text.Trim()
    End Function

    Private Function Q(s As String) As String
        If String.IsNullOrEmpty(s) Then Return s
        If s.Contains(" ") AndAlso Not s.StartsWith("""") Then Return """" & s & """"
        Return s
    End Function

    Private Sub LogMessage(message As String)
        If debugMode AndAlso logWriter IsNot Nothing Then
            logWriter.WriteLine(DateTime.Now.ToString("yyyy/MM/dd HH:mm:ss") & " " & message)
            logWriter.Flush()
        End If
    End Sub

    ' ---- language ---------------------------------------------------------

    Private Function LoadLang() As String
        Try
            Using k = Registry.CurrentUser.OpenSubKey("Software\FileDO")
                If k IsNot Nothing Then
                    Dim v = TryCast(k.GetValue("GuiLang"), String)
                    If v IsNot Nothing AndAlso Localization.Languages.Contains(v) Then Return v
                End If
            End Using
        Catch
        End Try
        Dim c = System.Globalization.CultureInfo.CurrentUICulture.TwoLetterISOLanguageName.ToLowerInvariant()
        If Localization.Languages.Contains(c) Then Return c
        Return "en"
    End Function

    Private Sub SaveLang(lang As String)
        Try
            Using k = Registry.CurrentUser.CreateSubKey("Software\FileDO")
                k.SetValue("GuiLang", lang)
            End Using
        Catch
        End Try
    End Sub

    Private Sub ApplyLanguage()
        dict = Localization.GetDict(currentLang)

        Me.Text = L("ui_title")
        lblLang.Text = L("ui_lang")
        lblTarget.Text = L("ui_target")
        lblOperation.Text = L("ui_operation")
        lblPath2.Text = L("ui_dest")
        lblSize.Text = L("ui_size")
        lblFlags.Text = L("ui_flags")
        btnBrowse.Text = L("ui_browse")
        btnBrowse2.Text = L("ui_browse")
        btnCopy.Text = L("ui_copy")
        btnRun.Text = L("ui_run")
        lblCommandTitle.Text = "Command:"
        grpDuplicateOptions.Text = L("dup_title")
        rbOld.Text = L("dup_old")
        rbNew.Text = L("dup_new")
        rbAbc.Text = L("dup_abc")
        rbXyz.Text = L("dup_xyz")
        chkMove.Text = L("dup_move")
        lblCompareDel.Text = L("cmp_label")

        ' static flag tooltips
        tip.SetToolTip(chkMax, L("fl_max"))
        tip.SetToolTip(chkDelete, L("fl_del"))
        tip.SetToolTip(chkNoDel, L("fl_nodel"))
        tip.SetToolTip(chkShort, L("fl_short"))
        tip.SetToolTip(chkHist, L("fl_hist"))
        tip.SetToolTip(chkForce, L("fl_force"))
    End Sub

    Private Sub OnLangChanged(sender As Object, e As EventArgs) Handles cmbLang.SelectedIndexChanged
        If updating Then Return
        If cmbLang.SelectedIndex < 0 Then Return
        currentLang = Localization.Languages(cmbLang.SelectedIndex)
        SaveLang(currentLang)
        ApplyLanguage()
        RefreshAll()
    End Sub

    ' ---- single place that reconciles UI state + builds the command -------

    Private Sub RefreshAll()
        If updating Then Return
        updating = True
        Try
            EnforceTargetForOperation()
            UpdateUiForOperation()
            BuildCommand()
            BuildHelp()
        Finally
            updating = False
        End Try
    End Sub

    ' Keep target checkboxes consistent with the chosen operation.
    Private Sub EnforceTargetForOperation()
        Dim op As String = CurrentOp()

        If op = "probe" OrElse op = "recover" Then
            chkDevice.Checked = True
            chkFolder.Checked = False
            chkNetwork.Checked = False
            chkFile.Checked = False
            Return
        End If

        Dim fileInvalid As Boolean = {"speed", "test", "fill", "clean", "cd", "wipe"}.Contains(op)
        If fileInvalid AndAlso chkFile.Checked Then
            chkFile.Checked = False
            chkDevice.Checked = True
        End If
    End Sub

    Private Sub UpdateUiForOperation()
        Dim op As String = CurrentOp()
        Dim isTarget As Boolean = UsesTargetType(op)
        Dim isTwo As Boolean = IsTwoPath(op)
        Dim isProbe As Boolean = (op = "probe" OrElse op = "recover")
        Dim noPath As Boolean = (op = "hist" OrElse op = "help")

        chkDevice.Enabled = isTarget AndAlso Not isProbe
        chkFolder.Enabled = isTarget AndAlso Not isProbe
        chkNetwork.Enabled = isTarget AndAlso Not isProbe
        chkFile.Enabled = (op = "info")

        ' drive picker for device target; text box for everything else
        Dim showDrive As Boolean = (GetSelectedTarget() = "device") AndAlso (isTarget OrElse isProbe)
        cmbDrive.Visible = showDrive
        txtPath.Visible = (Not noPath) AndAlso (Not showDrive)
        btnBrowse.Visible = (Not noPath) AndAlso (Not showDrive)

        If showDrive Then
            lblPath.Text = L("ui_drive")
        ElseIf isTwo Then
            lblPath.Text = L("ui_source")
        ElseIf op = "from" Then
            lblPath.Text = L("ui_list")
        ElseIf op = "check" Then
            lblPath.Text = L("ui_folder")
        Else
            lblPath.Text = L("ui_path")
        End If
        lblPath.Visible = Not noPath

        lblPath2.Visible = isTwo
        txtPath2.Visible = isTwo
        btnBrowse2.Visible = isTwo

        Dim usesSize As Boolean = (op = "speed" OrElse op = "fill")
        lblSize.Enabled = usesSize
        txtSize.Enabled = usesSize AndAlso Not chkMax.Checked

        chkMax.Enabled = usesSize
        chkDelete.Enabled = {"test", "fill", "cd"}.Contains(op)
        chkNoDel.Enabled = (op = "speed" OrElse op = "fill")
        chkShort.Enabled = {"info", "speed", "test", "fill"}.Contains(op)
        chkForce.Enabled = (op = "wipe")
        chkHist.Enabled = isTarget

        If Not chkMax.Enabled Then chkMax.Checked = False
        If Not chkDelete.Enabled Then chkDelete.Checked = False
        If Not chkNoDel.Enabled Then chkNoDel.Checked = False
        If Not chkShort.Enabled Then chkShort.Checked = False
        If Not chkForce.Enabled Then chkForce.Checked = False
        If Not chkHist.Enabled Then chkHist.Checked = False

        grpDuplicateOptions.Visible = (op = "cd")
        lblCompareDel.Visible = (op = "compare")
        cmbCompareDel.Visible = (op = "compare")

        txtMoveTarget.Enabled = chkMove.Checked
        btnBrowseMove.Enabled = chkMove.Checked

        ' shrink the help panel when the dup/compare band is in use
        If grpDuplicateOptions.Visible OrElse cmbCompareDel.Visible Then
            txtHelp.Height = 70
        Else
            txtHelp.Height = 150
        End If
    End Sub

    Private Sub BuildCommand()
        Dim op As String = CurrentOp()
        Dim p1 As String = CurrentPath1()
        Dim p2 As String = txtPath2.Text.Trim()
        Dim size As String = txtSize.Text.Trim()
        Dim parts As New List(Of String) From {"filedo.exe"}

        If IsCopyFamily(op) Then
            parts.Add(op)
            If p1 <> "" Then parts.Add(Q(p1))
            If p2 <> "" Then parts.Add(Q(p2))
        ElseIf op = "compare" Then
            parts.Add("compare")
            If p1 <> "" Then parts.Add(Q(p1))
            If p2 <> "" Then parts.Add(Q(p2))
            If cmbCompareDel.SelectedIndex > 0 Then parts.Add(cmbCompareDel.SelectedItem.ToString())
        ElseIf op = "check" Then
            parts.Add("check")
            If p1 <> "" Then parts.Add(Q(p1))
        ElseIf op = "from" Then
            parts.Add("from")
            If p1 <> "" Then parts.Add(Q(p1))
        ElseIf op = "hist" Then
            parts.Add("hist")
        ElseIf op = "help" Then
            parts.Add("help")
        ElseIf op = "probe" OrElse op = "recover" Then
            parts.Add("device")
            If p1 <> "" Then parts.Add(Q(p1))
            parts.Add(op)
        Else
            ' target operations: info / speed / test / fill / clean / cd / wipe
            Dim t As String = GetSelectedTarget()
            If t <> "" Then parts.Add(t)
            If p1 <> "" Then parts.Add(Q(p1))

            Select Case op
                Case "speed"
                    parts.Add("speed")
                    If chkMax.Checked Then
                        parts.Add("max")
                    ElseIf size <> "" Then
                        parts.Add(size)
                    End If
                Case "fill"
                    parts.Add("fill")
                    If chkMax.Checked Then
                        parts.Add("max")
                    ElseIf size <> "" Then
                        parts.Add(size)
                    End If
                Case "wipe"
                    parts.Add("wipe")
                Case "info", "test", "clean", "cd"
                    parts.Add(op)
            End Select

            If chkDelete.Enabled AndAlso chkDelete.Checked Then parts.Add("del")
            If chkNoDel.Enabled AndAlso chkNoDel.Checked Then parts.Add("nodel")
            If chkShort.Enabled AndAlso chkShort.Checked Then parts.Add("short")
            If chkForce.Enabled AndAlso chkForce.Checked Then parts.Add("--force")

            If op = "cd" Then
                If rbOld.Checked Then parts.Add("old")
                If rbNew.Checked Then parts.Add("new")
                If rbAbc.Checked Then parts.Add("abc")
                If rbXyz.Checked Then parts.Add("xyz")
                If chkMove.Checked AndAlso txtMoveTarget.Text.Trim() <> "" Then
                    parts.Add("move")
                    parts.Add(Q(txtMoveTarget.Text.Trim()))
                End If
            End If

            If chkHist.Enabled AndAlso chkHist.Checked Then parts.Add("hist")
        End If

        txtCommand.Text = String.Join(" ", parts)
        LogMessage("Command: " & txtCommand.Text)
    End Sub

    Private Sub BuildHelp()
        Dim op As String = CurrentOp()
        Dim lines As New List(Of String)

        lines.Add(L("op_" & op))
        tip.SetToolTip(cmbOperation, L("op_" & op))

        If op <> "hist" AndAlso op <> "help" Then
            lines.Add("")
            lines.Add(ExampleValue())
        End If

        If ShowSysDriveNote() Then
            lines.Add("")
            lines.Add(L("hl_note") & " " & L("note_sysdrive"))
        End If

        Dim flags = ActiveFlagDescriptions()
        If flags.Count > 0 Then
            lines.Add("")
            lines.Add(L("hl_flags"))
            For Each f As String In flags
                lines.Add("  " & f)
            Next
        End If

        txtHelp.Lines = lines.ToArray()
    End Sub

    Private Function ExampleValue() As String
        Dim op As String = CurrentOp()
        If IsTwoPath(op) OrElse op = "check" Then Return L("ex_folder")
        If op = "from" Then Return L("ex_file")
        Select Case GetSelectedTarget()
            Case "device" : Return L("ex_device")
            Case "folder" : Return L("ex_folder")
            Case "network" : Return L("ex_network")
            Case "file" : Return L("ex_file")
        End Select
        Return L("ex_folder")
    End Function

    Private Function ShowSysDriveNote() As Boolean
        Dim op As String = CurrentOp()
        If GetSelectedTarget() <> "device" Then Return False
        If Not {"test", "fill", "speed"}.Contains(op) Then Return False
        Dim p = CurrentPath1().TrimEnd("\"c).ToUpperInvariant()
        Return p = SystemDriveLetter().ToUpperInvariant()
    End Function

    Private Function ActiveFlagDescriptions() As List(Of String)
        Dim r As New List(Of String)
        If chkMax.Enabled AndAlso chkMax.Checked Then r.Add(L("fl_max"))
        If chkDelete.Enabled AndAlso chkDelete.Checked Then r.Add(L("fl_del"))
        If chkNoDel.Enabled AndAlso chkNoDel.Checked Then r.Add(L("fl_nodel"))
        If chkShort.Enabled AndAlso chkShort.Checked Then r.Add(L("fl_short"))
        If chkHist.Enabled AndAlso chkHist.Checked Then r.Add(L("fl_hist"))
        If chkForce.Enabled AndAlso chkForce.Checked Then r.Add(L("fl_force"))
        Return r
    End Function

    ' ---- events -----------------------------------------------------------

    Private Sub OnTargetChanged(sender As Object, e As EventArgs) _
        Handles chkDevice.CheckedChanged, chkFolder.CheckedChanged,
                chkNetwork.CheckedChanged, chkFile.CheckedChanged
        If updating Then Return
        Dim cb As CheckBox = TryCast(sender, CheckBox)
        If cb IsNot Nothing AndAlso cb.Checked Then
            updating = True
            If cb IsNot chkDevice Then chkDevice.Checked = False
            If cb IsNot chkFolder Then chkFolder.Checked = False
            If cb IsNot chkNetwork Then chkNetwork.Checked = False
            If cb IsNot chkFile Then chkFile.Checked = False
            updating = False
        End If
        RefreshAll()
    End Sub

    Private Sub OnOperationChanged(sender As Object, e As EventArgs) Handles cmbOperation.SelectedIndexChanged
        RefreshAll()
    End Sub

    Private Sub OnInputChanged(sender As Object, e As EventArgs) _
        Handles txtPath.TextChanged, txtPath2.TextChanged, txtSize.TextChanged,
                txtMoveTarget.TextChanged, cmbCompareDel.SelectedIndexChanged,
                cmbDrive.SelectedIndexChanged, cmbDrive.TextChanged
        RefreshAll()
    End Sub

    Private Sub OnFlagChanged(sender As Object, e As EventArgs) _
        Handles chkMax.CheckedChanged, chkDelete.CheckedChanged, chkNoDel.CheckedChanged,
                chkShort.CheckedChanged, chkHist.CheckedChanged, chkForce.CheckedChanged,
                rbOld.CheckedChanged, rbNew.CheckedChanged, rbAbc.CheckedChanged,
                rbXyz.CheckedChanged, chkMove.CheckedChanged
        RefreshAll()
    End Sub

    Private Sub btnBrowse_Click(sender As Object, e As EventArgs) Handles btnBrowse.Click
        Dim op As String = CurrentOp()
        Dim pickFile As Boolean = (op = "from") OrElse (op = "info" AndAlso chkFile.Checked)
        BrowseInto(txtPath, pickFile)
    End Sub

    Private Sub btnBrowse2_Click(sender As Object, e As EventArgs) Handles btnBrowse2.Click
        BrowseInto(txtPath2, False)
    End Sub

    Private Sub btnBrowseMove_Click(sender As Object, e As EventArgs) Handles btnBrowseMove.Click
        BrowseInto(txtMoveTarget, False)
    End Sub

    Private Sub BrowseInto(box As TextBox, pickFile As Boolean)
        If pickFile Then
            Using dlg As New OpenFileDialog()
                dlg.Title = "Select file"
                dlg.CheckFileExists = True
                If dlg.ShowDialog() = DialogResult.OK Then box.Text = dlg.FileName
            End Using
        Else
            Using dlg As New FolderBrowserDialog()
                dlg.ShowNewFolderButton = True
                If dlg.ShowDialog() = DialogResult.OK Then box.Text = dlg.SelectedPath
            End Using
        End If
    End Sub

    Private Sub btnCopy_Click(sender As Object, e As EventArgs) Handles btnCopy.Click
        Dim cmd As String = txtCommand.Text.Trim()
        If cmd <> "" Then
            Try
                Clipboard.SetText(cmd)
            Catch ex As Exception
                MessageBox.Show("Could not copy to clipboard: " & ex.Message, "FileDO",
                                MessageBoxButtons.OK, MessageBoxIcon.Warning)
            End Try
        End If
    End Sub

    Private Sub btnRun_Click(sender As Object, e As EventArgs) Handles btnRun.Click
        Dim full As String = txtCommand.Text.Trim()
        If full = "" Then Return

        Dim args As String = full
        Dim lower As String = full.ToLowerInvariant()
        If lower.StartsWith("filedo.exe ") Then
            args = full.Substring("filedo.exe ".Length)
        ElseIf lower.StartsWith("filedo ") Then
            args = full.Substring("filedo ".Length)
        End If
        args = args.Trim()

        ' Resolve filedo.exe next to this GUI; otherwise rely on PATH / the MSIX alias.
        Dim exePath As String = Path.Combine(Application.StartupPath, "filedo.exe")
        If Not File.Exists(exePath) Then exePath = "filedo.exe"

        Dim inner As String = """" & exePath & """ " & args & " & pause"
        Try
            Dim psi As New ProcessStartInfo() With {
                .FileName = "cmd.exe",
                .Arguments = "/c """ & inner & """",
                .UseShellExecute = True,
                .CreateNoWindow = False
            }
            Dim home As String = Environment.GetFolderPath(Environment.SpecialFolder.UserProfile)
            If Directory.Exists(home) Then psi.WorkingDirectory = home

            Process.Start(psi)
            LogMessage("Started: " & inner)
        Catch ex As Exception
            MessageBox.Show("Error starting process: " & ex.Message, "FileDO",
                            MessageBoxButtons.OK, MessageBoxIcon.Error)
            LogMessage("Error starting process: " & ex.Message)
        End Try
    End Sub

    Private Sub MainForm_FormClosing(sender As Object, e As FormClosingEventArgs) Handles MyBase.FormClosing
        LogMessage("=== FileDO VB.NET GUI End ===")
        logWriter?.Close()
    End Sub
End Class
