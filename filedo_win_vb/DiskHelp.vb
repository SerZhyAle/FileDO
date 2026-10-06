' The Disk manager's help: the keyboard map as one table, the help window, and the first-steps window.
'
' The keyboard map is data (DiskShortcuts.All), not code scattered over handlers. The manager's key
' handler, every tooltip and menu item that shows a shortcut, and the help window's table all read
' it, so a shortcut cannot be listed and not work, or work and not be listed - the self-test walks it.
'
' Both windows are secondary windows in the sense of APP-BEHAVIOUR rule 1: modal, owned by the
' manager, opened centred on it, and with one path that leaves without changing anything, which
' Escape, the close box and the button are all the same one. The first-steps window is the "first
' run" of rule 11: it asks before it does anything, none of its answers reaches outside the machine
' except a link the user chooses to click, and "Not now" is a real answer that leaves a working
' window. Nothing here downloads anything (rule 4); a link is followed because it was clicked.

' What a key does that is not one of the actions of the matrix.
Friend Enum DiskCommand
    Perform         ' a DiskAction
    Add             ' choose .fdd files to add to the list
    Help
    MainWindow      ' bring the FileDO main window forward
    Filter          ' the focus goes to the filter box
    ClearFilter
    SelectAll
    CopyPath
    ContextMenu
    DefaultAction   ' a double-click's action for the row (spec 6.2, D3)
End Enum

Friend Class DiskShortcut
    Public ReadOnly Chord As Keys
    Public ReadOnly Command As DiskCommand
    Public ReadOnly Action As DiskAction?
    Public ReadOnly LabelKey As String
    ' Keys that a text box also uses (Ctrl+C, Ctrl+A, Delete, Enter) act on the list only while the
    ' list has the focus, and are left to the box otherwise.
    Public ReadOnly ListOnly As Boolean

    Public Sub New(chord As Keys, command As DiskCommand, action As DiskAction?, labelKey As String, listOnly As Boolean)
        Me.Chord = chord
        Me.Command = command
        Me.Action = action
        Me.LabelKey = labelKey
        Me.ListOnly = listOnly
    End Sub
End Class

