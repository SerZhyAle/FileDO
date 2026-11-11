' Both settings surfaces host the same reversible value controls. Close promises no rollback.
'
' This window is the standalone Disk manager's settings surface, so it keeps page navigation like the
' shell's (SETTINGS-NAVIGATION 2.0 section 2.1, SP-0169 decision D5): the shell's own Settings and About
' rows - the same RailRow entries, glyphs, hue and geometry - on the start side, the page beside them.
' Settings is first, About last (section 3). Choosing a page changes only the pane: the window keeps its
' rectangle and no value is committed or discarded (section 4.3). About is built on its first visit.
Friend Class DiskSettingsDialog
    Inherits Form

    Friend ReadOnly Panel As SettingsPanel
    Private ReadOnly closeButton As Button
    Private ReadOnly nav As FlowLayoutPanel
    Private ReadOnly pageHost As System.Windows.Forms.Panel
    Private ReadOnly navRows As New List(Of RailEntry)
    Private about As AboutView

    ' For SelfTest.vb: the navigation rows, in order, and the About page once built.
    Friend ReadOnly Property NavRowsForTest As IList(Of RailEntry)
        Get
            Return navRows.AsReadOnly()
        End Get
    End Property

    Friend ReadOnly Property AboutForTest As AboutView
        Get
            Return about
        End Get
    End Property

    ' The two destinations, in rail order, and the one shown last in this run of the program.
    Friend Shared ReadOnly PageKeys As String() = {"rail_job_settings", "rail_job_about"}
    Private Shared lastPage As String = "rail_job_settings"

    ' The navigation column, in design pixels: the longest name of the two in any language, the glyph
    ' column and a scroll bar's room.
    Private Const NavWidth As Integer = 168

    Friend Sub New()
        Text = Localization.T("rail_job_settings")
        Font = Theme.FontBody()
        FormBorderStyle = FormBorderStyle.Sizable
        MinimizeBox = False
        MaximizeBox = False
        ShowInTaskbar = False
        StartPosition = FormStartPosition.CenterParent
        AutoScaleMode = AutoScaleMode.Font
        Size = Ui.PxSize(Me, 620 + NavWidth, 610)
        MinimumSize = Ui.PxSize(Me, 420 + NavWidth, 360)
        AppIcon.Apply(Me)
        Dim root As New TableLayoutPanel With {.Dock = DockStyle.Fill, .ColumnCount = 2, .RowCount = 2, .Padding = Ui.PxPad(Me, 0, 0, 12, 12)}
        root.ColumnStyles.Add(New ColumnStyle(SizeType.Absolute, CSng(Ui.Px(Me, NavWidth))))
        root.ColumnStyles.Add(New ColumnStyle(SizeType.Percent, 100))
        root.RowStyles.Add(New RowStyle(SizeType.Percent, 100))
        root.RowStyles.Add(New RowStyle(SizeType.AutoSize))

        nav = New FlowLayoutPanel With {
            .Dock = DockStyle.Fill,
            .FlowDirection = FlowDirection.TopDown,
            .WrapContents = False,
            .AutoScroll = True,
            .Margin = New Padding(0),
            .Padding = Ui.PxPad(Me, 0, 6, 0, 6),
            .Name = "settings-nav:dialog"
        }
        For Each key In PageKeys
            Dim row = RailRow.All.First(Function(r) r.Key = key)
            Dim e As New RailEntry With {
                .Key = key,
                .Name = "rail:" & key,
                .Text = Localization.T(key),
                .Glyph = row.Glyph,
                .Hue = row.Hue,
                .Band = 0,
                .TabStop = True,
                .RowUnit = Ui.Px(Me, ShellForm.RailTargetHeight),
                .DefaultActionText = Localization.T("shell_acc_open"),
                .Width = Ui.Px(Me, NavWidth),
                .Height = Ui.Px(Me, ShellForm.RailTargetHeight),
                .Margin = New Padding(0)
            }
            e.AccessibleName = Localization.T(key)
            e.AccessibleRole = AccessibleRole.PushButton
            AddHandler e.Click, Sub(s As Object, a As EventArgs) ShowPage(DirectCast(s, RailEntry).Key)
            navRows.Add(e)
            nav.Controls.Add(e)
        Next
        AddHandler nav.SizeChanged, Sub() LayoutNav()

        pageHost = New System.Windows.Forms.Panel With {.Dock = DockStyle.Fill, .Margin = Ui.PxPad(Me, 12, 12, 0, 0)}
        Panel = New SettingsPanel()
        pageHost.Controls.Add(Panel)

        closeButton = New GlyphButton With {.Text = Localization.T("settings_close"), .Glyph = GlyphRef.Vocabulary("nav.close"), .Tier = 16, .AutoSize = True, .AutoSizeMode = AutoSizeMode.GrowAndShrink, .Anchor = AnchorStyles.Right, .DialogResult = DialogResult.Cancel}
        CancelButton = closeButton
        root.Controls.Add(nav, 0, 0)
        root.SetRowSpan(nav, 2)
        root.Controls.Add(pageHost, 1, 0)
        root.Controls.Add(closeButton, 1, 1)
        Controls.Add(root)
        AddHandler Panel.ThemeChanged, AddressOf RepaintTheme
        ShowPage(lastPage)
        RepaintTheme()
        Theme.Watch(Me, AddressOf RepaintTheme)
    End Sub

    ' The page on screen, by its rail key.
    Friend ReadOnly Property CurrentPage As String
        Get
            Return currentKey
        End Get
    End Property

    Private currentKey As String = ""

    ' Shows one destination in the pane, as one frame: the pane is suspended while the other page goes
    ' and this one comes, so no half-switched state is painted (section 5.1).
    Friend Sub ShowPage(key As String)
        If Array.IndexOf(PageKeys, key) < 0 Then key = PageKeys(0)
        lastPage = key
        currentKey = key
        If key = "rail_job_about" AndAlso about Is Nothing Then
            about = New AboutView() With {.Visible = False}
            about.ApplyTheme()
            pageHost.Controls.Add(about)
        End If
        Ui.SuspendTree(pageHost)
        Try
            Panel.Visible = key = "rail_job_settings"
            If about IsNot Nothing Then about.Visible = key = "rail_job_about"
        Finally
            Ui.ResumeTree(pageHost)
        End Try
        For Each row In navRows
            row.Selected = row.Key = key
        Next
    End Sub

    ' Every row as wide as the column and as tall as its name needs (APP-BEHAVIOUR rule 2).
    Private Sub LayoutNav()
        Dim w = nav.ClientSize.Width
        If w < Ui.Px(Me, 60) Then Return
        For Each row In navRows
            row.RowUnit = Ui.Px(Me, ShellForm.RailTargetHeight)
            row.Size = New Size(w, row.PreferredRowHeight(w))
        Next
    End Sub

    Friend Sub DrawClientForCapture(bitmap As Bitmap)
        Controls(0).DrawToBitmap(bitmap, New Rectangle(0, 0, bitmap.Width, bitmap.Height))
    End Sub

    Protected Overrides Sub OnHandleCreated(e As EventArgs)
        MyBase.OnHandleCreated(e)
        Chrome.Apply(Me)
    End Sub

    ' Shown with the tree suspended and laid out once: with the layout live, every Visible change
    ' made the nested AutoSize panels measure their labels again (0.7 s on every show).
    Protected Overrides Sub SetVisibleCore(value As Boolean)
        If value AndAlso Not Visible AndAlso Panel IsNot Nothing Then
            Ui.SuspendTree(Me)
            Try
                MyBase.SetVisibleCore(value)
            Finally
                Ui.ResumeTree(Me)
            End Try
        Else
            MyBase.SetVisibleCore(value)
        End If
    End Sub

    Protected Overrides Sub OnLoad(e As EventArgs)
        MyBase.OnLoad(e)
        Dim work = Screen.FromHandle(Handle).WorkingArea
        Size = New Size(Math.Min(Width, work.Width), Math.Min(Height, work.Height))
        LayoutNav()
    End Sub

    Protected Overrides Sub WndProc(ByRef m As Message)
        MyBase.WndProc(m)
        If m.Msg = &H1A AndAlso ShellSettings.ThemeChoice() = "auto" AndAlso Panel IsNot Nothing Then
            Theme.Refresh()
            RepaintTheme()
        End If
    End Sub

    Friend Sub RepaintTheme()
        BackColor = Theme.Current.Background
        ForeColor = Theme.Current.Text
        Panel.ApplyTheme()
        If nav IsNot Nothing Then
            nav.BackColor = Theme.Current.SurfaceAlt
            For Each row In navRows
                row.Invalidate()
            Next
        End If
        If about IsNot Nothing Then about.ApplyTheme()

        Chrome.Apply(Me)
        AppHost.SettingsThemeChanged()
    End Sub
End Class
