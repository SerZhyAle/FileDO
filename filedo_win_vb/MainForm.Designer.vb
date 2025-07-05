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
        Me.btnRun = New System.Windows.Forms.Button()
        Me.lblCommandTitle = New System.Windows.Forms.Label()
        Me.lblCommand = New System.Windows.Forms.Label()
        Me.SuspendLayout()
        '
        'lblTarget
        '
        Me.lblTarget.AutoSize = True
        Me.lblTarget.Location = New System.Drawing.Point(12, 15)
        Me.lblTarget.Name = "lblTarget"
        Me.lblTarget.Size = New System.Drawing.Size(41, 13)
        Me.lblTarget.TabIndex = 0
        Me.lblTarget.Text = "Target:"
        '
        'chkDevice
        '
        Me.chkDevice.AutoSize = True
        Me.chkDevice.Checked = True
        Me.chkDevice.CheckState = System.Windows.Forms.CheckState.Checked
        Me.chkDevice.Location = New System.Drawing.Point(80, 12)
        Me.chkDevice.Name = "chkDevice"
        Me.chkDevice.Size = New System.Drawing.Size(60, 17)
        Me.chkDevice.TabIndex = 1
        Me.chkDevice.Text = "device"
        Me.chkDevice.UseVisualStyleBackColor = True
        '
        'chkFolder
        '
        Me.chkFolder.AutoSize = True
        Me.chkFolder.Location = New System.Drawing.Point(150, 12)
        Me.chkFolder.Name = "chkFolder"
        Me.chkFolder.Size = New System.Drawing.Size(53, 17)
        Me.chkFolder.TabIndex = 2
        Me.chkFolder.Text = "folder"
        Me.chkFolder.UseVisualStyleBackColor = True
        '
        'chkNetwork
        '
        Me.chkNetwork.AutoSize = True
        Me.chkNetwork.Location = New System.Drawing.Point(210, 12)
        Me.chkNetwork.Name = "chkNetwork"
        Me.chkNetwork.Size = New System.Drawing.Size(66, 17)
        Me.chkNetwork.TabIndex = 3
        Me.chkNetwork.Text = "network"
        Me.chkNetwork.UseVisualStyleBackColor = True
        '
        'chkFile
        '
        Me.chkFile.AutoSize = True
        Me.chkFile.Location = New System.Drawing.Point(285, 12)
        Me.chkFile.Name = "chkFile"
        Me.chkFile.Size = New System.Drawing.Size(39, 17)
        Me.chkFile.TabIndex = 4
        Me.chkFile.Text = "file"
        Me.chkFile.UseVisualStyleBackColor = True
        '
        'lblOperation
        '
        Me.lblOperation.AutoSize = True
        Me.lblOperation.Location = New System.Drawing.Point(12, 45)
        Me.lblOperation.Name = "lblOperation"
        Me.lblOperation.Size = New System.Drawing.Size(56, 13)
        Me.lblOperation.TabIndex = 5
        Me.lblOperation.Text = "Operation:"
        '
        'chkNoOp
        '
        Me.chkNoOp.AutoSize = True
        Me.chkNoOp.Checked = True
        Me.chkNoOp.CheckState = System.Windows.Forms.CheckState.Checked
        Me.chkNoOp.Location = New System.Drawing.Point(80, 42)
        Me.chkNoOp.Name = "chkNoOp"
        Me.chkNoOp.Size = New System.Drawing.Size(50, 17)
        Me.chkNoOp.TabIndex = 6
        Me.chkNoOp.Text = "none"
        Me.chkNoOp.UseVisualStyleBackColor = True
        '
        'chkInfo
        '
        Me.chkInfo.AutoSize = True
        Me.chkInfo.Location = New System.Drawing.Point(140, 42)
        Me.chkInfo.Name = "chkInfo"
        Me.chkInfo.Size = New System.Drawing.Size(44, 17)
        Me.chkInfo.TabIndex = 7
        Me.chkInfo.Text = "info"
        Me.chkInfo.UseVisualStyleBackColor = True
        '
        'chkSpeed
        '
        Me.chkSpeed.AutoSize = True
        Me.chkSpeed.Location = New System.Drawing.Point(190, 42)
        Me.chkSpeed.Name = "chkSpeed"
        Me.chkSpeed.Size = New System.Drawing.Size(54, 17)
        Me.chkSpeed.TabIndex = 8
        Me.chkSpeed.Text = "speed"
        Me.chkSpeed.UseVisualStyleBackColor = True
        '
        'chkFill
        '
        Me.chkFill.AutoSize = True
        Me.chkFill.Location = New System.Drawing.Point(250, 42)
        Me.chkFill.Name = "chkFill"
        Me.chkFill.Size = New System.Drawing.Size(36, 17)
        Me.chkFill.TabIndex = 9
        Me.chkFill.Text = "fill"
        Me.chkFill.UseVisualStyleBackColor = True
        '
        'chkTest
        '
        Me.chkTest.AutoSize = True
        Me.chkTest.Location = New System.Drawing.Point(80, 65)
        Me.chkTest.Name = "chkTest"
        Me.chkTest.Size = New System.Drawing.Size(43, 17)
        Me.chkTest.TabIndex = 10
        Me.chkTest.Text = "test"
        Me.chkTest.UseVisualStyleBackColor = True
        '
        'chkClean
        '
        Me.chkClean.AutoSize = True
        Me.chkClean.Location = New System.Drawing.Point(140, 65)
        Me.chkClean.Name = "chkClean"
        Me.chkClean.Size = New System.Drawing.Size(51, 17)
        Me.chkClean.TabIndex = 11
        Me.chkClean.Text = "clean"
        Me.chkClean.UseVisualStyleBackColor = True
        '
        'lblPath
        '
        Me.lblPath.AutoSize = True
        Me.lblPath.Location = New System.Drawing.Point(12, 95)
        Me.lblPath.Name = "lblPath"
        Me.lblPath.Size = New System.Drawing.Size(32, 13)
        Me.lblPath.TabIndex = 12
        Me.lblPath.Text = "Path:"
        '
        'txtPath
        '
        Me.txtPath.Location = New System.Drawing.Point(80, 92)
        Me.txtPath.Name = "txtPath"
        Me.txtPath.Size = New System.Drawing.Size(200, 20)
        Me.txtPath.TabIndex = 13
        '
        'btnBrowse
        '
        Me.btnBrowse.Location = New System.Drawing.Point(290, 90)
        Me.btnBrowse.Name = "btnBrowse"
        Me.btnBrowse.Size = New System.Drawing.Size(70, 25)
        Me.btnBrowse.TabIndex = 14
        Me.btnBrowse.Text = "Browse"
        Me.btnBrowse.UseVisualStyleBackColor = True
        '
        'lblSize
        '
        Me.lblSize.AutoSize = True
        Me.lblSize.Location = New System.Drawing.Point(12, 125)
        Me.lblSize.Name = "lblSize"
        Me.lblSize.Size = New System.Drawing.Size(30, 13)
        Me.lblSize.TabIndex = 15
        Me.lblSize.Text = "Size:"
        '
        'txtSize
        '
        Me.txtSize.Location = New System.Drawing.Point(80, 122)
        Me.txtSize.Name = "txtSize"
        Me.txtSize.Size = New System.Drawing.Size(60, 20)
        Me.txtSize.TabIndex = 16
        Me.txtSize.Text = "100"
        '
        'lblFlags
        '
        Me.lblFlags.AutoSize = True
        Me.lblFlags.Location = New System.Drawing.Point(12, 155)
        Me.lblFlags.Name = "lblFlags"
        Me.lblFlags.Size = New System.Drawing.Size(35, 13)
        Me.lblFlags.TabIndex = 17
        Me.lblFlags.Text = "Flags:"
        '
        'chkMax
        '
        Me.chkMax.AutoSize = True
        Me.chkMax.Location = New System.Drawing.Point(80, 152)
        Me.chkMax.Name = "chkMax"
        Me.chkMax.Size = New System.Drawing.Size(46, 17)
        Me.chkMax.TabIndex = 18
        Me.chkMax.Text = "max"
        Me.chkMax.UseVisualStyleBackColor = True
        '
        'chkHelp
        '
        Me.chkHelp.AutoSize = True
        Me.chkHelp.Location = New System.Drawing.Point(135, 152)
        Me.chkHelp.Name = "chkHelp"
        Me.chkHelp.Size = New System.Drawing.Size(47, 17)
        Me.chkHelp.TabIndex = 19
        Me.chkHelp.Text = "help"
        Me.chkHelp.UseVisualStyleBackColor = True
        '
        'chkHist
        '
        Me.chkHist.AutoSize = True
        Me.chkHist.Location = New System.Drawing.Point(190, 152)
        Me.chkHist.Name = "chkHist"
        Me.chkHist.Size = New System.Drawing.Size(42, 17)
        Me.chkHist.TabIndex = 20
        Me.chkHist.Text = "hist"
        Me.chkHist.UseVisualStyleBackColor = True
        '
        'chkShort
        '
        Me.chkShort.AutoSize = True
        Me.chkShort.Location = New System.Drawing.Point(240, 152)
        Me.chkShort.Name = "chkShort"
        Me.chkShort.Size = New System.Drawing.Size(50, 17)
        Me.chkShort.TabIndex = 21
        Me.chkShort.Text = "short"
        Me.chkShort.UseVisualStyleBackColor = True
        '
        'btnRun
        '
        Me.btnRun.Font = New System.Drawing.Font("Microsoft Sans Serif", 10.0!, System.Drawing.FontStyle.Bold)
        Me.btnRun.Location = New System.Drawing.Point(200, 180)
        Me.btnRun.Name = "btnRun"
        Me.btnRun.Size = New System.Drawing.Size(100, 35)
        Me.btnRun.TabIndex = 22
        Me.btnRun.Text = "RUN"
        Me.btnRun.UseVisualStyleBackColor = True
        '
        'lblCommandTitle
        '
        Me.lblCommandTitle.AutoSize = True
        Me.lblCommandTitle.Location = New System.Drawing.Point(12, 230)
        Me.lblCommandTitle.Name = "lblCommandTitle"
        Me.lblCommandTitle.Size = New System.Drawing.Size(57, 13)
        Me.lblCommandTitle.TabIndex = 23
        Me.lblCommandTitle.Text = "Command:"
        '
        'lblCommand
        '
        Me.lblCommand.BorderStyle = System.Windows.Forms.BorderStyle.FixedSingle
        Me.lblCommand.Location = New System.Drawing.Point(12, 250)
        Me.lblCommand.Name = "lblCommand"
        Me.lblCommand.Size = New System.Drawing.Size(450, 20)
        Me.lblCommand.TabIndex = 24
        Me.lblCommand.Text = "filedo.exe device"
        Me.lblCommand.TextAlign = System.Drawing.ContentAlignment.MiddleLeft
        '
        'MainForm
        '
        Me.AutoScaleDimensions = New System.Drawing.SizeF(6.0!, 13.0!)
        Me.AutoScaleMode = System.Windows.Forms.AutoScaleMode.Font
        Me.ClientSize = New System.Drawing.Size(484, 291)
        Me.Controls.Add(Me.lblCommand)
        Me.Controls.Add(Me.lblCommandTitle)
        Me.Controls.Add(Me.btnRun)
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
    Friend WithEvents btnRun As Button
    Friend WithEvents lblCommandTitle As Label
    Friend WithEvents lblCommand As Label
End Class