Friend Module DiskShortcuts

    ' In the order the help window lists them. The same operations the spec's 6.2 names, and the rest
    ' of what the toolbar offers.
    Friend ReadOnly All As DiskShortcut() = {
        New DiskShortcut(Keys.Control Or Keys.N, DiskCommand.Perform, DiskAction.NewDisk, "vd_mgr_act_new", False),
        New DiskShortcut(Keys.Control Or Keys.Shift Or Keys.O, DiskCommand.MainWindow, Nothing, "vd_mgr_btn_main", False),
        New DiskShortcut(Keys.Control Or Keys.O, DiskCommand.Add, Nothing, "vd_mgr_btn_add", False),
        New DiskShortcut(Keys.Control Or Keys.M, DiskCommand.Perform, DiskAction.Mount, "vd_mgr_act_mount", False),
        New DiskShortcut(Keys.Control Or Keys.Shift Or Keys.M, DiskCommand.Perform, DiskAction.MountReadOnly, "vd_mgr_act_mount_ro", False),
        New DiskShortcut(Keys.Control Or Keys.U, DiskCommand.Perform, DiskAction.Unmount, "vd_mgr_act_unmount", False),
        New DiskShortcut(Keys.Control Or Keys.E, DiskCommand.Perform, DiskAction.OpenDrive, "vd_mgr_act_open", False),
        New DiskShortcut(Keys.Control Or Keys.S, DiskCommand.Perform, DiskAction.SaveNow, "vd_mgr_act_save", False),
        New DiskShortcut(Keys.Control Or Keys.Shift Or Keys.S, DiskCommand.Perform, DiskAction.ShareDisk, "vd_share_register", False),
        New DiskShortcut(Keys.Control Or Keys.I, DiskCommand.Perform, DiskAction.Info, "vd_mgr_act_info", False),
        New DiskShortcut(Keys.Control Or Keys.Shift Or Keys.V, DiskCommand.Perform, DiskAction.Verify, "vd_mgr_act_verify", False),
        New DiskShortcut(Keys.Return, DiskCommand.DefaultAction, Nothing, "vd_mgr_key_default", True),
        New DiskShortcut(Keys.Delete, DiskCommand.Perform, DiskAction.Forget, "vd_mgr_act_forget", True),
        New DiskShortcut(Keys.Control Or Keys.C, DiskCommand.CopyPath, Nothing, "vd_mgr_act_copy_path", True),
        New DiskShortcut(Keys.Control Or Keys.A, DiskCommand.SelectAll, Nothing, "vd_mgr_key_select_all", True),
        New DiskShortcut(Keys.Apps, DiskCommand.ContextMenu, Nothing, "vd_mgr_key_menu", True),
        New DiskShortcut(Keys.Shift Or Keys.F10, DiskCommand.ContextMenu, Nothing, "vd_mgr_key_menu", True),
        New DiskShortcut(Keys.F5, DiskCommand.Perform, DiskAction.Refresh, "vd_mgr_act_refresh", False),
        New DiskShortcut(Keys.Control Or Keys.F, DiskCommand.Filter, Nothing, "vd_mgr_key_filter", False),
        New DiskShortcut(Keys.Escape, DiskCommand.ClearFilter, Nothing, "vd_mgr_key_clear_filter", False),
        New DiskShortcut(Keys.F1, DiskCommand.Help, Nothing, "vd_mgr_key_help", False)
    }

    ' The shortcut a key press is, or Nothing.
    Friend Function Find(keyData As Keys, listFocused As Boolean) As DiskShortcut
        For Each s In All
            If s.Chord = keyData AndAlso (Not s.ListOnly OrElse listFocused) Then Return s
        Next
        Return Nothing
    End Function

    ' "Ctrl+Shift+M" - the names of the keys on a keyboard, which no translation changes.
    Friend Function KeyText(chord As Keys) As String
        Dim parts As New List(Of String)
        If (chord And Keys.Control) <> 0 Then parts.Add("Ctrl")
        If (chord And Keys.Shift) <> 0 Then parts.Add("Shift")
        If (chord And Keys.Alt) <> 0 Then parts.Add("Alt")
        Dim k = chord And Keys.KeyCode
        Select Case k
            Case Keys.Delete : parts.Add("Del")
            Case Keys.Escape : parts.Add("Esc")
            Case Keys.Return : parts.Add("Enter")
            Case Keys.Apps : parts.Add("Menu")
            Case Else : parts.Add(k.ToString())
        End Select
        Return String.Join("+", parts.ToArray())
    End Function

    ' The key of an action, "" when it has none. Unmounting an image is unmounting: one key, and the
    ' window says which of the two it does from what is selected.
    Friend Function TextFor(a As DiskAction) As String
        Dim wanted = If(a = DiskAction.UnmountImage, DiskAction.Unmount, a)
        For Each s In All
            If s.Command = DiskCommand.Perform AndAlso s.Action.HasValue AndAlso s.Action.Value = wanted Then Return KeyText(s.Chord)
        Next
        Return ""
    End Function

    ' " (Ctrl+M)" for a tooltip, or "".
    Friend Function Suffix(a As DiskAction) As String
        Dim t = TextFor(a)
        Return If(t = "", "", " (" & t & ")")
    End Function

    ' The help window's rows: the keys of one label joined ("Menu / Shift+F10"), in table order.
    Friend Function HelpRows() As List(Of KeyValuePair(Of String, String))
        Dim out As New List(Of KeyValuePair(Of String, String))
        Dim at As New Dictionary(Of String, Integer)(StringComparer.Ordinal)
        For Each s In All
            Dim t = KeyText(s.Chord)
            If at.ContainsKey(s.LabelKey) Then
                out(at(s.LabelKey)) = New KeyValuePair(Of String, String)(out(at(s.LabelKey)).Key & " / " & t, s.LabelKey)
            Else
                at(s.LabelKey) = out.Count
                out.Add(New KeyValuePair(Of String, String)(t, s.LabelKey))
            End If
        Next
        Return out
    End Function

