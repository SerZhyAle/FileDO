' Native value and keyboard semantics are retained (WINDOWS-UI section 4).
Imports System.Reflection
Imports System.Runtime.CompilerServices

Friend Module NativeEditors
    Private ReadOnly combos As New ConditionalWeakTable(Of ComboBox, Object)
    Friend Sub Apply(root As Control)
        Dim box = TryCast(root, ComboBox)
        If box IsNot Nothing Then
            box.BackColor = Theme.Current.Surface
            box.ForeColor = Theme.Current.Text
            box.ItemHeight = Math.Max(Ui.Px(box, 28), box.Font.Height + Ui.Px(box, 4))
            Dim attached As Object = Nothing
            If box.DropDownStyle = ComboBoxStyle.DropDownList AndAlso box.DrawMode = DrawMode.Normal AndAlso Not combos.TryGetValue(box, attached) Then
                combos.Add(box, New Object())
                box.DrawMode = DrawMode.OwnerDrawFixed
                GetType(Control).GetMethod("SetStyle", BindingFlags.Instance Or BindingFlags.NonPublic).Invoke(box,
                    New Object() {ControlStyles.UserPaint Or ControlStyles.AllPaintingInWmPaint Or ControlStyles.OptimizedDoubleBuffer Or ControlStyles.ResizeRedraw, True})
                AddHandler box.Paint, AddressOf PaintCombo
                AddHandler box.DrawItem, AddressOf DrawItem
                AddHandler box.GotFocus, Sub() box.Invalidate()
                AddHandler box.LostFocus, Sub() box.Invalidate()
                AddHandler box.EnabledChanged, Sub() box.Invalidate()
                AddHandler box.SelectedIndexChanged, Sub() box.Invalidate()
                AddHandler box.FontChanged, Sub() Apply(box)
                AddHandler box.DpiChangedAfterParent, Sub() Apply(box)
                AddHandler box.MouseWheel, Sub(sender, e)
                                              If Not box.Focused Then
                                                  Dim handled = TryCast(e, HandledMouseEventArgs)
                                                  If handled IsNot Nothing Then handled.Handled = True
                                              End If
                                          End Sub
            End If
            box.Invalidate()
        End If
        For Each child As Control In root.Controls
            Apply(child)
        Next
    End Sub

    Private Sub PaintCombo(sender As Object, e As PaintEventArgs)
        Dim box = DirectCast(sender, ComboBox)
        Dim p = Theme.Current
        Dim rect = box.ClientRectangle
        If rect.Width < 2 OrElse rect.Height < 2 Then Return
        Dim rtl = box.RightToLeft = RightToLeft.Yes
        Dim arrowWidth = Math.Min(rect.Width \ 2, Math.Max(Ui.Px(box, 28), rect.Height))
        Dim arrow As New Rectangle(If(rtl, 0, rect.Width - arrowWidth), 0, arrowWidth, rect.Height)
        Dim ink = If(box.Enabled, p.Text, p.TextDisabled)
        Using fill As New SolidBrush(p.Surface), face As New SolidBrush(p.SurfaceAlt)
            e.Graphics.FillRectangle(fill, rect)
            e.Graphics.FillRectangle(face, arrow)
        End Using
        Dim inset = Ui.Px(box, 6)
        Dim textRect As New Rectangle(If(rtl, arrowWidth + inset, inset), 0, Math.Max(1, rect.Width - arrowWidth - inset * 2), rect.Height)
        Dim flags = TextFormatFlags.NoPrefix Or TextFormatFlags.SingleLine Or TextFormatFlags.VerticalCenter
        If rtl Then flags = flags Or TextFormatFlags.RightToLeft Or TextFormatFlags.Right
        TextRenderer.DrawText(e.Graphics, box.GetItemText(box.SelectedItem), box.Font, textRect, ink, flags)
        Dim glyph = Theme.ChevronGlyph(True)
        Glyphs.Draw(e.Graphics, glyph, New Rectangle(arrow.Left + (arrow.Width - Ui.Px(box, 16)) \ 2,
                    (rect.Height - Ui.Px(box, 16)) \ 2, Ui.Px(box, 16), Ui.Px(box, 16)), ink)
        Using border As New Pen(If(box.Focused, p.Accent, p.Border), If(box.Focused, 2, 1))
            e.Graphics.DrawRectangle(border, 0, 0, rect.Width - 1, rect.Height - 1)
        End Using
    End Sub

    Private Sub DrawItem(sender As Object, e As DrawItemEventArgs)
        Dim box = DirectCast(sender, ComboBox)
        Dim p = Theme.Current
        Dim selected = (e.State And DrawItemState.Selected) <> 0
        Using fill As New SolidBrush(If(selected, p.Accent, p.Surface))
            e.Graphics.FillRectangle(fill, e.Bounds)
        End Using
        If e.Index >= 0 AndAlso e.Index < box.Items.Count Then
            Dim flags = TextFormatFlags.NoPrefix Or TextFormatFlags.SingleLine Or TextFormatFlags.VerticalCenter
            If box.RightToLeft = RightToLeft.Yes Then flags = flags Or TextFormatFlags.RightToLeft Or TextFormatFlags.Right
            TextRenderer.DrawText(e.Graphics, box.GetItemText(box.Items(e.Index)), box.Font, e.Bounds,
                                  If(selected, p.AccentText, If(box.Enabled, p.Text, p.TextDisabled)), flags)
        End If
        e.DrawFocusRectangle()
    End Sub
End Module
