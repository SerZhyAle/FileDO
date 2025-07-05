<Global.Microsoft.VisualBasic.CompilerServices.DesignerGenerated()>
Partial Class MainForm
    Inherits System.Windows.Forms.Form

    'Form overrides dispose to clean up the component list.
    <System.Diagnostics.DebuggerNonUserCode()>
    Protected Overrides Sub Dispose(ByVal disposing As Boolean)
        Try
            If disposing AndAlso components IsNot Nothing Then
                components.Dispose()
            End If
        Finally
            MyBase.Dispose(disposing)
        End Try
    End Sub

    'Required by the Windows Form Designer
    Private components As System.ComponentModel.IContainer

    'NOTE: The following procedure is required by the Windows Form Designer
    'It can be modified using the Windows Form Designer.  
    'Do not modify it using the code editor.
    <System.Diagnostics.DebuggerStepThrough()>
    Private Sub InitializeComponent()
        Me.lblTarget = New System.Windows.Forms.Label()
        Me.chkDevice = New System.Windows.Forms.CheckBox()
        Me.chkFolder = New System.Windows.Forms.CheckBox()
        Me.chkNetwork = New System.Windows.Forms.CheckBox()
        Me.chkFile = New System.Windows.Forms.CheckBox()
        Me.lblOperation = New System.Windows.Forms.Label()
        Me.chkNoOp = New System.Windows.Forms.CheckBox()
        Me.chkInfo = New System.Windows.Forms.CheckBox()
        Me.chkSpeed = New System.Windows.Forms.CheckBox()
        Me.chkFill = New System.Windows.Forms.CheckBox()
        Me.chkTest = New System.Windows.Forms.CheckBox()
        Me.chkClean = New System.Windows.Forms.CheckBox()
        Me.lblPath = New System.Windows.Forms.Label()
        Me.txtPath = New System.Windows.Forms.TextBox()
        Me.btnBrowse = New System.Windows.Forms.Button()
        Me.lblSize = New System.Windows.Forms.Label()
        Me.txtSize = New System.Windows.Forms.TextBox()
        Me.lblFlags = New System.Windows.Forms.Label()
        Me.chkMax = New System.Windows.Forms.CheckBox()
        Me.chkHelp = New System.Windows.Forms.CheckBox()
        Me.chkHist = New System.Windows.Forms.CheckBox()
        Me.chkShort = New System.Windows.Forms.CheckBox()
        Me.chkDelete = New System.Windows.Forms.CheckBox()
        Me.btnRun = New System.Windows.Forms.Button()
        Me.lblCommandTitle = New System.Windows.Forms.Label()
        Me.txtCommand = New System.Windows.Forms.TextBox()
        Me.SuspendLayout()
        '
        'lblTarget
        '
        Me.lblTarget.AutoSize = True
        Me.lblTarget.Location = New System.Drawing.Point(18, 23)
        Me.lblTarget.Margin = New System.Windows.Forms.Padding(4, 0, 4, 0)
        Me.lblTarget.Name = "lblTarget"
        Me.lblTarget.Size = New System.Drawing.Size(59, 20)
        Me.lblTarget.TabIndex = 0
        Me.lblTarget.Text = "Target:"
        '
        'chkDevice
        '
        Me.chkDevice.AutoSize = True
        Me.chkDevice.Checked = True
        Me.chkDevice.CheckState = System.Windows.Forms.CheckState.Checked
        Me.chkDevice.Location = New System.Drawing.Point(120, 18)
        Me.chkDevice.Margin = New System.Windows.Forms.Padding(4, 5, 4, 5)
        Me.chkDevice.Name = "chkDevice"
        Me.chkDevice.Size = New System.Drawing.Size(86, 27)
        Me.chkDevice.TabIndex = 1
        Me.chkDevice.Text = "device"
        Me.chkDevice.UseVisualStyleBackColor = True
        '
        'chkFolder
        '
        Me.chkFolder.AutoSize = True
        Me.chkFolder.Location = New System.Drawing.Point(225, 18)
        Me.chkFolder.Margin = New System.Windows.Forms.Padding(4, 5, 4, 5)
        Me.chkFolder.Name = "chkFolder"
        Me.chkFolder.Size = New System.Drawing.Size(81, 27)
        Me.chkFolder.TabIndex = 2
        Me.chkFolder.Text = "folder"
        Me.chkFolder.UseVisualStyleBackColor = True
        '
        'chkNetwork
        '
        Me.chkNetwork.AutoSize = True
        Me.chkNetwork.Location = New System.Drawing.Point(315, 18)
        Me.chkNetwork.Margin = New System.Windows.Forms.Padding(4, 5, 4, 5)
        Me.chkNetwork.Name = "chkNetwork"
        Me.chkNetwork.Size = New System.Drawing.Size(97, 27)
        Me.chkNetwork.TabIndex = 3
        Me.chkNetwork.Text = "network"
        Me.chkNetwork.UseVisualStyleBackColor = True
        '
        'chkFile
        '
        Me.chkFile.AutoSize = True
        Me.chkFile.Location = New System.Drawing.Point(428, 18)
        Me.chkFile.Margin = New System.Windows.Forms.Padding(4, 5, 4, 5)
        Me.chkFile.Name = "chkFile"
        Me.chkFile.Size = New System.Drawing.Size(61, 27)
        Me.chkFile.TabIndex = 4
        Me.chkFile.Text = "file"
        Me.chkFile.UseVisualStyleBackColor = True
        '
        'lblOperation
        '
        Me.lblOperation.AutoSize = True
        Me.lblOperation.Location = New System.Drawing.Point(18, 69)
        Me.lblOperation.Margin = New System.Windows.Forms.Padding(4, 0, 4, 0)
        Me.lblOperation.Name = "lblOperation"
        Me.lblOperation.Size = New System.Drawing.Size(83, 20)
        Me.lblOperation.TabIndex = 5
        Me.lblOperation.Text = "Operation:"
        '
        'chkNoOp
        '
        Me.chkNoOp.AutoSize = True
        Me.chkNoOp.Checked = True
        Me.chkNoOp.CheckState = System.Windows.Forms.CheckState.Checked
        Me.chkNoOp.Location = New System.Drawing.Point(120, 65)
        Me.chkNoOp.Margin = New System.Windows.Forms.Padding(4, 5, 4, 5)
        Me.chkNoOp.Name = "chkNoOp"
        Me.chkNoOp.Size = New System.Drawing.Size(77, 27)
        Me.chkNoOp.TabIndex = 6
        Me.chkNoOp.Text = "none"
        Me.chkNoOp.UseVisualStyleBackColor = True
        '
        'chkInfo
        '
        Me.chkInfo.AutoSize = True
        Me.chkInfo.Location = New System.Drawing.Point(210, 65)
        Me.chkInfo.Margin = New System.Windows.Forms.Padding(4, 5, 4, 5)
        Me.chkInfo.Name = "chkInfo"
        Me.chkInfo.Size = New System.Drawing.Size(67, 27)
        Me.chkInfo.TabIndex = 7
        Me.chkInfo.Text = "info"
        Me.chkInfo.UseVisualStyleBackColor = True
        '
        'chkSpeed
        '
        Me.chkSpeed.AutoSize = True
        Me.chkSpeed.Location = New System.Drawing.Point(285, 65)
        Me.chkSpeed.Margin = New System.Windows.Forms.Padding(4, 5, 4, 5)
        Me.chkSpeed.Name = "chkSpeed"
        Me.chkSpeed.Size = New System.Drawing.Size(85, 27)
        Me.chkSpeed.TabIndex = 8
        Me.chkSpeed.Text = "speed"
        Me.chkSpeed.UseVisualStyleBackColor = True
        '
        'chkFill
        '
        Me.chkFill.AutoSize = True
        Me.chkFill.Location = New System.Drawing.Point(375, 65)
        Me.chkFill.Margin = New System.Windows.Forms.Padding(4, 5, 4, 5)
        Me.chkFill.Name = "chkFill"
        Me.chkFill.Size = New System.Drawing.Size(55, 27)
        Me.chkFill.TabIndex = 9
        Me.chkFill.Text = "fill"
        Me.chkFill.UseVisualStyleBackColor = True
        '
        'chkTest
        '
        Me.chkTest.AutoSize = True
        Me.chkTest.Location = New System.Drawing.Point(120, 100)
        Me.chkTest.Margin = New System.Windows.Forms.Padding(4, 5, 4, 5)
        Me.chkTest.Name = "chkTest"
        Me.chkTest.Size = New System.Drawing.Size(68, 27)
        Me.chkTest.TabIndex = 10
        Me.chkTest.Text = "test"
        Me.chkTest.UseVisualStyleBackColor = True
        '
        'chkClean
        '
        Me.chkClean.AutoSize = True
        Me.chkClean.Location = New System.Drawing.Point(210, 100)
        Me.chkClean.Margin = New System.Windows.Forms.Padding(4, 5, 4, 5)
        Me.chkClean.Name = "chkClean"
        Me.chkClean.Size = New System.Drawing.Size(79, 27)
        Me.chkClean.TabIndex = 11
        Me.chkClean.Text = "clean"
        Me.chkClean.UseVisualStyleBackColor = True
        '
        'lblPath
        '
        Me.lblPath.AutoSize = True
        Me.lblPath.Location = New System.Drawing.Point(18, 146)
        Me.lblPath.Margin = New System.Windows.Forms.Padding(4, 0, 4, 0)
        Me.lblPath.Name = "lblPath"
        Me.lblPath.Size = New System.Drawing.Size(46, 20)
        Me.lblPath.TabIndex = 12
        Me.lblPath.Text = "Path:"
        '
        'txtPath
        '
        Me.txtPath.Location = New System.Drawing.Point(120, 142)
        Me.txtPath.Margin = New System.Windows.Forms.Padding(4, 5, 4, 5)
        Me.txtPath.Name = "txtPath"
        Me.txtPath.Size = New System.Drawing.Size(298, 26)
        Me.txtPath.TabIndex = 13
        '
        'btnBrowse
        '
        Me.btnBrowse.Location = New System.Drawing.Point(435, 138)
        Me.btnBrowse.Margin = New System.Windows.Forms.Padding(4, 5, 4, 5)
        Me.btnBrowse.Name = "btnBrowse"
        Me.btnBrowse.Size = New System.Drawing.Size(105, 38)
        Me.btnBrowse.TabIndex = 14
        Me.btnBrowse.Text = "Browse"
        Me.btnBrowse.UseVisualStyleBackColor = True
        '
        'lblSize
        '
        Me.lblSize.AutoSize = True
        Me.lblSize.Location = New System.Drawing.Point(18, 192)
        Me.lblSize.Margin = New System.Windows.Forms.Padding(4, 0, 4, 0)
        Me.lblSize.Name = "lblSize"
        Me.lblSize.Size = New System.Drawing.Size(44, 20)
        Me.lblSize.TabIndex = 15
        Me.lblSize.Text = "Size:"
        '
        'txtSize
        '
        Me.txtSize.Location = New System.Drawing.Point(120, 188)
        Me.txtSize.Margin = New System.Windows.Forms.Padding(4, 5, 4, 5)
        Me.txtSize.Name = "txtSize"
        Me.txtSize.Size = New System.Drawing.Size(88, 26)
        Me.txtSize.TabIndex = 16
        Me.txtSize.Text = "100"
        '
        'lblFlags
        '
        Me.lblFlags.AutoSize = True
        Me.lblFlags.Location = New System.Drawing.Point(18, 238)
        Me.lblFlags.Margin = New System.Windows.Forms.Padding(4, 0, 4, 0)
        Me.lblFlags.Name = "lblFlags"
        Me.lblFlags.Size = New System.Drawing.Size(52, 20)
        Me.lblFlags.TabIndex = 17
        Me.lblFlags.Text = "Flags:"
        '
        'chkMax
        '
        Me.chkMax.AutoSize = True
        Me.chkMax.Location = New System.Drawing.Point(120, 234)
        Me.chkMax.Margin = New System.Windows.Forms.Padding(4, 5, 4, 5)
        Me.chkMax.Name = "chkMax"
        Me.chkMax.Size = New System.Drawing.Size(70, 27)
        Me.chkMax.TabIndex = 18
        Me.chkMax.Text = "max"
        Me.chkMax.UseVisualStyleBackColor = True
        '
        'chkHelp
        '
        Me.chkHelp.AutoSize = True
        Me.chkHelp.Location = New System.Drawing.Point(202, 234)
        Me.chkHelp.Margin = New System.Windows.Forms.Padding(4, 5, 4, 5)
        Me.chkHelp.Name = "chkHelp"
        Me.chkHelp.Size = New System.Drawing.Size(71, 27)
        Me.chkHelp.TabIndex = 19
        Me.chkHelp.Text = "help"
        Me.chkHelp.UseVisualStyleBackColor = True
        '
        'chkHist
        '
        Me.chkHist.AutoSize = True
        Me.chkHist.Location = New System.Drawing.Point(285, 234)
        Me.chkHist.Margin = New System.Windows.Forms.Padding(4, 5, 4, 5)
        Me.chkHist.Name = "chkHist"
        Me.chkHist.Size = New System.Drawing.Size(66, 27)
        Me.chkHist.TabIndex = 20
        Me.chkHist.Text = "hist"
        Me.chkHist.UseVisualStyleBackColor = True
        '
        'chkShort
        '
        Me.chkShort.AutoSize = True
        Me.chkShort.Location = New System.Drawing.Point(360, 234)
        Me.chkShort.Margin = New System.Windows.Forms.Padding(4, 5, 4, 5)
        Me.chkShort.Name = "chkShort"
        Me.chkShort.Size = New System.Drawing.Size(77, 27)
        Me.chkShort.TabIndex = 21
        Me.chkShort.Text = "short"
        Me.chkShort.UseVisualStyleBackColor = True
        '
        'chkDelete
        '
        Me.chkDelete.AutoSize = True
        Me.chkDelete.Location = New System.Drawing.Point(450, 234)
        Me.chkDelete.Margin = New System.Windows.Forms.Padding(4, 5, 4, 5)
        Me.chkDelete.Name = "chkDelete"
        Me.chkDelete.Size = New System.Drawing.Size(85, 27)
        Me.chkDelete.TabIndex = 22
        Me.chkDelete.Text = "delete"
        Me.chkDelete.UseVisualStyleBackColor = True
        '
        'btnRun
        '
        Me.btnRun.Font = New System.Drawing.Font("Microsoft Sans Serif", 10.0!, System.Drawing.FontStyle.Bold)
        Me.btnRun.Location = New System.Drawing.Point(390, 307)
        Me.btnRun.Margin = New System.Windows.Forms.Padding(4, 5, 4, 5)
        Me.btnRun.Name = "btnRun"
        Me.btnRun.Size = New System.Drawing.Size(150, 54)
        Me.btnRun.TabIndex = 23
        Me.btnRun.Text = "RUN"
        Me.btnRun.UseVisualStyleBackColor = True
        '
        'lblCommandTitle
        '
        Me.lblCommandTitle.AutoSize = True
        Me.lblCommandTitle.Location = New System.Drawing.Point(18, 277)
        Me.lblCommandTitle.Margin = New System.Windows.Forms.Padding(4, 0, 4, 0)
        Me.lblCommandTitle.Name = "lblCommandTitle"
        Me.lblCommandTitle.Size = New System.Drawing.Size(86, 20)
        Me.lblCommandTitle.TabIndex = 24
        Me.lblCommandTitle.Text = "Command:"
        '
        'txtCommand
        '
        Me.txtCommand.Location = New System.Drawing.Point(120, 271)
        Me.txtCommand.Margin = New System.Windows.Forms.Padding(4, 5, 4, 5)
        Me.txtCommand.Name = "txtCommand"
        Me.txtCommand.Size = New System.Drawing.Size(420, 26)
        Me.txtCommand.TabIndex = 25
        Me.txtCommand.Text = "filedo.exe device"
        '
        'MainForm
        '
        Me.AutoScaleDimensions = New System.Drawing.SizeF(9.0!, 20.0!)
        Me.AutoScaleMode = System.Windows.Forms.AutoScaleMode.Font
        Me.ClientSize = New System.Drawing.Size(551, 372)
        Me.Controls.Add(Me.txtCommand)
        Me.Controls.Add(Me.lblCommandTitle)
        Me.Controls.Add(Me.btnRun)
        Me.Controls.Add(Me.chkDelete)
        Me.Controls.Add(Me.chkShort)
        Me.Controls.Add(Me.chkHist)
        Me.Controls.Add(Me.chkHelp)
        Me.Controls.Add(Me.chkMax)
        Me.Controls.Add(Me.lblFlags)
        Me.Controls.Add(Me.txtSize)
        Me.Controls.Add(Me.lblSize)
        Me.Controls.Add(Me.btnBrowse)
        Me.Controls.Add(Me.txtPath)
        Me.Controls.Add(Me.lblPath)
        Me.Controls.Add(Me.chkClean)
        Me.Controls.Add(Me.chkTest)
        Me.Controls.Add(Me.chkFill)
        Me.Controls.Add(Me.chkSpeed)
        Me.Controls.Add(Me.chkInfo)
        Me.Controls.Add(Me.chkNoOp)
        Me.Controls.Add(Me.lblOperation)
        Me.Controls.Add(Me.chkFile)
        Me.Controls.Add(Me.chkNetwork)
        Me.Controls.Add(Me.chkFolder)
        Me.Controls.Add(Me.chkDevice)
        Me.Controls.Add(Me.lblTarget)
        Me.FormBorderStyle = System.Windows.Forms.FormBorderStyle.FixedSingle
        Me.Margin = New System.Windows.Forms.Padding(4, 5, 4, 5)
        Me.MaximizeBox = False
        Me.Name = "MainForm"
        Me.StartPosition = System.Windows.Forms.FormStartPosition.CenterScreen
        Me.Text = "FileDO GUI"
        Me.ResumeLayout(False)
        Me.PerformLayout()

    End Sub

    Friend WithEvents lblTarget As Label
    Friend WithEvents chkDevice As CheckBox
    Friend WithEvents chkFolder As CheckBox
    Friend WithEvents chkNetwork As CheckBox
    Friend WithEvents chkFile As CheckBox
    Friend WithEvents lblOperation As Label
    Friend WithEvents chkNoOp As CheckBox
    Friend WithEvents chkInfo As CheckBox
    Friend WithEvents chkSpeed As CheckBox
    Friend WithEvents chkFill As CheckBox
    Friend WithEvents chkTest As CheckBox
    Friend WithEvents chkClean As CheckBox
    Friend WithEvents lblPath As Label
    Friend WithEvents txtPath As TextBox
    Friend WithEvents btnBrowse As Button
    Friend WithEvents lblSize As Label
    Friend WithEvents txtSize As TextBox
    Friend WithEvents lblFlags As Label
    Friend WithEvents chkMax As CheckBox
    Friend WithEvents chkHelp As CheckBox
    Friend WithEvents chkHist As CheckBox
    Friend WithEvents chkShort As CheckBox
    Friend WithEvents chkDelete As CheckBox
    Friend WithEvents btnRun As Button
    Friend WithEvents lblCommandTitle As Label
    Friend WithEvents txtCommand As TextBox
End Class