End Module

' The links the help sends a user to: the guide of this window, the guides, the site, the project's
' documentation and its issue tracker - all in Links.vb, none opened but by a click.
Friend Module DiskHelpLinks

    Friend ReadOnly Property All As KeyValuePair(Of String, String)()
        Get
            Return New KeyValuePair(Of String, String)() {
                New KeyValuePair(Of String, String)("vd_help_menu_guide", Links.DiskGuide),
                New KeyValuePair(Of String, String)("vd_help_menu_share_guide", Links.ShareGuide),
                New KeyValuePair(Of String, String)("vd_help_menu_guides", Links.Guides),
                New KeyValuePair(Of String, String)("vd_help_menu_site", Links.Site),
                New KeyValuePair(Of String, String)("vd_help_menu_docs", Links.Readme),
                New KeyValuePair(Of String, String)("vd_help_menu_issues", Links.Issues)
            }
        End Get
    End Property

End Module

' The base of the two windows: a scrolling page of themed labels, and a footer of buttons. It sizes
' itself to its content up to most of the screen and scrolls beyond that, so a sentence that is long
' in German grows the window rather than being cut (APP-BEHAVIOUR rule 2).
Friend MustInherit Class DiskPageDialog
    Inherits Form

    Protected ReadOnly dict As Dictionary(Of String, String)
    Protected ReadOnly ownerControl As Control
    Protected ReadOnly page As TableLayoutPanel
    Protected ReadOnly footer As FlowLayoutPanel
    Protected ReadOnly contentWidth As Integer
    Private ReadOnly scroller As Panel
    Private ReadOnly rootLayout As TableLayoutPanel
    Private ReadOnly mutedLabels As New List(Of Label)
    Private ReadOnly headingLabels As New List(Of Label)
    Private ReadOnly strongLabels As New List(Of Label)
    Private ReadOnly linkLabels As New List(Of LinkLabel)
    Private ReadOnly plainButtons As New List(Of Button)

    Protected Sub New(d As Dictionary(Of String, String), title As String, owner As Control, Optional widthDesign As Integer = 620)
        dict = If(d, New Dictionary(Of String, String)())
        ownerControl = owner
        contentWidth = Ui.Px(owner, widthDesign)
        SuspendLayout()
        Text = title
        FormBorderStyle = FormBorderStyle.FixedDialog
        MinimizeBox = False
        MaximizeBox = False
        ShowInTaskbar = False
        ShowIcon = False
        KeyPreview = True
        StartPosition = FormStartPosition.CenterParent
        AutoScaleMode = AutoScaleMode.Font
        Font = Theme.FontBody()

        rootLayout = New TableLayoutPanel With {.Dock = DockStyle.Fill, .ColumnCount = 1, .RowCount = 2, .Margin = New Padding(0)}
        rootLayout.ColumnStyles.Add(New ColumnStyle(SizeType.Percent, 100.0F))
        rootLayout.RowStyles.Add(New RowStyle(SizeType.Percent, 100.0F))
        rootLayout.RowStyles.Add(New RowStyle(SizeType.AutoSize))

        scroller = New Panel With {.Dock = DockStyle.Fill, .AutoScroll = True, .Padding = Ui.PxPad(owner, 22, 18, 12, 8), .Margin = New Padding(0)}
        page = New TableLayoutPanel With {
            .AutoSize = True,
            .AutoSizeMode = AutoSizeMode.GrowAndShrink,
            .ColumnCount = 1,
            .Margin = New Padding(0),
            .Location = New Point(scroller.Padding.Left, scroller.Padding.Top)
        }
        page.ColumnStyles.Add(New ColumnStyle(SizeType.AutoSize))
        scroller.Controls.Add(page)

        footer = New FlowLayoutPanel With {
            .AutoSize = True,
            .AutoSizeMode = AutoSizeMode.GrowAndShrink,
            .FlowDirection = FlowDirection.RightToLeft,
            .WrapContents = True,
            .Dock = DockStyle.Fill,
            .Padding = Ui.PxPad(owner, 22, 10, 22, 14),
            .Margin = New Padding(0)
        }
        rootLayout.Controls.Add(scroller, 0, 0)
        rootLayout.Controls.Add(footer, 0, 1)
        Controls.Add(rootLayout)
    End Sub

    Protected Function T(key As String) As String
        Dim v As String = Nothing
        If dict.TryGetValue(key, v) Then Return Localization.Multiline(v)
        Return key
    End Function

    Protected Shared Function Tr(d As Dictionary(Of String, String), key As String) As String
        Dim v As String = Nothing
        If d IsNot Nothing AndAlso d.TryGetValue(key, v) Then Return Localization.Multiline(v)
        Return key
    End Function

    Protected Function P(designPixels As Integer) As Integer
        Return Ui.Px(ownerControl, designPixels)
    End Function

    Protected Function PPad(l As Integer, t As Integer, r As Integer, b As Integer) As Padding
        Return Ui.PxPad(ownerControl, l, t, r, b)
    End Function

    Protected Sub AddRow(c As Control)
        page.RowStyles.Add(New RowStyle(SizeType.AutoSize))
        page.Controls.Add(c, 0, page.RowStyles.Count - 1)
    End Sub

    Protected Function AddTitle(text As String) As Label
        Dim l As New Label With {.Text = text, .AutoSize = True, .MaximumSize = New Size(contentWidth, 0), .Margin = PPad(0, 0, 0, 10)}
        headingLabels.Add(l)
        AddRow(l)
        Return l
    End Function

    Protected Function AddSection(text As String) As Label
        Dim l As New Label With {.Text = text, .AutoSize = True, .MaximumSize = New Size(contentWidth, 0), .Margin = PPad(0, 16, 0, 6)}
        strongLabels.Add(l)
        AddRow(l)
        Return l
    End Function

    Protected Function AddParagraph(text As String, Optional muted As Boolean = False) As Label
        Dim l As New Label With {.Text = text, .AutoSize = True, .MaximumSize = New Size(contentWidth, 0), .Margin = PPad(0, 0, 0, 6)}
        If muted Then mutedLabels.Add(l)
        AddRow(l)
        Return l
    End Function

    Protected Function NewLabel(text As String, width As Integer, Optional muted As Boolean = False, Optional strong As Boolean = False) As Label
        Dim l As New Label With {.Text = text, .AutoSize = True, .MaximumSize = New Size(width, 0), .Margin = PPad(0, 2, 8, 2)}
        If muted Then mutedLabels.Add(l)
        If strong Then strongLabels.Add(l)
        Return l
    End Function

    Protected Function NewLink(text As String, url As String) As LinkLabel
        Dim l As New LinkLabel With {.Text = text, .AutoSize = True, .MaximumSize = New Size(contentWidth, 0), .Margin = PPad(0, 2, 0, 2), .Tag = url}
        l.AccessibleName = text
        AddHandler l.LinkClicked, Sub(s, e) Links.Open(Me, CStr(DirectCast(s, LinkLabel).Tag))
        linkLabels.Add(l)
        Return l
    End Function

    ' A button of the footer or of an answer column. The leave button is created with leave:=True and
    ' is both the accept and the cancel button, so Enter, Escape, the close box and the button are one
    ' path (APP-BEHAVIOUR rule 1).
    Protected Function NewButton(text As String, Optional glyph As GlyphRef = Nothing, Optional leave As Boolean = False) As GlyphButton
        Dim b As New GlyphButton With {.Text = text, .Glyph = glyph, .Tier = 20, .Margin = PPad(8, 0, 0, 0)}
        b.AccessibleName = text
        If leave Then
            b.DialogResult = DialogResult.Cancel
            CancelButton = b
            AcceptButton = b
        End If
        Return b
    End Function

    ' Sizes the window to its content, at most most of the screen, and themes it.
    Protected Sub Finish()
        ApplyTheme()
        Theme.Watch(Me, AddressOf Retheme)
        ResumeLayout(True)
        page.Width = contentWidth
        Dim work = Screen.FromControl(If(CType(ownerControl, Control), Me)).WorkingArea
        Dim pageHeight = page.GetPreferredSize(New Size(contentWidth, 0)).Height
        Dim footerHeight = footer.GetPreferredSize(New Size(contentWidth, 0)).Height
        Dim wanted = pageHeight + scroller.Padding.Vertical + footerHeight
        Dim most = CInt(work.Height * 0.85)
        Dim height = Math.Min(wanted, most)
        Dim width = contentWidth + scroller.Padding.Horizontal + If(wanted > most, SystemInformation.VerticalScrollBarWidth, 0)
        ClientSize = New Size(Math.Min(width, work.Width), height)
        scroller.AutoScrollMinSize = New Size(0, pageHeight + scroller.Padding.Vertical)
    End Sub

    Protected Overrides Sub OnHandleCreated(e As EventArgs)
        MyBase.OnHandleCreated(e)
        Chrome.Apply(Me)
    End Sub

    Protected Sub ApplyTheme()
        Dim p = Theme.Current
        BackColor = p.Surface
        ForeColor = p.Text
        Ui.HitTargetFloor(Me)
        rootLayout.BackColor = p.Surface
        scroller.BackColor = p.Surface
        page.BackColor = p.Surface
        footer.BackColor = p.Surface
        For Each c In AllChildren(page)
            c.Font = Theme.FontBody()
            If TypeOf c Is TextBox OrElse TypeOf c Is ComboBox Then
                c.BackColor = p.Surface
                c.ForeColor = p.Text
            End If
            If TypeOf c Is Label Then
                c.ForeColor = If(mutedLabels.Contains(DirectCast(c, Label)), p.MutedText, p.Text)
                c.BackColor = p.Surface
            ElseIf TypeOf c Is FlowLayoutPanel OrElse TypeOf c Is TableLayoutPanel Then
                c.BackColor = p.Surface
            End If
        Next
        For Each l In headingLabels
            l.Font = Theme.FontSubtitle()
        Next
        For Each l In strongLabels
            l.Font = Theme.FontBodyStrong()
        Next
        For Each l In linkLabels
            l.Font = Theme.FontBody()
            l.LinkColor = p.Link
            l.ActiveLinkColor = p.Link
            l.VisitedLinkColor = p.Link
            l.BackColor = p.Surface
        Next
    End Sub

    Friend Iterator Function AllChildren(root As Control) As IEnumerable(Of Control)
        For Each c As Control In root.Controls
            Yield c
            For Each inner In AllChildren(c)
                Yield inner
            Next
        Next
    End Function

    Friend Sub DrawClientForCapture(bmp As Bitmap)
        rootLayout.DrawToBitmap(bmp, New Rectangle(0, 0, bmp.Width, bmp.Height))
    End Sub

    ' Every text and every control of the window, for the self-test and for a repaint after a theme
    ' switch while it is open.
    Friend Sub Retheme()
        ApplyTheme()
        Chrome.Apply(Me)
        Invalidate(True)
    End Sub

