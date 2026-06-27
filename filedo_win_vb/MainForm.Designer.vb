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
        Me.components = New System.ComponentModel.Container()
        Me.tip = New System.Windows.Forms.ToolTip(Me.components)
        Me.lblLang = New System.Windows.Forms.Label()
        Me.cmbLang = New System.Windows.Forms.ComboBox()
        Me.lblTarget = New System.Windows.Forms.Label()
        Me.chkDevice = New System.Windows.Forms.CheckBox()
        Me.chkFolder = New System.Windows.Forms.CheckBox()
        Me.chkNetwork = New System.Windows.Forms.CheckBox()
        Me.chkFile = New System.Windows.Forms.CheckBox()
        Me.lblOperation = New System.Windows.Forms.Label()
        Me.cmbOperation = New System.Windows.Forms.ComboBox()
        Me.lblPath = New System.Windows.Forms.Label()
        Me.txtPath = New System.Windows.Forms.TextBox()
        Me.cmbDrive = New System.Windows.Forms.ComboBox()
        Me.btnBrowse = New System.Windows.Forms.Button()
        Me.lblPath2 = New System.Windows.Forms.Label()
        Me.txtPath2 = New System.Windows.Forms.TextBox()
        Me.btnBrowse2 = New System.Windows.Forms.Button()
        Me.lblSize = New System.Windows.Forms.Label()
        Me.txtSize = New System.Windows.Forms.TextBox()
        Me.lblFlags = New System.Windows.Forms.Label()
        Me.chkMax = New System.Windows.Forms.CheckBox()
        Me.chkDelete = New System.Windows.Forms.CheckBox()
        Me.chkNoDel = New System.Windows.Forms.CheckBox()
        Me.chkShort = New System.Windows.Forms.CheckBox()
        Me.chkHist = New System.Windows.Forms.CheckBox()
        Me.chkForce = New System.Windows.Forms.CheckBox()
        Me.txtHelp = New System.Windows.Forms.TextBox()
        Me.grpDuplicateOptions = New System.Windows.Forms.GroupBox()
        Me.rbOld = New System.Windows.Forms.RadioButton()
        Me.rbNew = New System.Windows.Forms.RadioButton()
        Me.rbAbc = New System.Windows.Forms.RadioButton()
        Me.rbXyz = New System.Windows.Forms.RadioButton()
        Me.chkMove = New System.Windows.Forms.CheckBox()
        Me.txtMoveTarget = New System.Windows.Forms.TextBox()
        Me.btnBrowseMove = New System.Windows.Forms.Button()
        Me.lblCompareDel = New System.Windows.Forms.Label()
        Me.cmbCompareDel = New System.Windows.Forms.ComboBox()
        Me.lblCommandTitle = New System.Windows.Forms.Label()
        Me.txtCommand = New System.Windows.Forms.TextBox()
        Me.btnCopy = New System.Windows.Forms.Button()
        Me.btnRun = New System.Windows.Forms.Button()
        Me.grpDuplicateOptions.SuspendLayout()
        Me.SuspendLayout()
        '
        'lblLang
        '
        Me.lblLang.AutoSize = True
        Me.lblLang.Location = New System.Drawing.Point(18, 18)
        Me.lblLang.Name = "lblLang"
        Me.lblLang.Size = New System.Drawing.Size(80, 20)
        Me.lblLang.TabIndex = 0
        Me.lblLang.Text = "Language:"
        '
        'cmbLang
        '
        Me.cmbLang.DropDownStyle = System.Windows.Forms.ComboBoxStyle.DropDownList
        Me.cmbLang.FormattingEnabled = True
        Me.cmbLang.Location = New System.Drawing.Point(120, 14)
        Me.cmbLang.Name = "cmbLang"
        Me.cmbLang.Size = New System.Drawing.Size(200, 28)
        Me.cmbLang.TabIndex = 1
        '
        'lblTarget
        '
        Me.lblTarget.AutoSize = True
        Me.lblTarget.Location = New System.Drawing.Point(18, 57)
        Me.lblTarget.Name = "lblTarget"
        Me.lblTarget.Size = New System.Drawing.Size(59, 20)
        Me.lblTarget.TabIndex = 2
        Me.lblTarget.Text = "Target:"
        '
        'chkDevice
        '
        Me.chkDevice.AutoSize = True
        Me.chkDevice.Checked = True
        Me.chkDevice.CheckState = System.Windows.Forms.CheckState.Checked
        Me.chkDevice.Location = New System.Drawing.Point(120, 55)
        Me.chkDevice.Name = "chkDevice"
        Me.chkDevice.Size = New System.Drawing.Size(86, 24)
        Me.chkDevice.TabIndex = 3
        Me.chkDevice.Text = "device"
        Me.chkDevice.UseVisualStyleBackColor = True
        '
        'chkFolder
        '
        Me.chkFolder.AutoSize = True
        Me.chkFolder.Location = New System.Drawing.Point(225, 55)
        Me.chkFolder.Name = "chkFolder"
        Me.chkFolder.Size = New System.Drawing.Size(81, 24)
        Me.chkFolder.TabIndex = 4
        Me.chkFolder.Text = "folder"
        Me.chkFolder.UseVisualStyleBackColor = True
        '
        'chkNetwork
        '
        Me.chkNetwork.AutoSize = True
        Me.chkNetwork.Location = New System.Drawing.Point(320, 55)
        Me.chkNetwork.Name = "chkNetwork"
        Me.chkNetwork.Size = New System.Drawing.Size(97, 24)
        Me.chkNetwork.TabIndex = 5
        Me.chkNetwork.Text = "network"
        Me.chkNetwork.UseVisualStyleBackColor = True
        '
        'chkFile
        '
        Me.chkFile.AutoSize = True
        Me.chkFile.Location = New System.Drawing.Point(435, 55)
        Me.chkFile.Name = "chkFile"
        Me.chkFile.Size = New System.Drawing.Size(61, 24)
        Me.chkFile.TabIndex = 6
        Me.chkFile.Text = "file"
        Me.chkFile.UseVisualStyleBackColor = True
        '
        'lblOperation
        '
        Me.lblOperation.AutoSize = True
        Me.lblOperation.Location = New System.Drawing.Point(18, 96)
        Me.lblOperation.Name = "lblOperation"
        Me.lblOperation.Size = New System.Drawing.Size(83, 20)
        Me.lblOperation.TabIndex = 7
        Me.lblOperation.Text = "Operation:"
        '
        'cmbOperation
        '
        Me.cmbOperation.DropDownStyle = System.Windows.Forms.ComboBoxStyle.DropDownList
        Me.cmbOperation.FormattingEnabled = True
        Me.cmbOperation.Location = New System.Drawing.Point(120, 92)
        Me.cmbOperation.Name = "cmbOperation"
        Me.cmbOperation.Size = New System.Drawing.Size(376, 28)
        Me.cmbOperation.TabIndex = 8
        '
        'lblPath
        '
        Me.lblPath.AutoSize = True
        Me.lblPath.Location = New System.Drawing.Point(18, 137)
        Me.lblPath.Name = "lblPath"
        Me.lblPath.Size = New System.Drawing.Size(46, 20)
        Me.lblPath.TabIndex = 9
        Me.lblPath.Text = "Path:"
        '
        'txtPath
        '
        Me.txtPath.Location = New System.Drawing.Point(120, 134)
        Me.txtPath.Name = "txtPath"
        Me.txtPath.Size = New System.Drawing.Size(376, 26)
        Me.txtPath.TabIndex = 10
        '
        'cmbDrive
        '
        Me.cmbDrive.FormattingEnabled = True
        Me.cmbDrive.Location = New System.Drawing.Point(120, 133)
        Me.cmbDrive.Name = "cmbDrive"
        Me.cmbDrive.Size = New System.Drawing.Size(200, 28)
        Me.cmbDrive.TabIndex = 11
        '
        'btnBrowse
        '
        Me.btnBrowse.Location = New System.Drawing.Point(504, 132)
        Me.btnBrowse.Name = "btnBrowse"
        Me.btnBrowse.Size = New System.Drawing.Size(86, 30)
        Me.btnBrowse.TabIndex = 12
        Me.btnBrowse.Text = "Browse"
        Me.btnBrowse.UseVisualStyleBackColor = True
        '
        'lblPath2
        '
        Me.lblPath2.AutoSize = True
        Me.lblPath2.Location = New System.Drawing.Point(18, 177)
        Me.lblPath2.Name = "lblPath2"
        Me.lblPath2.Size = New System.Drawing.Size(95, 20)
        Me.lblPath2.TabIndex = 13
        Me.lblPath2.Text = "Destination:"
        '
        'txtPath2
        '
        Me.txtPath2.Location = New System.Drawing.Point(120, 174)
        Me.txtPath2.Name = "txtPath2"
        Me.txtPath2.Size = New System.Drawing.Size(376, 26)
        Me.txtPath2.TabIndex = 14
        '
        'btnBrowse2
        '
        Me.btnBrowse2.Location = New System.Drawing.Point(504, 172)
        Me.btnBrowse2.Name = "btnBrowse2"
        Me.btnBrowse2.Size = New System.Drawing.Size(86, 30)
        Me.btnBrowse2.TabIndex = 15
        Me.btnBrowse2.Text = "Browse"
        Me.btnBrowse2.UseVisualStyleBackColor = True
        '
        'lblSize
        '
        Me.lblSize.AutoSize = True
        Me.lblSize.Location = New System.Drawing.Point(18, 219)
        Me.lblSize.Name = "lblSize"
        Me.lblSize.Size = New System.Drawing.Size(78, 20)
        Me.lblSize.TabIndex = 16
        Me.lblSize.Text = "Size (MB):"
        '
        'txtSize
        '
        Me.txtSize.Location = New System.Drawing.Point(120, 216)
        Me.txtSize.Name = "txtSize"
        Me.txtSize.Size = New System.Drawing.Size(88, 26)
        Me.txtSize.TabIndex = 17
        Me.txtSize.Text = "100"
        '
        'lblFlags
        '
        Me.lblFlags.AutoSize = True
        Me.lblFlags.Location = New System.Drawing.Point(230, 219)
        Me.lblFlags.Name = "lblFlags"
        Me.lblFlags.Size = New System.Drawing.Size(52, 20)
        Me.lblFlags.TabIndex = 18
        Me.lblFlags.Text = "Flags:"
        '
        'chkMax
        '
        Me.chkMax.AutoSize = True
        Me.chkMax.Location = New System.Drawing.Point(300, 217)
        Me.chkMax.Name = "chkMax"
        Me.chkMax.Size = New System.Drawing.Size(70, 24)
        Me.chkMax.TabIndex = 19
        Me.chkMax.Text = "max"
        Me.chkMax.UseVisualStyleBackColor = True
        '
        'chkDelete
        '
        Me.chkDelete.AutoSize = True
        Me.chkDelete.Location = New System.Drawing.Point(380, 217)
        Me.chkDelete.Name = "chkDelete"
        Me.chkDelete.Size = New System.Drawing.Size(57, 24)
        Me.chkDelete.TabIndex = 20
        Me.chkDelete.Text = "del"
        Me.chkDelete.UseVisualStyleBackColor = True
        '
        'chkNoDel
        '
        Me.chkNoDel.AutoSize = True
        Me.chkNoDel.Location = New System.Drawing.Point(445, 217)
        Me.chkNoDel.Name = "chkNoDel"
        Me.chkNoDel.Size = New System.Drawing.Size(78, 24)
        Me.chkNoDel.TabIndex = 21
        Me.chkNoDel.Text = "nodel"
        Me.chkNoDel.UseVisualStyleBackColor = True
        '
        'chkShort
        '
        Me.chkShort.AutoSize = True
        Me.chkShort.Location = New System.Drawing.Point(300, 247)
        Me.chkShort.Name = "chkShort"
        Me.chkShort.Size = New System.Drawing.Size(77, 24)
        Me.chkShort.TabIndex = 22
        Me.chkShort.Text = "short"
        Me.chkShort.UseVisualStyleBackColor = True
        '
        'chkHist
        '
        Me.chkHist.AutoSize = True
        Me.chkHist.Location = New System.Drawing.Point(380, 247)
        Me.chkHist.Name = "chkHist"
        Me.chkHist.Size = New System.Drawing.Size(66, 24)
        Me.chkHist.TabIndex = 23
        Me.chkHist.Text = "hist"
        Me.chkHist.UseVisualStyleBackColor = True
        '
        'chkForce
        '
        Me.chkForce.AutoSize = True
        Me.chkForce.Location = New System.Drawing.Point(445, 247)
        Me.chkForce.Name = "chkForce"
        Me.chkForce.Size = New System.Drawing.Size(91, 24)
        Me.chkForce.TabIndex = 24
        Me.chkForce.Text = "--force"
        Me.chkForce.UseVisualStyleBackColor = True
        '
        'txtHelp
        '
        Me.txtHelp.BackColor = System.Drawing.SystemColors.Info
        Me.txtHelp.BorderStyle = System.Windows.Forms.BorderStyle.FixedSingle
        Me.txtHelp.Location = New System.Drawing.Point(18, 283)
        Me.txtHelp.Multiline = True
        Me.txtHelp.Name = "txtHelp"
        Me.txtHelp.ReadOnly = True
        Me.txtHelp.ScrollBars = System.Windows.Forms.ScrollBars.Vertical
        Me.txtHelp.Size = New System.Drawing.Size(572, 150)
        Me.txtHelp.TabIndex = 25
        Me.txtHelp.TabStop = False
        '
        'grpDuplicateOptions
        '
        Me.grpDuplicateOptions.Controls.Add(Me.rbOld)
        Me.grpDuplicateOptions.Controls.Add(Me.rbNew)
        Me.grpDuplicateOptions.Controls.Add(Me.rbAbc)
        Me.grpDuplicateOptions.Controls.Add(Me.rbXyz)
        Me.grpDuplicateOptions.Controls.Add(Me.chkMove)
        Me.grpDuplicateOptions.Controls.Add(Me.txtMoveTarget)
        Me.grpDuplicateOptions.Controls.Add(Me.btnBrowseMove)
        Me.grpDuplicateOptions.Location = New System.Drawing.Point(18, 360)
        Me.grpDuplicateOptions.Name = "grpDuplicateOptions"
        Me.grpDuplicateOptions.Size = New System.Drawing.Size(572, 118)
        Me.grpDuplicateOptions.TabIndex = 26
        Me.grpDuplicateOptions.TabStop = False
        Me.grpDuplicateOptions.Text = "Duplicate options"
        Me.grpDuplicateOptions.Visible = False
        '
        'rbOld
        '
        Me.rbOld.AutoSize = True
        Me.rbOld.Location = New System.Drawing.Point(15, 24)
        Me.rbOld.Name = "rbOld"
        Me.rbOld.Size = New System.Drawing.Size(167, 24)
        Me.rbOld.TabIndex = 0
        Me.rbOld.Text = "Keep newest (old)"
        Me.rbOld.UseVisualStyleBackColor = True
        '
        'rbNew
        '
        Me.rbNew.AutoSize = True
        Me.rbNew.Checked = True
        Me.rbNew.Location = New System.Drawing.Point(220, 24)
        Me.rbNew.Name = "rbNew"
        Me.rbNew.Size = New System.Drawing.Size(167, 24)
        Me.rbNew.TabIndex = 1
        Me.rbNew.TabStop = True
        Me.rbNew.Text = "Keep oldest (new)"
        Me.rbNew.UseVisualStyleBackColor = True
        '
        'rbAbc
        '
        Me.rbAbc.AutoSize = True
        Me.rbAbc.Location = New System.Drawing.Point(15, 52)
        Me.rbAbc.Name = "rbAbc"
        Me.rbAbc.Size = New System.Drawing.Size(189, 24)
        Me.rbAbc.TabIndex = 2
        Me.rbAbc.Text = "Keep last alpha (abc)"
        Me.rbAbc.UseVisualStyleBackColor = True
        '
        'rbXyz
        '
        Me.rbXyz.AutoSize = True
        Me.rbXyz.Location = New System.Drawing.Point(220, 52)
        Me.rbXyz.Name = "rbXyz"
        Me.rbXyz.Size = New System.Drawing.Size(186, 24)
        Me.rbXyz.TabIndex = 3
        Me.rbXyz.Text = "Keep first alpha (xyz)"
        Me.rbXyz.UseVisualStyleBackColor = True
        '
        'chkMove
        '
        Me.chkMove.AutoSize = True
        Me.chkMove.Location = New System.Drawing.Point(15, 84)
        Me.chkMove.Name = "chkMove"
        Me.chkMove.Size = New System.Drawing.Size(177, 24)
        Me.chkMove.TabIndex = 4
        Me.chkMove.Text = "Move duplicates to:"
        Me.chkMove.UseVisualStyleBackColor = True
        '
        'txtMoveTarget
        '
        Me.txtMoveTarget.Location = New System.Drawing.Point(200, 82)
        Me.txtMoveTarget.Name = "txtMoveTarget"
        Me.txtMoveTarget.Size = New System.Drawing.Size(280, 26)
        Me.txtMoveTarget.TabIndex = 5
        '
        'btnBrowseMove
        '
        Me.btnBrowseMove.Location = New System.Drawing.Point(486, 80)
        Me.btnBrowseMove.Name = "btnBrowseMove"
        Me.btnBrowseMove.Size = New System.Drawing.Size(70, 30)
        Me.btnBrowseMove.TabIndex = 6
        Me.btnBrowseMove.Text = "..."
        Me.btnBrowseMove.UseVisualStyleBackColor = True
        '
        'lblCompareDel
        '
        Me.lblCompareDel.AutoSize = True
        Me.lblCompareDel.Location = New System.Drawing.Point(18, 366)
        Me.lblCompareDel.Name = "lblCompareDel"
        Me.lblCompareDel.Size = New System.Drawing.Size(95, 20)
        Me.lblCompareDel.TabIndex = 27
        Me.lblCompareDel.Text = "Delete rule:"
        Me.lblCompareDel.Visible = False
        '
        'cmbCompareDel
        '
        Me.cmbCompareDel.DropDownStyle = System.Windows.Forms.ComboBoxStyle.DropDownList
        Me.cmbCompareDel.FormattingEnabled = True
        Me.cmbCompareDel.Location = New System.Drawing.Point(120, 362)
        Me.cmbCompareDel.Name = "cmbCompareDel"
        Me.cmbCompareDel.Size = New System.Drawing.Size(376, 28)
        Me.cmbCompareDel.TabIndex = 28
        Me.cmbCompareDel.Visible = False
        '
        'lblCommandTitle
        '
        Me.lblCommandTitle.AutoSize = True
        Me.lblCommandTitle.Location = New System.Drawing.Point(18, 498)
        Me.lblCommandTitle.Name = "lblCommandTitle"
        Me.lblCommandTitle.Size = New System.Drawing.Size(86, 20)
        Me.lblCommandTitle.TabIndex = 29
        Me.lblCommandTitle.Text = "Command:"
        '
        'txtCommand
        '
        Me.txtCommand.Font = New System.Drawing.Font("Consolas", 10.0!)
        Me.txtCommand.Location = New System.Drawing.Point(120, 495)
        Me.txtCommand.Name = "txtCommand"
        Me.txtCommand.Size = New System.Drawing.Size(470, 27)
        Me.txtCommand.TabIndex = 30
        Me.txtCommand.Text = "filedo.exe device C: info"
        '
        'btnCopy
        '
        Me.btnCopy.Location = New System.Drawing.Point(18, 535)
        Me.btnCopy.Name = "btnCopy"
        Me.btnCopy.Size = New System.Drawing.Size(170, 45)
        Me.btnCopy.TabIndex = 31
        Me.btnCopy.Text = "Copy command"
        Me.btnCopy.UseVisualStyleBackColor = True
        '
        'btnRun
        '
        Me.btnRun.Font = New System.Drawing.Font("Microsoft Sans Serif", 10.0!, System.Drawing.FontStyle.Bold)
        Me.btnRun.Location = New System.Drawing.Point(440, 535)
        Me.btnRun.Name = "btnRun"
        Me.btnRun.Size = New System.Drawing.Size(150, 45)
        Me.btnRun.TabIndex = 32
        Me.btnRun.Text = "RUN"
        Me.btnRun.UseVisualStyleBackColor = True
        '
        'MainForm
        '
        Me.AutoScaleDimensions = New System.Drawing.SizeF(9.0!, 20.0!)
        Me.AutoScaleMode = System.Windows.Forms.AutoScaleMode.Font
        Me.ClientSize = New System.Drawing.Size(610, 592)
        Me.Controls.Add(Me.btnRun)
        Me.Controls.Add(Me.btnCopy)
        Me.Controls.Add(Me.txtCommand)
        Me.Controls.Add(Me.lblCommandTitle)
        Me.Controls.Add(Me.cmbCompareDel)
        Me.Controls.Add(Me.lblCompareDel)
        Me.Controls.Add(Me.grpDuplicateOptions)
        Me.Controls.Add(Me.txtHelp)
        Me.Controls.Add(Me.chkForce)
        Me.Controls.Add(Me.chkHist)
        Me.Controls.Add(Me.chkShort)
        Me.Controls.Add(Me.chkNoDel)
        Me.Controls.Add(Me.chkDelete)
        Me.Controls.Add(Me.chkMax)
        Me.Controls.Add(Me.lblFlags)
        Me.Controls.Add(Me.txtSize)
        Me.Controls.Add(Me.lblSize)
        Me.Controls.Add(Me.btnBrowse2)
        Me.Controls.Add(Me.txtPath2)
        Me.Controls.Add(Me.lblPath2)
        Me.Controls.Add(Me.btnBrowse)
        Me.Controls.Add(Me.cmbDrive)
        Me.Controls.Add(Me.txtPath)
        Me.Controls.Add(Me.lblPath)
        Me.Controls.Add(Me.cmbOperation)
        Me.Controls.Add(Me.lblOperation)
        Me.Controls.Add(Me.chkFile)
        Me.Controls.Add(Me.chkNetwork)
        Me.Controls.Add(Me.chkFolder)
        Me.Controls.Add(Me.chkDevice)
        Me.Controls.Add(Me.lblTarget)
        Me.Controls.Add(Me.cmbLang)
        Me.Controls.Add(Me.lblLang)
        Me.FormBorderStyle = System.Windows.Forms.FormBorderStyle.FixedSingle
        Me.MaximizeBox = False
        Me.Name = "MainForm"
        Me.StartPosition = System.Windows.Forms.FormStartPosition.CenterScreen
        Me.Text = "FileDO GUI"
        Me.grpDuplicateOptions.ResumeLayout(False)
        Me.grpDuplicateOptions.PerformLayout()
        Me.ResumeLayout(False)
        Me.PerformLayout()

    End Sub

    Friend WithEvents tip As ToolTip
    Friend WithEvents lblLang As Label
    Friend WithEvents cmbLang As ComboBox
    Friend WithEvents lblTarget As Label
    Friend WithEvents chkDevice As CheckBox
    Friend WithEvents chkFolder As CheckBox
    Friend WithEvents chkNetwork As CheckBox
    Friend WithEvents chkFile As CheckBox
    Friend WithEvents lblOperation As Label
    Friend WithEvents cmbOperation As ComboBox
    Friend WithEvents lblPath As Label
    Friend WithEvents txtPath As TextBox
    Friend WithEvents cmbDrive As ComboBox
    Friend WithEvents btnBrowse As Button
    Friend WithEvents lblPath2 As Label
    Friend WithEvents txtPath2 As TextBox
    Friend WithEvents btnBrowse2 As Button
    Friend WithEvents lblSize As Label
    Friend WithEvents txtSize As TextBox
    Friend WithEvents lblFlags As Label
    Friend WithEvents chkMax As CheckBox
    Friend WithEvents chkDelete As CheckBox
    Friend WithEvents chkNoDel As CheckBox
    Friend WithEvents chkShort As CheckBox
    Friend WithEvents chkHist As CheckBox
    Friend WithEvents chkForce As CheckBox
    Friend WithEvents txtHelp As TextBox
    Friend WithEvents grpDuplicateOptions As GroupBox
    Friend WithEvents rbOld As RadioButton
    Friend WithEvents rbNew As RadioButton
    Friend WithEvents rbAbc As RadioButton
    Friend WithEvents rbXyz As RadioButton
    Friend WithEvents chkMove As CheckBox
    Friend WithEvents txtMoveTarget As TextBox
    Friend WithEvents btnBrowseMove As Button
    Friend WithEvents lblCompareDel As Label
    Friend WithEvents cmbCompareDel As ComboBox
    Friend WithEvents lblCommandTitle As Label
    Friend WithEvents txtCommand As TextBox
    Friend WithEvents btnCopy As Button
    Friend WithEvents btnRun As Button
End Class
