' The left rail - the whole navigation of the shell (SP-0006 section 5.1).
'
' Two rules from section 11 shape this file more than anything else:
'
'   Item 6, flat and owner-drawn. WinForms' own button draws a 3D border and a grey face that no
'   amount of colour tokens can hide, so the entries paint themselves. Double buffering is on
'   because a rail that flickers on resize reads as cheap however good the palette is.
'
'   Item 8, accessibility is part of "good-looking". Every entry is a real focusable control with
'   an AccessibleName, the focus ring is drawn, and **selection is never colour alone**: the
'   selected entry carries an accent bar on its left edge and a bold label, so it is still the
'   selected entry in a screenshot printed in grey.
'
' Two rules of the shared desktop contracts:
'
'   APP-BEHAVIOUR rule 2 - growing text grows the surface. A label that does not fit on one line
'   wraps, and the row grows to hold it; nothing here is shortened with an ellipsis, in any of the
'   five languages. SelfTest measures every label against the rectangle it is drawn into.
'
'   APP-BEHAVIOUR rule 9 - a control automation can reach. Every row answers "do your default
'   action" through its accessible object, which is what UI Automation's Invoke reaches, and every
'   row is named "rail:<key>" so a script finds it by that name instead of by where it is drawn.
'
' The rail's content is SP-0006 section 5.2, with one correction from the owner: the rows name the
' job, they do not ask the reader a question. "Is this drive honest?" is a line of copy; "Capacity
' test (fake drives)" is what the row runs, and it is the shorter of the two in every locale.