End Class

' The help window: what the window is, what each state of a row says, the keyboard, what is good to
' know, and where to read more. Opened by F1 and by the Help button.
Friend Class DiskHelpDialog
    Inherits DiskPageDialog

    ' The states the legend explains, in the order of spec 5.4, with the key of each one's meaning.
    Friend Shared ReadOnly Legend As DiskRowState() = {
        DiskRowState.Mounted, DiskRowState.Unsaved, DiskRowState.ServerGone, DiskRowState.Image,
        DiskRowState.NotMounted, DiskRowState.Unclean, DiskRowState.Missing, DiskRowState.Different,
        DiskRowState.Unreadable, DiskRowState.Busy}

    Friend Shared Function LegendKey(state As DiskRowState) As String
        Return "vd_help_state_" & state.ToString().ToLowerInvariant()
    End Function

    Friend Shared Function StateWordKey(state As DiskRowState) As String
        Select Case state
            Case DiskRowState.Mounted : Return "vd_mgr_state_mounted"
            Case DiskRowState.Unsaved : Return "vd_help_state_unsaved_word"
            Case DiskRowState.ServerGone : Return "vd_mgr_state_server_gone"
            Case DiskRowState.Image : Return "vd_mgr_state_image"
            Case DiskRowState.NotMounted : Return "vd_mgr_state_not_mounted"
            Case DiskRowState.Unclean : Return "vd_mgr_state_unclean"
            Case DiskRowState.Missing : Return "vd_mgr_state_missing"
            Case DiskRowState.Different : Return "vd_mgr_state_different"
            Case DiskRowState.Unreadable : Return "vd_mgr_state_unreadable"
        End Select
        Return "vd_mgr_state_busy_other"
    End Function

    Private ReadOnly glyphBoxes As New List(Of GlyphBox)

    Public Sub New(d As Dictionary(Of String, String), owner As Control, packaged As Boolean)
        MyBase.New(d, Tr(d, "vd_help_title"), owner, 640)
        AddTitle(T("vd_help_heading"))
        AddParagraph(T("vd_help_intro"))
        If packaged Then AddParagraph(T("vd_welcome_packaged"), muted:=True)

        AddSection(T("vd_help_states_title"))
        For Each st In Legend
            AddRow(StateRow(st))
        Next

        AddSection(T("vd_help_keys_title"))
        AddRow(KeyTable())

        AddSection(T("vd_help_notes_title"))
        AddParagraph(T("vd_share_help"))
        AddParagraph(T("vd_help_state_fms"))
        AddParagraph(T("vd_mgr_close_mounted"))
        AddParagraph(T("vd_help_note_uac"))
        AddParagraph(T("vd_help_note_autostart"))
        AddParagraph(T("vd_facts_obfuscated"))
        AddParagraph(T("vd_facts_encrypted"))
        AddParagraph(T("vd_mgr_detail_open_while_mounted"))

        AddSection(T("vd_help_links_title"))
        AddRow(NewLink(T("vd_share_install"), Links.FMSInstall))
        For Each kv In DiskHelpLinks.All
            AddRow(NewLink(T(kv.Key), kv.Value))
        Next

        Dim close = NewButton(T("shell_btn_close"), Nothing, True)
        close.Margin = PPad(0, 0, 0, 0)
        footer.Controls.Add(close)
        Finish()
        RethemeGlyphs()
        ' The keyboard starts on the leave button, not on the first link a long page would scroll to.
        ActiveControl = close
    End Sub

    ' One state: its glyph in its tone (or a blank where a state has none), its word, and what it means.
    Private Function StateRow(state As DiskRowState) As Control
        Dim row As New TableLayoutPanel With {.AutoSize = True, .AutoSizeMode = AutoSizeMode.GrowAndShrink, .ColumnCount = 2, .Margin = PPad(0, 0, 0, 6)}
        row.ColumnStyles.Add(New ColumnStyle(SizeType.Absolute, P(28)))
        row.ColumnStyles.Add(New ColumnStyle(SizeType.AutoSize))
        Dim box As New GlyphBox With {.Tier = 16, .Glyph = DiskStates.GlyphOf(state), .Tag = state, .Margin = PPad(2, 3, 0, 0)}
        glyphBoxes.Add(box)
        Dim text As New FlowLayoutPanel With {.AutoSize = True, .AutoSizeMode = AutoSizeMode.GrowAndShrink, .FlowDirection = FlowDirection.TopDown,
                                              .WrapContents = False, .Margin = New Padding(0)}
        text.Controls.Add(NewLabel(T(StateWordKey(state)), contentWidth - P(36), strong:=True))
        text.Controls.Add(NewLabel(T(LegendKey(state)), contentWidth - P(36), muted:=True))
        row.Controls.Add(box, 0, 0)
        row.Controls.Add(text, 1, 0)
        Return row
    End Function

    Private Function KeyTable() As Control
        Dim table As New TableLayoutPanel With {.AutoSize = True, .AutoSizeMode = AutoSizeMode.GrowAndShrink, .ColumnCount = 2, .Margin = New Padding(0)}
        table.ColumnStyles.Add(New ColumnStyle(SizeType.Absolute, P(180)))
        table.ColumnStyles.Add(New ColumnStyle(SizeType.AutoSize))
        For Each kv In DiskShortcuts.HelpRows()
            table.RowStyles.Add(New RowStyle(SizeType.AutoSize))
            Dim r = table.RowStyles.Count - 1
            table.Controls.Add(NewLabel(kv.Key, P(172), strong:=True), 0, r)
            table.Controls.Add(NewLabel(T(kv.Value), contentWidth - P(190)), 1, r)
        Next
        Return table
    End Function

    ' The legend's glyphs are drawn in the tone of the palette in force.
    Private Sub RethemeGlyphs()
        Dim p = Theme.Current
        For Each b In glyphBoxes
            b.ForeColor = DiskStates.ToneOf(CType(b.Tag, DiskRowState), p)
        Next
    End Sub

    Friend ReadOnly Property RowCount As Integer
        Get
            Return glyphBoxes.Count
        End Get
    End Property

