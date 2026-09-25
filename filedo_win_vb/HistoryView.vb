Imports System.IO

' History & Run Reports view (SP-0006 section 7.7 / M2).
Public Class HistoryView
    Inherits UserControl

    Private ReadOnly dict As Dictionary(Of String, String)
    Private rootTable As TableLayoutPanel
    Private reportListBox As ListBox
    Private detailsBox As TextBox
    Private openReportBtn As Button
    Private showDirBtn As Button
    Private refreshBtn As Button
    Private reportFiles As New List(Of String)()
    Private header As Label
    Private stateLabel As Label
    Private ReadOnly buttons As New List(Of Button)

    ' SHELL-03: what the details box reads of one report - its head (where the command and the
    ' verdict are) and its end. A report of a long run can be hundreds of megabytes, and this window
    ' is a 32-bit process.
    Friend Const ReportHeadBytes As Integer = 4 * 1024
    Friend Const ReportTailBytes As Integer = 256 * 1024

    ' Which read the details box is waiting for; an older read that finishes late is dropped.
    Private readVersion As Integer = 0

    ' The list is read when the page is shown (ShellForm calls RefreshReports on every visit), not
    ' here: a data folder that cannot be read must not stop the window from being built (SHELL-13).
    Public Sub New()
        dict = Localization.GetDict(ShellSettings.Language())
        DoubleBuffered = True
        Dock = DockStyle.Fill
        BuildLayout()
        ApplyTheme()
    End Sub

    Private Function L(key As String) As String
        Dim v As String = Nothing
        If dict IsNot Nothing AndAlso dict.TryGetValue(key, v) Then Return v
        Return key
    End Function

    Private Sub BuildLayout()
        SuspendLayout()

        rootTable = New TableLayoutPanel With {
            .Dock = DockStyle.Fill,
            .ColumnCount = 2,
            .RowCount = 3,
            .Padding = Ui.PxPad(Me, 18, 16, 18, 16),
            .Margin = New Padding(0)
        }
        rootTable.ColumnStyles.Add(New ColumnStyle(SizeType.Percent, 45.0F))
        rootTable.ColumnStyles.Add(New ColumnStyle(SizeType.Percent, 55.0F))
        rootTable.RowStyles.Add(New RowStyle(SizeType.AutoSize))
        rootTable.RowStyles.Add(New RowStyle(SizeType.Percent, 100.0F))
        rootTable.RowStyles.Add(New RowStyle(SizeType.AutoSize))

        header = Ui.StepHeader(Me, L("rail_job_history"))
        stateLabel = New Label With {.Text = "", .AutoSize = True, .Margin = Ui.PxPad(Me, 0, 0, 0, 8)}

        reportListBox = New ListBox With {
            .Dock = DockStyle.Fill,
            .IntegralHeight = False,
            .Margin = New Padding(0, 0, 8, 8)
        }
        reportListBox.AccessibleName = L("rail_job_history")
        AddHandler reportListBox.SelectedIndexChanged, AddressOf ReportListBox_SelectedIndexChanged

        detailsBox = New TextBox With {
            .Dock = DockStyle.Fill,
            .Multiline = True,
            .ReadOnly = True,
            .ScrollBars = ScrollBars.Both,
            .Margin = New Padding(8, 0, 0, 8)
        }
        detailsBox.AccessibleName = L("shell_history_details")

        Dim btnRow As New FlowLayoutPanel With {
            .Dock = DockStyle.Fill,
            .AutoSize = True,
            .FlowDirection = FlowDirection.LeftToRight,
            .Margin = New Padding(0)
        }

        openReportBtn = New Button With {.Text = L("shell_btn_open_report"), .AutoSize = True, .Margin = New Padding(0, 0, 8, 0)}
        AddHandler openReportBtn.Click, AddressOf OpenReportBtn_Click

        showDirBtn = New Button With {.Text = L("shell_btn_show_reports_dir"), .AutoSize = True, .Margin = New Padding(0, 0, 8, 0)}
        AddHandler showDirBtn.Click, AddressOf ShowDirBtn_Click

        refreshBtn = New Button With {.Text = L("shell_btn_refresh"), .AutoSize = True, .Margin = New Padding(0, 0, 8, 0)}
        AddHandler refreshBtn.Click, Sub() RefreshReports()

        btnRow.Controls.Add(openReportBtn)
        btnRow.Controls.Add(showDirBtn)
        btnRow.Controls.Add(refreshBtn)
        buttons.Add(openReportBtn)
        buttons.Add(showDirBtn)
        buttons.Add(refreshBtn)

        Dim headRow As New FlowLayoutPanel With {
            .Dock = DockStyle.Top,
            .AutoSize = True,
            .AutoSizeMode = AutoSizeMode.GrowAndShrink,
            .FlowDirection = FlowDirection.TopDown,
            .WrapContents = False,
            .Margin = New Padding(0)
        }
        headRow.Controls.Add(header)
        headRow.Controls.Add(stateLabel)

        rootTable.Controls.Add(headRow, 0, 0)
        rootTable.SetColumnSpan(headRow, 2)
        rootTable.Controls.Add(reportListBox, 0, 1)
        rootTable.Controls.Add(detailsBox, 1, 1)
        rootTable.Controls.Add(btnRow, 0, 2)
        rootTable.SetColumnSpan(btnRow, 2)

        Controls.Add(rootTable)
        ResumeLayout(True)
    End Sub

    Public Sub RefreshReports()
        readVersion += 1
        reportListBox.Items.Clear()
        reportFiles.Clear()
        detailsBox.Clear()

        ' SHELL-13: a reports folder that cannot be made or read is a sentence on the page with its
        ' cause, never an exception that keeps the window from opening.
        Try
            ' D8: with the switch off the page says so rather than showing an empty list, because an
            ' empty list reads as "nothing happened" and the truth is "nothing was recorded".
            stateLabel.Text = If(ShellSettings.HistoryEnabled(),
                                 Localization.Format(L("shell_reports_dir_fmt"), Runner.GetReportsDir()),
                                 L("shell_history_off"))

            Dim dir = Runner.GetReportsDir()
            If Directory.Exists(dir) Then
                Dim files = Directory.GetFiles(dir, "report_*.log").OrderByDescending(Function(f) File.GetLastWriteTime(f)).ToList()
                For Each f In files
                    reportFiles.Add(f)
                    Dim fi As New FileInfo(f)
                    reportListBox.Items.Add(fi.LastWriteTime.ToString("yyyy-MM-dd HH:mm:ss") & " - " & fi.Name)
                Next
            End If
        Catch ex As Exception
            ShellLog.Write("list the run reports", ex)
            stateLabel.Text = Localization.Format(L("shell_report_read_error"), Problems.Cause(ex))
        End Try

        If reportListBox.Items.Count > 0 Then
            reportListBox.SelectedIndex = 0
        Else
            detailsBox.Text = L("shell_no_history_records")
        End If
    End Sub

    ' The selected report is read off the window's thread, head and end only (SHELL-03).
    Private Sub ReportListBox_SelectedIndexChanged(sender As Object, e As EventArgs)
        Dim idx = reportListBox.SelectedIndex
        If idx < 0 OrElse idx >= reportFiles.Count Then Return
        Dim path = reportFiles(idx)
        readVersion += 1
        Dim mine = readVersion
        detailsBox.Text = ""
        Dim note = L("shell_report_truncated")
        Task.Run(Function() ReadReportView(path, note)).ContinueWith(
            Sub(t)
                Dim text As String
                If t.IsFaulted Then
                    Dim ex = t.Exception.GetBaseException()
                    ShellLog.Write("read report " & path, ex)
                    text = Localization.Format(L("shell_report_read_error"), Problems.Cause(ex))
                Else
                    text = t.Result
                End If
                ShowDetails(mine, text)
            End Sub)
    End Sub

    Private Sub ShowDetails(version As Integer, text As String)
        If IsDisposed OrElse Not IsHandleCreated Then Return
        Try
            BeginInvoke(Sub()
                            If version = readVersion Then detailsBox.Text = text
                        End Sub)
        Catch ex As InvalidOperationException
            ' The window closed while the report was read.
        End Try
    End Sub

    ' A report as the details box shows it: whole when it is small, otherwise its head, a line
    ' saying it was shortened, and its last ReportTailBytes - each cut at a line start, so neither a
    ' line nor a UTF-8 character is shown in half.
    Friend Shared Function ReadReportView(path As String, truncatedNote As String) As String
        Using fs As New FileStream(path, FileMode.Open, FileAccess.Read, FileShare.ReadWrite Or FileShare.Delete)
            If fs.Length <= ReportHeadBytes + ReportTailBytes Then
                Using r As New StreamReader(fs, New System.Text.UTF8Encoding(False), True)
                    Return r.ReadToEnd()
                End Using
            End If

            Dim head = ReadBytes(fs, 0, ReportHeadBytes)
            Dim tail = ReadBytes(fs, fs.Length - ReportTailBytes, ReportTailBytes)

            Dim headText = System.Text.Encoding.UTF8.GetString(head)
            Dim lastNl = headText.LastIndexOf(ControlChars.Lf)
            If lastNl >= 0 Then headText = headText.Substring(0, lastNl + 1)

            Dim tailText = System.Text.Encoding.UTF8.GetString(tail)
            Dim firstNl = tailText.IndexOf(ControlChars.Lf)
            If firstNl >= 0 Then tailText = tailText.Substring(firstNl + 1)

            Return headText & Environment.NewLine & "[.. " & truncatedNote & " ..]" & Environment.NewLine &
                   Environment.NewLine & tailText
        End Using
    End Function

    Private Shared Function ReadBytes(fs As FileStream, offset As Long, count As Integer) As Byte()
        Dim buf(count - 1) As Byte
        fs.Seek(offset, SeekOrigin.Begin)
        Dim got = 0
        While got < count
            Dim n = fs.Read(buf, got, count - got)
            If n <= 0 Then Exit While
            got += n
        End While
        If got < count Then ReDim Preserve buf(Math.Max(0, got) - 1)
        Return buf
    End Function

    Private Sub OpenReportBtn_Click(sender As Object, e As EventArgs)
        Dim idx = reportListBox.SelectedIndex
        If idx >= 0 AndAlso idx < reportFiles.Count Then
            Try
                Process.Start("notepad.exe", """" & reportFiles(idx) & """")
            Catch ex As Exception
                ShellLog.Write("open report in notepad", ex)
                If ShellDialog.Problem(ShellDialog.OwnerOf(Me),
                                       Localization.Format(L("shell_report_open_failed"), Problems.Cause(ex)),
                                       L("shell_btn_show_reports_dir")) Then
                    ShowDirBtn_Click(sender, e)
                End If
            End Try
        End If
    End Sub

    Private Sub ShowDirBtn_Click(sender As Object, e As EventArgs)
        Dim dir As String
        Try
            dir = Runner.GetReportsDir()
        Catch ex As Exception
            ShellLog.Write("find the reports folder", ex)
            ShellDialog.Problem(ShellDialog.OwnerOf(Me), Problems.Cause(ex))
            Return
        End Try
        Ui.OpenFolder(ShellDialog.OwnerOf(Me), dir)
    End Sub

    Public Sub ApplyTheme()
        Dim p = Theme.Current
        BackColor = p.Surface
        ForeColor = p.Text

        header.Font = Theme.FontSubtitle()
        header.ForeColor = p.Accent
        stateLabel.Font = Theme.FontCaption()
        stateLabel.ForeColor = p.MutedText

        reportListBox.Font = Theme.FontBody()
        reportListBox.BackColor = p.SurfaceAlt
        reportListBox.ForeColor = p.Text
        reportListBox.BorderStyle = BorderStyle.FixedSingle

        detailsBox.Font = Theme.FontMono()
        detailsBox.BackColor = p.SurfaceAlt
        detailsBox.ForeColor = p.Text
        detailsBox.BorderStyle = BorderStyle.FixedSingle

        For Each b In buttons
            b.Font = Theme.FontBody()
            Ui.StyleButton(b, p.SurfaceAlt, p.Text, p.Border)
        Next

        Invalidate(True)
    End Sub

End Class