' The rail's rows in order: the one table ShellForm builds the rail from and SelfTest measures.
'
' It is also the rail's glyph map (ICON-SET rung 2, SP-0016 T1): each row names the vocabulary
' meaning it shows, and the drawing comes from the vendored catalog file of that id (ICON-EXTERNAL
' rule 5). Twelve of these meanings entered the vocabulary in ICON-SET 0.14 at FileDO's request;
' before that the rows drew Segoe stand-ins, several of them another meaning's picture. A future
' row whose meaning the vocabulary lacks takes GlyphRef.Waiting with the id proposed for it, and
' SelfTest holds the number of such rows at zero unless the baseline is raised with a reason.
'
' The Disks group (SP-0004 P6) is partly that case. Its list of containers draws the .fdd type's
' own meaning, content.disk-container (ICON-SET 0.16), the drawing the Explorer type icon is made
' from. Six rows mean what a vocabulary record already means - info, verify, export, save, a copy
' (Clone), a delete (Destroy) - and draw that drawing. The other ten are meanings ICON-SET has no
' record for (nothing for create, mount, unmount, compact, grow, format, seal, a password change, a
' schedule or a name), so they draw a Segoe stand-in under a proposed id until the records land and
' assets\sync-icon-glyphs.ps1 vendors them; SelfTest's baseline says so. Those meanings - and the rest
' of the Disks rows' pictures - are defined once, in DiskGlyphs.vb, which the Disk manager's toolbar,
' menus and buttons read too, so a meaning looks the same wherever it is shown (ICON-SET rule 1).
'
' The groups (the owner, 2026-09-30: the folded headers ran into one another). Three things follow:
'
'   - A group header carries the vocabulary's glyph of what the group is about, in the colour look of
'     ICON-RENDER section 10 (Hue names the FileDO tone in Theme.vb), and the chevron moves to the
'     right edge. Its rows keep the mono look: they are actions, and the header says the kind.
'   - A group with one job is no group: the job stands in the rail by itself (RailRow.Alone), in the
'     hue the group would have had, and a section with one row has no header to fold.
'   - The rail is a checker of bands: each block (a header with its rows, or a lone job) is one
'     band, and neighbouring blocks alternate (ShellForm.BuildRail), so shut headers read as
'     separate rows instead of one dark column.
Public Class RailRow
    Public ReadOnly Key As String
    Public ReadOnly Glyph As GlyphRef
    Public ReadOnly IsGroup As Boolean

    ' The Theme.GroupTone key the row's glyph is drawn in - a header's, or a lone job's. Empty for a
    ' row inside a group, which draws in the text colour.
    Public ReadOnly Hue As String

    ' A job that stands at the level of the headers because its group would have held only it.
    Public ReadOnly IsAlone As Boolean

    Private Sub New(key As String, glyph As GlyphRef, isGroup As Boolean, hue As String, isAlone As Boolean)
        Me.Key = key
        Me.Glyph = glyph
        Me.IsGroup = isGroup
        Me.Hue = hue
        Me.IsAlone = isAlone
    End Sub

    Public Shared Function Group(key As String, glyph As GlyphRef, hue As String) As RailRow
        Return New RailRow(key, glyph, True, hue, False)
    End Function

    Public Shared Function Job(key As String, glyph As GlyphRef) As RailRow
        Return New RailRow(key, glyph, False, "", False)
    End Function

    Public Shared Function Alone(key As String, glyph As GlyphRef, hue As String) As RailRow
        Return New RailRow(key, glyph, False, hue, True)
    End Function

    ' The Disks group's first row opens the Disk Manager, a window of its own (SP-0063 4.2, D1) -
    ' not a page of the shell, so it is no job of the catalogue. It shows the .fdd type's own
    ' meaning, as the list does (D8: no new catalog record).
    Public Const DiskManagerKey As String = "rail_job_vd_manager"

    ' Every row leads somewhere today. A row for a job that is still being written is not listed as
    ' "coming later": a rail is navigation, not a roadmap, and a row that answers nothing when it
    ' is clicked costs the reader more than the announcement is worth.
    Public Shared ReadOnly All As RailRow() = {
        RailRow.Group("rail_group_check", GlyphRef.Vocabulary("action.verify"), "program.check"),
        RailRow.Job("rail_job_capacity", GlyphRef.Vocabulary("feature.capacity-test")),
        RailRow.Job("rail_job_speed", GlyphRef.Vocabulary("feature.speed-test")),
        RailRow.Job("rail_job_info", GlyphRef.Vocabulary("app.info")),
        RailRow.Job("rail_job_damaged", GlyphRef.Vocabulary("action.verify")),
        RailRow.Job("rail_job_probe", GlyphRef.Vocabulary("feature.raw-probe")),
        RailRow.Job("rail_job_recover", GlyphRef.Vocabulary("action.recover-drive")),
        RailRow.Group("rail_group_tidy", GlyphRef.Vocabulary("action.sort"), "program.tidy"),
        RailRow.Job("rail_job_duplicates", GlyphRef.Vocabulary("action.find-duplicates")),
        RailRow.Job("rail_job_compare", GlyphRef.Vocabulary("action.compare")),
        RailRow.Job("rail_job_clean", GlyphRef.Vocabulary("action.delete")),
        RailRow.Alone("rail_job_copy", GlyphRef.Vocabulary("action.copy"), "program.copy"),
        RailRow.Group("rail_group_erase", GlyphRef.Vocabulary("action.delete"), "program.erase"),
        RailRow.Job("rail_job_fill", GlyphRef.Vocabulary("action.fill-space")),
        RailRow.Job("rail_job_wipe", GlyphRef.Vocabulary("action.wipe")),
        RailRow.Group("rail_group_protect", GlyphRef.Vocabulary("action.secure"), "program.protect"),
        RailRow.Job("rail_job_secure", GlyphRef.Vocabulary("action.secure")),
        RailRow.Job("rail_job_unsecure", GlyphRef.Vocabulary("action.unsecure")),
        RailRow.Job("rail_job_reveal", GlyphRef.Vocabulary("nav.open-external")),
        RailRow.Group("rail_group_disks", GlyphRef.Vocabulary("system.storage"), "program.disks"),
        RailRow.Job(DiskManagerKey, DiskGlyphs.DiskContainer),
        RailRow.Job("rail_job_vd_list", DiskGlyphs.DiskContainer),
        RailRow.Job("rail_job_vd_new", DiskGlyphs.CreateDisk),
        RailRow.Job("rail_job_vd_mount", DiskGlyphs.MountDisk),
        RailRow.Job("rail_job_vd_unmount", DiskGlyphs.UnmountDisk),
        RailRow.Job("rail_job_vd_info", GlyphRef.Vocabulary("app.info")),
        RailRow.Job("rail_job_vd_verify", GlyphRef.Vocabulary("action.verify")),
        RailRow.Job("rail_job_vd_export", GlyphRef.Vocabulary("action.export")),
        RailRow.Job("rail_job_vd_save", GlyphRef.Vocabulary("action.save")),
        RailRow.Job("rail_job_vd_compact", DiskGlyphs.CompactDisk),
        RailRow.Job("rail_job_vd_grow", DiskGlyphs.GrowDisk),
        RailRow.Job("rail_job_vd_format", DiskGlyphs.FormatDisk),
        RailRow.Job("rail_job_vd_seal", DiskGlyphs.SealDisk),
        RailRow.Job("rail_job_vd_clone", GlyphRef.Vocabulary("action.copy")),
        RailRow.Job("rail_job_vd_pass", DiskGlyphs.ChangePassword),
        RailRow.Job("rail_job_vd_destroy", GlyphRef.Vocabulary("action.delete")),
        RailRow.Job("rail_job_vd_auto", DiskGlyphs.AutoMount),
        RailRow.Job("rail_job_vd_add", DiskGlyphs.RememberName),
        RailRow.Alone("rail_job_history", GlyphRef.Vocabulary("content.history"), "program.history"),
        RailRow.Alone("rail_job_command", GlyphRef.Vocabulary("app.command-line"), "program.command"),
        RailRow.Group("rail_group_program", GlyphRef.Vocabulary("app.settings"), "program.app"),
        RailRow.Job("rail_job_settings", GlyphRef.Vocabulary("app.settings")),
        RailRow.Job("rail_job_about", GlyphRef.Vocabulary("app.info"))
    }
