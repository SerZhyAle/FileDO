<Global.Microsoft.VisualBasic.CompilerServices.DesignerGenerated()>
Partial Class AboutForm
    Inherits System.Windows.Forms.Form

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

    Private components As System.ComponentModel.IContainer

    <System.Diagnostics.DebuggerStepThrough()>
    Private Sub InitializeComponent()
        Me.components = New System.ComponentModel.Container()
        Me.lblProduct = New System.Windows.Forms.Label()
        Me.lblTagline = New System.Windows.Forms.Label()
        Me.lblBuild = New System.Windows.Forms.Label()
        Me.lblCli = New System.Windows.Forms.Label()
        Me.lblAuthor = New System.Windows.Forms.Label()
        Me.lnkMail = New System.Windows.Forms.LinkLabel()
        Me.lblLinks = New System.Windows.Forms.Label()
        Me.lnkSite = New System.Windows.Forms.LinkLabel()
        Me.lnkGitHub = New System.Windows.Forms.LinkLabel()
        Me.lnkIssues = New System.Windows.Forms.LinkLabel()
        Me.lnkPrivacy = New System.Windows.Forms.LinkLabel()
        Me.lnkPortfolio = New System.Windows.Forms.LinkLabel()
        Me.lblLicense = New System.Windows.Forms.Label()
        Me.lblLogsHint = New System.Windows.Forms.Label()
        Me.btnSendLogs = New System.Windows.Forms.Button()
        Me.btnClose = New System.Windows.Forms.Button()
        Me.tipAbout = New System.Windows.Forms.ToolTip(Me.components)
        Me.SuspendLayout()
        '
        'lblProduct
        '
        Me.lblProduct.AutoSize = True
        Me.lblProduct.Font = New System.Drawing.Font("Microsoft Sans Serif", 15.0!, System.Drawing.FontStyle.Bold)
        Me.lblProduct.Location = New System.Drawing.Point(18, 16)
        Me.lblProduct.Name = "lblProduct"
        Me.lblProduct.Size = New System.Drawing.Size(100, 30)
        Me.lblProduct.TabIndex = 0
        Me.lblProduct.Text = "FileDO"
        '
        'lblTagline
        '
        Me.lblTagline.Location = New System.Drawing.Point(18, 56)
        Me.lblTagline.Name = "lblTagline"
        ' Five lines, not four: the German, French and Russian taglines each need four, so a
        ' four-line box clipped them. The spare line is headroom for the next translation.
        Me.lblTagline.Size = New System.Drawing.Size(504, 102)
        Me.lblTagline.TabIndex = 1
        Me.lblTagline.Text = "Tagline"
        '
        'lblBuild
        '
        Me.lblBuild.AutoSize = True
        Me.lblBuild.Location = New System.Drawing.Point(18, 166)
        Me.lblBuild.Name = "lblBuild"
        Me.lblBuild.Size = New System.Drawing.Size(80, 20)
        Me.lblBuild.TabIndex = 2
        Me.lblBuild.Text = "GUI build:"
        '
        'lblCli
        '
        Me.lblCli.AutoEllipsis = True
        Me.lblCli.Location = New System.Drawing.Point(18, 192)
        Me.lblCli.Name = "lblCli"
        Me.lblCli.Size = New System.Drawing.Size(504, 24)
        Me.lblCli.TabIndex = 3
        Me.lblCli.Text = "CLI:"
        '
        'lblAuthor
        '
        Me.lblAuthor.AutoSize = True
        Me.lblAuthor.Location = New System.Drawing.Point(18, 226)
        Me.lblAuthor.Name = "lblAuthor"
        Me.lblAuthor.Size = New System.Drawing.Size(80, 20)
        Me.lblAuthor.TabIndex = 4
        Me.lblAuthor.Text = "Author:"
        '
        'lnkMail
        '
        Me.lnkMail.AutoSize = True
        Me.lnkMail.Location = New System.Drawing.Point(18, 252)
        Me.lnkMail.Name = "lnkMail"
        Me.lnkMail.Size = New System.Drawing.Size(150, 20)
        Me.lnkMail.TabIndex = 5
        Me.lnkMail.TabStop = True
        Me.lnkMail.Text = "serzhyale@gmail.com"
        '
        'lblLinks
        '
        Me.lblLinks.AutoSize = True
        Me.lblLinks.Location = New System.Drawing.Point(18, 288)
        Me.lblLinks.Name = "lblLinks"
        Me.lblLinks.Size = New System.Drawing.Size(50, 20)
        Me.lblLinks.TabIndex = 6
        Me.lblLinks.Text = "Links:"
        '
        'lnkSite
        '
        Me.lnkSite.AutoSize = True
        Me.lnkSite.Location = New System.Drawing.Point(38, 314)
        Me.lnkSite.Name = "lnkSite"
        Me.lnkSite.Size = New System.Drawing.Size(100, 20)
        Me.lnkSite.TabIndex = 7
        Me.lnkSite.TabStop = True
        Me.lnkSite.Text = "Website"
        '
        'lnkGitHub
        '
        Me.lnkGitHub.AutoSize = True
        Me.lnkGitHub.Location = New System.Drawing.Point(38, 340)
        Me.lnkGitHub.Name = "lnkGitHub"
        Me.lnkGitHub.Size = New System.Drawing.Size(100, 20)
        Me.lnkGitHub.TabIndex = 8
        Me.lnkGitHub.TabStop = True
        Me.lnkGitHub.Text = "Source code"
        '
        'lnkIssues
        '
        Me.lnkIssues.AutoSize = True
        Me.lnkIssues.Location = New System.Drawing.Point(38, 366)
        Me.lnkIssues.Name = "lnkIssues"
        Me.lnkIssues.Size = New System.Drawing.Size(100, 20)
        Me.lnkIssues.TabIndex = 9
        Me.lnkIssues.TabStop = True
        Me.lnkIssues.Text = "Report an issue"
        '
        'lnkPrivacy
        '
        Me.lnkPrivacy.AutoSize = True
        Me.lnkPrivacy.Location = New System.Drawing.Point(280, 314)
        Me.lnkPrivacy.Name = "lnkPrivacy"
        Me.lnkPrivacy.Size = New System.Drawing.Size(100, 20)
        Me.lnkPrivacy.TabIndex = 10
        Me.lnkPrivacy.TabStop = True
        Me.lnkPrivacy.Text = "Privacy"
        '
        'lnkPortfolio
        '
        Me.lnkPortfolio.AutoSize = True
        Me.lnkPortfolio.Location = New System.Drawing.Point(280, 340)
        Me.lnkPortfolio.Name = "lnkPortfolio"
        Me.lnkPortfolio.Size = New System.Drawing.Size(100, 20)
        Me.lnkPortfolio.TabIndex = 11
        Me.lnkPortfolio.TabStop = True
        Me.lnkPortfolio.Text = "Other tools"
        '
        'lblLicense
        '
        Me.lblLicense.Location = New System.Drawing.Point(18, 404)
        Me.lblLicense.Name = "lblLicense"
        ' Two lines: the Russian licence line does not fit on one and lost its last word.
        Me.lblLicense.Size = New System.Drawing.Size(504, 48)
        Me.lblLicense.TabIndex = 12
        Me.lblLicense.Text = "MIT License"
        '
        'lblLogsHint
        '
        Me.lblLogsHint.Location = New System.Drawing.Point(18, 462)
        Me.lblLogsHint.Name = "lblLogsHint"
        Me.lblLogsHint.Size = New System.Drawing.Size(504, 82)
        Me.lblLogsHint.TabIndex = 13
        Me.lblLogsHint.Text = "Logs hint"
        '
        'btnSendLogs
        '
        Me.btnSendLogs.Location = New System.Drawing.Point(18, 556)
        Me.btnSendLogs.Name = "btnSendLogs"
        Me.btnSendLogs.Size = New System.Drawing.Size(300, 40)
        Me.btnSendLogs.TabIndex = 14
        Me.btnSendLogs.Text = "Send logs"
        Me.btnSendLogs.UseVisualStyleBackColor = True
        '
        'btnClose
        '
        Me.btnClose.Location = New System.Drawing.Point(392, 556)
        Me.btnClose.Name = "btnClose"
        Me.btnClose.Size = New System.Drawing.Size(130, 40)
        Me.btnClose.TabIndex = 15
        Me.btnClose.Text = "Close"
        Me.btnClose.UseVisualStyleBackColor = True
        '
        'AboutForm
        '
        Me.AcceptButton = Me.btnClose
        Me.AutoScaleDimensions = New System.Drawing.SizeF(9.0!, 20.0!)
        Me.AutoScaleMode = System.Windows.Forms.AutoScaleMode.Font
        Me.CancelButton = Me.btnClose
        Me.ClientSize = New System.Drawing.Size(540, 616)
        Me.Controls.Add(Me.btnClose)
        Me.Controls.Add(Me.btnSendLogs)
        Me.Controls.Add(Me.lblLogsHint)
        Me.Controls.Add(Me.lblLicense)
        Me.Controls.Add(Me.lnkPortfolio)
        Me.Controls.Add(Me.lnkPrivacy)
        Me.Controls.Add(Me.lnkIssues)
        Me.Controls.Add(Me.lnkGitHub)
        Me.Controls.Add(Me.lnkSite)
        Me.Controls.Add(Me.lblLinks)
        Me.Controls.Add(Me.lnkMail)
        Me.Controls.Add(Me.lblAuthor)
        Me.Controls.Add(Me.lblCli)
        Me.Controls.Add(Me.lblBuild)
        Me.Controls.Add(Me.lblTagline)
        Me.Controls.Add(Me.lblProduct)
        Me.FormBorderStyle = System.Windows.Forms.FormBorderStyle.FixedDialog
        Me.MaximizeBox = False
        Me.MinimizeBox = False
        Me.Name = "AboutForm"
        Me.ShowInTaskbar = False
        Me.StartPosition = System.Windows.Forms.FormStartPosition.CenterParent
        Me.Text = "About FileDO"
        Me.ResumeLayout(False)
        Me.PerformLayout()
    End Sub

    Friend WithEvents lblProduct As Label
    Friend WithEvents lblTagline As Label
    Friend WithEvents lblBuild As Label
    Friend WithEvents lblCli As Label
    Friend WithEvents lblAuthor As Label
    Friend WithEvents lnkMail As LinkLabel
    Friend WithEvents lblLinks As Label
    Friend WithEvents lnkSite As LinkLabel
    Friend WithEvents lnkGitHub As LinkLabel
    Friend WithEvents lnkIssues As LinkLabel
    Friend WithEvents lnkPrivacy As LinkLabel
    Friend WithEvents lnkPortfolio As LinkLabel
    Friend WithEvents lblLicense As Label
    Friend WithEvents lblLogsHint As Label
    Friend WithEvents btnSendLogs As Button
    Friend WithEvents btnClose As Button
    Friend WithEvents tipAbout As ToolTip
End Class