End Class

' What the first-steps window's answer was.
Friend Enum DiskWelcomeAnswer
    NotNow
    CreateDisk
    AddExisting
    OpenGuide
End Enum

' The first-steps window (APP-BEHAVIOUR rule 11): shown once, the first time the Disk manager opens,
' and from Help whenever the user asks. It says in four sentences what a virtual disk is and what to
' expect - the honest words about obfuscated and encrypted, the consent Windows asks for, that disks
' stay mounted - and offers three ways to start and one to do nothing. The answers are a full-width
' column, so a long caption in any language wraps instead of being cut; "Not now" is the leave path.
Friend Class DiskWelcomeDialog
    Inherits DiskPageDialog

    Private answerValue As DiskWelcomeAnswer = DiskWelcomeAnswer.NotNow
    Private ReadOnly answers As New List(Of GlyphButton)

    Public Sub New(d As Dictionary(Of String, String), owner As Control, packaged As Boolean)
        MyBase.New(d, Tr(d, "vd_welcome_title"), owner, 560)
        AddTitle(T("vd_welcome_title"))
        AddParagraph(T("vd_welcome_what"))
        AddParagraph(T("vd_facts_obfuscated") & Environment.NewLine & T("vd_facts_encrypted"))
        AddParagraph(T(If(packaged, "vd_welcome_packaged", "vd_help_note_uac")) & If(packaged, "", " " & T("vd_mgr_close_mounted")))
        AddParagraph(T("vd_welcome_hint"), muted:=True)

        AddAnswer(T("vd_welcome_create"), DiskGlyphs.CreateDisk, DiskWelcomeAnswer.CreateDisk)
        AddAnswer(T("vd_welcome_add"), DiskGlyphs.RememberName, DiskWelcomeAnswer.AddExisting)
        AddAnswer(T("vd_welcome_guide"), DiskGlyphs.OpenExternal, DiskWelcomeAnswer.OpenGuide)

        Dim later = NewButton(T("vd_welcome_later"), Nothing, True)
        later.Margin = PPad(0, 0, 0, 0)
        footer.Controls.Add(later)
        Finish()
        ActiveControl = later
    End Sub

    Private Sub AddAnswer(text As String, glyph As GlyphRef, answer As DiskWelcomeAnswer)
        Dim b = NewButton(text, glyph)
        b.AutoSize = False
        b.Dock = DockStyle.Top
        b.Height = b.GetPreferredSize(Size.Empty).Height + P(6)
        b.Margin = PPad(0, 4, 0, 4)
        AddHandler b.Click, Sub()
                                answerValue = answer
                                DialogResult = DialogResult.OK
                                Close()
                            End Sub
        answers.Add(b)
        AddRow(b)
    End Sub

    Public ReadOnly Property Answer As DiskWelcomeAnswer
        Get
            Return answerValue
        End Get
    End Property

    Friend ReadOnly Property AnswerCount As Integer
        Get
            Return answers.Count
        End Get
    End Property

End Class