End Class

' One row of the rail. A group header is a RailEntry too, drawn differently - and, since the groups
' collapse, it is a control the user operates: it is focusable, it answers Space and Enter like any
' other row, and it draws a chevron that says whether the group is open or shut.
Public Class RailEntry
    Inherits Control

    Public Property Glyph As GlyphRef = Nothing
    Public Property Key As String = ""          ' the localization key, kept for a relayout
    Public Property IsGroupHeader As Boolean = False

    ' The Theme.GroupTone key the glyph is drawn in (the colour look), or "" for the text colour.
    Public Property Hue As String = ""

    ' Which of the rail's two bands the row sits on: 0 the rail's own surface, 1 the step beside it.
    ' A block - a header with its rows, or a lone job - is one band, and the next block takes the
    ' other, so the rail reads as a checker instead of one column of the same dark.
    Public Property Band As Integer = 0

    ' The height of a one-line row at the current DPI (ShellForm.RailTargetHeight through Ui.Px).
    ' The glyph column and the label's left edge are measured from it rather than from Height, so a
    ' row that has grown to hold a wrapped label keeps its glyph where every other row has it.
    Public Property RowUnit As Integer = 44

    ' The verb of the row's default action, in the window's language ("Open", "Expand").
    Public Property DefaultActionText As String = ""

    Private selectedValue As Boolean = False
    Private collapsedValue As Boolean = False
    Private hasSelectedChildValue As Boolean = False
    Private hot As Boolean = False

    ' The fonts a row paints with. They are made once for the process, not once per paint: a paint
    ' runs on every hover, and a font made per paint and never disposed is a GDI object leaked each
    ' time (SP-0014 T1).
    Private Shared fontBody As Font
    Private Shared fontStrong As Font
    Private Shared fontCaption As Font

    Private Shared Sub EnsureFonts()
        If fontBody IsNot Nothing Then Return
        fontBody = Theme.FontBody()
        fontStrong = Theme.FontBodyStrong()
        fontCaption = Theme.FontCaptionStrong()
    End Sub

    ' The font a label is drawn in. A row is measured in its bold face whether or not it is
    ' selected, so choosing a row never makes it grow a line. A header is always the bold caption:
    ' it names a kind of job and has to stay a heading among the rows it holds.
    Friend Shared Function LabelFont(isHeader As Boolean, selected As Boolean) As Font
        EnsureFonts()
        If isHeader Then Return fontCaption
        Return If(selected, fontStrong, fontBody)
    End Function

    Friend Shared Function MeasureFont(isHeader As Boolean) As Font
        Return LabelFont(isHeader, True)
    End Function

    Private Const LabelFlags As TextFormatFlags =
        TextFormatFlags.Left Or TextFormatFlags.WordBreak Or TextFormatFlags.NoPrefix

    ' Group header only: whether its rows are hidden. The chevron follows it.
    Public Property Collapsed As Boolean
        Get
            Return collapsedValue
        End Get
        Set(value As Boolean)
            If collapsedValue <> value Then
                collapsedValue = value
                Invalidate()
            End If
        End Set
    End Property

    ' Group header only: the selected row is inside this group. When the group is shut, the header
    ' is the only thing left on screen that can say where the user is - so it shows the accent bar
    ' the hidden row would have shown.
    Public Property HasSelectedChild As Boolean
        Get
            Return hasSelectedChildValue
        End Get
        Set(value As Boolean)
            If hasSelectedChildValue <> value Then
                hasSelectedChildValue = value
                Invalidate()
            End If
        End Set
    End Property

    Public Property Selected As Boolean
        Get
            Return selectedValue
        End Get
        Set(value As Boolean)
            If selectedValue <> value Then
                selectedValue = value
                Invalidate()
            End If
        End Set
    End Property

    Public Sub New()
        SetStyle(ControlStyles.AllPaintingInWmPaint Or
                 ControlStyles.OptimizedDoubleBuffer Or
                 ControlStyles.ResizeRedraw Or
                 ControlStyles.UserPaint, True)
    End Sub

    ' ---- geometry --------------------------------------------------------

    ' The rectangle the label is drawn into, for a row of this width and height. Paint, the row's
    ' height and SelfTest's overflow check all use this one function, so they cannot disagree.
    Friend Function LabelBounds(width As Integer, height As Integer) As Rectangle
        ' A header's icon sits in the column every row's glyph has, so its label starts where theirs
        ' do; the chevron takes the right edge, which the label keeps clear of.
        Dim left = CInt(RowUnit * 0.2) + CInt(RowUnit * 1.1)
        Dim right = If(IsGroupHeader, ChevronColumn(), 4)
        Return New Rectangle(left, 0, Math.Max(1, width - left - right), height)
    End Function

    ' The width the chevron of a header takes at the right edge: its glyph and a margin each side.
    Private Function ChevronColumn() As Integer
        Return GlyphPixels() + CInt(RowUnit * 0.4)
    End Function

    ' The glyph tier of a rail row and of a group's chevron: 20, the dense-row tier of ICON-RENDER
    ' rule 5 (section 10 item E), in the same units as RowUnit - so a 44 px target holds a 20 px
    ' glyph at every DPI.
    Friend Const GlyphTier As Integer = 20

    Private Function GlyphPixels() As Integer
        Return Math.Max(8, CInt(Math.Round(RowUnit * GlyphTier / CDbl(ShellForm.RailTargetHeight))))
    End Function

    ' The square a glyph is drawn into: the row's glyph column (or the header's chevron column),
    ' centred on the row's height as the label is.
    Friend Function GlyphSquare(height As Integer) As Rectangle
        Dim px = GlyphPixels()
        Return New Rectangle(CInt(RowUnit * 0.3), Math.Max(0, (height - px) \ 2), px, px)
    End Function

    ' A header's chevron: the same tier, in the column at the right edge.
    Friend Function ChevronSquare(width As Integer, height As Integer) As Rectangle
        Dim px = GlyphPixels()
        Return New Rectangle(width - ChevronColumn() + (ChevronColumn() - px) \ 2, Math.Max(0, (height - px) \ 2), px, px)
    End Function

    ' The vertical breathing room above and below a label that has wrapped.
    Private Function VerticalPad() As Integer
        Return Math.Max(2, CInt(RowUnit * 0.18))
    End Function

    ' The height this row needs at this width: one rail unit, or more when the label wraps.
    Friend Function PreferredRowHeight(width As Integer) As Integer
        Dim lw = LabelBounds(width, RowUnit).Width
        Dim textH = TextRenderer.MeasureText(If(Text, ""), MeasureFont(IsGroupHeader),
                                             New Size(lw, Integer.MaxValue), LabelFlags).Height
        Return Math.Max(RowUnit, textH + 2 * VerticalPad())
    End Function

    ' True when the label, wrapped at this width, fits the rectangle it will be drawn into - and no
    ' single word of it is wider than that rectangle, which wrapping cannot help.
    ' The tallest a row may grow to hold its label, in rail units. The row always grows to fit its
    ' text (PreferredRowHeight), so measuring the text against the row it made proves nothing
    ' (SHELL-15); what can fail is a label so long that the row it needs is no longer a row.
    Friend Const MaxRowUnits As Integer = 2

    Friend Function LabelFits(width As Integer, ByRef detail As String) As Boolean
        Dim h = PreferredRowHeight(width)
        Dim box = LabelBounds(width, h)
        Dim f = MeasureFont(IsGroupHeader)
        Dim text = If(Me.Text, "")
        Dim wrapped = TextRenderer.MeasureText(text, f, New Size(box.Width, Integer.MaxValue), LabelFlags)
        If h > RowUnit * MaxRowUnits Then
            detail = "wraps to " & wrapped.Height.ToString() & " px - a " & h.ToString() & " px row, over " &
                     MaxRowUnits.ToString() & " rail units of " & RowUnit.ToString() & " px"
            Return False
        End If
        For Each word In text.Split(New Char() {" "c}, StringSplitOptions.RemoveEmptyEntries)
            Dim w = TextRenderer.MeasureText(word, f, New Size(Integer.MaxValue, Integer.MaxValue),
                                             TextFormatFlags.SingleLine Or TextFormatFlags.NoPrefix).Width
            If w > box.Width Then
                detail = """" & word & """ is " & w.ToString() & " px in a " & box.Width.ToString() & " px label"
                Return False
            End If
        Next
        detail = box.Width.ToString() & "x" & box.Height.ToString()
        Return True
    End Function

    ' ---- input -----------------------------------------------------------

    Protected Overrides Sub OnMouseEnter(e As EventArgs)
        MyBase.OnMouseEnter(e)
        hot = True
        Invalidate()
    End Sub

    Protected Overrides Sub OnMouseLeave(e As EventArgs)
        MyBase.OnMouseLeave(e)
        hot = False
        Invalidate()
    End Sub

    Protected Overrides Sub OnGotFocus(e As EventArgs)
        MyBase.OnGotFocus(e)
        Invalidate()
    End Sub

    Protected Overrides Sub OnLostFocus(e As EventArgs)
        MyBase.OnLostFocus(e)
        Invalidate()
    End Sub

    ' Space and Enter choose the entry, because a rail that can be reached with Tab and not used
    ' with the keyboard is worse than one that cannot be reached at all.
    Protected Overrides Function IsInputKey(keyData As Keys) As Boolean
        If keyData = Keys.Space OrElse keyData = Keys.Enter Then Return True
        Return MyBase.IsInputKey(keyData)
    End Function

    Protected Overrides Sub OnKeyDown(e As KeyEventArgs)
        MyBase.OnKeyDown(e)
        If e.KeyCode = Keys.Space OrElse e.KeyCode = Keys.Enter Then
            OnClick(EventArgs.Empty)
            e.Handled = True
        End If
    End Sub

    ' What a click does, for the accessible object's default action.
    Friend Sub PerformClick()
        OnClick(EventArgs.Empty)
    End Sub

    ' For SelfTest.vb: paint this row, in the state set, into any Graphics - so every state can be
    ' painted under both palettes without a window on screen (T1).
    Friend Sub PaintForTest(g As Graphics, hover As Boolean)
        hot = hover
        OnPaint(New PaintEventArgs(g, New Rectangle(0, 0, Width, Height)))
        hot = False
    End Sub

    ' ---- accessibility ---------------------------------------------------

    Protected Overrides Function CreateAccessibilityInstance() As AccessibleObject
        Return New RailEntryAccessible(Me)
    End Function

    ' The row as UI Automation and a screen reader see it: its name, its state, and a default
    ' action that does what a click does.
    Private Class RailEntryAccessible
        Inherits ControlAccessibleObject

        Private ReadOnly entry As RailEntry

        Public Sub New(owner As RailEntry)
            MyBase.New(owner)
            entry = owner
        End Sub

        Public Overrides ReadOnly Property DefaultAction As String
            Get
                Return If(entry.DefaultActionText, "")
            End Get
        End Property

        Public Overrides Sub DoDefaultAction()
            entry.PerformClick()
        End Sub

        Public Overrides ReadOnly Property State As AccessibleStates
            Get
                Dim s = MyBase.State
                If entry.IsGroupHeader Then
                    s = s Or If(entry.Collapsed, AccessibleStates.Collapsed, AccessibleStates.Expanded)
                ElseIf entry.Selected Then
                    s = s Or AccessibleStates.Selected
                End If
                Return s
            End Get
        End Property
    End Class

    ' ---- paint -----------------------------------------------------------

    Protected Overrides Sub OnPaint(e As PaintEventArgs)
        Dim p = Theme.Current
        Dim g = e.Graphics
        g.TextRenderingHint = Drawing.Text.TextRenderingHint.ClearTypeGridFit

        Using b As New SolidBrush(BackColourFor(p))
            g.FillRectangle(b, ClientRectangle)
        End Using

        If IsGroupHeader Then
            If hot Then
                Using b As New SolidBrush(HoverColourFor(p))
                    g.FillRectangle(b, ClientRectangle)
                End Using
            End If

            ' A shut group still has to say that the current job is inside it, and it says so the
            ' same way a selected row does - an accent bar, not a colour on the text.
            If Collapsed AndAlso HasSelectedChild Then
                DrawAccentBar(g, p)
            End If

            ' What the group is about, in its hue; whether it is open, at the right edge.
            Glyphs.Draw(g, Glyph, GlyphSquare(Height), Theme.GroupTone(Hue, p))
            Glyphs.Draw(g, Theme.ChevronGlyph(Collapsed), ChevronSquare(Width, Height), p.MutedText)

            DrawLabel(g, p.Text, LabelFont(True, False))
            DrawFocus(g, p)
            Return
        End If

        ' The row's own background: selected, hot, or the band's.
        If Selected Then
            Using b As New SolidBrush(p.SurfaceSelected)
                g.FillRectangle(b, ClientRectangle)
            End Using
        ElseIf hot Then
            Using b As New SolidBrush(HoverColourFor(p))
                g.FillRectangle(b, ClientRectangle)
            End Using
        End If

        ' The accent bar. This is the part that survives a grey print-out, and it is why selection
        ' does not depend on the fill above.
        If Selected Then DrawAccentBar(g, p)

        ' A lone job stands where a header would, so it shows the colour look; a row inside a group
        ' is mono.
        If Glyph IsNot Nothing Then
            Glyphs.Draw(g, Glyph, GlyphSquare(Height), If(Hue <> "", Theme.GroupTone(Hue, p), p.Text))
        End If

        DrawLabel(g, p.Text, LabelFont(False, Selected))
        DrawFocus(g, p)
    End Sub

    ' The surfaces a row can be painted on, one function so the paint and the self-test's contrast
    ' rows read the same roles.
    Friend Function BackColourFor(p As Theme.Palette) As Color
        Return If(Band = 1, p.SurfaceBand, p.SurfaceAlt)
    End Function

    Friend Function HoverColourFor(p As Theme.Palette) As Color
        Return If(Band = 1, p.BandHover, p.ControlHover)
    End Function

    ' The label, wrapped inside its rectangle and centred on the row's height as a block.
    Private Sub DrawLabel(g As Graphics, colour As Color, f As Font)
        Dim box = LabelBounds(Width, Height)
        Dim text = If(Me.Text, "")
        Dim size = TextRenderer.MeasureText(g, text, f, New Size(box.Width, Integer.MaxValue), LabelFlags)
        Dim top = Math.Max(0, (Height - size.Height) \ 2)
        TextRenderer.DrawText(g, text, f, New Rectangle(box.Left, top, box.Width, Math.Min(size.Height, Height)),
                              colour, LabelFlags)
    End Sub

    Private Sub DrawAccentBar(g As Graphics, p As Theme.Palette)
        Dim barHeight = Math.Min(Height, CInt(RowUnit * 0.55))
        Using b As New SolidBrush(p.Accent)
            g.FillRectangle(b, New Rectangle(0, (Height - barHeight) \ 2,
                                             Math.Max(3, CInt(Width * 0.015)), barHeight))
        End Using
    End Sub

    ' The focus ring, drawn rather than inherited: WinForms' dotted rectangle is invisible on a
    ' dark surface.
    Private Sub DrawFocus(g As Graphics, p As Theme.Palette)
        If Not Focused Then Return
        Using pen As New Pen(p.Accent, 2.0F)
            Dim r = ClientRectangle
            r.Inflate(-2, -2)
            g.DrawRectangle(pen, r)
        End Using
    End Sub

End Class
