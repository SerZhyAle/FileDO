Imports System.IO
Imports System.Diagnostics

Public Class MainForm
    Private debugMode As Boolean = False
    Private logWriter As StreamWriter = Nothing

    Private Sub MainForm_Load(sender As Object, e As EventArgs) Handles MyBase.Load
        CheckDebugMode()
        InitializeForm()
        UpdateCommand()
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
        LogMessage("Initializing form")
        
        txtSize.Text = "100"
        chkDevice.Checked = True
        chkNoOp.Checked = True
        
        ' Initialize duplicate options
        rbNew.Checked = True        ' Default to "new" mode (keep oldest)
        txtMoveTarget.Enabled = False
        btnBrowseMove.Enabled = False
        grpDuplicateOptions.Visible = False
        
        SetDefaultPath()
        UpdateOperationsAvailability()
        
        LogMessage("Form initialized")
    End Sub

    Private Sub SetDefaultPath()
        If chkDevice.Checked Or chkFolder.Checked Then
            txtPath.Text = "C:\"
        Else
            txtPath.Text = ""
        End If
    End Sub

    Private Sub UpdateOperationsAvailability()
        LogMessage("Updating operations availability")
        
        Dim isFile As Boolean = chkFile.Checked
        Dim isNetwork As Boolean = chkNetwork.Checked
        
        ' Operation availability
        chkFill.Enabled = Not isFile
        chkClean.Enabled = Not isFile And Not isNetwork
        chkTest.Enabled = Not isNetwork
        chkDups.Enabled = Not isFile  ' Дубликаты можно искать в папках, на устройствах и сетевых ресурсах
        
        ' Clear disabled operations
        If Not chkFill.Enabled And chkFill.Checked Then chkFill.Checked = False
        If Not chkClean.Enabled And chkClean.Checked Then chkClean.Checked = False
        If Not chkTest.Enabled And chkTest.Checked Then chkTest.Checked = False
        If Not chkDups.Enabled And chkDups.Checked Then chkDups.Checked = False
        
        ' Flag availability based on operation
        Dim operation As String = GetSelectedOperation()
        chkDelete.Enabled = (operation = "test" Or operation = "fill" Or operation = "cd")
        chkMax.Enabled = (operation = "speed" Or operation = "fill")
        
        ' Duplicate options visibility
        grpDuplicateOptions.Visible = (operation = "cd")
        
        ' Clear disabled flags
        If Not chkDelete.Enabled And chkDelete.Checked Then chkDelete.Checked = False
        If Not chkMax.Enabled And chkMax.Checked Then chkMax.Checked = False
    End Sub

    Private Sub LogMessage(message As String)
        If debugMode AndAlso logWriter IsNot Nothing Then
            logWriter.WriteLine(DateTime.Now.ToString("yyyy/MM/dd HH:mm:ss") & " " & message)
            logWriter.Flush()
        End If
    End Sub

    Private Sub UpdateCommand()
        LogMessage("UpdateCommand called")
        
        Dim target As String = GetSelectedTarget()
        Dim operation As String = GetSelectedOperation()
        Dim path As String = txtPath.Text.Trim()
        Dim size As String = txtSize.Text.Trim()
        Dim flags As String = GetSelectedFlags()
        
        LogMessage($"Components: target='{target}', op='{operation}', path='{path}', size='{size}', flags='{flags}'")
        
        Dim cmd As String = "filedo.exe"
        If Not String.IsNullOrEmpty(target) Then cmd &= " " & target
        If Not String.IsNullOrEmpty(path) Then cmd &= " " & path
        If Not String.IsNullOrEmpty(operation) Then cmd &= " " & operation
        If (operation = "speed" Or operation = "fill") AndAlso Not String.IsNullOrEmpty(size) Then
            cmd &= " " & size
        End If
        If Not String.IsNullOrEmpty(flags) Then cmd &= " " & flags
        
        txtCommand.Text = cmd
        LogMessage($"Final command: '{cmd}'")
    End Sub

    Private Function GetSelectedTarget() As String
        If chkDevice.Checked Then Return "device"
        If chkFolder.Checked Then Return "folder"
        If chkNetwork.Checked Then Return "network"
        If chkFile.Checked Then Return "file"
        Return ""
    End Function

    Private Function GetSelectedOperation() As String
        If chkInfo.Checked Then Return "info"
        If chkSpeed.Checked Then Return "speed"
        If chkFill.Checked Then Return "fill"
        If chkTest.Checked Then Return "test"
        If chkClean.Checked Then Return "clean"
        If chkDups.Checked Then Return "cd"
        Return ""
    End Function

    Private Function GetSelectedFlags() As String
        Dim flags As New List(Of String)
        
        ' Standard flags
        If chkMax.Checked Then flags.Add("max")
        If chkDelete.Checked Then flags.Add("del")
        If chkHelp.Checked Then flags.Add("help")
        If chkHist.Checked Then flags.Add("hist")
        If chkShort.Checked Then flags.Add("short")
        
        ' Duplicate selection mode flags
        If chkDups.Checked Then
            If rbOld.Checked Then flags.Add("old")
            If rbNew.Checked Then flags.Add("new")
            If rbAbc.Checked Then flags.Add("abc")
            If rbXyz.Checked Then flags.Add("xyz")
            
            ' Move option for duplicates
            If chkMove.Checked AndAlso Not String.IsNullOrWhiteSpace(txtMoveTarget.Text) Then
                flags.Add("move")
                flags.Add(txtMoveTarget.Text)
            End If
        End If
        
        Return String.Join(" ", flags)
    End Function

    Private Sub chkDevice_CheckedChanged(sender As Object, e As EventArgs) Handles chkDevice.CheckedChanged
        LogMessage("Device checkbox changed")
        If chkDevice.Checked Then
            chkFolder.Checked = False
            chkNetwork.Checked = False
            chkFile.Checked = False
        End If
        SetDefaultPath()
        UpdateOperationsAvailability()
        UpdateCommand()
    End Sub

    Private Sub chkFolder_CheckedChanged(sender As Object, e As EventArgs) Handles chkFolder.CheckedChanged
        LogMessage("Folder checkbox changed")
        If chkFolder.Checked Then
            chkDevice.Checked = False
            chkNetwork.Checked = False
            chkFile.Checked = False
        End If
        SetDefaultPath()
        UpdateOperationsAvailability()
        UpdateCommand()
    End Sub

    Private Sub chkNetwork_CheckedChanged(sender As Object, e As EventArgs) Handles chkNetwork.CheckedChanged
        LogMessage("Network checkbox changed")
        If chkNetwork.Checked Then
            chkDevice.Checked = False
            chkFolder.Checked = False
            chkFile.Checked = False
        End If
        SetDefaultPath()
        UpdateOperationsAvailability()
        UpdateCommand()
    End Sub

    Private Sub chkFile_CheckedChanged(sender As Object, e As EventArgs) Handles chkFile.CheckedChanged
        LogMessage("File checkbox changed")
        If chkFile.Checked Then
            chkDevice.Checked = False
            chkFolder.Checked = False
            chkNetwork.Checked = False
        End If
        SetDefaultPath()
        UpdateOperationsAvailability()
        UpdateCommand()
    End Sub

    Private Sub chkNoOp_CheckedChanged(sender As Object, e As EventArgs) Handles chkNoOp.CheckedChanged
        If chkNoOp.Checked Then
            chkInfo.Checked = False
            chkSpeed.Checked = False
            chkFill.Checked = False
            chkTest.Checked = False
            chkClean.Checked = False
        End If
        UpdateCommand()
    End Sub

    Private Sub chkInfo_CheckedChanged(sender As Object, e As EventArgs) Handles chkInfo.CheckedChanged
        If chkInfo.Checked Then chkNoOp.Checked = False : ClearOtherOperations("info")
        UpdateCommand()
    End Sub

    Private Sub chkSpeed_CheckedChanged(sender As Object, e As EventArgs) Handles chkSpeed.CheckedChanged
        If chkSpeed.Checked Then chkNoOp.Checked = False : ClearOtherOperations("speed")
        UpdateCommand()
    End Sub

    Private Sub chkFill_CheckedChanged(sender As Object, e As EventArgs) Handles chkFill.CheckedChanged
        If chkFill.Checked Then chkNoOp.Checked = False : ClearOtherOperations("fill")
        UpdateCommand()
    End Sub

    Private Sub chkTest_CheckedChanged(sender As Object, e As EventArgs) Handles chkTest.CheckedChanged
        If chkTest.Checked Then chkNoOp.Checked = False : ClearOtherOperations("test")
        UpdateCommand()
    End Sub

    Private Sub chkClean_CheckedChanged(sender As Object, e As EventArgs) Handles chkClean.CheckedChanged
        If chkClean.Checked Then chkNoOp.Checked = False : ClearOtherOperations("clean")
        UpdateCommand()
    End Sub

    Private Sub ClearOtherOperations(except As String)
        If except <> "info" Then chkInfo.Checked = False
        If except <> "speed" Then chkSpeed.Checked = False
        If except <> "fill" Then chkFill.Checked = False
        If except <> "test" Then chkTest.Checked = False
        If except <> "clean" Then chkClean.Checked = False
        If except <> "cd" Then chkDups.Checked = False
        UpdateOperationsAvailability()
    End Sub

    Private Sub chkMax_CheckedChanged(sender As Object, e As EventArgs) Handles chkMax.CheckedChanged, chkHelp.CheckedChanged, chkHist.CheckedChanged, chkShort.CheckedChanged, chkDelete.CheckedChanged
        UpdateCommand()
    End Sub

    Private Sub txtCommand_TextChanged(sender As Object, e As EventArgs) Handles txtCommand.TextChanged
        LogMessage("Command text manually changed")
    End Sub

    Private Sub txtPath_TextChanged(sender As Object, e As EventArgs) Handles txtPath.TextChanged
        LogMessage("Path text changed")
        UpdateCommand()
    End Sub

    Private Sub txtSize_TextChanged(sender As Object, e As EventArgs) Handles txtSize.TextChanged
        LogMessage("Size text changed")
        UpdateCommand()
    End Sub

    Private Sub btnBrowse_Click(sender As Object, e As EventArgs) Handles btnBrowse.Click
        LogMessage("Browse button clicked")
        
        Using dialog As New FolderBrowserDialog()
            dialog.Description = "Select folder or drive"
            dialog.ShowNewFolderButton = False
            
            If dialog.ShowDialog() = DialogResult.OK Then
                txtPath.Text = dialog.SelectedPath
                LogMessage($"Path selected: {dialog.SelectedPath}")
            End If
        End Using
    End Sub

    Private Sub btnRun_Click(sender As Object, e As EventArgs) Handles btnRun.Click
        LogMessage("RUN button clicked")
        
        Dim path As String = txtPath.Text.Trim()
        If String.IsNullOrEmpty(path) Then
            MessageBox.Show("Path is required", "Error", MessageBoxButtons.OK, MessageBoxIcon.Warning)
            LogMessage("Path is empty")
            Return
        End If
        
        If Not File.Exists("filedo.exe") Then
            MessageBox.Show("filedo.exe not found in current directory", "Error", MessageBoxButtons.OK, MessageBoxIcon.Error)
            LogMessage("filedo.exe not found")
            Return
        End If
        
        Dim command As String = txtCommand.Text.Trim()
        LogMessage($"Executing command: {command}")
        
        Try
            If command.StartsWith("filedo.exe ") Then
                Dim args As String = command.Substring(11) ' Remove "filedo.exe "
                Dim fullCmd As String = $"cmd /c ""filedo.exe {args} & pause"""
                
                LogMessage($"Full command: {fullCmd}")
                
                Dim psi As New ProcessStartInfo() With {
                    .FileName = "cmd",
                    .Arguments = "/c """ & "filedo.exe " & args & " & pause""",
                    .UseShellExecute = True,
                    .CreateNoWindow = False
                }
                
                Process.Start(psi)
                LogMessage("Process started successfully")
            Else
                MessageBox.Show("Command must start with 'filedo.exe'", "Error", MessageBoxButtons.OK, MessageBoxIcon.Error)
            End If
            
        Catch ex As Exception
            MessageBox.Show($"Error starting process: {ex.Message}", "Error", MessageBoxButtons.OK, MessageBoxIcon.Error)
            LogMessage($"Error starting process: {ex.Message}")
        End Try
    End Sub

    Private Sub MainForm_FormClosing(sender As Object, e As FormClosingEventArgs) Handles MyBase.FormClosing
        LogMessage("=== FileDO VB.NET GUI End ===")
        logWriter?.Close()
    End Sub

    Private Sub chkDups_CheckedChanged(sender As Object, e As EventArgs) Handles chkDups.CheckedChanged
        If chkDups.Checked Then 
            chkNoOp.Checked = False : ClearOtherOperations("cd")
            grpDuplicateOptions.Visible = True
        Else
            grpDuplicateOptions.Visible = False
        End If
        UpdateCommand()
    End Sub

    Private Sub chkMove_CheckedChanged(sender As Object, e As EventArgs) Handles chkMove.CheckedChanged
        txtMoveTarget.Enabled = chkMove.Checked
        btnBrowseMove.Enabled = chkMove.Checked
        
        ' Uncheck delete if move is checked
        If chkMove.Checked Then
            chkDelete.Checked = False
        End If
        UpdateCommand()
    End Sub

    Private Sub rbDuplicateMode_CheckedChanged(sender As Object, e As EventArgs) Handles rbOld.CheckedChanged, rbNew.CheckedChanged, rbAbc.CheckedChanged, rbXyz.CheckedChanged
        UpdateCommand()
    End Sub

    Private Sub btnBrowseMove_Click(sender As Object, e As EventArgs) Handles btnBrowseMove.Click
        Using folderDialog As New FolderBrowserDialog()
            folderDialog.Description = "Select folder for duplicates"
            folderDialog.ShowNewFolderButton = True
            
            If folderDialog.ShowDialog() = DialogResult.OK Then
                txtMoveTarget.Text = folderDialog.SelectedPath
                UpdateCommand()
            End If
        End Using
    End Sub
End Class
